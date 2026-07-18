package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

type UserHandler struct {
	users *service.UserService
}

func NewUserHandler(users *service.UserService) *UserHandler {
	return &UserHandler{users: users}
}

func pathID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.ErrValidation.Wrap(err)
	}
	return id, nil
}

func (h *UserHandler) List(c *gin.Context) {
	var q usermodel.ListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	resp, err := h.users.List(c.Request.Context(), q)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(resp))
}

func (h *UserHandler) Create(c *gin.Context) {
	var req usermodel.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	resp, err := h.users.Create(c.Request.Context(), req)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, common.OK(resp))
}

func (h *UserHandler) Update(c *gin.Context) {
	targetID, err := pathID(c)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	actorID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}
	var req usermodel.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	resp, err := h.users.Update(c.Request.Context(), actorID, targetID, req)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(resp))
}

func (h *UserHandler) ResetPassword(c *gin.Context) {
	targetID, err := pathID(c)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	var req usermodel.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	if err := h.users.ResetPassword(c.Request.Context(), targetID, req); err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(nil))
}

func (h *UserHandler) Delete(c *gin.Context) {
	targetID, err := pathID(c)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	actorID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}
	if err := h.users.Delete(c.Request.Context(), actorID, targetID); err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(nil))
}
