package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/model/catalog"
	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/dashscope"
	"github.com/chenhao/omnigen-ai/server/internal/provider/t8star"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

const fakeDashscopeKey = "sk-dashscope-seeded-plaintext-1234567890"
const fakeT8starKey = "sk-t8star-seeded-plaintext-0987654321"

func dashscopeSettings(overrides map[settingmodel.Key]string) *fakeOptimizeSettings {
	values := map[settingmodel.Key]string{
		settingmodel.KeyDashscopeAPIKey:   fakeDashscopeKey,
		settingmodel.KeyT8starAPIKey:      fakeT8starKey,
		settingmodel.KeyRegion:            "cn-beijing",
		settingmodel.KeyDashscopeEndpoint: "",
		settingmodel.KeyT8starEndpoint:    "",
		settingmodel.KeyWorkspaceID:       "",
	}
	for k, v := range overrides {
		values[k] = v
	}
	return newFakeOptimizeSettings(values)
}

// newImageService is for the ~25 pre-quota tests in this file that pass
// arbitrary literal userIDs and have no opinion about quota. It goes through
// legacyUnlimitedQuotaRepo (generation_image_mock_test.go), which invents an
// unlimited-quota user for any id on first use — see that type's doc comment
// for why quota-sensitive tests must NOT use this constructor.
func newImageService(t *testing.T, settings *fakeOptimizeSettings, factory *recordingImageFactory) (*service.ImageGenerationService, *fakeTaskRepo) {
	t.Helper()
	taskRepo := newFakeTaskRepo()
	userRepo := newFakeRepo()
	quota := service.NewQuotaService(&legacyUnlimitedQuotaRepo{userRepo})
	svc := service.NewImageGenerationServiceWithFactory(settings, taskRepo, quota, factory.Factory())
	return svc, taskRepo
}

// newImageServiceWithQuota is for tests that actually exercise quota
// enforcement: it wires the plain *fakeUserRepo directly, with none of
// legacyUnlimitedQuotaRepo's auto-provisioning. An unseeded userID here
// fails loudly with QUOTA_EXCEEDED (fakeUserRepo's real-semantics-faithful
// "unknown user" branch, see mock_test.go) instead of silently getting
// unlimited credit — a quota test that reuses a bare literal userID by
// accident will fail immediately rather than passing without ever
// exercising a limit. Callers must seed a user (seedUser) and set its
// QuotaTotal before calling GenerateImage.
func newImageServiceWithQuota(t *testing.T, settings *fakeOptimizeSettings, factory *recordingImageFactory) (*service.ImageGenerationService, *fakeTaskRepo, *fakeUserRepo) {
	t.Helper()
	taskRepo := newFakeTaskRepo()
	userRepo := newFakeRepo()
	quota := service.NewQuotaService(userRepo)
	svc := service.NewImageGenerationServiceWithFactory(settings, taskRepo, quota, factory.Factory())
	return svc, taskRepo, userRepo
}

func TestGenerateImage_HappyPath_DashScope(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingImageFactory{
		result: &provider.ImageResult{
			Images: []string{"https://cdn.example.com/a.png"},
			Usage:  map[string]any{"image_count": 1},
			Model:  "qwen-image-plus",
		},
	}
	svc, repo := newImageService(t, settings, factory)

	task, err := svc.GenerateImage(context.Background(), 42, service.GenerateImageRequest{
		Model:  "qwen-image-plus",
		Prompt: "a cat",
		Params: provider.ImageParams{Size: "1328*1328", N: ptr(1)},
	})
	require.NoError(t, err)

	assert.Equal(t, generationmodel.StatusSucceeded, task.Status)
	assert.Equal(t, generationmodel.TaskModeImgGen, task.Mode)
	assert.Equal(t, []string{"https://cdn.example.com/a.png"}, task.ResultURLs)
	assert.Nil(t, task.Note)
	assert.NotZero(t, task.ID, "必须真的落库拿到 ID")

	require.Equal(t, 1, factory.calls)
	assert.Equal(t, catalog.ProtocolDashScope, factory.lastProtocol)
	assert.Equal(t, fakeDashscopeKey, factory.lastAPIKey)
	assert.Equal(t, "cn-beijing", factory.lastRegion)

	require.Len(t, repo.all(), 1)
	assert.Equal(t, generationmodel.StatusSucceeded, repo.all()[0].Status)
}

