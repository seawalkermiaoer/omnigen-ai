// Package catalog 是模型目录的单一数据源。
//
// 旧系统里同一份模型列表散落在三处（server.js 的 MODELS 常量、
// public/js/imggen.js、public/js/imgedit.js），且视频模型 id 完全不在任何
// "目录" 里——它们只以 <option value> 与 JS 字符串字面量的形式出现在
// public/index.html、public/js/{r2v,i2v}.js 中，服务端从不校验。这导致图片
// 模型有校验、视频模型没有校验的不对称 bug（server.js:333-336 只检查
// MODELS.IMAGE / MODELS.IMAGE_OPENAI，POST /api/create-task 从不检查 model）。
//
// 本包把全部信息收敛为一份结构化目录，供 API 校验层与前端下拉框共用。
// 导入时统一使用 catalogmodel 别名，避免与 service/catalog、repository 等包名冲突。
package catalog

// Capability 描述一个模型能做什么。
type Capability string

const (
	CapabilityT2I            Capability = "t2i"
	CapabilityEdit           Capability = "edit"
	CapabilityT2V            Capability = "t2v"
	CapabilityI2V            Capability = "i2v"
	CapabilityR2V            Capability = "r2v"
	CapabilityOptimizeText   Capability = "optimize_text"
	CapabilityOptimizeVision Capability = "optimize_vision"
)

// Protocol 描述调用该模型所走的上游协议 / 凭证体系。
// dashscope：阿里云百炼原生协议（含 compatible-mode），使用 apiKey/region/
// endpoint/workspaceId 那一套凭证；openai：t8star 中转站的 OpenAI
// chat-completions 协议，使用独立的 endpoint/apiKey。
type Protocol string

const (
	ProtocolDashScope Protocol = "dashscope"
	ProtocolOpenAI    Protocol = "openai"
)

// 可选参数名，与旧系统里判空逻辑（server.js:391-398）里出现的字段一一对应。
// size / negative_prompt 用真值判断，其余用 != null，因此 Supports 只表达
// "这个模型认不认这个参数"，不表达判空方式——判空方式是 provider 层的事。
const (
	ParamThinkingMode     = "thinking_mode"
	ParamEnableSequential = "enable_sequential"
	ParamNegativePrompt   = "negative_prompt"
	ParamPromptExtend     = "prompt_extend"
	ParamSeed             = "seed"
	ParamWatermark        = "watermark"
)

// Model 描述目录里的一个模型及其参数约束。
type Model struct {
	ID           string
	Capabilities []Capability
	Protocol     Protocol

	// Sizes 是可选的固定尺寸集合：qwen 系列为固定像素字符串
	// （如 "1328*1328"），wan2.7 系列为 "1K"/"2K"/"4K"；视频模型与
	// gpt-image-2（忽略 size 参数）为空。
	Sizes []string

	// MaxN 是一次生成的最大图片数（count 下拉框的上限），最小恒为 1。
	MaxN int
	// SequentialMaxN 是开启 enable_sequential 时的最大图片数；
	// 不支持顺序生成的模型为 0。
	SequentialMaxN int

	// Supports 列出该模型认可的可选参数名（见上面的 Param* 常量）。
	Supports []string

	// Regions 是允许调用该模型的 region 白名单；为空表示不限。
	Regions []string

	// MaxImages 是该模型作为图片编辑/参考输入时允许上传的最大图片数；
	// 纯文生图/文生视频模型（不接受图片输入）为 0。
	// wan2.7-r2v 例外：旧系统里它的图片上限是动态的
	// （5 - 已有参考视频数，见 r2v.js:11），这里记录的是图+视频合计上限
	// （5），由调用方结合当前视频数再做一次动态校验。
	MaxImages int

	// MinImageEdge 是 i2v 模型对上传图片宽、高的最小像素要求
	// （宽和高都要达到，不是取短边——见 i2v.js:145 `w < minSize || h < minSize`）。
	// 非 i2v 模型为 0。
	MinImageEdge int
	// RatioMin / RatioMax 是 i2v 模型允许的宽高比区间（宽/高）。非 i2v 模型为 0。
	RatioMin float64
	RatioMax float64
}

// SupportsParam 报告该模型是否认可名为 name 的可选参数。
func (m Model) SupportsParam(name string) bool {
	for _, s := range m.Supports {
		if s == name {
			return true
		}
	}
	return false
}

