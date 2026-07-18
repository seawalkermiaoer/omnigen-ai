package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// settingsWithEndpoint 是全部测试共用的凭证基线：apiKey/region 固定，只有
// endpoint 按测试需要变化——endpoint 是否以 "http" 开头决定了降级链要不要
// 尝试第二个 base（见 optimize.go Optimize 方法里 endpointVariants 的注释）。
func settingsWithEndpoint(endpoint string) map[settingmodel.Key]string {
	return map[settingmodel.Key]string{
		settingmodel.KeyDashscopeAPIKey: "sk-test-key",
		settingmodel.KeyRegion:          "cn-beijing",
		settingmodel.KeyEndpoint:        endpoint,
		settingmodel.KeyWorkspaceID:     "",
	}
}

func TestOptimize_MissingAPIKeyReturnsSettingIncompleteWithoutCallingProvider(t *testing.T) {
	settings := newFakeOptimizeSettings(map[settingmodel.Key]string{
		settingmodel.KeyRegion: "cn-beijing",
		// KeyDashscopeAPIKey 故意不设置，GetDecrypted 对未配置的 key 返回 ""。
	})
	factory := newScriptedProviderFactory(nil)
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

	_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "t2v", Draft: "hi"})

	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrSettingIncomplete)
	assert.Empty(t, factory.attempts, "apiKey 缺失时不应该调用 provider 工厂")
}

// ── mode -> 提示词选择：7 个 mode 各自选中自己的提示词 ────────────────────

func TestOptimize_EachModeSelectsItsOwnSystemPrompt(t *testing.T) {
	modes := []generation.OptimizeMode{
		generation.ModeR2V, generation.ModeT2V, generation.ModeR2VWan,
		generation.ModeI2VWan, generation.ModeI2V, generation.ModeT2I, generation.ModeImggenEdit,
	}
	for _, mode := range modes {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
			factory := newScriptedProviderFactory([]scriptedResult{{text: "optimized", model: "qwen3.7-plus"}})
			svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

			_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{
				Mode:  string(mode),
				Draft: "一段草稿",
			})
			require.NoError(t, err)
			require.Len(t, factory.capturedReqs, 1)
			assert.Equal(t, generation.SystemPrompts[mode], factory.capturedReqs[0].SystemPrompt)
		})
	}
}

// TestOptimize_ImgeditAliasNeverFallsBackToT2VPrompt 覆盖旧 bug：
// public/js/task.js 在图片编辑 tab 没有图片时会把 mode 留成 'imgedit'，
// 旧服务端因此静默用 t2v（文生视频）提示词优化一次图片编辑请求。
func TestOptimize_ImgeditAliasNeverFallsBackToT2VPrompt(t *testing.T) {
	settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
	factory := newScriptedProviderFactory([]scriptedResult{{text: "edited", model: "qwen3.7-plus"}})
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

	_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "imgedit", Draft: "把背景换成海边"})

	require.NoError(t, err)
	require.Len(t, factory.capturedReqs, 1)
	assert.Equal(t, generation.SystemPrompts[generation.ModeImggenEdit], factory.capturedReqs[0].SystemPrompt)
	assert.NotEqual(t, generation.SystemPrompts[generation.ModeT2V], factory.capturedReqs[0].SystemPrompt)
}

func TestOptimize_UnknownModeFallsBackToT2VAndLogsWarning(t *testing.T) {
	var buf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prevLogger)

	settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
	factory := newScriptedProviderFactory([]scriptedResult{{text: "ok", model: "qwen3.7-plus"}})
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

	_, mdl, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "totally-bogus-mode", Draft: "hi"})

	require.NoError(t, err)
	assert.Equal(t, "qwen3.7-plus", mdl)
	require.Len(t, factory.capturedReqs, 1)
	assert.Equal(t, generation.SystemPrompts[generation.ModeT2V], factory.capturedReqs[0].SystemPrompt)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "level=WARN", "未知 mode 的回退必须被记录下来，不能像旧代码一样静默")
	assert.Contains(t, logOutput, "totally-bogus-mode")
}

// ── 协议/模型族选择：hasImages 同时驱动 vision 模型族与素材透传 ──────────

