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

// 未显式传 QuotaTotal 时默认给 100，而不是不限量——不限量会让每个新账号
// 都能无限使用，与配额功能的初衷相悖。
func TestUserService_Create_DefaultsQuotaTo100(t *testing.T) {
	svc, _ := newUserService(t)

	got, err := svc.Create(context.Background(), usermodel.CreateRequest{
		Username: "defaultquota", Password: "password123", Role: usermodel.RoleUser,
	})
	require.NoError(t, err)

	require.NotNil(t, got.QuotaTotal)
	assert.Equal(t, 100, *got.QuotaTotal)
	assert.Equal(t, 0, got.QuotaUsed)
}

// 管理员创建时显式传的额度值必须原样生效，不被默认值覆盖。
func TestUserService_Create_ExplicitQuota(t *testing.T) {
	svc, _ := newUserService(t)

	quota := 250
	got, err := svc.Create(context.Background(), usermodel.CreateRequest{
		Username: "customquota", Password: "password123", Role: usermodel.RoleUser,
		QuotaTotal: &quota,
	})
	require.NoError(t, err)

	require.NotNil(t, got.QuotaTotal)
	assert.Equal(t, 250, *got.QuotaTotal)
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

// 改额度：显式传一个新的具体值。
func TestUserService_Update_QuotaTotal(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	original := 100
	target := seedUser(t, repo, "victim", "password123", usermodel.RoleUser, usermodel.StatusActive)
	target.QuotaTotal = &original

	got, err := svc.Update(context.Background(), admin.ID, target.ID, usermodel.UpdateRequest{
		QuotaTotal: ptr(500),
	})
	require.NoError(t, err)
	require.NotNil(t, got.QuotaTotal)
	assert.Equal(t, 500, *got.QuotaTotal)
}

// 改成不限量：QuotaUnlimited=true 必须把 quota_total 置空，
// 且优先于同一请求里可能一并传来的 QuotaTotal。
func TestUserService_Update_QuotaUnlimited(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	original := 100
	target := seedUser(t, repo, "victim", "password123", usermodel.RoleUser, usermodel.StatusActive)
	target.QuotaTotal = &original

	got, err := svc.Update(context.Background(), admin.ID, target.ID, usermodel.UpdateRequest{
		QuotaTotal:     ptr(999), // 应被 QuotaUnlimited 盖过
		QuotaUnlimited: ptr(true),
	})
	require.NoError(t, err)
	assert.Nil(t, got.QuotaTotal, "QuotaUnlimited=true 应把额度置为不限量")
}

// 显式传 QuotaUnlimited=false（而不是不传）时，应当落到"设为具体值"这条
// 分支，跟不传 QuotaUnlimited 时效果一样——只有 QuotaUnlimited 非 nil 且为
// true 才应该赢。这条钉住 Update 里 switch 的第一个 case 判的是
// `*req.QuotaUnlimited`（true 才短路），而不是`req.QuotaUnlimited != nil`
// （只看有没有传）。
func TestUserService_Update_QuotaTotal_WithExplicitFalseUnlimited(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	original := 100
	target := seedUser(t, repo, "victim", "password123", usermodel.RoleUser, usermodel.StatusActive)
	target.QuotaTotal = &original

	got, err := svc.Update(context.Background(), admin.ID, target.ID, usermodel.UpdateRequest{
		QuotaTotal:     ptr(50),
		QuotaUnlimited: ptr(false),
	})
	require.NoError(t, err)
	require.NotNil(t, got.QuotaTotal, "QuotaUnlimited=false 不应被误判为「改成不限量」")
	assert.Equal(t, 50, *got.QuotaTotal)
}

// 未提供 QuotaTotal/QuotaUnlimited 时，原有额度不应被改动或清空——
// 与 UpdateRequest 其余字段的"局部更新"语义一致。
func TestUserService_Update_QuotaUntouchedWhenNotProvided(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	original := 42
	target := seedUser(t, repo, "victim", "password123", usermodel.RoleUser, usermodel.StatusActive)
	target.QuotaTotal = &original

	got, err := svc.Update(context.Background(), admin.ID, target.ID, usermodel.UpdateRequest{
		DisplayName: ptr("只改名字"),
	})
	require.NoError(t, err)
	require.NotNil(t, got.QuotaTotal)
	assert.Equal(t, 42, *got.QuotaTotal)
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
	// 首个管理员必须保持不限量，不能被 Create 的「默认 100」逻辑意外限额——
	// 否则每次全新部署引导出来的 admin 会莫名其妙只有 100 次额度。
	assert.Nil(t, created.QuotaTotal, "bootstrap admin 应为不限量")
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
