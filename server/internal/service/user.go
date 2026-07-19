package service

import (
	"context"
	"fmt"
	"log/slog"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// UserService 承载用户管理的业务规则。
//
// 契约：所有接收 actorID 的方法，其 actorID 必须来自已认证的上下文
// （handler 层的 middleware.UserIDFrom），绝不能取自请求体或查询参数。
// 自我保护与「最后一个管理员」两条规则的正确性完全依赖这一点——
// 若 actorID 可被调用方指定，攻击者只要传一个别人的 ID 就能绕过自我保护。
type UserService struct {
	users repository.UserRepository
}

func NewUserService(users repository.UserRepository) *UserService {
	return &UserService{users: users}
}

// defaultQuotaTotal 是新建用户在未显式指定额度时的默认算力配额。默认给
// 一个有限值而不是 nil（不限量）——留空表示不限量会让每个新账号都能无限
// 使用，与配额功能的初衷相悖。管理员创建时可以传 CreateRequest.QuotaTotal
// 覆盖它。EnsureBootstrapAdmin 引导的首个管理员是唯一例外：那条路径不走
// 这条默认逻辑，保持不限量，见该方法注释。
const defaultQuotaTotal = 100

func (s *UserService) Create(ctx context.Context, req usermodel.CreateRequest) (*usermodel.UserResponse, error) {
	return s.create(ctx, req, true)
}

// create 是 Create 与 EnsureBootstrapAdmin 共用的内部实现。
// applyDefaultQuota 控制 req.QuotaTotal 为 nil 时是否补上 defaultQuotaTotal：
// 普通创建（Create）走 true；EnsureBootstrapAdmin 走 false，
// 让首个管理员保持不限量，不被这条默认逻辑意外限额。
func (s *UserService) create(ctx context.Context, req usermodel.CreateRequest, applyDefaultQuota bool) (*usermodel.UserResponse, error) {
	hash, err := password.Hash(req.Password)
	if err != nil {
		return nil, mapHashError(err)
	}
	quotaTotal := req.QuotaTotal
	if quotaTotal == nil && applyDefaultQuota {
		def := defaultQuotaTotal
		quotaTotal = &def
	}
	u := &usermodel.User{
		Username:     req.Username,
		PasswordHash: hash,
		DisplayName:  req.DisplayName,
		Role:         req.Role,
		Status:       usermodel.StatusActive,
		QuotaTotal:   quotaTotal,
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
	// QuotaUnlimited 优先于 QuotaTotal：两者同时出现在一次请求里时，
	// "改成不限量"的意图应当赢，而不是被同一请求里可能一并传来的
	// 具体额度值悄悄覆盖回去。
	switch {
	case req.QuotaUnlimited != nil && *req.QuotaUnlimited:
		target.QuotaTotal = nil
	case req.QuotaTotal != nil:
		q := *req.QuotaTotal
		target.QuotaTotal = &q
	}

	if err := s.users.Update(ctx, target); err != nil {
		return nil, err
	}
	resp := usermodel.FromEntity(*target)
	return &resp, nil
}

// ResetPassword 由管理员重置任意用户的密码，不需要旧密码。
// 刻意不接收 actorID：本系统的管理员之间互相信任——他们本就能互相
// 升降级、并共用同一套上游 API Key，禁止管理员之间重置密码
// 只会增加摩擦而不产生真正的安全边界。
// 用户自助改密走 AuthService.ChangePassword，那条路径需要验证旧密码。
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
		// 必须说出来，不能静默返回。
		//
		// 踩过的坑：改了 config.yaml 的 bootstrap.admin_password、重建容器后
		// 登录不上。原因是 pgdata 卷还在、老 admin 还在，这里直接返回，
		// 配置里的新密码一个字都没被用到——而日志里什么都没有，表现和
		// "密码填错了"一模一样，只能靠读源码才想得明白。
		//
		// 只记事实与后续动作，绝不记密码本身：启动日志经常被贴进工单、
		// 聊天窗口和公开的构建输出里。
		slog.Info("已存在活跃管理员，本次跳过自举，bootstrap.admin_password 未被使用；"+
			"如需改密请登录后在用户管理里修改，或直接更新数据库中的 password_hash",
			"activeAdmins", n)
		return nil
	}
	// 走内部 create(..., false)，不经过 Create 的默认限额逻辑——
	// 首个管理员必须保持不限量，否则每次全新部署引导出来的 admin
	// 会莫名其妙只有 100 次额度。
	_, err = s.create(ctx, usermodel.CreateRequest{
		Username:    username,
		Password:    plainPassword,
		DisplayName: username,
		Role:        usermodel.RoleAdmin,
	}, false)
	if err != nil {
		return fmt.Errorf("播种首个管理员失败（检查 BOOTSTRAP_ADMIN_USERNAME / BOOTSTRAP_ADMIN_PASSWORD，密码上限 72 字节）: %w", err)
	}
	return nil
}