// AllowsRegion 报告该模型是否可以在给定 region 下调用。
// Regions 为空表示不限制，任何 region 都返回 true。
func (m Model) AllowsRegion(region string) bool {
	if len(m.Regions) == 0 {
		return true
	}
	for _, r := range m.Regions {
		if r == region {
			return true
		}
	}
	return false
}

// HasCapability 报告该模型是否具备给定能力。
func (m Model) HasCapability(c Capability) bool {
	for _, cap := range m.Capabilities {
		if cap == c {
			return true
		}
	}
	return false
}

// wanRegions 是 wan2.7 系列的 region 白名单——见 imggen.js:134、imgedit.js:199、
// r2v.js:146、i2v.js:218 里完全相同的 `['cn-beijing', 'ap-southeast-1']` 判断。
var wanRegions = []string{"cn-beijing", "ap-southeast-1"}

// qwenImageSizes 是 qwen 系列图片模型（生成与编辑共用）的固定尺寸集合，
// 顺序与 imggen.js:getQwenSizes / imgedit.js:getQwenEditSizes 一致。
var qwenImageSizes = []string{"1664*928", "1472*1140", "1328*1328", "1140*1472", "928*1664"}

// wanImageSizes 是 wan2.7 系列图片模型的尺寸集合（getWanSizes / getWanEditSizes）。
var wanImageSizes = []string{"1K", "2K", "4K"}

// qwenParams / wanImageParams 是两族图片模型分别支持的可选参数集合。
// thinking_mode / enable_sequential 只在 wan 图片模型上出现
// （imggen.js:148-151、imgedit.js:214-217：`if (isWanModel(model)) {...}`）。
var qwenImageParams = []string{ParamNegativePrompt, ParamSeed, ParamWatermark}
var wanImageParams = []string{ParamThinkingMode, ParamEnableSequential, ParamNegativePrompt, ParamSeed, ParamWatermark}

// happyhorseVideoParams / wanVideoParams 是视频模型支持的可选参数集合。
// seed / watermark 对全部视频模型都生效（task.js:collectParams 无条件收集）；
// negative_prompt / prompt_extend 只在 wan 视频模型上出现
// （r2v.js:176-178、i2v.js:240-244：`if (wan) {...}`）。
var happyhorseVideoParams = []string{ParamSeed, ParamWatermark}
var wanVideoParams = []string{ParamNegativePrompt, ParamPromptExtend, ParamSeed, ParamWatermark}

