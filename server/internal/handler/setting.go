package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// SettingHandler 只做绑定/校验/委派，业务规则（加解密、"空值=不修改"语义、
// 上游探测）全部在 service.SettingService——与 UserHandler/AuthHandler 的
// 分工一致。
type SettingHandler struct {
	settings *service.SettingService
}

func NewSettingHandler(settings *service.SettingService) *SettingHandler {
	return &SettingHandler{settings: settings}
}

// Get 处理 GET /api/settings（任意已登录用户）：返回全部配置项的脱敏视图。
func (h *SettingHandler) Get(c *gin.Context) {
	resp, err := h.settings.Get(c.Request.Context())
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(resp))
}

// Update 处理 PUT /api/settings（仅 admin，由路由层的 middleware.RequireAdmin
// 把关）。actorID 必须来自认证上下文，绝不能取自请求体——与
// UserHandler.Update/Delete 的 actorID 契约完全一致，理由也相同：一旦
// actorID 可被调用方指定，写审计字段 updated_by 就形同虚设。
func (h *SettingHandler) Update(c *gin.Context) {
	actorID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}
	var req settingmodel.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	resp, err := h.settings.Update(c.Request.Context(), actorID, req)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(resp))
}

// TestConnection 处理 POST /api/settings/test（仅 admin）：用当前落库的凭证
// 试调上游，验证它们确实有效。
//
// 请求体 {provider} 决定测哪一套凭证；省略/空字符串等价于 "dashscope"，
// 保持与现有前端（从不发这个字段）的兼容。请求体本身也是可选的——
// ShouldBindJSON 在完全没有 body 时返回 io.EOF，这里把它当成"未提供
// provider"处理，而不是校验错误。
func (h *SettingHandler) TestConnection(c *gin.Context) {
	var req settingmodel.TestConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	if err := h.settings.TestConnection(c.Request.Context(), req.Provider); err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(nil))
}
