package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/chenhao/omnigen-ai/server/internal/model/catalog"
	"github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/dashscope"
)

// OptimizeRequest 是调用方（handler 层，Task 8 之后的任务负责校验/解析
// HTTP body）传给 OptimizeService.Optimize 的入参，字段对应
// server.js:555 `req.body` 里除凭证之外的部分——旧系统里 apiKey/region/
// workspaceId/endpoint 由客户端每次请求携带，新架构改成服务端配置
// （见 SettingReader），不再信任客户端传来的凭证/endpoint，这与
// dashscope.Client 文档里"endpoint 只能来自服务端 app_settings"的说明
// 一致。
//
// 这个类型本该属于 internal/model/generation（design 里 Task 10 会给
// generation 包补 types/request/response.go），但那部分由并行任务负责、
// 此刻尚未落地。为了不抢占对方可能选择的字段/命名，这里先在 service 包内部
// 定义一个够用的最小形状；等对方任务的类型就位后可以按需对齐或合并，
// 不影响这里的行为契约。
type OptimizeRequest struct {
	// Mode 是客户端原始传入的 mode 字符串，未经过任何校验或归一化——
	// generation.ResolveMode 负责把它解析成 7 个合法 key 之一（含
	// "imgedit" -> imggen_edit 的别名修复，以及未知 mode 回退 t2v）。
	Mode string
	// Draft 是用户输入框里的草稿文本，可能为空、也可能是纯空白。
	Draft string
	// Images 是参考图（data URI 或 URL），顺序即用户上传顺序，透传给
	// provider 时不会被重新排序。
	Images []string
	// VideoCount 是参考视频数量（r2v_wan / i2v_wan 场景），只用于拼装
	// sourceDesc 的文案，不影响协议选择——协议选择只看 len(Images)>0。
	VideoCount int
}

// SettingReader 是 OptimizeService 依赖的最小配置读取接口。生产环境用
// *SettingService 满足（方法签名与 SettingService.GetDecrypted 完全一致）；
// 测试注入一个返回固定值的假实现，不需要真正的 repository/加密 key。
type SettingReader interface {
	GetDecrypted(ctx context.Context, key settingmodel.Key) (string, error)
}

// ProviderFactory 用一组凭证 + endpoint 覆盖值构造一个
// provider.OptimizeProvider 实例。
//
// 之所以需要一个工厂函数、而不是让 OptimizeService 直接持有单个 provider
// 实例，是因为降级链的"endpoint 内层"（见 Optimize 方法注释）需要用同一份
// apiKey/region/workspaceID、但两个不同的 endpoint 覆盖值分别构造出
// "配置的 endpoint" 与 "官方 endpoint" 两个 provider 实例——传入原始
// endpoint 配置值得到前者，传入空字符串强制 provider 内部按 region 推导、
// 忽略 endpoint 覆盖，就得到后者（与 dashscope.Client.baseURL() 对空
// endpoint 的处理完全对应，见该方法注释）。测试注入的假工厂用它来记录
// "这一次调用发生在哪个 endpoint 变体上"。
type ProviderFactory func(apiKey, region, workspaceID, endpoint string) provider.OptimizeProvider

// NewDashScopeOptimizeProviderFactory 是生产环境使用的默认工厂，绑定到
// 真正的 dashscope.Client。
func NewDashScopeOptimizeProviderFactory() ProviderFactory {
	return func(apiKey, region, workspaceID, endpoint string) provider.OptimizeProvider {
		return dashscope.New(apiKey, region, workspaceID, endpoint)
	}
}

// OptimizeService 承载 /api/optimize-prompt 的业务规则：7 套系统提示词的
// mode 选择、sourceDesc/draftText 的拼装、以及"模型外层、endpoint 内层，
// 仅 AccessDenied 触发"的降级重试链（server.js:561-644 的移植目标）。
type OptimizeService struct {
	settings SettingReader
	factory  ProviderFactory
}

// NewOptimizeService 构造生产用的 OptimizeService，provider 走真正的
// dashscope.Client。
func NewOptimizeService(settings SettingReader) *OptimizeService {
	return NewOptimizeServiceWithFactory(settings, NewDashScopeOptimizeProviderFactory())
}

