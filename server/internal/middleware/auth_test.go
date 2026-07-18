package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
)

// stubLoader 替代 repository，仅用于中间件测试。
type stubLoader struct{ users map[int64]*usermodel.User }

func (s stubLoader) GetByID(_ context.Context, id int64) (*usermodel.User, error) {
	u, ok := s.users[id]
	if !ok {
		return nil, apperr.ErrUserNotFound
	}
	return u, nil
}

func setup(t *testing.T, users map[int64]*usermodel.User) (*gin.Engine, *jwtx.Manager) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	jwtMgr := jwtx.NewManager("mw-test-secret", time.Hour)

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	auth := r.Group("/", middleware.Auth(jwtMgr, stubLoader{users: users}))
	auth.GET("/whoami", func(c *gin.Context) {
		id, _ := middleware.UserIDFrom(c)
		c.JSON(http.StatusOK, common.OK(gin.H{"id": id}))
	})
	auth.GET("/admin-only", middleware.RequireAdmin(), func(c *gin.Context) {
		c.JSON(http.StatusOK, common.OK(nil))
	})
	return r, jwtMgr
}

func do(r *gin.Engine, path, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w
}

func codeOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Code
}

func activeUser(id int64, role usermodel.Role) *usermodel.User {
	return &usermodel.User{
		ID: id, Username: "u", Role: role, Status: usermodel.StatusActive,
		PasswordChangedAt: time.Now().Add(-24 * time.Hour),
	}
}

func TestAuth_NoTokenReturns401(t *testing.T) {
	r, _ := setup(t, map[int64]*usermodel.User{})
	w := do(r, "/whoami", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "AUTH_UNAUTHORIZED", codeOf(t, w))
}

func TestAuth_GarbageTokenReturns401(t *testing.T) {
	r, _ := setup(t, map[int64]*usermodel.User{})
	w := do(r, "/whoami", "not-a-real-token")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuth_ValidTokenPasses(t *testing.T) {
	users := map[int64]*usermodel.User{7: activeUser(7, usermodel.RoleUser)}
	r, jwtMgr := setup(t, users)

	token, err := jwtMgr.Generate(7, usermodel.RoleUser)
	require.NoError(t, err)

	w := do(r, "/whoami", token)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"id":7`)
}

// 这是「乙方案」的核心断言：token 仍然有效，但用户已被禁用，
// 必须立即拒绝，而不是等 token 过期。
// 改密后旧 token 必须立即失效——这是 spec 承诺的「改密立即生效」，
// 只查 status 是兑现不了的。
func TestAuth_TokenIssuedBeforePasswordChangeRejected(t *testing.T) {
	u := activeUser(7, usermodel.RoleUser)
	u.PasswordChangedAt = time.Now().Add(-time.Hour)
	users := map[int64]*usermodel.User{7: u}
	r, jwtMgr := setup(t, users)

	token, err := jwtMgr.Generate(7, usermodel.RoleUser)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, do(r, "/whoami", token).Code)

	u.PasswordChangedAt = time.Now().Add(time.Minute) // 用户在别处改了密码

	w := do(r, "/whoami", token)
	assert.Equal(t, http.StatusUnauthorized, w.Code, "改密后旧 token 必须立即失效")
	assert.Equal(t, "AUTH_UNAUTHORIZED", codeOf(t, w))
}

// 改密与签发落在同一秒内时，新 token 必须仍然有效。
// jwt 把 IssuedAt 截断到整秒，若不做同精度处理，
// 刚签发的 token 会被误判为改密前的旧 token。
func TestAuth_TokenIssuedSameSecondAsPasswordChangeAccepted(t *testing.T) {
	now := time.Now()
	u := activeUser(7, usermodel.RoleUser)
	// 模拟「刚刚改完密码」：亚秒精度，且与下面签发的 token 同一秒
	u.PasswordChangedAt = now.Truncate(time.Second).Add(500 * time.Millisecond)
	users := map[int64]*usermodel.User{7: u}
	r, jwtMgr := setup(t, users)

	token, err := jwtMgr.Generate(7, usermodel.RoleUser)
	require.NoError(t, err)

	w := do(r, "/whoami", token)
	assert.Equal(t, http.StatusOK, w.Code,
		"改密同一秒内签发的 token 不应被判为过期")
}

func TestAuth_DisabledUserRejectedImmediately(t *testing.T) {
	u := activeUser(7, usermodel.RoleUser)
	users := map[int64]*usermodel.User{7: u}
	r, jwtMgr := setup(t, users)

	token, err := jwtMgr.Generate(7, usermodel.RoleUser)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, do(r, "/whoami", token).Code)

	u.Status = usermodel.StatusDisabled // admin 在别处禁用了该用户

	w := do(r, "/whoami", token)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "AUTH_USER_DISABLED", codeOf(t, w))
}

// 用户被删除后，其 token 也必须立即失效。
func TestAuth_DeletedUserRejected(t *testing.T) {
	users := map[int64]*usermodel.User{7: activeUser(7, usermodel.RoleUser)}
	r, jwtMgr := setup(t, users)

	token, err := jwtMgr.Generate(7, usermodel.RoleUser)
	require.NoError(t, err)
	delete(users, 7)

	w := do(r, "/whoami", token)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireAdmin_RejectsPlainUser(t *testing.T) {
	users := map[int64]*usermodel.User{7: activeUser(7, usermodel.RoleUser)}
	r, jwtMgr := setup(t, users)

	token, err := jwtMgr.Generate(7, usermodel.RoleUser)
	require.NoError(t, err)

	w := do(r, "/admin-only", token)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "AUTH_FORBIDDEN", codeOf(t, w))
}

// 角色以数据库为准，不信 token 里的 role 声明：
// token 说自己是 admin，但库里是 user，必须拒绝。
func TestRequireAdmin_TrustsDatabaseNotToken(t *testing.T) {
	users := map[int64]*usermodel.User{7: activeUser(7, usermodel.RoleUser)}
	r, jwtMgr := setup(t, users)

	forged, err := jwtMgr.Generate(7, usermodel.RoleAdmin)
	require.NoError(t, err)

	w := do(r, "/admin-only", forged)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireAdmin_AllowsAdmin(t *testing.T) {
	users := map[int64]*usermodel.User{9: activeUser(9, usermodel.RoleAdmin)}
	r, jwtMgr := setup(t, users)

	token, err := jwtMgr.Generate(9, usermodel.RoleAdmin)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, do(r, "/admin-only", token).Code)
}
