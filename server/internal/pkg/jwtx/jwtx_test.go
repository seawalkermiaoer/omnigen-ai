package jwtx_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
)

func newManager(t *testing.T, ttl time.Duration) *jwtx.Manager {
	t.Helper()
	return jwtx.NewManager("unit-test-secret", ttl)
}

func TestGenerateThenParse(t *testing.T) {
	m := newManager(t, time.Hour)

	token, err := m.Generate(42, usermodel.RoleAdmin)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := m.Parse(token)
	require.NoError(t, err)

	assert.Equal(t, "42", claims.Subject)
	assert.Equal(t, usermodel.RoleAdmin, claims.Role)

	uid, err := jwtx.UserID(claims)
	require.NoError(t, err)
	assert.Equal(t, int64(42), uid)
}

func TestParse_RejectsExpiredToken(t *testing.T) {
	m := newManager(t, -time.Minute) // 签发即过期

	token, err := m.Generate(1, usermodel.RoleUser)
	require.NoError(t, err)

	_, err = m.Parse(token)
	require.Error(t, err)
}

func TestParse_RejectsWrongSecret(t *testing.T) {
	issuer := jwtx.NewManager("secret-a", time.Hour)
	verifier := jwtx.NewManager("secret-b", time.Hour)

	token, err := issuer.Generate(1, usermodel.RoleUser)
	require.NoError(t, err)

	_, err = verifier.Parse(token)
	require.Error(t, err)
}

// 防 alg=none 降级攻击：篡改算法头的 token 必须被拒。
func TestParse_RejectsNoneAlgorithm(t *testing.T) {
	m := newManager(t, time.Hour)
	// header {"alg":"none","typ":"JWT"} + payload {"sub":"1"}，无签名
	forged := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiIxIn0."

	_, err := m.Parse(forged)
	require.Error(t, err)
}

func TestParse_RejectsGarbage(t *testing.T) {
	m := newManager(t, time.Hour)
	_, err := m.Parse("obviously-not-a-jwt")
	require.Error(t, err)
}
