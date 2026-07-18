package handler_test

import (
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
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	"github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/dashscope"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// fakeOptimizeHandlerSettings is a minimal service.SettingReader satisfied
// by a fixed map — same reasoning as fakeImageSettings in
// generation_image_test.go, the service-level fake lives in package
// service_test and isn't visible here.
type fakeOptimizeHandlerSettings struct {
	values map[settingmodel.Key]string
}

func (f *fakeOptimizeHandlerSettings) GetDecrypted(_ context.Context, key settingmodel.Key) (string, error) {
	return f.values[key], nil
}

var _ service.SettingReader = (*fakeOptimizeHandlerSettings)(nil)

// capturingOptimizeProviderFactory always returns the same
// (result, err) pair and records every provider.OptimizeRequest it's
// called with, so tests can assert which system prompt actually reached
// the "upstream" call (e.g. the imgedit-alias regression check).
type capturingOptimizeProviderFactory struct {
	text  string
	model string
	err   error

	capturedReqs []provider.OptimizeRequest
}

func (f *capturingOptimizeProviderFactory) Factory() service.ProviderFactory {
	return func(apiKey, region, workspaceID, endpoint string) provider.OptimizeProvider {
		return optimizeProviderStubFunc(func(_ context.Context, req provider.OptimizeRequest) (string, string, error) {
			f.capturedReqs = append(f.capturedReqs, req)
			if f.err != nil {
				return "", "", f.err
			}
			return f.text, f.model, nil
		})
	}
}

type optimizeProviderStubFunc func(ctx context.Context, req provider.OptimizeRequest) (string, string, error)

func (f optimizeProviderStubFunc) Optimize(ctx context.Context, req provider.OptimizeRequest) (string, string, error) {
	return f(ctx, req)
}

var _ provider.OptimizeProvider = optimizeProviderStubFunc(nil)

// newOptimizeTestEnv wires the real middleware chain (auth + error
// handling) around OptimizeHandler, mirroring newImageGenTestEnv in
// generation_image_test.go.
func newOptimizeTestEnv(t *testing.T, factory *capturingOptimizeProviderFactory) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := newMemRepo()
	jwtMgr := jwtx.NewManager("optimize-handler-test-secret", time.Hour)

	hash, err := password.Hash("password123")
	require.NoError(t, err)
	u := &usermodel.User{
		Username: "alice", PasswordHash: hash, DisplayName: "alice",
		Role: usermodel.RoleUser, Status: usermodel.StatusActive,
	}
	require.NoError(t, repo.Create(context.Background(), u))
	token, err := jwtMgr.Generate(u.ID, u.Role)
	require.NoError(t, err)

	settings := &fakeOptimizeHandlerSettings{values: map[settingmodel.Key]string{
		settingmodel.KeyDashscopeAPIKey: "sk-optimize-handler-test-seeded-plaintext-key",
		settingmodel.KeyRegion:          "cn-beijing",
	}}
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())
	h := handler.NewOptimizeHandler(svc)

	r := gin.New()
	r.Use(middleware.Recovery(), middleware.ErrorHandler())
	authed := r.Group("/api", middleware.Auth(jwtMgr, repo))
	authed.POST("/optimize-prompt", h.Optimize)

	return r, token
}

