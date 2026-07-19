package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	statsmodel "github.com/chenhao/omnigen-ai/server/internal/model/stats"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// fakeStatsHandlerRepo is a fixed-response repository.StatsRepository for
// exercising the full HTTP path (auth, query binding, response shape) —
// service.StatsService's permission-narrowing logic itself is covered by
// internal/service/stats_test.go's fakeStatsRepo, so this double doesn't
// need to record anything, just return a report with all three blocks
// populated so the "valid request returns the three blocks" test has
// something non-trivial to assert on.
type fakeStatsHandlerRepo struct{}

func (fakeStatsHandlerRepo) GetReport(context.Context, statsmodel.Query) (*statsmodel.Report, error) {
	return &statsmodel.Report{
		Overview: statsmodel.Overview{
			TotalCalls:      3,
			SucceededCalls:  2,
			FailedCalls:     1,
			TotalTokens:     100,
			TokensAvailable: true,
			VideoSeconds:    30,
		},
		ByModel: []statsmodel.ByModel{
			{Model: "happyhorse-1.1-t2v", Mode: "t2v", Calls: 1, Succeeded: 1, VideoSeconds: 30},
		},
		ByDay: []statsmodel.ByDay{
			{Day: time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC), Calls: 3, Succeeded: 2, Failed: 1},
		},
	}, nil
}

var _ repository.StatsRepository = fakeStatsHandlerRepo{}

const statsHandlerTestSecret = "stats-handler-test-secret"

// newStatsTestEnv wires the real middleware chain around StatsHandler,
// mirroring newVideoGenTestEnv in generation_video_test.go — this exercises
// auth end-to-end rather than stubbing UserIDFrom/RoleFrom.
func newStatsTestEnv(t *testing.T) (r *gin.Engine, userToken, adminToken string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := newMemRepo()
	jwtMgr := jwtx.NewManager(statsHandlerTestSecret, time.Hour)

	hash, err := password.Hash("password123")
	require.NoError(t, err)

	u := &usermodel.User{
		Username: "alice", PasswordHash: hash, DisplayName: "alice",
		Role: usermodel.RoleUser, Status: usermodel.StatusActive,
	}
	require.NoError(t, repo.Create(context.Background(), u))
	userToken, err = jwtMgr.Generate(u.ID, u.Role)
	require.NoError(t, err)

	admin := &usermodel.User{
		Username: "bob", PasswordHash: hash, DisplayName: "bob",
		Role: usermodel.RoleAdmin, Status: usermodel.StatusActive,
	}
	require.NoError(t, repo.Create(context.Background(), admin))
	adminToken, err = jwtMgr.Generate(admin.ID, admin.Role)
	require.NoError(t, err)

	statsSvc := service.NewStatsService(fakeStatsHandlerRepo{})
	statsH := handler.NewStatsHandler(statsSvc)

	r = gin.New()
	r.Use(middleware.Recovery(), middleware.ErrorHandler())
	authed := r.Group("/api", middleware.Auth(jwtMgr, repo))
	authed.GET("/stats", statsH.Get)

	return r, userToken, adminToken
}

func TestStatsHandler_NoAuth_401(t *testing.T) {
	r, _, _ := newStatsTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestStatsHandler_MalformedFrom_422(t *testing.T) {
	r, userToken, _ := newStatsTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/stats?from=not-a-date", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
}

func TestStatsHandler_MalformedTo_422(t *testing.T) {
	r, userToken, _ := newStatsTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/stats?to=2026-13-99", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
}

func TestStatsHandler_ValidRequest_ReturnsThreeBlocks(t *testing.T) {
	r, userToken, _ := newStatsTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/stats?from=2026-07-01T00%3A00%3A00Z&to=2026-07-20T00%3A00%3A00Z", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())

	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)

	overview, ok := data["overview"].(map[string]any)
	require.True(t, ok, "响应缺少 overview 块")
	assert.Equal(t, float64(3), overview["totalCalls"])

	byModel, ok := data["byModel"].([]any)
	require.True(t, ok, "响应缺少 byModel 块")
	assert.Len(t, byModel, 1)

	byDay, ok := data["byDay"].([]any)
	require.True(t, ok, "响应缺少 byDay 块")
	assert.Len(t, byDay, 1)
}

// TestStatsHandler_Admin_QueryStillWorks is a smoke test that the admin
// path through the full middleware chain also reaches the handler
// successfully — the actual permission-narrowing assertions live in
// internal/service/stats_test.go, this just confirms nothing in the HTTP
// layer trips over RoleAdmin.
func TestStatsHandler_Admin_QueryStillWorks(t *testing.T) {
	r, _, adminToken := newStatsTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/stats?userId=5", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())
}