// t8star 协议成功时，provider.ImageResult.Note（散文说明）必须原样带进落库
// 的那一行——这是 t8star 协议独有的字段，Task 11 的说明里点名要求。
func TestGenerateImage_HappyPath_T8star_CarriesNoteIntoPersistedRow(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingImageFactory{
		result: &provider.ImageResult{
			Images: []string{"https://ai.t8star.org/img/1.png"},
			Usage:  map[string]any{"tokens": 123},
			Model:  "gpt-image-2-pro",
			Note:   "给你画好了，一只可爱的小猫。",
		},
	}
	svc, repo := newImageService(t, settings, factory)

	task, err := svc.GenerateImage(context.Background(), 7, service.GenerateImageRequest{
		Model:  "gpt-image-2",
		Prompt: "a cute cat",
	})
	require.NoError(t, err)

	assert.Equal(t, catalog.ProtocolOpenAI, factory.lastProtocol)
	assert.Equal(t, fakeT8starKey, factory.lastAPIKey)

	require.NotNil(t, task.Note)
	assert.Equal(t, "给你画好了，一只可爱的小猫。", *task.Note)
	// t8star 回显的具体子模型要覆盖请求里的目录 id。
	assert.Equal(t, "gpt-image-2-pro", task.Model)

	require.Len(t, repo.all(), 1)
	require.NotNil(t, repo.all()[0].Note)
	assert.Equal(t, "给你画好了，一只可爱的小猫。", *repo.all()[0].Note)
}

func TestGenerateImage_UnknownModel_ValidationError(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingImageFactory{}
	svc, repo := newImageService(t, settings, factory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model: "does-not-exist",
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls, "未知模型不应该打任何上游请求")
	assert.Empty(t, repo.all(), "校验失败不落库")
}

func TestGenerateImage_QwenImage_NEqual2_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}}}
	svc, _ := newImageService(t, settings, factory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "qwen-image",
		Prompt: "x",
		Params: provider.ImageParams{N: ptr(2)},
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls)
}

func TestGenerateImage_QwenImage_NEqual1_Accepted(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}, Model: "qwen-image"}}
	svc, _ := newImageService(t, settings, factory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "qwen-image",
		Prompt: "x",
		Params: provider.ImageParams{N: ptr(1)},
	})

	require.NoError(t, err)
	assert.Equal(t, 1, factory.calls)
}

func TestGenerateImage_QwenImageEditPlus_NEqual6_Accepted_NEqual7_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	images := []string{"img1"}

	okFactory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}, Model: "qwen-image-edit-plus"}}
	svc, _ := newImageService(t, settings, okFactory)
	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "qwen-image-edit-plus",
		Prompt: "edit",
		Images: images,
		Params: provider.ImageParams{N: ptr(6)},
	})
	require.NoError(t, err, "MaxN=6，n=6 应该被接受")
	assert.Equal(t, 1, okFactory.calls)

	rejectFactory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}}}
	svc2, _ := newImageService(t, settings, rejectFactory)
	_, err = svc2.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "qwen-image-edit-plus",
		Prompt: "edit",
		Images: images,
		Params: provider.ImageParams{N: ptr(7)},
	})
	require.Error(t, err, "MaxN=6，n=7 应该被拒绝")
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, rejectFactory.calls)
}

func TestGenerateImage_Wan27_USEast1_Rejected_CNBeijing_Accepted(t *testing.T) {
	rejectSettings := dashscopeSettings(map[settingmodel.Key]string{settingmodel.KeyRegion: "us-east-1"})
	rejectFactory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}}}
	svc, _ := newImageService(t, rejectSettings, rejectFactory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "wan2.7-image-pro",
		Prompt: "x",
		Params: provider.ImageParams{Size: "1K", N: ptr(1)},
	})
	require.Error(t, err, "us-east-1 不在 wan2.7 的 region 白名单里")
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, rejectFactory.calls, "region 被拒时不应该打任何上游请求")

	okSettings := dashscopeSettings(map[settingmodel.Key]string{settingmodel.KeyRegion: "cn-beijing"})
	okFactory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}, Model: "wan2.7-image-pro"}}
	svc2, _ := newImageService(t, okSettings, okFactory)

	_, err = svc2.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "wan2.7-image-pro",
		Prompt: "x",
		Params: provider.ImageParams{Size: "1K", N: ptr(1)},
	})
	require.NoError(t, err, "cn-beijing 在白名单内应该被接受")
	assert.Equal(t, 1, okFactory.calls)
}