// NewOptimizeServiceWithFactory 允许调用方（测试环境）注入假的
// ProviderFactory，不必打真实网络请求。
func NewOptimizeServiceWithFactory(settings SettingReader, factory ProviderFactory) *OptimizeService {
	return &OptimizeService{settings: settings, factory: factory}
}

// Optimize 对应 server.js `/api/optimize-prompt` 处理函数
// （server.js:552-648）的移植：解析 mode 选出系统提示词、拼装
// sourceDesc/draftText、按"模型外层、endpoint 内层"的顺序重试，仅
// AccessDenied 触发下一次尝试，返回实际成功的文本与模型 id（供前端展示）。
//
// 与旧代码的一个刻意差异：旧代码 `SYSTEM_PROMPTS[mode] || SYSTEM_PROMPTS.t2v`
// 对未知 mode 的回退是完全静默的；这里在 fellBack 时打一条 warn 日志，
// 方便运维观察到"客户端传了一个我们不认识的 mode"这种情况，而不是像旧系统
// 一样悄无声息地把请求当成 t2v 处理掉。
func (s *OptimizeService) Optimize(ctx context.Context, req OptimizeRequest) (text string, actualModel string, err error) {
	apiKey, err := s.settings.GetDecrypted(ctx, settingmodel.KeyDashscopeAPIKey)
	if err != nil {
		return "", "", err
	}
	if apiKey == "" {
		return "", "", apperr.ErrSettingIncomplete.Wrap(errors.New("dashscope_api_key 尚未配置"))
	}
	region, err := s.settings.GetDecrypted(ctx, settingmodel.KeyRegion)
	if err != nil {
		return "", "", err
	}
	// optimize 服务始终只对接 DashScope（文本/视觉两族优化模型都是 DashScope
	// 模型），所以只读 dashscope_endpoint。
	endpoint, err := s.settings.GetDecrypted(ctx, settingmodel.KeyDashscopeEndpoint)
	if err != nil {
		return "", "", err
	}
	workspaceID, err := s.settings.GetDecrypted(ctx, settingmodel.KeyWorkspaceID)
	if err != nil {
		return "", "", err
	}

	hasImages := len(req.Images) > 0
	// videoCount 直接用 req.VideoCount：老代码的 `Number.isInteger(videoCount)
	// ? videoCount : 0` 只是在防"类型不对/未传"这两种 JS 特有的输入形态，
	// Go 的 int 字段天然不存在这个问题（未传时零值就是 0）。后续逻辑全部
	// 只关心 videoCount > 0，负数与 0 效果一致，不需要额外 clamp。
	videoCount := req.VideoCount

	prompt, mode, fellBack := generation.PromptForMode(req.Mode)
	if fellBack {
		slog.Warn("optimize-prompt: 未知 mode，已回退到 t2v", "rawMode", req.Mode)
	}

	sourceDesc := buildSourceDesc(mode, len(req.Images), videoCount)
	draftText := buildDraftText(req.Draft, sourceDesc, hasImages, videoCount)

	models := optimizeModelIDs(hasImages)

	// bases 表达的是 server.js:580-581：
	//
	//	const officialBase = getEndpoint(region, workspaceId);
	//	const bases = base === officialBase ? [base] : [base, officialBase];
	//
	// base===officialBase 当且仅当 endpoint 没有显式覆盖（resolveEndpoint
	// 在 endpoint 不是 "http" 开头时，会无视 endpoint 原样按 region 推导，
	// 与 officialBase 走的是同一条计算路径，结果必然相等）。因此不需要在
	// service 层重新算出两个具体的 base URL 字符串再比较，只需要判断
	// "endpoint 是不是一个显式的 http(s) 覆盖"这一个布尔条件：
	//
	//   - 不是覆盖：bases 只有一个变体，直接把 endpoint 原样传给 provider
	//     （provider 内部会发现它不是 http 开头，自己按 region 推导，效果
	//     与 officialBase 完全一致）。
	//   - 是覆盖：bases 有两个变体——先传 endpoint 本身（provider 直接用它
	//     作 base），失败且是 AccessDenied 时再传空字符串（provider 视为
	//     "没有覆盖"，强制按 region 推导，等价于 officialBase）。
	endpointVariants := []string{endpoint}
	if strings.HasPrefix(endpoint, "http") {
		endpointVariants = append(endpointVariants, "")
	}

	var lastErr error
	for modelIndex, model := range models {
		for baseIndex, ep := range endpointVariants {
			p := s.factory(apiKey, region, workspaceID, ep)
			text, actualModel, callErr := p.Optimize(ctx, provider.OptimizeRequest{
				Model:        model,
				SystemPrompt: prompt,
				UserText:     draftText,
				Images:       req.Images,
			})
			if callErr == nil {
				return text, actualModel, nil
			}
			lastErr = callErr

			canRetry := baseIndex < len(endpointVariants)-1 || modelIndex < len(models)-1
			if errors.Is(callErr, provider.ErrAccessDenied) && canRetry {
				continue
			}
			// 上游状态归一化：同 generation_image.go，见 upstream_error.go
			// 顶部注释。特意放在重试判断之后——AccessDenied 触发的重试要看
			// 的是原始 callErr（provider.ErrAccessDenied 哨兵），归一化后的
			// 错误依然能被 errors.Is 命中同一个哨兵（Wrap 保留了 Unwrap
			// 链），但没必要在还可能重试的中间状态就替换它。
			return "", "", normalizeUpstreamError(callErr)
		}
	}
	// 理论上 models/endpointVariants 都非空（models 来自 catalog 固定登记
	// 的两个能力各自的至少一个模型；endpointVariants 至少含一个元素），
	// 循环体内每条路径要么 return 成功、要么在最后一次尝试时直接
	// return callErr，不会真的执行到这里；保留这一行只是让函数在类型上
	// 总能返回一个非 nil error，避免调用方需要处理"什么都没返回"的情形。
	return "", "", normalizeUpstreamError(lastErr)
}

