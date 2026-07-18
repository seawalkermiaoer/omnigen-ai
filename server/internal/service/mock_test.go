package service_test

import (
	"context"

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

func (f *fakeUserRepo) add(u *usermodel.User) *usermodel.User {
	u.ID = f.nextID
	f.nextID++
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

func (f *fakeUserRepo) Update(_ context.Context, u *usermodel.User) error {
	if _, ok := f.users[u.ID]; !ok {
		return apperr.ErrUserNotFound
	}
	clone := *u
	f.users[u.ID] = &clone
	return nil
}

func (f *fakeUserRepo) UpdatePasswordHash(_ context.Context, id int64, hash string) error {
	u, ok := f.users[id]
	if !ok {
		return apperr.ErrUserNotFound
	}
	u.PasswordHash = hash
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
