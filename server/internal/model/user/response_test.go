package user_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
)

func TestFromEntity_OmitsPasswordHash(t *testing.T) {
	entity := usermodel.User{
		ID:           7,
		Username:     "alice",
		PasswordHash: "$2a$10$SUPERSECRETHASHVALUE",
		DisplayName:  "Alice",
		Role:         usermodel.RoleAdmin,
		Status:       usermodel.StatusActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	raw, err := json.Marshal(usermodel.FromEntity(entity))
	require.NoError(t, err)

	assert.NotContains(t, string(raw), "SUPERSECRETHASH")
	assert.NotContains(t, strings.ToLower(string(raw)), "passwordhash")
	assert.Contains(t, string(raw), `"username":"alice"`)
	assert.Contains(t, string(raw), `"role":"admin"`)
}

func TestListQuery_OffsetAndLimit(t *testing.T) {
	q := usermodel.ListQuery{Page: 3, PageSize: 20}
	assert.Equal(t, 40, q.Offset())
	assert.Equal(t, 20, q.Limit())
}