// optimizeModelIDs 按 hasImages 从目录里取出对应能力的模型 id 列表，顺序
// 就是目录登记顺序（主模型在前，降级目标在后）——对应 server.js:582-584
// 的 `[MODELS.VISION_OPTIMIZE, ...MODELS.VISION_OPTIMIZE_FALLBACKS]` /
// `[MODELS.TEXT_OPTIMIZE, ...MODELS.TEXT_OPTIMIZE_FALLBACKS]`。
func optimizeModelIDs(hasImages bool) []string {
	capability := catalog.CapabilityOptimizeText
	if hasImages {
		capability = catalog.CapabilityOptimizeVision
	}
	models := catalog.ByCapability(capability)
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	return ids
}

// buildSourceDesc 照抄 server.js:563-571 的 sourceDesc IIFE。
func buildSourceDesc(mode generation.OptimizeMode, imageCount, videoCount int) string {
	var parts []string
	if imageCount > 0 {
		useTuLabel := mode == generation.ModeR2VWan || mode == generation.ModeImggenEdit
		var label string
		if useTuLabel {
			label = fmt.Sprintf("对应「图1」～「图%d」", imageCount)
		} else {
			label = fmt.Sprintf("对应 [Image 1]…[Image %d]", imageCount)
		}
		parts = append(parts, fmt.Sprintf("%d 张参考图（%s）", imageCount, label))
	}
	if videoCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 个参考视频（对应「视频1」～「视频%d」，仅以 URL 形式存在，模型看不到画面）", videoCount, videoCount))
	}
	if len(parts) == 0 {
		return ""
	}
	return "用户提供了 " + strings.Join(parts, "、") + "。"
}

// buildDraftText 照抄 server.js:573-577 的三分支拼装，字面量与换行都逐字
// 保留。注意判断"有没有草稿"用的是 trim 之后是否非空（对应 JS 的
// `draft && draft.trim()`），但拼进模板的仍是未 trim 的原始 draft
// （JS 模板字符串里写的是 `${draft}` 而不是 `${draft.trim()}`）。
func buildDraftText(draft, sourceDesc string, hasImages bool, videoCount int) string {
	if strings.TrimSpace(draft) != "" {
		return fmt.Sprintf("%s\n\n用户草稿：\n%s\n\n请基于以上信息，输出优化后的 prompt。", sourceDesc, draft)
	}
	if hasImages || videoCount > 0 {
		return fmt.Sprintf("%s\n\n用户没有提供草稿。请自由构思一段短视频的镜头描述。", sourceDesc)
	}
	return "用户没有提供草稿。请自由构思一段 5 秒视频的镜头描述。"
}
