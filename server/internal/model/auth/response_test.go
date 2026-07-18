package auth_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmodel "github.com/chenhao/omnigen-ai/server/internal/model/auth"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
)

// LoginResponse 靠嵌入 UserResponse 天然不含密码哈希，
// 但这个保证是结构性的、没有断言守着——有人给 LoginResponse
// 加个"顺手"的字段就会悄悄失效。
func TestLoginResponse_OmitsPasswordHash(t *testing.T) {
	entity := usermodel.User{
		ID:           7,
		Username:     "alice",
		PasswordHash: "$2a$10$SUPERSECRETHASHVALUE",
		Role:         usermodel.RoleAdmin,
		Status:       usermodel.StatusActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	raw, err := json.Marshal(authmodel.LoginResponse{
		Token: "some.jwt.token",
		User:  usermodel.FromEntity(entity),
	})
	require.NoError(t, err)

	assert.NotContains(t, string(raw), "SUPERSECRETHASH")
	assert.NotContains(t, strings.ToLower(string(raw)), "passwordhash")
	assert.Contains(t, string(raw), `"token":"some.jwt.token"`)
	assert.Contains(t, string(raw), `"username":"alice"`)
}
