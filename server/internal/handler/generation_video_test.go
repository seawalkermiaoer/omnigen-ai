package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// fakeVideoTaskRepo is a real-enough in-memory repository.TaskRepository for
// exercising the full HTTP path (ownership scoping, listing) — same shape
// as fakeImageTaskRepo but with working GetByIDForUser/ListForUser, since
// the video handler's Get/List actually depend on them.
type fakeVideoTaskRepo struct {
	mu     sync.Mutex
	tasks  map[int64]*generationmodel.Task
	nextID int64
}

func newFakeVideoTaskRepo() *fakeVideoTaskRepo {
	return &fakeVideoTaskRepo{tasks: map[int64]*generationmodel.Task{}, nextID: 1}
}

func (f *fakeVideoTaskRepo) Create(_ context.Context, t *generationmodel.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t.ID = f.nextID
	f.nextID++
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now
	clone := *t
	f.tasks[t.ID] = &clone
	return nil
}

func (f *fakeVideoTaskRepo) GetByIDForUser(_ context.Context, id, userID int64) (*generationmodel.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok || t.UserID != userID {
		return nil, apperr.ErrTaskNotFound
	}
	clone := *t
	return &clone, nil
}

func (f *fakeVideoTaskRepo) ListForUser(_ context.Context, userID int64, offset, limit int) ([]generationmodel.Task, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var owned []*generationmodel.Task
	for _, t := range f.tasks {
		if t.UserID == userID {
			owned = append(owned, t)
		}
	}
	sort.Slice(owned, func(i, j int) bool { return owned[i].ID > owned[j].ID })
	total := int64(len(owned))
	if offset >= len(owned) {
		return []generationmodel.Task{}, total, nil
	}
	end := offset + limit
	if end > len(owned) {
		end = len(owned)
	}
	out := make([]generationmodel.Task, 0, end-offset)
	for _, t := range owned[offset:end] {
		out = append(out, *t)
	}
	return out, total, nil
}

func (f *fakeVideoTaskRepo) UpdateStatus(context.Context, int64, generationmodel.Status, string, string) error {
	return nil
}
func (f *fakeVideoTaskRepo) UpdateResult(context.Context, int64, []string, map[string]any, string) error {
	return nil
}
func (f *fakeVideoTaskRepo) ClaimPending(context.Context, int) ([]generationmodel.Task, error) {
	return nil, nil
}

var _ repository.TaskRepository = (*fakeVideoTaskRepo)(nil)

type fakeVideoSettings struct {
	values map[settingmodel.Key]string
}

func (f *fakeVideoSettings) GetDecrypted(_ context.Context, key settingmodel.Key) (string, error) {
	return f.values[key], nil
}

var _ service.SettingReader = (*fakeVideoSettings)(nil)

type videoProviderStub struct {
	taskID string
	err    error
}

func (s videoProviderStub) CreateVideoTask(context.Context, provider.VideoRequest) (string, error) {
	return s.taskID, s.err
}
func (s videoProviderStub) PollTask(context.Context, string) (*provider.TaskResult, error) {
	return nil, nil
}

func fixedVideoProviderFactory(taskID string, err error) service.VideoProviderFactory {
	return func(string, string, string, string) provider.VideoProvider {
		return videoProviderStub{taskID: taskID, err: err}
	}
}

const videoGenSeededKey = "sk-video-gen-handler-test-seeded-plaintext-key"

// newVideoGenTestEnv wires the real middleware chain around
// VideoGenerationHandler, mirroring newImageGenTestEnv in
// generation_image_test.go.
func newVideoGenTestEnv(t *testing.T, factory service.VideoProviderFactory) (*gin.Engine, string, *fakeVideoTaskRepo, int64) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := newMemRepo()
	jwtMgr := jwtx.NewManager("video-gen-handler-test-secret", time.Hour)

	hash, err := password.Hash("password123")
	require.NoError(t, err)
	u := &usermodel.User{
		Username: "alice", PasswordHash: hash, DisplayName: "alice",
		Role: usermodel.RoleUser, Status: usermodel.StatusActive,
	}
	require.NoError(t, repo.Create(context.Background(), u))
	token, err := jwtMgr.Generate(u.ID, u.Role)
	require.NoError(t, err)

	settings := &fakeVideoSettings{values: map[settingmodel.Key]string{
		settingmodel.KeyDashscopeAPIKey: videoGenSeededKey,
		settingmodel.KeyRegion:          "cn-beijing",
	}}
	tasksRepo := newFakeVideoTaskRepo()
	videoSvc := service.NewVideoGenerationServiceWithFactory(settings, tasksRepo, factory)
	videoH := handler.NewVideoGenerationHandler(videoSvc)

	r := gin.New()
	r.Use(middleware.Recovery(), middleware.ErrorHandler())
	authed := r.Group("/api", middleware.Auth(jwtMgr, repo))
	authed.POST("/generate/video", videoH.Generate)
	authed.GET("/tasks/:id", videoH.Get)
	authed.GET("/tasks", videoH.List)

	return r, token, tasksRepo, u.ID
}

