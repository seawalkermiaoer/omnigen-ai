package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

func newUserService(t *testing.T) (*service.UserService, *fakeUserRepo) {
	t.Helper()
	repo := newFakeRepo()
	return service.NewUserService(repo), repo
}

func ptr[T any](v T) *T { return &v }

func TestUserService_Create(t *testing.T) {
	svc, _ := newUserService(t)

	got, err := svc.Create(context.Background(), usermodel.CreateRequest{
		Username: "newbie", Password: "password123",
		DisplayName: "新人", Role: usermodel.RoleUser,
	})
	require.NoError(t, err)

	assert.Equal(t, "newbie", got.Username)
	assert.Equal(t, usermodel.RoleUser, got.Role)
	assert.Equal(t, usermodel.StatusActive, got.Status, "新建用户默认 active")
}

func TestUserService_Create_DuplicateUsername(t *testing.T) {
	svc, repo := newUserService(t)
	seedUser(t, repo, "taken", "password123", usermodel.RoleUser, usermodel.StatusActive)

	_, err := svc.Create(context.Background(), usermodel.CreateRequest{
		Username: "taken", Password: "password123", Role: usermodel.RoleUser,
	})
	assertCode(t, err, "USER_USERNAME_TAKEN")
}

func TestUserService_Create_HashesPassword(t *testing.T) {
	svc, repo := newUserService(t)

	_, err := svc.Create(context.Background(), usermodel.CreateRequest{
		Username: "hashme", Password: "password123", Role: usermodel.RoleUser,
	})
	require.NoError(t, err)

	stored, err := repo.GetByUsername(context.Background(), "hashme")
	require.NoError(t, err)
	assert.NotEqual(t, "password123", stored.PasswordHash)
	assert.True(t, password.Verify(stored.PasswordHash, "password123"))
}

