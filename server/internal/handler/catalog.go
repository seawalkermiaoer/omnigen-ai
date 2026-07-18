package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/model/catalog"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
)

// CatalogResponse 包一层 {"models": [...]}而不是直接返回裸数组，给前端／
// 未来扩展（比如加一个 updatedAt 或版本号）留出口子，且与其它接口
// （SettingsResponse 等）"顶层是个对象"的形状保持一致。
type CatalogResponse struct {
	Models []catalog.Model `json:"models"`
}

// CatalogHandler 没有依赖：目录（internal/model/catalog）是编译期常量数据，
// 不经过 service/repository。
type CatalogHandler struct{}

func NewCatalogHandler() *CatalogHandler { return &CatalogHandler{} }

// Get 处理 GET /api/catalog（任意已登录用户）：返回全部模型目录，
// 前端所有下拉框、参数面板由它驱动，不再各自维护一份。
func (h *CatalogHandler) Get(c *gin.Context) {
	c.JSON(http.StatusOK, common.OK(CatalogResponse{Models: catalog.All()}))
}
