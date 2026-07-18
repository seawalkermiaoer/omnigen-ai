package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

func sampleUser(name string, role usermodel.Role) *usermodel.User {
	return &usermodel.User{
		Username:     name,
		PasswordHash: "$2a$10$fakehashfakehashfakehashfakehashfakehashfakehashfakeha",
		DisplayName:  name,
		Role:         role,
		Status:       usermodel.StatusActive,
	}
}

func TestUserRepo_CreateThenGetByID(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)

		u := sampleUser("alice", usermodel.RoleAdmin)
		require.NoError(t, repo.Create(ctx, u))
		assert.NotZero(t, u.ID, "Create 应回填自增 ID")
		assert.False(t, u.CreatedAt.IsZero(), "Create 应回填 CreatedAt")

		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, "alice", got.Username)
		assert.Equal(t, usermodel.RoleAdmin, got.Role)
		assert.Equal(t, usermodel.StatusActive, got.Status)
	})
}

func TestUserRepo_GetByUsername(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		require.NoError(t, repo.Create(ctx, sampleUser("bob", usermodel.RoleUser)))

		got, err := repo.GetByUsername(ctx, "bob")
		require.NoError(t, err)
		assert.Equal(t, "bob", got.Username)
	})
}

func TestUserRepo_NotFoundReturnsAppError(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)

		_, err := repo.GetByID(ctx, 999999)
		require.Error(t, err)
		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "USER_NOT_FOUND", appErr.Code())

		_, err = repo.GetByUsername(ctx, "ghost")
		require.Error(t, err)
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "USER_NOT_FOUND", appErr.Code())
	})
}

func TestUserRepo_DuplicateUsernameReturnsTaken(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		require.NoError(t, repo.Create(ctx, sampleUser("carol", usermodel.RoleUser)))

		err := repo.Create(ctx, sampleUser("carol", usermodel.RoleUser))
		require.Error(t, err)
		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "USER_USERNAME_TAKEN", appErr.Code())
	})
}

func TestUserRepo_Update(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("dave", usermodel.RoleUser)
		require.NoError(t, repo.Create(ctx, u))

		u.DisplayName = "Dave 改名"
		u.Status = usermodel.StatusDisabled
		u.Role = usermodel.RoleAdmin
		require.NoError(t, repo.Update(ctx, u))

		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, "Dave 改名", got.DisplayName)
		assert.Equal(t, usermodel.StatusDisabled, got.Status)
		assert.Equal(t, usermodel.RoleAdmin, got.Role)
		assert.True(t, got.UpdatedAt.After(got.CreatedAt) || got.UpdatedAt.Equal(got.CreatedAt))
	})
}

func TestUserRepo_Delete(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("erin", usermodel.RoleUser)
		require.NoError(t, repo.Create(ctx, u))

		require.NoError(t, repo.Delete(ctx, u.ID))

		_, err := repo.GetByID(ctx, u.ID)
		require.Error(t, err)

		// 删除不存在的用户应报 USER_NOT_FOUND，而非静默成功
		err = repo.Delete(ctx, u.ID)
		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "USER_NOT_FOUND", appErr.Code())
	})
}

func TestUserRepo_ListPaginates(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		for _, n := range []string{"u1", "u2", "u3", "u4", "u5"} {
			require.NoError(t, repo.Create(ctx, sampleUser(n, usermodel.RoleUser)))
		}

		items, total, err := repo.List(ctx, 0, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total, "total 应为总数而非当页数量")
		assert.Len(t, items, 2)

		page3, total, err := repo.List(ctx, 4, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total)
		assert.Len(t, page3, 1)
	})
}

func TestUserRepo_CountActiveAdmins(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)

		require.NoError(t, repo.Create(ctx, sampleUser("admin1", usermodel.RoleAdmin)))
		require.NoError(t, repo.Create(ctx, sampleUser("admin2", usermodel.RoleAdmin)))
		require.NoError(t, repo.Create(ctx, sampleUser("plain", usermodel.RoleUser)))

		disabled := sampleUser("admin3", usermodel.RoleAdmin)
		disabled.Status = usermodel.StatusDisabled
		require.NoError(t, repo.Create(ctx, disabled))

		n, err := repo.CountActiveAdmins(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n, "被禁用的 admin 不计入")
	})
}

func TestUserRepo_UpdatePasswordHash(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("frank", usermodel.RoleUser)
		require.NoError(t, repo.Create(ctx, u))

		newHash := "$2a$10$brandnewhashbrandnewhashbrandnewhashbrandnewhashbrandn"
		require.NoError(t, repo.UpdatePasswordHash(ctx, u.ID, newHash))

		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, newHash, got.PasswordHash)
	})
}
