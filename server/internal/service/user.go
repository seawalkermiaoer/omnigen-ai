package service

import (
	"context"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

type UserService struct {
	users repository.UserRepository
}

func NewUserService(users repository.UserRepository) *UserService {
	return &UserService{users: users}
}

func (s *UserService) Create(ctx context.Context, req usermodel.CreateRequest) (*usermodel.UserResponse, error) {
	hash, err := password.Hash(req.Password)
	if err != nil {
		return nil, mapHashError(err)
	}
	u := &usermodel.User{
		Username:     req.Username,
		PasswordHash: hash,
		DisplayName:  req.DisplayName,
		Role:         req.Role,
		Status:       usermodel.StatusActive,
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, err
	}
	resp := usermodel.FromEntity(*u)
	return &resp, nil
}

func (s *UserService) List(ctx context.Context, q usermodel.ListQuery) (*usermodel.UserListResponse, error) {
	items, total, err := s.users.List(ctx, q.Offset(), q.Limit())
	if err != nil {
		return nil, err
	}
	return &usermodel.UserListResponse{Total: total, Items: usermodel.FromEntities(items)}, nil
}

// Update 修改目标用户。两条护栏：
//   - 不允许对自己做降级或禁用（改自己的显示名可以）
//   - 不允许把系统里最后一个活跃 admin 降级或禁用
func (s *UserService) Update(ctx context.Context, actorID, targetID int64, req usermodel.UpdateRequest) (*usermodel.UserResponse, error) {
	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return nil, err
	}

	demoting := req.Role != nil && *req.Role != usermodel.RoleAdmin && target.IsAdmin()
	disabling := req.Status != nil && *req.Status == usermodel.StatusDisabled && target.IsActive()

	if actorID == targetID && (demoting || disabling) {
		return nil, apperr.ErrModifySelf
	}
	if demoting || disabling {
		if err := s.ensureNotLastAdmin(ctx, *target); err != nil {
			return nil, err
		}
	}

	if req.DisplayName != nil {
		target.DisplayName = *req.DisplayName
	}
	if req.Role != nil {
		target.Role = *req.Role
	}
	if req.Status != nil {
		target.Status = *req.Status
	}

	if err := s.users.Update(ctx, target); err != nil {
		return nil, err
	}
	resp := usermodel.FromEntity(*target)
	return &resp, nil
}

func (s *UserService) ResetPassword(ctx context.Context, targetID int64, req usermodel.ResetPasswordRequest) error {
	hash, err := password.Hash(req.Password)
	if err != nil {
		return mapHashError(err)
	}
	return s.users.UpdatePasswordHash(ctx, targetID, hash)
}

func (s *UserService) Delete(ctx context.Context, actorID, targetID int64) error {
	if actorID == targetID {
		return apperr.ErrModifySelf
	}
	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return err
	}
	if err := s.ensureNotLastAdmin(ctx, *target); err != nil {
		return err
	}
	return s.users.Delete(ctx, targetID)
}

// ensureNotLastAdmin 在目标是活跃 admin 且系统中仅剩这一个时拒绝操作，
// 避免把自己彻底锁在系统外面。
func (s *UserService) ensureNotLastAdmin(ctx context.Context, target usermodel.User) error {
	if !target.IsAdmin() || !target.IsActive() {
		return nil
	}
	n, err := s.users.CountActiveAdmins(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return apperr.ErrLastAdmin
	}
	return nil
}

// EnsureBootstrapAdmin 在系统中没有任何活跃 admin 时创建首个管理员。
// 已存在则跳过，绝不覆盖——否则每次重启都会重置密码。
func (s *UserService) EnsureBootstrapAdmin(ctx context.Context, username, plainPassword string) error {
	n, err := s.users.CountActiveAdmins(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = s.Create(ctx, usermodel.CreateRequest{
		Username:    username,
		Password:    plainPassword,
		DisplayName: username,
		Role:        usermodel.RoleAdmin,
	})
	return err
}