func TestOptimize_HasImagesTrueUsesVisionModelsAndPassesImagesInOrder(t *testing.T) {
	settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
	factory := newScriptedProviderFactory([]scriptedResult{{text: "ok", model: "qwen-vl-max-latest"}})
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

	images := []string{"data:image/png;base64,AAA", "data:image/png;base64,BBB"}
	_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{
		Mode:   "r2v",
		Draft:  "草稿",
		Images: images,
	})
	require.NoError(t, err)

	require.Len(t, factory.attempts, 1)
	assert.Equal(t, "qwen-vl-max-latest", factory.attempts[0].model, "有图时应该选用 vision 主模型 VISION_OPTIMIZE")
	require.Len(t, factory.capturedReqs, 1)
	// Images 顺序必须原样透传：具体"图在前文在后"的 content 数组拼装是
	// provider 层（dashscope.Optimize -> buildImageContent）的职责，已由
	// dashscope 包自己的 TestOptimize_WithImagesUsesNativeProtocolImagesFirstThenText
	// 覆盖；这里只断言 service 层没有弄丢或打乱图片顺序。
	assert.Equal(t, images, factory.capturedReqs[0].Images)
}

func TestOptimize_HasImagesFalseUsesTextModelsAndSendsNoImages(t *testing.T) {
	settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
	factory := newScriptedProviderFactory([]scriptedResult{{text: "ok", model: "qwen3.7-plus"}})
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

	_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "t2v", Draft: "草稿"})
	require.NoError(t, err)

	require.Len(t, factory.attempts, 1)
	assert.Equal(t, "qwen3.7-plus", factory.attempts[0].model, "无图时应该选用 text 主模型 TEXT_OPTIMIZE")
	require.Len(t, factory.capturedReqs, 1)
	assert.Empty(t, factory.capturedReqs[0].Images, "无图时不应该向 provider 传任何图片")
}

// ── sourceDesc / draftText 拼装 ──────────────────────────────────────────

func TestOptimize_SourceDescUsesTuLabelForR2VWanAndImggenEdit(t *testing.T) {
	for _, mode := range []generation.OptimizeMode{generation.ModeR2VWan, generation.ModeImggenEdit} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
			factory := newScriptedProviderFactory([]scriptedResult{{text: "ok", model: "qwen-vl-max-latest"}})
			svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

			_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{
				Mode:       string(mode),
				Images:     []string{"a", "b"},
				VideoCount: 1,
				// draft 留空（无草稿分支），这样 UserText 就是
				// sourceDesc + 固定后缀，方便精确断言 sourceDesc 本身。
			})
			require.NoError(t, err)
			require.Len(t, factory.capturedReqs, 1)

			wantSourceDesc := "用户提供了 2 张参考图（对应「图1」～「图2」）、1 个参考视频（对应「视频1」～「视频1」，仅以 URL 形式存在，模型看不到画面）。"
			wantDraftText := wantSourceDesc + "\n\n用户没有提供草稿。请自由构思一段短视频的镜头描述。"
			assert.Equal(t, wantDraftText, factory.capturedReqs[0].UserText)
		})
	}
}

func TestOptimize_SourceDescUsesImageLabelForOtherModes(t *testing.T) {
	settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
	factory := newScriptedProviderFactory([]scriptedResult{{text: "ok", model: "qwen-vl-max-latest"}})
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

	_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{
		Mode:   "r2v",
		Images: []string{"a", "b", "c"},
		// VideoCount 为 0：视频文案不应该出现。
	})
	require.NoError(t, err)
	require.Len(t, factory.capturedReqs, 1)

	wantSourceDesc := "用户提供了 3 张参考图（对应 [Image 1]…[Image 3]）。"
	wantDraftText := wantSourceDesc + "\n\n用户没有提供草稿。请自由构思一段短视频的镜头描述。"
	assert.Equal(t, wantDraftText, factory.capturedReqs[0].UserText)
}

