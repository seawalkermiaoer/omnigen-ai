package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmodel "github.com/chenhao/omnigen-ai/server/internal/model/auth"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

func newAuthService(t *testing.T) (*service.AuthService, *fakeUserRepo) {
	t.Helper()
	svc, repo, _ := newAuthServiceWithManager(t)
	return svc, repo
}

func newAuthServiceWithManager(t *testing.T) (*service.AuthService, *fakeUserRepo, *jwtx.Manager) {
	t.Helper()
	repo := newFakeRepo()
	jwtManager := jwtx.NewManager("test-secret", time.Hour)
	return service.NewAuthService(repo, jwtManager), repo, jwtManager
}

func seedUser(t *testing.T, repo *fakeUserRepo, name, plain string, role usermodel.Role, status usermodel.Status) *usermodel.User {
	t.Helper()
	hash, err := password.Hash(plain)
	require.NoError(t, err)
	return repo.add(&usermodel.User{
		Username: name, PasswordHash: hash, DisplayName: name,
		Role: role, Status: status,
	})
}

func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr), "期望 *apperr.AppError，实际 %T", err)
	assert.Equal(t, code, appErr.Code())
}

func TestLogin_Succeeds(t *testing.T) {
	svc, repo, jwtManager := newAuthServiceWithManager(t)
	u := seedUser(t, repo, "alice", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	resp, err := svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "alice", Password: "password123",
	})
	require.NoError(t, err)

	assert.NotEmpty(t, resp.Token)
	assert.Equal(t, "alice", resp.User.Username)
	assert.Equal(t, usermodel.RoleAdmin, resp.User.Role)

	// 只断言 token 非空还不够：Generate 就算把 subject 签成别的用户 ID
	// 也能通过。这里真正解析 token，核对身份与角色声明与被登录用户一致。
	claims, err := jwtManager.Parse(resp.Token)
	require.NoError(t, err, "签发的 token 必须能被同一 Manager 解析")
	gotUserID, err := jwtx.UserID(claims)
	require.NoError(t, err)
	assert.Equal(t, u.ID, gotUserID)
	assert.Equal(t, usermodel.RoleAdmin, claims.Role)
}

func TestLogin_WrongPassword(t *testing.T) {
	svc, repo := newAuthService(t)
	seedUser(t, repo, "alice", "password123", usermodel.RoleUser, usermodel.StatusActive)

	_, err := svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "alice", Password: "wrong",
	})
	assertCode(t, err, "AUTH_INVALID_CREDENTIALS")
}

// 用户不存在与密码错误必须返回同一个错误码，
// 否则接口就成了用户名枚举器。
func TestLogin_UnknownUserSameCodeAsWrongPassword(t *testing.T) {
	svc, _ := newAuthService(t)

	_, err := svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "nobody", Password: "whatever",
	})
	assertCode(t, err, "AUTH_INVALID_CREDENTIALS")
}

func TestLogin_DisabledUserRejected(t *testing.T) {
	svc, repo := newAuthService(t)
	seedUser(t, repo, "banned", "password123", usermodel.RoleUser, usermodel.StatusDisabled)

	_, err := svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "banned", Password: "password123",
	})
	assertCode(t, err, "AUTH_USER_DISABLED")
}

func TestChangePassword_Succeeds(t *testing.T) {
	svc, repo := newAuthService(t)
	u := seedUser(t, repo, "alice", "oldpassword", usermodel.RoleUser, usermodel.StatusActive)

	require.NoError(t, svc.ChangePassword(context.Background(), u.ID, authmodel.ChangePasswordRequest{
		OldPassword: "oldpassword", NewPassword: "newpassword1",
	}))

	_, err := svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "alice", Password: "newpassword1",
	})
	require.NoError(t, err, "新密码应可登录")

	_, err = svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "alice", Password: "oldpassword",
	})
	assertCode(t, err, "AUTH_INVALID_CREDENTIALS")
}

// ChangePassword 必须推进 PasswordChangedAt，Task 10 的中间件靠它
// 让改密前签发的 token 立即失效，否则被窃取的旧 token 仍能撑到 TTL 到期。
func TestChangePassword_AdvancesPasswordChangedAt(t *testing.T) {
	svc, repo := newAuthService(t)
	u := seedUser(t, repo, "alice", "oldpassword", usermodel.RoleUser, usermodel.StatusActive)
	before := u.PasswordChangedAt

	require.NoError(t, svc.ChangePassword(context.Background(), u.ID, authmodel.ChangePasswordRequest{
		OldPassword: "oldpassword", NewPassword: "newpassword1",
	}))

	after, err := repo.GetByID(context.Background(), u.ID)
	require.NoError(t, err)
	assert.True(t, after.PasswordChangedAt.After(before),
		"改密后 PasswordChangedAt 应晚于改密前：before=%v after=%v", before, after.PasswordChangedAt)
}

// 25 个中文字符按字节算是 75 字节，超过 bcrypt 72 字节硬上限，
// 但按 rune 算只有 25 个字符，能通过前端与 gin binding 的 max=72 校验，
// 只会在 service 层的 password.Hash 里触顶。mapHashError 存在的唯一理由
// 就是把这种情况映射成 422 而不是 500。
func TestChangePassword_ChineseTooLongMapsTo422(t *testing.T) {
	svc, repo := newAuthService(t)
	u := seedUser(t, repo, "alice", "oldpassword", usermodel.RoleUser, usermodel.StatusActive)

	tooLong := strings.Repeat("测", 25)
	require.Len(t, tooLong, 75, "25 个中文字符应为 75 字节")

	err := svc.ChangePassword(context.Background(), u.ID, authmodel.ChangePasswordRequest{
		OldPassword: "oldpassword", NewPassword: tooLong,
	})
	assertCode(t, err, "USER_PASSWORD_TOO_LONG")
}

func TestChangePassword_WrongOldPassword(t *testing.T) {
	svc, repo := newAuthService(t)
	u := seedUser(t, repo, "alice", "oldpassword", usermodel.RoleUser, usermodel.StatusActive)

	err := svc.ChangePassword(context.Background(), u.ID, authmodel.ChangePasswordRequest{
		OldPassword: "not-the-old-one", NewPassword: "newpassword1",
	})
	assertCode(t, err, "AUTH_WRONG_OLD_PASSWORD")
}

func TestGetCurrentUser(t *testing.T) {
	svc, repo := newAuthService(t)
	u := seedUser(t, repo, "alice", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	got, err := svc.GetCurrentUser(context.Background(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "alice", got.Username)
	assert.Equal(t, usermodel.RoleAdmin, got.Role)
}

func TestGetCurrentUser_NotFound(t *testing.T) {
	svc, _ := newAuthService(t)
	_, err := svc.GetCurrentUser(context.Background(), 12345)
	assertCode(t, err, "USER_NOT_FOUND")
}
