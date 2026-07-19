package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	statsmodel "github.com/chenhao/omnigen-ai/server/internal/model/stats"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// fakeStatsRepo records the last Query it was called with, so tests can
// assert on exactly what service.StatsService handed down to the
// repository — the permission narrowing under test happens entirely in the
// service, so the repository call is the observation point.
type fakeStatsRepo struct {
	lastQuery statsmodel.Query
	report    *statsmodel.Report
}

func newFakeStatsRepo() *fakeStatsRepo {
	return &fakeStatsRepo{report: &statsmodel.Report{
		ByModel: []statsmodel.ByModel{},
		ByDay:   []statsmodel.ByDay{},
	}}
}

func (f *fakeStatsRepo) GetReport(_ context.Context, q statsmodel.Query) (*statsmodel.Report, error) {
	f.lastQuery = q
	return f.report, nil
}

func int64Ptr(v int64) *int64 { return &v }

// TestStatsService_NonAdmin_UserIDParamIgnored is the core assertion of this
// layer: a non-admin passing someone else's userId gets their OWN data —
// the repository must observe UserID == actorID, not the value the caller
// passed in. This must be an overwrite, not a validate-and-reject: ignoring
// the parameter is the correct semantic for a caller who, from a regular
// user's perspective, shouldn't even know this parameter exists.
func TestStatsService_NonAdmin_UserIDParamIgnored(t *testing.T) {
	repo := newFakeStatsRepo()
	svc := service.NewStatsService(repo)

	const actorID int64 = 42
	someoneElse := int64Ptr(999)

	_, err := svc.GetReport(context.Background(), actorID, usermodel.RoleUser, statsmodel.Query{UserID: someoneElse})
	require.NoError(t, err)

	require.NotNil(t, repo.lastQuery.UserID)
	assert.Equal(t, actorID, *repo.lastQuery.UserID, "非管理员传别人的 userId 必须被覆写成自己的 actorID")
}

// TestStatsService_NonAdmin_NoUserIDStillScopedToSelf covers the case where
// a non-admin doesn't pass userId at all — they should still only ever see
// their own data, not "no filter" the way an admin's nil would mean.
func TestStatsService_NonAdmin_NoUserIDStillScopedToSelf(t *testing.T) {
	repo := newFakeStatsRepo()
	svc := service.NewStatsService(repo)

	const actorID int64 = 7
	_, err := svc.GetReport(context.Background(), actorID, usermodel.RoleUser, statsmodel.Query{})
	require.NoError(t, err)

	require.NotNil(t, repo.lastQuery.UserID)
	assert.Equal(t, actorID, *repo.lastQuery.UserID)
}

// TestStatsService_Admin_NoUserIDReturnsAll: an admin who doesn't pass
// userId gets the all-users aggregate — the repository must see UserID as
// nil, untouched.
func TestStatsService_Admin_NoUserIDReturnsAll(t *testing.T) {
	repo := newFakeStatsRepo()
	svc := service.NewStatsService(repo)

	_, err := svc.GetReport(context.Background(), 1, usermodel.RoleAdmin, statsmodel.Query{})
	require.NoError(t, err)

	assert.Nil(t, repo.lastQuery.UserID)
}

// TestStatsService_Admin_UserIDFilters: an admin who does pass userId gets
// exactly that user's data — the value passes through unchanged.
func TestStatsService_Admin_UserIDFilters(t *testing.T) {
	repo := newFakeStatsRepo()
	svc := service.NewStatsService(repo)

	target := int64Ptr(123)
	_, err := svc.GetReport(context.Background(), 1, usermodel.RoleAdmin, statsmodel.Query{UserID: target})
	require.NoError(t, err)

	require.NotNil(t, repo.lastQuery.UserID)
	assert.Equal(t, int64(123), *repo.lastQuery.UserID)
}

// TestStatsService_TimeRangePassesThroughUnchanged: From/To are never
// touched by the permission-narrowing logic, for either role.
func TestStatsService_TimeRangePassesThroughUnchanged(t *testing.T) {
	repo := newFakeStatsRepo()
	svc := service.NewStatsService(repo)

	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)

	_, err := svc.GetReport(context.Background(), 1, usermodel.RoleAdmin, statsmodel.Query{From: &from, To: &to})
	require.NoError(t, err)

	require.NotNil(t, repo.lastQuery.From)
	require.NotNil(t, repo.lastQuery.To)
	assert.True(t, from.Equal(*repo.lastQuery.From))
	assert.True(t, to.Equal(*repo.lastQuery.To))

	// 非管理员同样不应改动时间区间，只应改动 UserID。
	repo2 := newFakeStatsRepo()
	svc2 := service.NewStatsService(repo2)
	_, err = svc2.GetReport(context.Background(), 5, usermodel.RoleUser, statsmodel.Query{From: &from, To: &to})
	require.NoError(t, err)
	require.NotNil(t, repo2.lastQuery.From)
	require.NotNil(t, repo2.lastQuery.To)
	assert.True(t, from.Equal(*repo2.lastQuery.From))
	assert.True(t, to.Equal(*repo2.lastQuery.To))
}