func TestOptimize_SourceDescEmptyWhenNoImagesAndNoVideos(t *testing.T) {
	settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
	factory := newScriptedProviderFactory([]scriptedResult{{text: "ok", model: "qwen3.7-plus"}})
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

	_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "t2i"})
	require.NoError(t, err)
	require.Len(t, factory.capturedReqs, 1)

	assert.Equal(t, "用户没有提供草稿。请自由构思一段 5 秒视频的镜头描述。", factory.capturedReqs[0].UserText)
}

func TestOptimize_DraftTextThreeBranches(t *testing.T) {
	t.Run("draft present overrides everything", func(t *testing.T) {
		settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
		factory := newScriptedProviderFactory([]scriptedResult{{text: "ok", model: "qwen-vl-max-latest"}})
		svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

		_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{
			Mode:   "r2v",
			Images: []string{"a"},
			Draft:  "赛博朋克风格的开场",
		})
		require.NoError(t, err)
		require.Len(t, factory.capturedReqs, 1)

		wantSourceDesc := "用户提供了 1 张参考图（对应 [Image 1]…[Image 1]）。"
		want := wantSourceDesc + "\n\n用户草稿：\n赛博朋克风格的开场\n\n请基于以上信息，输出优化后的 prompt。"
		assert.Equal(t, want, factory.capturedReqs[0].UserText)
	})

	t.Run("whitespace-only draft counts as no draft", func(t *testing.T) {
		settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
		factory := newScriptedProviderFactory([]scriptedResult{{text: "ok", model: "qwen3.7-plus"}})
		svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

		_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "t2v", Draft: "   \n\t  "})
		require.NoError(t, err)
		require.Len(t, factory.capturedReqs, 1)
		assert.Equal(t, "用户没有提供草稿。请自由构思一段 5 秒视频的镜头描述。", factory.capturedReqs[0].UserText)
	})

	t.Run("no draft but has media", func(t *testing.T) {
		settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
		factory := newScriptedProviderFactory([]scriptedResult{{text: "ok", model: "qwen3.7-plus"}})
		svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

		_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "i2v_wan", VideoCount: 1})
		require.NoError(t, err)
		require.Len(t, factory.capturedReqs, 1)

		wantSourceDesc := "用户提供了 1 个参考视频（对应「视频1」～「视频1」，仅以 URL 形式存在，模型看不到画面）。"
		want := wantSourceDesc + "\n\n用户没有提供草稿。请自由构思一段短视频的镜头描述。"
		assert.Equal(t, want, factory.capturedReqs[0].UserText)
	})

	t.Run("no draft and no media", func(t *testing.T) {
		settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
		factory := newScriptedProviderFactory([]scriptedResult{{text: "ok", model: "qwen3.7-plus"}})
		svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

		_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "t2v"})
		require.NoError(t, err)
		require.Len(t, factory.capturedReqs, 1)
		assert.Equal(t, "用户没有提供草稿。请自由构思一段 5 秒视频的镜头描述。", factory.capturedReqs[0].UserText)
	})
}

// ── 降级链：模型外层、endpoint 内层，仅 AccessDenied 触发 ─────────────────

func TestOptimize_AccessDeniedRetriesEndpointThenSucceeds(t *testing.T) {
	settings := newFakeOptimizeSettings(settingsWithEndpoint("https://custom.example.com"))
	factory := newScriptedProviderFactory([]scriptedResult{
		{err: fmt.Errorf("%w: no access on custom endpoint", provider.ErrAccessDenied)},
		{text: "optimized!", model: "qwen3.7-plus"},
	})
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

	text, model, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "t2v", Draft: "hi"})

	require.NoError(t, err)
	assert.Equal(t, "optimized!", text)
	assert.Equal(t, "qwen3.7-plus", model)
	assert.Equal(t, []optimizeAttempt{
		{model: "qwen3.7-plus", endpoint: "https://custom.example.com"},
		{model: "qwen3.7-plus", endpoint: ""},
	}, factory.attempts, "第二次尝试应该换成官方 endpoint（空字符串代表 provider 内部按 region 推导），且模型不变")
}

