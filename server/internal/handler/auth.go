package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	authmodel "github.com/chenhao/omnigen-ai/server/internal/model/auth"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

type AuthHandler struct {
	auth *service.AuthService
}

func NewAuthHandler(auth *service.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req authmodel.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	resp, err := h.auth.Login(c.Request.Context(), req)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(resp))
}

// Logout 是无状态的：token 由前端丢弃即可。
// 保留该接口是为了给前端一个统一的登出动作，并留下审计日志。
func (h *AuthHandler) Logout(c *gin.Context) {
	c.JSON(http.StatusOK, common.OK(nil))
}

func (h *AuthHandler) Me(c *gin.Context) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}
	resp, err := h.auth.GetCurrentUser(c.Request.Context(), userID)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(resp))
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}
	var req authmodel.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	if err := h.auth.ChangePassword(c.Request.Context(), userID, req); err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(nil))
}