// catalog 是目录的唯一数据源。All / ByID / ByCapability 均从这里派生。
var catalog = []Model{
	// ── 图片生成/编辑模型：dashscope 原生协议 ──────────────────────
	{
		ID:           "qwen-image-plus",
		Capabilities: []Capability{CapabilityT2I},
		Protocol:     ProtocolDashScope,
		Sizes:        qwenImageSizes,
		MaxN:         1,
		Supports:     qwenImageParams,
	},
	{
		ID:           "qwen-image",
		Capabilities: []Capability{CapabilityT2I},
		Protocol:     ProtocolDashScope,
		Sizes:        qwenImageSizes,
		MaxN:         1,
		Supports:     qwenImageParams,
	},
	{
		ID:           "qwen-image-edit-plus",
		Capabilities: []Capability{CapabilityEdit},
		Protocol:     ProtocolDashScope,
		Sizes:        qwenImageSizes,
		MaxN:         6,
		Supports:     qwenImageParams,
		MaxImages:    3,
	},
	{
		ID:           "qwen-image-edit",
		Capabilities: []Capability{CapabilityEdit},
		Protocol:     ProtocolDashScope,
		Sizes:        qwenImageSizes,
		MaxN:         1,
		Supports:     qwenImageParams,
		MaxImages:    3,
	},
	{
		// wan2.7-image-pro 同时出现在 imggen.js（文生图）与 imgedit.js（图片
		// 编辑）的模型下拉框里，因此同时具备 t2i 与 edit 两种能力；
		// MaxImages 只在 edit 场景下有意义。
		ID:             "wan2.7-image-pro",
		Capabilities:   []Capability{CapabilityT2I, CapabilityEdit},
		Protocol:       ProtocolDashScope,
		Sizes:          wanImageSizes,
		MaxN:           4,
		SequentialMaxN: 12,
		Supports:       wanImageParams,
		Regions:        wanRegions,
		MaxImages:      9,
	},
	{
		ID:             "wan2.7-image",
		Capabilities:   []Capability{CapabilityT2I, CapabilityEdit},
		Protocol:       ProtocolDashScope,
		Sizes:          wanImageSizes,
		MaxN:           4,
		SequentialMaxN: 12,
		Supports:       wanImageParams,
		Regions:        wanRegions,
		MaxImages:      9,
	},

	// ── 图片模型：t8star / OpenAI chat-completions 协议 ─────────────
	{
		// gpt-image-2 是聊天补全 API：完全不接受 size/n/seed/watermark/
		// negative_prompt（imggen.js:68-75、imgedit.js:70-77、
		// lib/providers/t8star.js buildPayload 只拼 model/prompt/images）。
		// MaxN=1 反映 UI 从不为它渲染 count 选择器、也不发送 n 的事实，
		// 不是上游的硬性约定——t8star 的回复里可能出现任意张图。
		ID:           "gpt-image-2",
		Capabilities: []Capability{CapabilityT2I, CapabilityEdit},
		Protocol:     ProtocolOpenAI,
		MaxN:         1,
		MaxImages:    3,
	},

	// ── 视频模型：只出现在 index.html 的 <option value> 与
	// public/js/{r2v,i2v}.js 的字符串字面量里，旧 server.js 从不校验 ──
	{
		ID:           "happyhorse-1.1-r2v",
		Capabilities: []Capability{CapabilityR2V},
		Protocol:     ProtocolDashScope,
		Supports:     happyhorseVideoParams,
		MaxImages:    9,
	},
	{
		// wan2.7-r2v 的图片上限在旧系统里是动态的：5 - 当前参考视频数
		// （r2v.js:11 `maxImagesForR2v`），MaxImages 记录的是图+视频合计
		// 上限；参数校验时还要再减去已有的参考视频数。
		ID:           "wan2.7-r2v",
		Capabilities: []Capability{CapabilityR2V},
		Protocol:     ProtocolDashScope,
		Supports:     wanVideoParams,
		Regions:      wanRegions,
		MaxImages:    5,
	},
	{
		ID:           "happyhorse-1.1-i2v",
		Capabilities: []Capability{CapabilityI2V},
		Protocol:     ProtocolDashScope,
		Supports:     happyhorseVideoParams,
		MaxImages:    1,
		MinImageEdge: 300,
		RatioMin:     0.4,
		RatioMax:     2.5,
	},
	{
		// wan2.7-i2v 的图片上限是 2（first_frame + last_frame）；first_clip
		// 是既有视频的 URL，不是上传图片，不计入 MaxImages。
		ID:           "wan2.7-i2v-2026-04-25",
		Capabilities: []Capability{CapabilityI2V},
		Protocol:     ProtocolDashScope,
		Supports:     wanVideoParams,
		Regions:      wanRegions,
		MaxImages:    2,
		MinImageEdge: 240,
		RatioMin:     0.125,
		RatioMax:     8,
	},
	{
		ID:           "happyhorse-1.1-t2v",
		Capabilities: []Capability{CapabilityT2V},
		Protocol:     ProtocolDashScope,
		Supports:     happyhorseVideoParams,
	},

	// ── prompt 优化模型（含降级链） ──────────────────────────────
	{
		ID:           "qwen3.7-plus",
		Capabilities: []Capability{CapabilityOptimizeText},
		Protocol:     ProtocolDashScope,
	},
	{
		// TEXT_OPTIMIZE_FALLBACKS：主模型不可用账号的降级目标。
		ID:           "qwen-plus",
		Capabilities: []Capability{CapabilityOptimizeText},
		Protocol:     ProtocolDashScope,
	},
	{
		ID:           "qwen-vl-max-latest",
		Capabilities: []Capability{CapabilityOptimizeVision},
		Protocol:     ProtocolDashScope,
	},
	{
		// VISION_OPTIMIZE_FALLBACKS。
		ID:           "qwen-vl-plus",
		Capabilities: []Capability{CapabilityOptimizeVision},
		Protocol:     ProtocolDashScope,
	},
}

// All 返回目录里的全部模型（副本，调用方修改不影响目录本身）。
func All() []Model {
	out := make([]Model, len(catalog))
	copy(out, catalog)
	return out
}

// ByID 按模型 id 查找，返回零值与 false 表示未找到。
func ByID(id string) (Model, bool) {
	for _, m := range catalog {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// ByCapability 返回具备给定能力的全部模型。
func ByCapability(c Capability) []Model {
	var out []Model
	for _, m := range catalog {
		if m.HasCapability(c) {
			out = append(out, m)
		}
	}
	return out
}