// ── dashscope_endpoint / t8star_endpoint 是两个独立的键 ──────────────────
//
// 在旧的共享 endpoint 设计下，这四个测试里至少前两个会失败：
// generation_image.go 无论协议是什么都只读 settingmodel.KeyEndpoint 一个
// 键，t8star 请求会拿到本该只属于 dashscope 的地址，反之亦然。

// TestGenerateImage_T8star_NeverReceivesDashscopeEndpoint 是本次修复的
// 回归测试：只配置 dashscope_endpoint、不配置 t8star_endpoint 时，
// gpt-image-2（t8star/openai 协议）请求传给 provider 工厂的 endpoint 必须
// 是空字符串（=> t8star.ResolveBaseURL 会把它解析成默认地址
// https://ai.t8star.org，见 t8star.go），绝不能是 dashscope_endpoint 那个
// 值——旧代码合用一个 KeyEndpoint 时，这里断言会失败，factory.lastEndpoint
// 会等于 dashscopeOnlyEndpoint。
func TestGenerateImage_T8star_NeverReceivesDashscopeEndpoint(t *testing.T) {
	const dashscopeOnlyEndpoint = "https://dashscope-relay.example.com"
	settings := dashscopeSettings(map[settingmodel.Key]string{
		settingmodel.KeyDashscopeEndpoint: dashscopeOnlyEndpoint,
		settingmodel.KeyT8starEndpoint:    "",
	})
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}, Model: "gpt-image-2"}}
	svc, _ := newImageService(t, settings, factory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "gpt-image-2",
		Prompt: "x",
	})
	require.NoError(t, err)

	require.Equal(t, 1, factory.calls)
	assert.Equal(t, catalog.ProtocolOpenAI, factory.lastProtocol)
	assert.Empty(t, factory.lastEndpoint, "t8star 请求不应该收到 dashscope_endpoint 的值")
	assert.NotEqual(t, dashscopeOnlyEndpoint, factory.lastEndpoint)
}

// TestGenerateImage_Dashscope_NeverReceivesT8starEndpoint 是上一条的镜像
// 场景：只配置 t8star_endpoint 时，qwen-image（dashscope 协议）请求不能
// 拿到那个地址。
func TestGenerateImage_Dashscope_NeverReceivesT8starEndpoint(t *testing.T) {
	const t8starOnlyEndpoint = "https://ai.t8star.org"
	settings := dashscopeSettings(map[settingmodel.Key]string{
		settingmodel.KeyDashscopeEndpoint: "",
		settingmodel.KeyT8starEndpoint:    t8starOnlyEndpoint,
	})
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}, Model: "qwen-image"}}
	svc, _ := newImageService(t, settings, factory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "qwen-image",
		Prompt: "x",
		Params: provider.ImageParams{N: ptr(1)},
	})
	require.NoError(t, err)

	require.Equal(t, 1, factory.calls)
	assert.Equal(t, catalog.ProtocolDashScope, factory.lastProtocol)
	assert.Empty(t, factory.lastEndpoint, "dashscope 请求不应该收到 t8star_endpoint 的值")
	assert.NotEqual(t, t8starOnlyEndpoint, factory.lastEndpoint)
}

// TestGenerateImage_DashscopeEndpoint_NonEmptyPassedVerbatim 确认非空的
// dashscope_endpoint 原样透传给 provider 工厂——resolveRegionURL 的回退只
// 在它为空时才发生（见 dashscope/endpoint.go baseURL()），这里只需要确认
// service 层没有在中途篡改这个值。
func TestGenerateImage_DashscopeEndpoint_NonEmptyPassedVerbatim(t *testing.T) {
	const custom = "https://openapi.bsutopia.com"
	settings := dashscopeSettings(map[settingmodel.Key]string{settingmodel.KeyDashscopeEndpoint: custom})
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}, Model: "qwen-image-plus"}}
	svc, _ := newImageService(t, settings, factory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "qwen-image-plus",
		Prompt: "x",
		Params: provider.ImageParams{Size: "1328*1328", N: ptr(1)},
	})
	require.NoError(t, err)
	assert.Equal(t, custom, factory.lastEndpoint)
}