func TestUserService_List(t *testing.T) {
	svc, repo := newUserService(t)
	for _, n := range []string{"a", "b", "c"} {
		seedUser(t, repo, n, "password123", usermodel.RoleUser, usermodel.StatusActive)
	}

	got, err := svc.List(context.Background(), usermodel.ListQuery{Page: 1, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, int64(3), got.Total)
	assert.Len(t, got.Items, 2)
}

func TestUserService_Update(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	other := seedUser(t, repo, "victim", "password123", usermodel.RoleUser, usermodel.StatusActive)
	seedUser(t, repo, "admin2", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	got, err := svc.Update(context.Background(), admin.ID, other.ID, usermodel.UpdateRequest{
		DisplayName: ptr("改过的名字"),
		Status:      ptr(usermodel.StatusDisabled),
	})
	require.NoError(t, err)
	assert.Equal(t, "改过的名字", got.DisplayName)
	assert.Equal(t, usermodel.StatusDisabled, got.Status)

	// 局部更新：只传 DisplayName 时，未提供的字段必须原样保留——
	// 这正是 UpdateRequest 全部字段用指针的意义所在，也是
	// fakeUserRepo.Update 被特意加固为只写四个字段的原因。
	got2, err := svc.Update(context.Background(), admin.ID, other.ID, usermodel.UpdateRequest{
		DisplayName: ptr("再改一次"),
	})
	require.NoError(t, err)
	assert.Equal(t, "再改一次", got2.DisplayName)
	assert.Equal(t, usermodel.RoleUser, got2.Role, "未提供 Role 不应被清空或改变")
	assert.Equal(t, usermodel.StatusDisabled, got2.Status, "未提供 Status 不应被重置")

	stored, err := repo.GetByID(context.Background(), other.ID)
	require.NoError(t, err)
	assert.Equal(t, "victim", stored.Username, "Update 不应触碰 Username")
	assert.True(t, password.Verify(stored.PasswordHash, "password123"), "Update 不应触碰密码哈希")
}

func TestUserService_Update_CannotModifySelf(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	seedUser(t, repo, "admin2", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	_, err := svc.Update(context.Background(), admin.ID, admin.ID, usermodel.UpdateRequest{
		Status: ptr(usermodel.StatusDisabled),
	})
	assertCode(t, err, "USER_CANNOT_MODIFY_SELF")

	_, err = svc.Update(context.Background(), admin.ID, admin.ID, usermodel.UpdateRequest{
		Role: ptr(usermodel.RoleUser),
	})
	assertCode(t, err, "USER_CANNOT_MODIFY_SELF")
}

// 只改自己的显示名是允许的，不属于危险自我操作。
func TestUserService_Update_SelfDisplayNameAllowed(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	got, err := svc.Update(context.Background(), admin.ID, admin.ID, usermodel.UpdateRequest{
		DisplayName: ptr("我的新昵称"),
	})
	require.NoError(t, err)
	assert.Equal(t, "我的新昵称", got.DisplayName)
}

func TestUserService_Update_CannotDemoteLastAdmin(t *testing.T) {
	svc, repo := newUserService(t)
	actor := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	// 手动制造「另一个 admin 是唯一活跃 admin」的场景：把 actor 设为禁用之外的场景不现实，
	// 因此直接用两个 admin 中删掉一个的方式验证。
	lone := seedUser(t, repo, "lonely-admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	require.NoError(t, repo.Delete(context.Background(), actor.ID))

	_, err := svc.Update(context.Background(), 9999, lone.ID, usermodel.UpdateRequest{
		Role: ptr(usermodel.RoleUser),
	})
	assertCode(t, err, "USER_LAST_ADMIN")

	_, err = svc.Update(context.Background(), 9999, lone.ID, usermodel.UpdateRequest{
		Status: ptr(usermodel.StatusDisabled),
	})
	assertCode(t, err, "USER_LAST_ADMIN")
}

func TestUserService_Delete(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	target := seedUser(t, repo, "target", "password123", usermodel.RoleUser, usermodel.StatusActive)

	require.NoError(t, svc.Delete(context.Background(), admin.ID, target.ID))

	_, err := repo.GetByID(context.Background(), target.ID)
	assertCode(t, err, "USER_NOT_FOUND")
}

func TestUserService_Delete_CannotDeleteSelf(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	seedUser(t, repo, "admin2", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	err := svc.Delete(context.Background(), admin.ID, admin.ID)
	assertCode(t, err, "USER_CANNOT_MODIFY_SELF")
}

func TestUserService_Delete_CannotDeleteLastAdmin(t *testing.T) {
	svc, repo := newUserService(t)
	lone := seedUser(t, repo, "lonely-admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	err := svc.Delete(context.Background(), 9999, lone.ID)
	assertCode(t, err, "USER_LAST_ADMIN")
}

func TestUserService_ResetPassword(t *testing.T) {
	svc, repo := newUserService(t)
	target := seedUser(t, repo, "target", "oldpassword", usermodel.RoleUser, usermodel.StatusActive)

	require.NoError(t, svc.ResetPassword(context.Background(), target.ID,
		usermodel.ResetPasswordRequest{Password: "resetpassword"}))

	stored, err := repo.GetByID(context.Background(), target.ID)
	require.NoError(t, err)
	assert.True(t, password.Verify(stored.PasswordHash, "resetpassword"))
	assert.False(t, password.Verify(stored.PasswordHash, "oldpassword"))
}

func TestUserService_EnsureBootstrapAdmin_CreatesWhenNone(t *testing.T) {
	svc, repo := newUserService(t)

	require.NoError(t, svc.EnsureBootstrapAdmin(context.Background(), "root", "rootpassword"))

	created, err := repo.GetByUsername(context.Background(), "root")
	require.NoError(t, err)
	assert.Equal(t, usermodel.RoleAdmin, created.Role)
	assert.True(t, password.Verify(created.PasswordHash, "rootpassword"))
}

// 已存在 admin 时不得覆盖——否则每次重启都会重置密码。
func TestUserService_EnsureBootstrapAdmin_SkipsWhenAdminExists(t *testing.T) {
	svc, repo := newUserService(t)
	existing := seedUser(t, repo, "admin", "originalpass", usermodel.RoleAdmin, usermodel.StatusActive)

	require.NoError(t, svc.EnsureBootstrapAdmin(context.Background(), "root", "rootpassword"))

	_, err := repo.GetByUsername(context.Background(), "root")
	assertCode(t, err, "USER_NOT_FOUND")

	unchanged, err := repo.GetByID(context.Background(), existing.ID)
	require.NoError(t, err)
	assert.True(t, password.Verify(unchanged.PasswordHash, "originalpass"))
}
