package service_test

import (
	"context"
	"time"

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
