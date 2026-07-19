package service

import (
	"context"

	statsmodel "github.com/chenhao/omnigen-ai/server/internal/model/stats"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// StatsService wraps repository.StatsRepository with the one piece of
// business logic this domain has: permission narrowing.
//
// A non-admin's userId query parameter is unconditionally overwritten with
// their own actorID — never validated and rejected. Ignoring the parameter
// is the correct semantic here: from a regular user's point of view that
// parameter simply doesn't exist. Rejecting it (e.g. with a 403) would
// instead confirm that the parameter exists and that they're not allowed to
// use it, which leaks more than silently scoping the query does. An admin's
// UserID passes through untouched — nil means "all users", set means "that
// one user".
type StatsService struct {
	stats repository.StatsRepository
}

func NewStatsService(stats repository.StatsRepository) *StatsService {
	return &StatsService{stats: stats}
}

// GetReport runs the three-block aggregation, scoped per actorRole:
//
//   - non-admin: q.UserID is forced to &actorID, regardless of what the
//     caller passed in q.UserID.
//   - admin: q.UserID passes through as given (nil = all users, set = that
//     user).
func (s *StatsService) GetReport(ctx context.Context, actorID int64, actorRole usermodel.Role, q statsmodel.Query) (*statsmodel.Report, error) {
	if actorRole != usermodel.RoleAdmin {
		q.UserID = &actorID
	}
	return s.stats.GetReport(ctx, q)
}
