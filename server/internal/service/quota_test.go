package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

func TestQuotaService_Consume_ThenRefund(t *testing.T) {
	repo := newFakeRepo()
	u := seedUser(t, repo, "q", "password123", usermodel.RoleUser, usermodel.StatusActive)
	total := 2
	u.QuotaTotal = &total

	q := service.NewQuotaService(repo)

	require.NoError(t, q.Consume(context.Background(), u.ID))
	assert.Equal(t, 1, repo.users[u.ID].QuotaUsed)

	require.NoError(t, q.Refund(context.Background(), u.ID))
	assert.Equal(t, 0, repo.users[u.ID].QuotaUsed)
}

func TestQuotaService_Consume_ExhaustedSurfacesQuotaExceeded(t *testing.T) {
	repo := newFakeRepo()
	u := seedUser(t, repo, "q2", "password123", usermodel.RoleUser, usermodel.StatusActive)
	total := 1
	u.QuotaTotal = &total

	q := service.NewQuotaService(repo)
	require.NoError(t, q.Consume(context.Background(), u.ID))

	err := q.Consume(context.Background(), u.ID)
	assertCode(t, err, "QUOTA_EXCEEDED")
}