func TestOptimizeHandler_NoAuth_401(t *testing.T) {
	r, _ := newOptimizeTestEnv(t, &capturingOptimizeProviderFactory{text: "x", model: "qwen3.7-plus"})

	body := `{"mode":"t2v","draft":"a cat running"}`
	req := httptest.NewRequest(http.MethodPost, "/api/optimize-prompt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// 故意不带 Authorization header。

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestOptimizeHandler_MalformedBody_422(t *testing.T) {
	r, token := newOptimizeTestEnv(t, &capturingOptimizeProviderFactory{text: "x", model: "qwen3.7-plus"})

	req := httptest.NewRequest(http.MethodPost, "/api/optimize-prompt", strings.NewReader(`{"mode":`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
}

func TestOptimizeHandler_HappyPath_ReturnsPromptAndModel(t *testing.T) {
	factory := &capturingOptimizeProviderFactory{text: "a cat sprinting across a sunlit field", model: "qwen-plus"}
	r, token := newOptimizeTestEnv(t, factory)

	body := `{"mode":"t2v","draft":"a cat running"}`
	req := httptest.NewRequest(http.MethodPost, "/api/optimize-prompt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())

	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, common.CodeOK, resp.Code)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok, "响应 data 应该是个对象，实际是 %#v", resp.Data)
	assert.Equal(t, "a cat sprinting across a sunlit field", data["prompt"])
	assert.Equal(t, "qwen-plus", data["model"])

	require.Len(t, factory.capturedReqs, 1)
	assert.Equal(t, generation.SystemPrompts[generation.ModeT2V], factory.capturedReqs[0].SystemPrompt)
}

// TestOptimizeHandler_UnknownMode_FallsBackToT2VAndStillSucceeds asserts the
// actual behavior service.Optimize defines for an unrecognized mode
// (generation.PromptForMode falls back to t2v with a warning log) — not a
// new HTTP-layer rule. The request should still succeed end-to-end.
func TestOptimizeHandler_UnknownMode_FallsBackToT2VAndStillSucceeds(t *testing.T) {
	factory := &capturingOptimizeProviderFactory{text: "generic motion prompt", model: "qwen3.7-plus"}
	r, token := newOptimizeTestEnv(t, factory)

	body := `{"mode":"totally-bogus-mode","draft":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/optimize-prompt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())
	require.Len(t, factory.capturedReqs, 1)
	assert.Equal(t, generation.SystemPrompts[generation.ModeT2V], factory.capturedReqs[0].SystemPrompt)
}

// TestOptimizeHandler_ImgeditAlias_ReachesImageEditPromptNotT2V is a
// regression test for a real bug in the old system: the "imgedit" alias
// (sent by the client when the image-edit tab has no images yet) must
// resolve to the image-edit system prompt, never silently fall back to the
// t2v (text-to-video) prompt. Covered at the service layer already
// (optimize_test.go); this asserts the same contract survives the HTTP
// binding layer.
func TestOptimizeHandler_ImgeditAlias_ReachesImageEditPromptNotT2V(t *testing.T) {
	factory := &capturingOptimizeProviderFactory{text: "edited", model: "qwen3.7-plus"}
	r, token := newOptimizeTestEnv(t, factory)

	body := `{"mode":"imgedit","draft":"把背景换成海边"}`
	req := httptest.NewRequest(http.MethodPost, "/api/optimize-prompt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())
	require.Len(t, factory.capturedReqs, 1)
	assert.Equal(t, generation.SystemPrompts[generation.ModeImggenEdit], factory.capturedReqs[0].SystemPrompt)
	assert.NotEqual(t, generation.SystemPrompts[generation.ModeT2V], factory.capturedReqs[0].SystemPrompt)
}

func TestOptimizeHandler_UpstreamFailure_ReturnsErrorResponseNot500(t *testing.T) {
	upstreamErr := dashscope.ErrUpstreamHTTP.Wrap(errors.New("dashscope: 上游返回 HTTP 401"))
	factory := &capturingOptimizeProviderFactory{err: upstreamErr}
	r, token := newOptimizeTestEnv(t, factory)

	body := `{"mode":"t2v","draft":"a cat running"}`
	req := httptest.NewRequest(http.MethodPost, "/api/optimize-prompt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusInternalServerError, w.Code, "响应=%s", w.Body.String())

	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEqual(t, apperr.ErrInternal.Code(), resp.Code, "上游失败必须给出具体错误码，不是泛化的 INTERNAL_ERROR")
	assert.Equal(t, dashscope.CodeUpstreamHTTP, resp.Code)
}
