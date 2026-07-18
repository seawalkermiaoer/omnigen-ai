package middleware

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
)

// UserLoader 是中间件对 repository 的最小依赖面。
type UserLoader interface {
	GetByID(ctx context.Context, id int64) (*usermodel.User, error)
}

// Auth 校验 JWT 签名，随后按 ID 回查数据库确认用户仍存在、处于 active、
// 且 token 的签发时间不早于该用户最后一次改密的时间。
// 这次查询是刻意为之：它让禁用、删除、改密立即生效，
// 代价是每请求一次主键查询——在本系统的量级下可忽略。
func Auth(jwtMgr *jwtx.Manager, users UserLoader) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		if raw == "" || !strings.HasPrefix(raw, "Bearer ") {
			Fail(c, apperr.ErrUnauthorized)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
		if token == "" {
			Fail(c, apperr.ErrUnauthorized)
			return
		}

		claims, err := jwtMgr.Parse(token)
		if err != nil {
			Fail(c, apperr.ErrUnauthorized.Wrap(err))
			return
		}
		userID, err := jwtx.UserID(claims)
		if err != nil {
			Fail(c, apperr.ErrUnauthorized.Wrap(err))
			return
		}

		u, err := users.GetByID(c.Request.Context(), userID)
		if err != nil {
			// 用户已被删除：token 仍有签名有效性，但身份已不存在。
			Fail(c, apperr.ErrUnauthorized.Wrap(err))
			return
		}
		if !u.IsActive() {
			Fail(c, apperr.ErrUserDisabled)
			return
		}
		// 改密后签发时间早于改密时间的旧 token 一律作废。
		// 只查 status 不足以兑现「改密立即生效」——被窃取的 token
		// 会在改密后继续有效到过期为止。
		if claims.IssuedAt != nil && claims.IssuedAt.Time.Before(u.PasswordChangedAt) {
			Fail(c, apperr.ErrUnauthorized)
			return
		}

		c.Set(ctxUserID, u.ID)
		c.Set(ctxUserRole, u.Role) // 以数据库为准，不采信 token 里的 role
		c.Next()
	}
}

// RequireAdmin 必须挂在 Auth 之后。
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := RoleFrom(c)
		if !ok || role != usermodel.RoleAdmin {
			Fail(c, apperr.ErrForbidden)
			return
		}
		c.Next()
	}
}