// TestGenerateImage_GPTImage2_SucceedsEvenWhenRegionLookupErrors 证明 t8star
// 请求真的从不读取 region——不仅仅是"读到的空值恰好被目录允许"（gpt-image-2
// 的 Regions 恰好是空集合，AllowsRegion 对任何 region 都放行，所以哪怕读了
// 也不会被拒），而是压根不发生这次 GetDecrypted(KeyRegion) 调用。这里用
// keyErr 让 region 这个键一旦被读取就报错，断言请求仍然成功——旧代码在
// AllowsRegion 校验之前无条件读 region，这个测试在旧代码下会失败
// （返回 boom 错误而不是成功）。
func TestGenerateImage_GPTImage2_SucceedsEvenWhenRegionLookupErrors(t *testing.T) {
	settings := dashscopeSettings(nil)
	settings.keyErr = map[settingmodel.Key]error{
		settingmodel.KeyRegion: errors.New("boom: region 设置读取失败"),
	}
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}, Model: "gpt-image-2"}}
	svc, _ := newImageService(t, settings, factory)

	task, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "gpt-image-2",
		Prompt: "x",
	})

	require.NoError(t, err, "t8star 协议的请求不应该因为 region 读取失败而失败——它压根不该去读 region")
	assert.Equal(t, generationmodel.StatusSucceeded, task.Status)
	assert.Empty(t, factory.lastRegion)
}

func TestGenerateImage_SizeOutsideCatalog_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}}}
	svc, _ := newImageService(t, settings, factory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "qwen-image-plus",
		Prompt: "x",
		Params: provider.ImageParams{Size: "9999*9999"},
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls)
}

func TestGenerateImage_ImageCountOverMaxImages_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}}}
	svc, _ := newImageService(t, settings, factory)

	// qwen-image-edit-plus MaxImages=3.
	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "qwen-image-edit-plus",
		Prompt: "edit",
		Images: []string{"a", "b", "c", "d"},
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls)
}

func TestGenerateImage_UnsupportedParam_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}}}
	svc, _ := newImageService(t, settings, factory)

	// gpt-image-2 的 Supports 是空集合，任何可选参数都不被认可。
	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "gpt-image-2",
		Prompt: "x",
		Params: provider.ImageParams{NegativePrompt: "no blur"},
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls)
}

func TestGenerateImage_CredentialsUnconfigured_SpecificCode(t *testing.T) {
	settings := dashscopeSettings(map[settingmodel.Key]string{settingmodel.KeyDashscopeAPIKey: ""})
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}}}
	svc, repo := newImageService(t, settings, factory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "qwen-image-plus",
		Prompt: "x",
	})

	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.ErrSettingIncomplete.Code(), appErr.Code(), "必须是明确的错误码，不是泛化的 500")
	assert.NotEqual(t, apperr.ErrInternal.Code(), appErr.Code())
	assert.Equal(t, 0, factory.calls, "凭证缺失不应该打任何上游请求")
	assert.Empty(t, repo.all(), "凭证缺失不落库")
}

func TestGenerateImage_UpstreamFailure_PersistsFailedRow_AndReturnsError(t *testing.T) {
	settings := dashscopeSettings(nil)
	upstreamErr := dashscope.ErrUpstreamHTTP.Wrap(errors.New("dashscope: 上游返回 HTTP 500"))
	factory := &recordingImageFactory{err: upstreamErr}
	svc, repo := newImageService(t, settings, factory)

	task, err := svc.GenerateImage(context.Background(), 99, service.GenerateImageRequest{
		Model:  "qwen-image-plus",
		Prompt: "x",
	})

	require.Error(t, err)
	// errors.Is 仍然命中 dashscope 的哨兵——normalizeUpstreamError 用 Wrap
	// 而不是丢弃原始错误，Unwrap 链完整保留。
	assert.True(t, errors.Is(err, dashscope.ErrUpstreamHTTP))
	assert.Nil(t, task, "失败时不返回 task，调用方只应该看到 error")

	// 但顶层错误码/状态必须是归一化之后的通用上游码，不是 provider 包自己
	// 的 DASHSCOPE_* 码——这是本次任务的核心契约（service 层收口上游状态，
	// provider 层的码只作为 Internal() 里的细节留存）。
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.ErrUpstreamFailed.Code(), appErr.Code())
	assert.Equal(t, apperr.ErrUpstreamFailed.HTTPStatus(), appErr.HTTPStatus())

	rows := repo.all()
	require.Len(t, rows, 1, "上游失败也必须落一行 FAILED")
	failed := rows[0]
	assert.Equal(t, generationmodel.StatusFailed, failed.Status)
	require.NotNil(t, failed.ErrorCode)
	assert.Equal(t, apperr.ErrUpstreamFailed.Code(), *failed.ErrorCode)
	require.NotNil(t, failed.ErrorMessage)
	assert.NotEmpty(t, *failed.ErrorMessage)
	// 上游原始细节（真实状态码文案）仍然完整落库，供运维排查——归一化只是
	// 不让它决定我们自己的 HTTP 状态，不是把它抹掉。
	assert.Contains(t, *failed.ErrorMessage, "上游返回 HTTP 500")
}

