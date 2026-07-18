package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// memRepo 是 handler 测试用的内存 repository。
type memRepo struct {
	users  map[int64]*usermodel.User
	nextID int64
}

func newMemRepo() *memRepo { return &memRepo{users: map[int64]*usermodel.User{}, nextID: 1} }

func (m *memRepo) Create(_ context.Context, u *usermodel.User) error {
	for _, e := range m.users {
		if e.Username == u.Username {
			return apperr.ErrUsernameTaken
		}
	}
	u.ID = m.nextID
	m.nextID++
	u.CreatedAt = time.Now()
	u.UpdatedAt = u.CreatedAt
	u.PasswordChangedAt = u.CreatedAt
	m.users[u.ID] = u
	return nil
}
func (m *memRepo) GetByID(_ context.Context, id int64) (*usermodel.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, apperr.ErrUserNotFound
	}
	c := *u
	return &c, nil
}
func (m *memRepo) GetByUsername(_ context.Context, name string) (*usermodel.User, error) {
	for _, u := range m.users {
		if u.Username == name {
			c := *u
			return &c, nil
		}
	}
	return nil, apperr.ErrUserNotFound
}
func (m *memRepo) List(_ context.Context, offset, limit int) ([]usermodel.User, int64, error) {
	all := []usermodel.User{}
	for id := int64(1); id < m.nextID; id++ {
		if u, ok := m.users[id]; ok {
			all = append(all, *u)
		}
	}
	total := int64(len(all))
	if offset >= len(all) {
		return []usermodel.User{}, total, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], total, nil
}
func (m *memRepo) Update(_ context.Context, u *usermodel.User) error {
	if _, ok := m.users[u.ID]; !ok {
		return apperr.ErrUserNotFound
	}
	c := *u
	c.UpdatedAt = time.Now()
	m.users[u.ID] = &c
	return nil
}
func (m *memRepo) UpdatePasswordHash(_ context.Context, id int64, hash string) error {
	u, ok := m.users[id]
	if !ok {
		return apperr.ErrUserNotFound
	}
	u.PasswordHash = hash
	u.UpdatedAt = time.Now()
	u.PasswordChangedAt = u.UpdatedAt
	return nil
}
func (m *memRepo) Delete(_ context.Context, id int64) error {
	if _, ok := m.users[id]; !ok {
		return apperr.ErrUserNotFound
	}
	delete(m.users, id)
	return nil
}
func (m *memRepo) CountActiveAdmins(_ context.Context) (int64, error) {
	var n int64
	for _, u := range m.users {
		if u.Role == usermodel.RoleAdmin && u.Status == usermodel.StatusActive {
			n++
		}
	}
	return n, nil
}

var _ repository.UserRepository = (*memRepo)(nil)

type testEnv struct {
	r      *gin.Engine
	repo   *memRepo
	jwtMgr *jwtx.Manager
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := newMemRepo()
	jwtMgr := jwtx.NewManager("handler-test-secret", time.Hour)
	authSvc := service.NewAuthService(repo, jwtMgr)
	userSvc := service.NewUserService(repo)

	authH := handler.NewAuthHandler(authSvc)
	userH := handler.NewUserHandler(userSvc)

	r := gin.New()
	r.Use(middleware.Recovery(), middleware.ErrorHandler())

	api := r.Group("/api")
	api.POST("/auth/login", authH.Login)

	authed := api.Group("", middleware.Auth(jwtMgr, repo))
	authed.GET("/auth/me", authH.Me)
	authed.POST("/auth/logout", authH.Logout)
	authed.PUT("/auth/password", authH.ChangePassword)

	admin := authed.Group("", middleware.RequireAdmin())
	admin.GET("/users", userH.List)
	admin.POST("/users", userH.Create)
	admin.PUT("/users/:id", userH.Update)
	admin.PUT("/users/:id/password", userH.ResetPassword)
	admin.DELETE("/users/:id", userH.Delete)

	return &testEnv{r: r, repo: repo, jwtMgr: jwtMgr}
}

func (e *testEnv) seed(t *testing.T, name, plain string, role usermodel.Role) *usermodel.User {
	t.Helper()
	hash, err := password.Hash(plain)
	require.NoError(t, err)
	u := &usermodel.User{Username: name, PasswordHash: hash, DisplayName: name,
		Role: role, Status: usermodel.StatusActive}
	require.NoError(t, e.repo.Create(context.Background(), u))
	return u
}

