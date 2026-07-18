package jwtx_test

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmodel "github.com/chenhao/omnigen-ai/server/internal/model/auth"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
)

// 单元测试用的 secret 长度与线上要求（≥32 字节）保持一致，
// 避免测试值和生产约束脱节造成误导。
const testSecret = "unit-test-secret-0123456789abcdef"

func newManager(t *testing.T, ttl time.Duration) *jwtx.Manager {
	t.Helper()
	return jwtx.NewManager(testSecret, ttl)
}

func TestGenerateThenParse(t *testing.T) {
	cases := []struct {
		name string
		role usermodel.Role
	}{
		{"admin", usermodel.RoleAdmin},
		{"user", usermodel.RoleUser},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newManager(t, time.Hour)

			token, err := m.Generate(42, tc.role)
			require.NoError(t, err)
			require.NotEmpty(t, token)

			claims, err := m.Parse(token)
			require.NoError(t, err)

			assert.Equal(t, "42", claims.Subject)
			assert.Equal(t, tc.role, claims.Role)

			uid, err := jwtx.UserID(claims)
			require.NoError(t, err)
			assert.Equal(t, int64(42), uid)
		})
	}
}

func TestParse_RejectsExpiredToken(t *testing.T) {
	m := newManager(t, -time.Minute) // 签发即过期

	token, err := m.Generate(1, usermodel.RoleUser)
	require.NoError(t, err)

	_, err = m.Parse(token)
	require.Error(t, err)
}

func TestParse_RejectsWrongSecret(t *testing.T) {
	issuer := jwtx.NewManager("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", time.Hour)
	verifier := jwtx.NewManager("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", time.Hour)

	token, err := issuer.Generate(1, usermodel.RoleUser)
	require.NoError(t, err)

	_, err = verifier.Parse(token)
	require.Error(t, err)
}

// Generate 固定签发 Issuer:"omnigen-ai"，Parse 必须校验它，而不仅仅验证签名。
// 否则任何拿到同一份 JWT_SECRET 的其他服务签发的 token 也会被当作合法 token 接受。
func TestParse_RejectsForeignIssuer(t *testing.T) {
	m := newManager(t, time.Hour)

	now := time.Now()
	claims := authmodel.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "1",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			Issuer:    "some-other-service",
		},
		Role: usermodel.RoleUser,
	}
	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	require.NoError(t, err)

	_, err = m.Parse(forged)
	require.Error(t, err)
}

// 防 alg=none 降级攻击：篡改算法头的 token 必须被拒。
// header/payload 现场用 base64 拼出来，方便读者核对内容而不必手算 base64。
func TestParse_RejectsNoneAlgorithm(t *testing.T) {
	m := newManager(t, time.Hour)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"1"}`))
	forged := header + "." + payload + "." // 无签名段

	_, err := m.Parse(forged)
	require.Error(t, err)
}

func TestParse_RejectsGarbage(t *testing.T) {
	m := newManager(t, time.Hour)
	_, err := m.Parse("obviously-not-a-jwt")
	require.Error(t, err)
}

func TestUserID_RejectsNonNumericSubject(t *testing.T) {
	claims := &authmodel.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "not-a-number"},
	}

	uid, err := jwtx.UserID(claims)
	require.Error(t, err)
	assert.Zero(t, uid)
}

func TestGenerate_HonorsTTL(t *testing.T) {
	m := newManager(t, time.Hour)

	token, err := m.Generate(1, usermodel.RoleUser)
	require.NoError(t, err)

	claims, err := m.Parse(token)
	require.NoError(t, err)

	assert.WithinDuration(t, time.Now().Add(time.Hour), claims.ExpiresAt.Time, 5*time.Second)
}
