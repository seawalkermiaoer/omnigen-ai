package auth

import (
	"github.com/golang-jwt/jwt/v5"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
)

// Claims 是 JWT 载荷。Subject 存 userID 的十进制字符串。
// Role 冗余在 token 里只用于快速拒绝，真正的权威状态每请求回查数据库。
type Claims struct {
	jwt.RegisteredClaims
	Role usermodel.Role `json:"role"`
}