func (e *testEnv) request(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func (e *testEnv) login(t *testing.T, name, plain string) string {
	t.Helper()
	w := e.request(t, http.MethodPost, "/api/auth/login", "",
		gin.H{"username": name, "password": plain})
	require.Equal(t, http.StatusOK, w.Code, "登录应成功，响应=%s", w.Body.String())

	var resp struct {
		Code string `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data.Token)
	return resp.Data.Token
}

func respCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var r common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &r))
	return r.Code
}

func TestLogin_Success(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "alice", "password123", usermodel.RoleAdmin)

	w := e.request(t, http.MethodPost, "/api/auth/login", "",
		gin.H{"username": "alice", "password": "password123"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, common.CodeOK, respCode(t, w))
	assert.Contains(t, w.Body.String(), `"username":"alice"`)
}

// 响应体绝不能带出密码哈希。
func TestLogin_ResponseHasNoPasswordHash(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "alice", "password123", usermodel.RoleAdmin)

	w := e.request(t, http.MethodPost, "/api/auth/login", "",
		gin.H{"username": "alice", "password": "password123"})

	assert.NotContains(t, w.Body.String(), "$2a$")
	assert.NotContains(t, w.Body.String(), "passwordHash")
	assert.NotContains(t, w.Body.String(), "password_hash")
}

func TestLogin_MissingFieldsReturns422(t *testing.T) {
	e := newTestEnv(t)
	w := e.request(t, http.MethodPost, "/api/auth/login", "", gin.H{"username": "alice"})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Equal(t, "VALIDATION_FAILED", respCode(t, w))
}

func TestLogin_BadCredentialsReturns401(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "alice", "password123", usermodel.RoleUser)

	w := e.request(t, http.MethodPost, "/api/auth/login", "",
		gin.H{"username": "alice", "password": "nope"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "AUTH_INVALID_CREDENTIALS", respCode(t, w))
}

func TestMe_RequiresToken(t *testing.T) {
	e := newTestEnv(t)
	w := e.request(t, http.MethodGet, "/api/auth/me", "", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMe_ReturnsCurrentUser(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "alice", "password123", usermodel.RoleAdmin)
	token := e.login(t, "alice", "password123")

	w := e.request(t, http.MethodGet, "/api/auth/me", token, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"username":"alice"`)
}

func TestChangePassword_Flow(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "alice", "password123", usermodel.RoleUser)
	token := e.login(t, "alice", "password123")

	w := e.request(t, http.MethodPut, "/api/auth/password", token,
		gin.H{"oldPassword": "password123", "newPassword": "newpassword1"})
	assert.Equal(t, http.StatusOK, w.Code)

	_ = e.login(t, "alice", "newpassword1")

	bad := e.request(t, http.MethodPost, "/api/auth/login", "",
		gin.H{"username": "alice", "password": "password123"})
	assert.Equal(t, http.StatusUnauthorized, bad.Code)
}

func TestUsers_PlainUserForbidden(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	e.seed(t, "plain", "password123", usermodel.RoleUser)
	token := e.login(t, "plain", "password123")

	w := e.request(t, http.MethodGet, "/api/users", token, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "AUTH_FORBIDDEN", respCode(t, w))
}

func TestUsers_AdminCanCreateAndList(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	w := e.request(t, http.MethodPost, "/api/users", token, gin.H{
		"username": "newbie", "password": "password123",
		"displayName": "新人", "role": "user",
	})
	require.Equal(t, http.StatusCreated, w.Code, "响应=%s", w.Body.String())
	assert.NotContains(t, w.Body.String(), "$2a$")

	list := e.request(t, http.MethodGet, "/api/users?page=1&pageSize=10", token, nil)
	assert.Equal(t, http.StatusOK, list.Code)
	assert.Contains(t, list.Body.String(), `"total":2`)
}

func TestUsers_CreateDuplicateReturns409(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	body := gin.H{"username": "dup", "password": "password123", "role": "user"}
	require.Equal(t, http.StatusCreated, e.request(t, http.MethodPost, "/api/users", token, body).Code)

	w := e.request(t, http.MethodPost, "/api/users", token, body)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "USER_USERNAME_TAKEN", respCode(t, w))
}

func TestUsers_CreateShortPasswordReturns422(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	w := e.request(t, http.MethodPost, "/api/users", token,
		gin.H{"username": "shorty", "password": "123", "role": "user"})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Equal(t, "VALIDATION_FAILED", respCode(t, w))
}

func TestUsers_BadIDReturns422(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	w := e.request(t, http.MethodDelete, "/api/users/not-a-number", token, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

// 端到端验证「乙方案」：禁用后旧 token 立刻失效。
func TestDisableUser_RevokesTokenImmediately(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	victim := e.seed(t, "victim", "password123", usermodel.RoleUser)

	adminToken := e.login(t, "admin", "password123")
	victimToken := e.login(t, "victim", "password123")

	require.Equal(t, http.StatusOK, e.request(t, http.MethodGet, "/api/auth/me", victimToken, nil).Code)

	w := e.request(t, http.MethodPut, "/api/users/"+strconv.FormatInt(victim.ID, 10), adminToken,
		gin.H{"status": "disabled"})
	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())

	after := e.request(t, http.MethodGet, "/api/auth/me", victimToken, nil)
	assert.Equal(t, http.StatusForbidden, after.Code, "禁用后旧 token 必须立即失效")
	assert.Equal(t, "AUTH_USER_DISABLED", respCode(t, after))
}