func TestOptimize_AccessDeniedRetriesNextModelAfterEndpointsExhausted(t *testing.T) {
	settings := newFakeOptimizeSettings(settingsWithEndpoint("https://custom.example.com"))
	factory := newScriptedProviderFactory([]scriptedResult{
		{err: fmt.Errorf("%w: attempt 1", provider.ErrAccessDenied)},
		{err: fmt.Errorf("%w: attempt 2", provider.ErrAccessDenied)},
		{text: "optimized via fallback model", model: "qwen-plus"},
	})
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

	text, model, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "t2v", Draft: "hi"})

	require.NoError(t, err)
	assert.Equal(t, "optimized via fallback model", text)
	assert.Equal(t, "qwen-plus", model)
	assert.Equal(t, []optimizeAttempt{
		{model: "qwen3.7-plus", endpoint: "https://custom.example.com"},
		{model: "qwen3.7-plus", endpoint: ""},
		{model: "qwen-plus", endpoint: "https://custom.example.com"},
	}, factory.attempts)
}

func TestOptimize_AllFourAttemptsFailReturnsLastError(t *testing.T) {
	settings := newFakeOptimizeSettings(settingsWithEndpoint("https://custom.example.com"))
	factory := newScriptedProviderFactory([]scriptedResult{
		{err: fmt.Errorf("%w: attempt 1", provider.ErrAccessDenied)},
		{err: fmt.Errorf("%w: attempt 2", provider.ErrAccessDenied)},
		{err: fmt.Errorf("%w: attempt 3", provider.ErrAccessDenied)},
		{err: fmt.Errorf("%w: attempt 4 final", provider.ErrAccessDenied)},
	})
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

	_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "t2v", Draft: "hi"})

	require.Error(t, err)
	assert.ErrorIs(t, err, provider.ErrAccessDenied)
	assert.Contains(t, err.Error(), "attempt 4 final", "耗尽全部尝试后应该返回最后一次的错误，而不是第一次的")
	assert.Equal(t, []optimizeAttempt{
		{model: "qwen3.7-plus", endpoint: "https://custom.example.com"},
		{model: "qwen3.7-plus", endpoint: ""},
		{model: "qwen-plus", endpoint: "https://custom.example.com"},
		{model: "qwen-plus", endpoint: ""},
	}, factory.attempts, "最多 2 模型 x 2 endpoint = 4 次尝试，不多不少")
}

func TestOptimize_NoEndpointOverrideOnlyTriesOneBasePerModel(t *testing.T) {
	// endpoint 为空（没有覆盖）时 bases 只有一个变体
	// （server.js `base === officialBase` 命中，bases=[base]），
	// 降级链退化成"只有模型维度"，2 个模型 x 1 个 base = 最多 2 次尝试。
	settings := newFakeOptimizeSettings(settingsWithEndpoint(""))
	factory := newScriptedProviderFactory([]scriptedResult{
		{err: fmt.Errorf("%w: attempt 1", provider.ErrAccessDenied)},
		{err: fmt.Errorf("%w: attempt 2 final", provider.ErrAccessDenied)},
	})
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

	_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "t2v", Draft: "hi"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "attempt 2 final")
	assert.Equal(t, []optimizeAttempt{
		{model: "qwen3.7-plus", endpoint: ""},
		{model: "qwen-plus", endpoint: ""},
	}, factory.attempts)
}

func TestOptimize_NonAccessDeniedErrorReturnsImmediatelyWithoutRetry(t *testing.T) {
	settings := newFakeOptimizeSettings(settingsWithEndpoint("https://custom.example.com"))
	boom := errors.New("upstream 500: internal server error")
	// script 只准备 1 条：如果 service 还去重试，scriptedProviderFactory
	// 会在第二次调用时 panic，测试直接失败——用这种方式确保"不重试"是被
	// 结构性保证的，不是只靠一次断言。
	factory := newScriptedProviderFactory([]scriptedResult{{err: boom}})
	svc := service.NewOptimizeServiceWithFactory(settings, factory.Factory())

	_, _, err := svc.Optimize(context.Background(), service.OptimizeRequest{Mode: "t2v", Draft: "hi"})

	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.False(t, errors.Is(err, provider.ErrAccessDenied))
	assert.Equal(t, []optimizeAttempt{
		{model: "qwen3.7-plus", endpoint: "https://custom.example.com"},
	}, factory.attempts, "非 AccessDenied 错误必须立即返回，不能触发降级重试")
}
