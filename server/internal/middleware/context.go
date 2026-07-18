package middleware

import (
	"github.com/gin-gonic/gin"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
)

const (
	ctxUserID   = "omnigen.userID"
	ctxUserRole = "omnigen.userRole"
)

func UserIDFrom(c *gin.Context) (int64, bool) {
	v, ok := c.Get(ctxUserID)
	if !ok {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}

func RoleFrom(c *gin.Context) (usermodel.Role, bool) {
	v, ok := c.Get(ctxUserRole)
	if !ok {
		return "", false
	}
	role, ok := v.(usermodel.Role)
	return role, ok
}