// TestGenerateImage_UpstreamAuthFailure_NormalizedTo502NotBare401 复现并锁定
// 本次修的 bug：DashScope 对一个失效 API key 返回 401，provider 层忠实转发
// 这个真实状态码（newUpstreamHTTPError），如果这个 401 原样冒充成
// /api/generate/image 自己的响应状态，web 端拦截器会把它当会话过期处理，
// 直接登出用户、丢掉正在填写的表单——见 upstream_error.go 顶部注释。
func TestGenerateImage_UpstreamAuthFailure_NormalizedTo502NotBare401(t *testing.T) {
	settings := dashscopeSettings(nil)
	upstreamErr := apperr.New(dashscope.CodeUpstreamHTTP, http.StatusUnauthorized).
		Wrap(errors.New("dashscope: invalid api key"))
	factory := &recordingImageFactory{err: upstreamErr}
	svc, repo := newImageService(t, settings, factory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "qwen-image-plus",
		Prompt: "x",
	})

	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.ErrUpstreamAuthFailed.Code(), appErr.Code())
	assert.Equal(t, http.StatusBadGateway, appErr.HTTPStatus(), "上游 401 绝不能变成我们自己的 401")
	assert.NotEqual(t, http.StatusUnauthorized, appErr.HTTPStatus())

	require.NotNil(t, appErr.Internal(), "上游细节必须还在 Internal() 里，供日志排查")
	assert.Contains(t, appErr.Internal().Error(), "invalid api key")

	rows := repo.all()
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].ErrorCode)
	assert.Equal(t, apperr.ErrUpstreamAuthFailed.Code(), *rows[0].ErrorCode)
	require.NotNil(t, rows[0].ErrorMessage)
	assert.Contains(t, *rows[0].ErrorMessage, "invalid api key", "详细原因进日志/落库，不进响应体")
}

// TestGenerateImage_UpstreamRateLimit_NormalizedTo429WithDedicatedCode 覆盖
// 429 分支：与 401/403 不同，429 是一个客户端可操作的明确信号（稍后重试），
// 因此保留 429 而不是折成 502，但错误码仍然是我们自己的
// UPSTREAM_RATE_LIMITED，不是裸的 provider 码。
func TestGenerateImage_UpstreamRateLimit_NormalizedTo429WithDedicatedCode(t *testing.T) {
	settings := dashscopeSettings(nil)
	upstreamErr := apperr.New(dashscope.CodeUpstreamHTTP, http.StatusTooManyRequests).
		Wrap(errors.New("dashscope: throttling"))
	factory := &recordingImageFactory{err: upstreamErr}
	svc, _ := newImageService(t, settings, factory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "qwen-image-plus",
		Prompt: "x",
	})

	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.ErrUpstreamRateLimited.Code(), appErr.Code())
	assert.Equal(t, http.StatusTooManyRequests, appErr.HTTPStatus())
}

// TestGenerateImage_ValidationErrorBeforeUpstreamCall_NotTouchedByNormalization
// 确保归一化只作用于真正打过上游的错误——校验失败（还没触到 provider）
// 必须原样透传 apperr.ErrValidation，不能被误判成"上游失败"。
func TestGenerateImage_ValidationErrorBeforeUpstreamCall_NotTouchedByNormalization(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}}}
	svc, _ := newImageService(t, settings, factory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "not-a-real-model",
		Prompt: "x",
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls, "校验失败不应该打任何上游请求")
}

