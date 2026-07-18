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
		settingmodel.KeyDashscopeAPIKey: fakeDashscopeKey,
		settingmodel.KeyT8starAPIKey:    fakeT8starKey,
		settingmodel.KeyRegion:          "cn-beijing",
		settingmodel.KeyEndpoint:        "",
		settingmodel.KeyWorkspaceID:     "",
	}
	for k, v := range overrides {
		values[k] = v
	}
	return newFakeOptimizeSettings(values)
}

func newImageService(t *testing.T, settings *fakeOptimizeSettings, factory *recordingImageFactory) (*service.ImageGenerationService, *fakeTaskRepo) {
	t.Helper()
	repo := newFakeTaskRepo()
	svc := service.NewImageGenerationServiceWithFactory(settings, repo, factory.Factory())
	return svc, repo
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
