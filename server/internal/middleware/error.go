package middleware

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

// Fail 让 handler 以统一方式上报错误，由 ErrorHandler 收口渲染。
func Fail(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()
}

// ErrorHandler 把 handler 上报的错误转成统一响应。
// AppError.Internal 只写日志，绝不进响应体。
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}
		err := c.Errors.Last().Err

		var appErr *apperr.AppError
		if !errors.As(err, &appErr) {
			appErr = apperr.ErrInternal.Wrap(err)
		}

		if appErr.HTTPStatus() >= http.StatusInternalServerError {
			slog.Error("请求处理失败",
				"code", appErr.Code(), "path", c.Request.URL.Path,
				"method", c.Request.Method, "internal", appErr.Internal())
		} else {
			slog.Info("请求被拒绝",
				"code", appErr.Code(), "path", c.Request.URL.Path, "method", c.Request.Method)
		}

		c.JSON(appErr.HTTPStatus(), common.Response{Code: appErr.Code()})
	}
}

// Recovery 兜住 panic，避免进程崩溃并保持响应格式统一。
func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		slog.Error("panic 恢复", "path", c.Request.URL.Path, "recovered", recovered)
		c.AbortWithStatusJSON(http.StatusInternalServerError,
			common.Response{Code: apperr.ErrInternal.Code()})
	})
}