// t8star 协议的上游失败同样必须落 FAILED 行——不是只有 dashscope 协议才有
// 这条规则。
func TestGenerateImage_T8starUpstreamFailure_PersistsFailedRow(t *testing.T) {
	settings := dashscopeSettings(nil)
	upstreamErr := t8star.ErrUpstreamError.Wrap(errors.New("t8star: call failed"))
	factory := &recordingImageFactory{err: upstreamErr}
	svc, repo := newImageService(t, settings, factory)

	_, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "gpt-image-2",
		Prompt: "x",
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, t8star.ErrUpstreamError))
	rows := repo.all()
	require.Len(t, rows, 1)
	assert.Equal(t, generationmodel.StatusFailed, rows[0].Status)
}

// 响应体（这里用持久化的 Task 序列化模拟"会发给前端的东西"）绝不能出现
// 凭证明文——无论是种子里的假 key 还是任何 "sk-" 前缀的字符串。
func TestGenerateImage_PersistedTask_NeverContainsCredentials(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingImageFactory{
		result: &provider.ImageResult{
			Images: []string{"https://cdn.example.com/a.png"},
			Usage:  map[string]any{"image_count": 1},
			Model:  "qwen-image-plus",
		},
	}
	svc, _ := newImageService(t, settings, factory)

	task, err := svc.GenerateImage(context.Background(), 1, service.GenerateImageRequest{
		Model:  "qwen-image-plus",
		Prompt: "x",
	})
	require.NoError(t, err)

	raw, err := json.Marshal(generationmodel.FromEntity(*task))
	require.NoError(t, err)
	body := string(raw)

	assert.False(t, strings.Contains(body, "sk-"), "响应体不得包含任何 sk- 前缀的凭证材料")
	assert.False(t, strings.Contains(body, fakeDashscopeKey))
	assert.False(t, strings.Contains(body, fakeT8starKey))
}

// ── 配额接入 ──────────────────────────────────────────────────────────

// 额度耗尽时必须在调用上游之前就拦住——不能先花钱再报错。
func TestGenerateImage_QuotaExhausted_NeverCallsUpstream(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}, Model: "qwen-image-plus"}}
	svc, taskRepo, userRepo := newImageServiceWithQuota(t, settings, factory)

	u := seedUser(t, userRepo, "quota-exhausted", "password123", usermodel.RoleUser, usermodel.StatusActive)
	zero := 0
	u.QuotaTotal = &zero

	_, err := svc.GenerateImage(context.Background(), u.ID, service.GenerateImageRequest{
		Model:  "qwen-image-plus",
		Prompt: "x",
		Params: provider.ImageParams{Size: "1328*1328", N: ptr(1)},
	})

	assertCode(t, err, "QUOTA_EXCEEDED")
	assert.Equal(t, 0, factory.calls, "额度耗尽不应该打任何上游请求")
	assert.Empty(t, taskRepo.all(), "额度耗尽不应该落任何 task 行")
}

func TestGenerateImage_Success_ConsumesOneAndMarksCharged(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}, Model: "qwen-image-plus"}}
	svc, taskRepo, userRepo := newImageServiceWithQuota(t, settings, factory)

	u := seedUser(t, userRepo, "quota-success", "password123", usermodel.RoleUser, usermodel.StatusActive)
	total := 2
	u.QuotaTotal = &total

	task, err := svc.GenerateImage(context.Background(), u.ID, service.GenerateImageRequest{
		Model:  "qwen-image-plus",
		Prompt: "x",
		Params: provider.ImageParams{Size: "1328*1328", N: ptr(1)},
	})
	require.NoError(t, err)

	assert.Equal(t, 1, userRepo.users[u.ID].QuotaUsed, "成功一次应该扣 1")
	assert.True(t, task.QuotaCharged, "成功的任务应该标记为已计费")

	rows := taskRepo.all()
	require.Len(t, rows, 1)
	assert.True(t, rows[0].QuotaCharged, "落库的行也应该标记为已计费")
}

func TestGenerateImage_UpstreamFailure_RefundsAndMarksNotCharged(t *testing.T) {
	settings := dashscopeSettings(nil)
	upstreamErr := dashscope.ErrUpstreamHTTP.Wrap(errors.New("dashscope: 上游返回 HTTP 500"))
	factory := &recordingImageFactory{err: upstreamErr}
	svc, taskRepo, userRepo := newImageServiceWithQuota(t, settings, factory)

	u := seedUser(t, userRepo, "quota-refund", "password123", usermodel.RoleUser, usermodel.StatusActive)
	total := 3
	u.QuotaTotal = &total

	_, err := svc.GenerateImage(context.Background(), u.ID, service.GenerateImageRequest{
		Model:  "qwen-image-plus",
		Prompt: "x",
	})
	require.Error(t, err)

	assert.Equal(t, 0, userRepo.users[u.ID].QuotaUsed, "上游失败应该退回额度")

	rows := taskRepo.all()
	require.Len(t, rows, 1, "上游失败也必须落一行 FAILED")
	assert.Equal(t, generationmodel.StatusFailed, rows[0].Status)
	assert.False(t, rows[0].QuotaCharged, "退款后的 FAILED 行不应该标记为已计费")
}

