package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/catalog"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/dashscope"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// The service-level fakes live in package service_test and aren't visible
// here — same reasoning as upload_test.go's fakeUploadStore/fakeUploadResolver,
// redeclared minimally so this file can exercise the full HTTP path (auth +
// binding + service + response shape) without a real database or network.

type fakeImageSettings struct {
	values map[settingmodel.Key]string
}

func (f *fakeImageSettings) GetDecrypted(_ context.Context, key settingmodel.Key) (string, error) {
	return f.values[key], nil
}

var _ service.SettingReader = (*fakeImageSettings)(nil)

// fakeImageTaskRepo is a minimal in-memory repository.TaskRepository —
// GenerateImage only calls Create, so that's the only method with real
// behavior here.
type fakeImageTaskRepo struct {
	tasks  []generationmodel.Task
	nextID int64
}

func (f *fakeImageTaskRepo) Create(_ context.Context, t *generationmodel.Task) error {
	f.nextID++
	t.ID = f.nextID
	f.tasks = append(f.tasks, *t)
	return nil
}
func (f *fakeImageTaskRepo) GetByIDForUser(context.Context, int64, int64) (*generationmodel.Task, error) {
	return nil, nil
}
func (f *fakeImageTaskRepo) ListForUser(context.Context, int64, int, int) ([]generationmodel.Task, int64, error) {
	return nil, 0, nil
}
func (f *fakeImageTaskRepo) UpdateStatus(context.Context, int64, generationmodel.Status, string, string) error {
	return nil
}
func (f *fakeImageTaskRepo) UpdateResult(context.Context, int64, []string, map[string]any, string) error {
	return nil
}
func (f *fakeImageTaskRepo) ClaimPending(context.Context, int) ([]generationmodel.Task, error) {
	return nil, nil
}

var _ repository.TaskRepository = (*fakeImageTaskRepo)(nil)

type imageProviderStub struct {
	result *provider.ImageResult
	err    error
}

func (s imageProviderStub) GenerateImage(context.Context, provider.ImageRequest) (*provider.ImageResult, error) {
	return s.result, s.err
}

func fixedImageProviderFactory(result *provider.ImageResult, err error) service.ImageProviderFactory {
	return func(catalog.Protocol, string, string, string, string) provider.ImageProvider {
		return imageProviderStub{result: result, err: err}
	}
}

// imageGenSeededKey is the fake credential threaded through settings for
// these tests — assertions check it never leaks into any HTTP response.
const imageGenSeededKey = "sk-image-gen-handler-test-seeded-plaintext-key"

// newImageGenTestEnv wires the real middleware chain (auth + error
// handling) around ImageGenerationHandler, mirroring newUploadTestEnv in
// upload_test.go.
func newImageGenTestEnv(t *testing.T, factory service.ImageProviderFactory) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := newMemRepo()
	jwtMgr := jwtx.NewManager("image-gen-handler-test-secret", time.Hour)

	hash, err := password.Hash("password123")
	require.NoError(t, err)
	u := &usermodel.User{
		Username: "alice", PasswordHash: hash, DisplayName: "alice",
		Role: usermodel.RoleUser, Status: usermodel.StatusActive,
	}
	require.NoError(t, repo.Create(context.Background(), u))
	token, err := jwtMgr.Generate(u.ID, u.Role)
	require.NoError(t, err)

	settings := &fakeImageSettings{values: map[settingmodel.Key]string{
		settingmodel.KeyDashscopeAPIKey: imageGenSeededKey,
		settingmodel.KeyRegion:          "cn-beijing",
	}}
	imgSvc := service.NewImageGenerationServiceWithFactory(settings, &fakeImageTaskRepo{}, factory)
	imgH := handler.NewImageGenerationHandler(imgSvc)

	r := gin.New()
	r.Use(middleware.Recovery(), middleware.ErrorHandler())
	authed := r.Group("/api", middleware.Auth(jwtMgr, repo))
	authed.POST("/generate/image", imgH.Generate)

	return r, token
}

func TestImageGenerationHandler_NoAuth_401(t *testing.T) {
	r, _ := newImageGenTestEnv(t, fixedImageProviderFactory(&provider.ImageResult{Images: []string{"x"}}, nil))

	body := `{"model":"qwen-image-plus","prompt":"a cat"}`
	req := httptest.NewRequest(http.MethodPost, "/api/generate/image", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// 故意不带 Authorization header。

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestImageGenerationHandler_MalformedBody_422(t *testing.T) {
	r, token := newImageGenTestEnv(t, fixedImageProviderFactory(&provider.ImageResult{Images: []string{"x"}}, nil))

	req := httptest.NewRequest(http.MethodPost, "/api/generate/image", strings.NewReader(`{"model":`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
}

func TestImageGenerationHandler_MissingModel_422(t *testing.T) {
	r, token := newImageGenTestEnv(t, fixedImageProviderFactory(&provider.ImageResult{Images: []string{"x"}}, nil))

	req := httptest.NewRequest(http.MethodPost, "/api/generate/image", strings.NewReader(`{"prompt":"a cat"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
}

func TestImageGenerationHandler_HappyPath_ResponseHasNoCredentials(t *testing.T) {
	r, token := newImageGenTestEnv(t, fixedImageProviderFactory(&provider.ImageResult{
		Images: []string{"https://cdn.example.com/result.png"},
		Usage:  map[string]any{"image_count": 1},
		Model:  "qwen-image-plus",
	}, nil))

	body := `{"model":"qwen-image-plus","prompt":"a cat","n":1,"size":"1328*1328"}`
	req := httptest.NewRequest(http.MethodPost, "/api/generate/image", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())

	raw := w.Body.String()
	assert.False(t, strings.Contains(raw, "sk-"), "响应体不得包含任何 sk- 前缀的凭证材料: %s", raw)
	assert.False(t, strings.Contains(raw, imageGenSeededKey), "响应体不得包含种子明文 key: %s", raw)

	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, common.CodeOK, resp.Code)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok, "响应 data 应该是个对象，实际是 %#v", resp.Data)
	assert.Equal(t, string(generationmodel.StatusSucceeded), data["status"])
	assert.Equal(t, string(generationmodel.TaskModeImgGen), data["mode"])
}

func TestImageGenerationHandler_UpstreamFailure_ReturnsErrorResponse(t *testing.T) {
	upstreamErr := dashscope.ErrUpstreamHTTP.Wrap(errors.New("dashscope: 上游返回 HTTP 500"))
	r, token := newImageGenTestEnv(t, fixedImageProviderFactory(nil, upstreamErr))

	body := `{"model":"qwen-image-plus","prompt":"a cat"}`
	req := httptest.NewRequest(http.MethodPost, "/api/generate/image", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())

	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEqual(t, apperr.ErrInternal.Code(), resp.Code, "上游失败必须给出具体错误码，不是泛化的 INTERNAL_ERROR")
	assert.Equal(t, dashscope.CodeUpstreamHTTP, resp.Code)
	assert.False(t, strings.Contains(w.Body.String(), "sk-"))
	assert.False(t, strings.Contains(w.Body.String(), imageGenSeededKey))
}
