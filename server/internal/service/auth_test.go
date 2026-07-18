package service_test

import (
	"context"
	"errors"
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
	repo := newFakeRepo()
	return service.NewAuthService(repo, jwtx.NewManager("test-secret", time.Hour)), repo
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
	svc, repo := newAuthService(t)
	seedUser(t, repo, "alice", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	resp, err := svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "alice", Password: "password123",
	})
	require.NoError(t, err)

	assert.NotEmpty(t, resp.Token)
	assert.Equal(t, "alice", resp.User.Username)
	assert.Equal(t, usermodel.RoleAdmin, resp.User.Role)
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