// 校验阶段失败（模型不存在 / 地域不允许 / 凭证未配置）不该动额度。
func TestGenerateImage_ValidationFailure_DoesNotTouchQuota(t *testing.T) {
	tests := []struct {
		name     string
		settings map[settingmodel.Key]string
		req      service.GenerateImageRequest
	}{
		{
			name: "未知模型",
			req: service.GenerateImageRequest{
				Model:  "does-not-exist",
				Prompt: "x",
			},
		},
		{
			name:     "地域不允许",
			settings: map[settingmodel.Key]string{settingmodel.KeyRegion: "us-east-1"},
			req: service.GenerateImageRequest{
				Model:  "wan2.7-image-pro",
				Prompt: "x",
				Params: provider.ImageParams{Size: "1K", N: ptr(1)},
			},
		},
		{
			name:     "凭证未配置",
			settings: map[settingmodel.Key]string{settingmodel.KeyDashscopeAPIKey: ""},
			req: service.GenerateImageRequest{
				Model:  "qwen-image-plus",
				Prompt: "x",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			settings := dashscopeSettings(tc.settings)
			factory := &recordingImageFactory{result: &provider.ImageResult{Images: []string{"x"}}}
			svc, taskRepo, userRepo := newImageServiceWithQuota(t, settings, factory)

			u := seedUser(t, userRepo, "quota-validation-"+tc.name, "password123", usermodel.RoleUser, usermodel.StatusActive)
			total := 5
			u.QuotaTotal = &total

			_, err := svc.GenerateImage(context.Background(), u.ID, tc.req)

			require.Error(t, err)
			assert.Equal(t, 0, userRepo.users[u.ID].QuotaUsed, "校验失败不该动额度")
			assert.Equal(t, 0, factory.calls, "校验失败不应该打任何上游请求")
			assert.Empty(t, taskRepo.all(), "校验失败不落库")
		})
	}
}

// panic 展开时函数不会走到任何 return 语句，命名返回值 retErr 因此仍是
// 零值 nil——只判断 retErr 会完全漏掉这条路径：用户被扣费，provider
// panic，middleware.Recovery 在更外层拦住并吐出干净的 500，从外部看
// 什么都没错，但额度已经凭空消失了。这个测试用一个会 panic 的假
// provider 复现该场景：扣费之后、provider 调用本身 panic，defer 必须
// 依然退款，并把 panic 原样重新抛出去交给 middleware.Recovery——不能
// 在这里悄悄吞掉。
func TestGenerateImage_ProviderPanics_StillRefundsQuota(t *testing.T) {
	settings := dashscopeSettings(nil)
	panicFactory := func(catalog.Protocol, string, string, string, string) provider.ImageProvider {
		return imageProviderFunc(func(context.Context, provider.ImageRequest) (*provider.ImageResult, error) {
			panic("boom: provider 内部崩溃")
		})
	}
	taskRepo := newFakeTaskRepo()
	userRepo := newFakeRepo()
	quota := service.NewQuotaService(userRepo)
	svc := service.NewImageGenerationServiceWithFactory(settings, taskRepo, quota, panicFactory)

	u := seedUser(t, userRepo, "quota-panic", "password123", usermodel.RoleUser, usermodel.StatusActive)
	total := 3
	u.QuotaTotal = &total

	assert.PanicsWithValue(t, "boom: provider 内部崩溃", func() {
		_, _ = svc.GenerateImage(context.Background(), u.ID, service.GenerateImageRequest{
			Model:  "qwen-image-plus",
			Prompt: "x",
		})
	}, "panic 必须原样重新抛出，交给 middleware.Recovery 处理，不能被这里悄悄吞掉")

	assert.Equal(t, 0, userRepo.users[u.ID].QuotaUsed, "provider panic 也必须退回额度，否则用户被扣费却什么都拿不到")
	assert.Empty(t, taskRepo.all(), "panic 展开时函数还没走到 Create，不应该落任何 task 行")
}
