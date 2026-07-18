package service

import (
	"context"
	"errors"

	authmodel "github.com/chenhao/omnigen-ai/server/internal/model/auth"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// mapHashError 把 password.Hash 的失败翻成对客户端有意义的错误码。
// bcrypt 的 72 字节上限按字节算，而 gin binding 的 max=72 按 rune 算，
// 24 个以上中文字符会通过前端与绑定校验却在这里失败——
// 那是用户输入问题，必须是 422 而不是 500。
func mapHashError(err error) *apperr.AppError {
	if errors.Is(err, password.ErrTooLong) {
		return apperr.ErrPasswordTooLong.Wrap(err)
	}
	return apperr.ErrInternal.Wrap(err)
}

type AuthService struct {
	users repository.UserRepository
	jwt   *jwtx.Manager
}

func NewAuthService(users repository.UserRepository, jwt *jwtx.Manager) *AuthService {
	return &AuthService{users: users, jwt: jwt}
}

// Login 校验凭据并签发 token。
// 用户不存在与密码错误返回同一错误码，避免接口沦为用户名枚举器。
func (s *AuthService) Login(ctx context.Context, req authmodel.LoginRequest) (*authmodel.LoginResponse, error) {
	u, err := s.users.GetByUsername(ctx, req.Username)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(err, &appErr) && appErr.Code() == apperr.ErrUserNotFound.Code() {
			return nil, apperr.ErrInvalidCredentials
		}
		return nil, err
	}

	if !password.Verify(u.PasswordHash, req.Password) {
		return nil, apperr.ErrInvalidCredentials
	}
	if !u.IsActive() {
		return nil, apperr.ErrUserDisabled
	}

	token, err := s.jwt.Generate(u.ID, u.Role)
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return &authmodel.LoginResponse{Token: token, User: usermodel.FromEntity(*u)}, nil
}

func (s *AuthService) ChangePassword(ctx context.Context, userID int64, req authmodel.ChangePasswordRequest) error {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if !password.Verify(u.PasswordHash, req.OldPassword) {
		return apperr.ErrWrongOldPassword
	}
	hash, err := password.Hash(req.NewPassword)
	if err != nil {
		return mapHashError(err)
	}
	return s.users.UpdatePasswordHash(ctx, userID, hash)
}

func (s *AuthService) GetCurrentUser(ctx context.Context, userID int64) (*usermodel.UserResponse, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	resp := usermodel.FromEntity(*u)
	return &resp, nil
}
