package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

// fakeUserRepo 是内存版 UserRepository，让 service 测试无需启动数据库。
type fakeUserRepo struct {
	users  map[int64]*usermodel.User
	nextID int64
}

func newFakeRepo() *fakeUserRepo {
	return &fakeUserRepo{users: map[int64]*usermodel.User{}, nextID: 1}
}

// add 模拟一次 INSERT ... RETURNING：分配 ID 并回填 CreatedAt/UpdatedAt/PasswordChangedAt，
// 与真实 repository.Create 的可观察行为保持一致。
func (f *fakeUserRepo) add(u *usermodel.User) *usermodel.User {
	u.ID = f.nextID
	f.nextID++
	now := time.Now()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	u.UpdatedAt = now
	if u.PasswordChangedAt.IsZero() {
		u.PasswordChangedAt = now
	}
	f.users[u.ID] = u
	return u
}

func (f *fakeUserRepo) Create(_ context.Context, u *usermodel.User) error {
	for _, existing := range f.users {
		if existing.Username == u.Username {
			return apperr.ErrUsernameTaken
		}
	}
	f.add(u)
	return nil
}

func (f *fakeUserRepo) GetByID(_ context.Context, id int64) (*usermodel.User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, apperr.ErrUserNotFound
	}
	clone := *u
	return &clone, nil
}

func (f *fakeUserRepo) GetByUsername(_ context.Context, username string) (*usermodel.User, error) {
	for _, u := range f.users {
		if u.Username == username {
			clone := *u
			return &clone, nil
		}
	}
	return nil, apperr.ErrUserNotFound
}

func (f *fakeUserRepo) List(_ context.Context, offset, limit int) ([]usermodel.User, int64, error) {
	all := make([]usermodel.User, 0, len(f.users))
	for id := int64(1); id < f.nextID; id++ {
		if u, ok := f.users[id]; ok {
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

// Update 只写 display_name/role/status/updated_at，
// 与真实 SQL（UPDATE users SET display_name=…, role=…, status=…, updated_at=now()）保持一致。
// 不能整体替换存储的 struct：调用方传入的 u 若是半成品（例如 PasswordHash 为空），
// 整体替换会悄悄冲掉真实 SQL 根本不会触碰的字段，掩盖 bug。
func (f *fakeUserRepo) Update(_ context.Context, u *usermodel.User) error {
	stored, ok := f.users[u.ID]
	if !ok {
		return apperr.ErrUserNotFound
	}
	stored.DisplayName = u.DisplayName
	stored.Role = u.Role
	stored.Status = u.Status
	stored.UpdatedAt = time.Now()
	u.UpdatedAt = stored.UpdatedAt
	return nil
}

// UpdatePasswordHash 同时推进 UpdatedAt 与 PasswordChangedAt，与真实 SQL
// （UPDATE users SET password_hash=…, updated_at=now(), password_changed_at=now() WHERE id=…）保持一致。
// PasswordChangedAt 是 Task 10 中间件用来让改密前签发的 token 立即失效的依据。
func (f *fakeUserRepo) UpdatePasswordHash(_ context.Context, id int64, hash string) error {
	u, ok := f.users[id]
	if !ok {
		return apperr.ErrUserNotFound
	}
	now := time.Now()
	u.PasswordHash = hash
	u.UpdatedAt = now
	u.PasswordChangedAt = now
	return nil
}

func (f *fakeUserRepo) Delete(_ context.Context, id int64) error {
	if _, ok := f.users[id]; !ok {
		return apperr.ErrUserNotFound
	}
	delete(f.users, id)
	return nil
}

func (f *fakeUserRepo) CountActiveAdmins(_ context.Context) (int64, error) {
	var n int64
	for _, u := range f.users {
		if u.Role == usermodel.RoleAdmin && u.Status == usermodel.StatusActive {
			n++
		}
	}
	return n, nil
}

// ConsumeQuota/RefundQuota 忠实复刻真实 repository 的语义（见
// internal/repository/user.go）：QuotaTotal 为 nil 表示不限量，不拦但仍计数；
// 退回不会把 QuotaUsed 减到负数，且没扣过就退是无操作，不是错误。
//
// 用户不存在时也返回 ErrQuotaExceeded 而不是 ErrUserNotFound——这不是疏忽，
// 是刻意对齐真实实现：真实 SQL 用一条 UPDATE 的 RowsAffected==0 同时覆盖
// "用户不存在"与"额度耗尽"两种情况，两者在那一层无法区分，也不需要区分。
// 这里若返回 ErrUserNotFound，会让针对本 double 写的测试断言出一个真实系统
// 根本不会产生的错误码——测试绿了，但绿得没有意义。别把这行"修"回去。
func (f *fakeUserRepo) ConsumeQuota(_ context.Context, id int64) error {
	u, ok := f.users[id]
	if !ok {
		return apperr.ErrQuotaExceeded
	}
	if u.QuotaTotal != nil && u.QuotaUsed >= *u.QuotaTotal {
		return apperr.ErrQuotaExceeded
	}
	u.QuotaUsed++
	u.UpdatedAt = time.Now()
	return nil
}

func (f *fakeUserRepo) RefundQuota(_ context.Context, id int64) error {
	u, ok := f.users[id]
	if !ok || u.QuotaUsed == 0 {
		return nil
	}
	u.QuotaUsed--
	u.UpdatedAt = time.Now()
	return nil
}

// TestFakeUserRepo_ConsumeQuota_MissingUserMatchesRealSemantics pins the
// double to the real repository's collapsed error: a nonexistent user ID
// must surface as QUOTA_EXCEEDED, not USER_NOT_FOUND. Without this test the
// double can silently drift back to the "obvious" mapping and start passing
// tests for a code path the real system can't produce
// (see repository.userRepository.ConsumeQuota).
func TestFakeUserRepo_ConsumeQuota_MissingUserMatchesRealSemantics(t *testing.T) {
	repo := newFakeRepo()

	err := repo.ConsumeQuota(context.Background(), 999999)
	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, "QUOTA_EXCEEDED", appErr.Code())
}