func TestVideoGenerationHandler_NoAuth_401(t *testing.T) {
	r, _, _, _ := newVideoGenTestEnv(t, fixedVideoProviderFactory("up-1", nil))

	body := `{"model":"happyhorse-1.1-t2v","prompt":"a cat"}`
	req := httptest.NewRequest(http.MethodPost, "/api/generate/video", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestVideoGenerationHandler_MissingModel_422(t *testing.T) {
	r, token, _, _ := newVideoGenTestEnv(t, fixedVideoProviderFactory("up-1", nil))

	req := httptest.NewRequest(http.MethodPost, "/api/generate/video", strings.NewReader(`{"prompt":"a cat"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
}

func TestVideoGenerationHandler_HappyPath_ReturnsLocalTaskID_NoCredentials(t *testing.T) {
	r, token, repo, userID := newVideoGenTestEnv(t, fixedVideoProviderFactory("dashscope-upstream-abc123", nil))

	body := `{"model":"happyhorse-1.1-t2v","prompt":"a cat running"}`
	req := httptest.NewRequest(http.MethodPost, "/api/generate/video", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())

	raw := w.Body.String()
	assert.False(t, strings.Contains(raw, "sk-"), "响应体不得包含任何 sk- 前缀的凭证材料: %s", raw)
	assert.False(t, strings.Contains(raw, videoGenSeededKey))

	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, string(generationmodel.StatusPending), data["status"])
	assert.Equal(t, "dashscope-upstream-abc123", data["upstreamTaskId"])

	localID := int64(data["id"].(float64))
	assert.NotZero(t, localID)

	stored, ok := repo.tasks[localID]
	require.True(t, ok, "落库的 id 必须能在 repository 里查到")
	assert.Equal(t, userID, stored.UserID)
}

func TestVideoGenerationHandler_GetOtherUsersTask_404NotFound(t *testing.T) {
	r, token, repo, _ := newVideoGenTestEnv(t, fixedVideoProviderFactory("up-1", nil))

	other := &generationmodel.Task{UserID: 999999, Mode: generationmodel.TaskModeT2V, Model: "happyhorse-1.1-t2v", Status: generationmodel.StatusPending, Prompt: "x"}
	require.NoError(t, repo.Create(context.Background(), other))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+itoa(other.ID), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code, "响应=%s", w.Body.String())

	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, apperr.ErrTaskNotFound.Code(), resp.Code)
}

func TestVideoGenerationHandler_List_ScopedToOwnUser(t *testing.T) {
	r, token, repo, userID := newVideoGenTestEnv(t, fixedVideoProviderFactory("up-1", nil))

	mine := &generationmodel.Task{UserID: userID, Mode: generationmodel.TaskModeT2V, Model: "happyhorse-1.1-t2v", Status: generationmodel.StatusSucceeded, Prompt: "mine"}
	require.NoError(t, repo.Create(context.Background(), mine))
	others := &generationmodel.Task{UserID: userID + 1, Mode: generationmodel.TaskModeT2V, Model: "happyhorse-1.1-t2v", Status: generationmodel.StatusSucceeded, Prompt: "not mine"}
	require.NoError(t, repo.Create(context.Background(), others))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())

	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(1), data["total"])
	items, ok := data["items"].([]any)
	require.True(t, ok)
	require.Len(t, items, 1)
	item := items[0].(map[string]any)
	assert.Equal(t, "mine", item["prompt"])
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}
