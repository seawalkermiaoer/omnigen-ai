package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/model/common"
)

// Pinger 抽象数据库连通性检查，便于测试时传 nil。
type Pinger interface {
	Ping(ctx context.Context) error
}

type HealthHandler struct {
	db Pinger
}

func NewHealthHandler(db Pinger) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) Check(c *gin.Context) {
	status := gin.H{"service": "up"}
	if h.db != nil {
		if err := h.db.Ping(c.Request.Context()); err != nil {
			status["database"] = "down"
			c.JSON(http.StatusServiceUnavailable, common.Response{
				Code: "HEALTH_DB_UNREACHABLE", Data: status,
			})
			return
		}
		status["database"] = "up"
	}
	c.JSON(http.StatusOK, common.OK(status))
}
