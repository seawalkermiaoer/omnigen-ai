package service

import (
	"context"

	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// QuotaService 封装算力额度的扣减与退回。
//
// 它刻意只有两个动作、不持有任何状态：并发安全完全由 repository 的
// 单条带条件 UPDATE 保证，service 层不做"先查再判断"的逻辑，否则
// 就把好不容易避开的竞态又加回来了。
type QuotaService struct {
	users repository.UserRepository
}

func NewQuotaService(users repository.UserRepository) *QuotaService {
	return &QuotaService{users: users}
}

// Consume 扣减一次。额度耗尽返回 apperr.ErrQuotaExceeded。
func (s *QuotaService) Consume(ctx context.Context, userID int64) error {
	return s.users.ConsumeQuota(ctx, userID)
}

// Refund 退回一次。这是尽力而为的补偿动作——退款失败不应该把
// 一个"生成已失败"的结果再变成另一个错误盖住原因，调用方只记日志。
func (s *QuotaService) Refund(ctx context.Context, userID int64) error {
	return s.users.RefundQuota(ctx, userID)
}
