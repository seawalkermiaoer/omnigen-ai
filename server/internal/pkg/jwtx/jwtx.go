// Package jwtx 封装 JWT 的签发与解析。
// token 只承载身份声明；用户是否仍然有效由中间件每请求回查数据库确认。
package jwtx

import (
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	authmodel "github.com/chenhao/omnigen-ai/server/internal/model/auth"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
)

// issuer 是本服务签发 token 时写入的 Issuer 声明。Parse 强制校验它，
// 防止其他共享同一份 JWT_SECRET 的服务签发的 token 被当作本服务的合法 token 接受。
const issuer = "omnigen-ai"

type Manager struct {
	secret []byte
	ttl    time.Duration
}

func NewManager(secret string, ttl time.Duration) *Manager {
	return &Manager{secret: []byte(secret), ttl: ttl}
}

func (m *Manager) Generate(userID int64, role usermodel.Role) (string, error) {
	now := time.Now()
	claims := authmodel.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
			Issuer:    issuer,
		},
		Role: role,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("签发 token 失败: %w", err)
	}
	return signed, nil
}

// Parse 校验签名与有效期。显式限定签名算法为 HS256，
// 防止攻击者把算法头改成 none 或非对称算法绕过校验。
func (m *Manager) Parse(token string) (*authmodel.Claims, error) {
	parsed, err := jwt.ParseWithClaims(
		token,
		&authmodel.Claims{},
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("不接受的签名算法: %v", t.Header["alg"])
			}
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
	)
	if err != nil {
		return nil, fmt.Errorf("token 校验失败: %w", err)
	}
	claims, ok := parsed.Claims.(*authmodel.Claims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("token 载荷非法")
	}
	return claims, nil
}

func UserID(c *authmodel.Claims) (int64, error) {
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("token 中的用户 ID 非法: %q", c.Subject)
	}
	return id, nil
}
