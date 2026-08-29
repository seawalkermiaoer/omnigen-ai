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
	CapabilityT2I  Capability = "t2i"
	CapabilityEdit Capability = "edit"
	CapabilityT2V  Capability = "t2v"
	CapabilityI2V  Capability = "i2v"
	CapabilityR2V  Capability = "r2v"
	// CapabilityF2V / CapabilityL2V 是 wan3.0 才有的两种入口：把一份文档
	// （docx/pdf/pptx…）或一个网页链接直接变成视频。它们在上游只是
	// input.media 里 type=file / type=link 的一条记录，但对使用者是两种
	// 完全不同的输入形态（传文件 vs 贴网址），所以在目录里作为独立能力
	// 存在，而不是塞进 r2v 的参考素材里——前端据此各自渲染一个页面。
	CapabilityF2V            Capability = "f2v"
	CapabilityL2V            Capability = "l2v"
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
	// ParamAudio 是 wan3.0 独有的"生成的视频里带不带声音"开关
	// （parameters.audio，默认 true）。wan2.7 / happyhorse 的接口没有这个
	// 字段，所以它和其它可选参数一样由 Supports 逐模型放行。
	ParamAudio = "audio"
)

// VideoProfile 是视频模型的"代际"，决定 media 数组怎么拼、哪些任务类型
// 存在。它是目录里的显式事实，不是从别的字段推出来的。
//
// 在 wan3.0 之前，service 层用 `len(model.Regions) > 0` 当作"这是不是 wan
// 模型"的判据——因为当时恰好只有 wan2.7 系列带地域白名单。wan3.0 全地域
// 可用（Regions 为空）却又是 wan 系列，这个巧合就此失效，继续沿用会把
// wan3.0 误判成 happyhorse。与其换一个新的巧合，不如把这件事写明白。
type VideoProfile string

const (
	// VideoProfileLegacy 是 happyhorse-1.1-* 系列：单一首帧、无参考视频、
	// 无 reference_voice。
	VideoProfileLegacy VideoProfile = ""
	// VideoProfileWan27 是 wan2.7 系列：i2v 有首帧/首尾帧/续写三种任务
	// 类型，r2v 的参考图与参考视频合计计数，两者都可带 reference_voice。
	VideoProfileWan27 VideoProfile = "wan2.7"
	// VideoProfileWan30 是 wan3.0 系列：一个 model id 同时具备 t2v/i2v/
	// r2v/f2v/l2v，媒体按 type 分别计数，没有 reference_voice、没有
	// first_clip（续写改为"参考视频 + 延长意图 prompt"，走 r2v 那条路）。
	VideoProfileWan30 VideoProfile = "wan3.0"
)

// MediaLimits 是按 media type 分别计数的上限。wan3.0 起上游对参考图、
// 参考视频、参考音频各自设限（10 / 5 / 5），不再是 wan2.7 那种"图 + 视频
// 合计不超过 5"的单一数字，Model.MaxImages 表达不了这种形状。
//
// 零值表示"该模型不按类型分别限制"，此时沿用 Model.MaxImages 的合计语义。
type MediaLimits struct {
	ReferenceImages int
	ReferenceVideos int
	ReferenceAudios int
}

// IsZero 报告这组上限是否未配置（即该模型走 MaxImages 的合计语义）。
func (l MediaLimits) IsZero() bool {
	return l.ReferenceImages == 0 && l.ReferenceVideos == 0 && l.ReferenceAudios == 0
}

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

	// ── 以下字段只对视频模型有意义 ──────────────────────────────
	//
	// resolution / duration / ratio 的取值范围原本是 service 与前端各写
	// 一份的全局常量，理由是"所有视频模型取值都一样"。wan3.0 打破了这个
	// 前提（多了 480P、时长 2~30 而不是 3~15、ratio 集合也不同），所以它
	// 们跟 Sizes/MaxN 一样成为目录字段，由后端目录单向驱动前端控件。

	// VideoProfile 决定 media 数组的拼装规则，见 VideoProfile 的文档。
	VideoProfile VideoProfile

	// Resolutions 是允许的 resolution 取值，DefaultResolution 是调用方
	// 没给时用的默认值（必须出现在 Resolutions 里）。
	Resolutions       []string
	DefaultResolution string

	// DurationMin/DurationMax 是 duration（秒）的闭区间，DefaultDuration
	// 是调用方没给时用的默认值。
	DurationMin     int
	DurationMax     int
	DefaultDuration int
	// SmartDuration 表示该模型接受 duration=-1（智能时长：由模型按
	// prompt 和素材自行决定长度）。为 false 时 -1 会被当作越界值拒绝。
	SmartDuration bool

	// Ratios 是允许的 ratio 取值，DefaultRatio 是默认值。
	Ratios       []string
	DefaultRatio string
	// I2VAutoRatio 表示该模型在 i2v 模式下不接受 ratio——宽高比完全由
	// 首帧决定，携带 ratio 的请求会被拒绝而不是被静默丢弃。
	// wan2.7 / happyhorse 为 true；wan3.0 的 ratio 在所有模式下都有效
	// （默认 adaptive，本身就是"跟随输入"的意思），为 false。
	I2VAutoRatio bool

	// MediaLimits 是按 media type 分别计数的上限，见 MediaLimits 的文档。
	MediaLimits MediaLimits
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

// videoCapabilities 是全部视频类能力，顺序即前端左侧导航里五个视频页面
// 的顺序。VideoCapabilities 依赖这个顺序来给出稳定输出。
var videoCapabilities = []Capability{
	CapabilityT2V, CapabilityI2V, CapabilityR2V, CapabilityF2V, CapabilityL2V,
}

// VideoCapabilities 返回该模型具备的视频类能力，顺序稳定（与
// videoCapabilities 一致，而不是与 m.Capabilities 的书写顺序一致）。
//
// 调用方用长度判断"这个模型的模式是否唯一"：wan3.0 之前每个视频模型
// 恰好只有一种视频能力，模式可以从模型 id 推出来；wan3.0 有五种，必须
// 由调用方显式指定模式。
func (m Model) VideoCapabilities() []Capability {
	var out []Capability
	for _, c := range videoCapabilities {
		if m.HasCapability(c) {
			out = append(out, c)
		}
	}
	return out
}

// AllowsResolution / AllowsRatio 报告取值是否在该模型的允许集合内。
// 集合为空表示该模型不接受这个参数（而不是"不限制"）——目录里每个视频
// 模型都显式列出了自己的取值，空集合只会出现在图片模型上。
func (m Model) AllowsResolution(v string) bool { return containsString(m.Resolutions, v) }
func (m Model) AllowsRatio(v string) bool      { return containsString(m.Ratios, v) }

// AllowsDuration 报告 duration（秒）是否可用于该模型：要么落在
// [DurationMin, DurationMax] 闭区间内，要么是该模型支持的智能时长 -1。
func (m Model) AllowsDuration(v int) bool {
	if v == SmartDurationValue {
		return m.SmartDuration
	}
	return v >= m.DurationMin && v <= m.DurationMax
}

// SmartDurationValue 是"智能时长"的哨兵值：上游用 duration=-1 表示由模型
// 依据 prompt 与素材自行决定视频长度。只有 Model.SmartDuration 为 true 的
// 模型接受它。
const SmartDurationValue = -1

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
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

// wan30VideoParams 少了 negative_prompt、多了 audio：wan3.0 的接口里
// input 只有 prompt 与 media 两个字段，没有 negative_prompt 的位置
// （见万相3.0视频生成API参考的请求体说明），而 parameters 多了一个
// audio 开关。这是 Supports 第一次真正用来"减"而不只是"加"参数——
// 一个带 negative_prompt 的 wan3.0 请求会被 service 层当场拒绝，不会
// 被悄悄丢掉。
var wan30VideoParams = []string{ParamPromptExtend, ParamSeed, ParamWatermark, ParamAudio}

// legacyVideoResolutions / legacyVideoRatios 是 happyhorse 与 wan2.7 共用的
// 取值集合，逐字来自旧 public/index.html 的 <select>（resolution-r2v/i2v/t2v
// 与 ratio-r2v/t2v，见 index.html:91-110,240-247,329-348）。它们过去是
// service 与前端各一份的全局常量，现在落到目录里由每个模型显式引用。
var legacyVideoResolutions = []string{"720P", "1080P"}
var legacyVideoRatios = []string{"16:9", "9:16", "3:4", "4:3", "4:5", "5:4", "1:1"}

// wan30VideoResolutions / wan30VideoRatios 来自万相3.0视频生成API参考的
// parameters 表：resolution 多出 480P，ratio 少了 4:5 / 5:4、多了
// adaptive（默认值，宽高比跟随输入素材）。顺序按文档列出的顺序。
var wan30VideoResolutions = []string{"480P", "720P", "1080P"}
var wan30VideoRatios = []string{"adaptive", "16:9", "4:3", "1:1", "3:4", "9:16"}

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
		ID:                "happyhorse-1.1-r2v",
		Capabilities:      []Capability{CapabilityR2V},
		Protocol:          ProtocolDashScope,
		VideoProfile:      VideoProfileLegacy,
		Supports:          happyhorseVideoParams,
		MaxImages:         9,
		Resolutions:       legacyVideoResolutions,
		DefaultResolution: "720P",
		DurationMin:       3,
		DurationMax:       15,
		DefaultDuration:   5,
		Ratios:            legacyVideoRatios,
		DefaultRatio:      "16:9",
	},
	{
		// wan2.7-r2v 的图片上限在旧系统里是动态的：5 - 当前参考视频数
		// （r2v.js:11 `maxImagesForR2v`），MaxImages 记录的是图+视频合计
		// 上限；参数校验时还要再减去已有的参考视频数。
		ID:                "wan2.7-r2v",
		Capabilities:      []Capability{CapabilityR2V},
		Protocol:          ProtocolDashScope,
		VideoProfile:      VideoProfileWan27,
		Supports:          wanVideoParams,
		Regions:           wanRegions,
		MaxImages:         5,
		Resolutions:       legacyVideoResolutions,
		DefaultResolution: "720P",
		DurationMin:       3,
		DurationMax:       15,
		DefaultDuration:   5,
		Ratios:            legacyVideoRatios,
		DefaultRatio:      "16:9",
	},
	{
		ID:                "happyhorse-1.1-i2v",
		Capabilities:      []Capability{CapabilityI2V},
		Protocol:          ProtocolDashScope,
		VideoProfile:      VideoProfileLegacy,
		Supports:          happyhorseVideoParams,
		MaxImages:         1,
		MinImageEdge:      300,
		RatioMin:          0.4,
		RatioMax:          2.5,
		Resolutions:       legacyVideoResolutions,
		DefaultResolution: "720P",
		DurationMin:       3,
		DurationMax:       15,
		DefaultDuration:   5,
		I2VAutoRatio:      true,
	},
	{
		// wan2.7-i2v 的图片上限是 2（first_frame + last_frame）；first_clip
		// 是既有视频的 URL，不是上传图片，不计入 MaxImages。
		ID:                "wan2.7-i2v-2026-04-25",
		Capabilities:      []Capability{CapabilityI2V},
		Protocol:          ProtocolDashScope,
		VideoProfile:      VideoProfileWan27,
		Supports:          wanVideoParams,
		Regions:           wanRegions,
		MaxImages:         2,
		MinImageEdge:      240,
		RatioMin:          0.125,
		RatioMax:          8,
		Resolutions:       legacyVideoResolutions,
		DefaultResolution: "720P",
		DurationMin:       3,
		DurationMax:       15,
		DefaultDuration:   5,
		I2VAutoRatio:      true,
	},
	{
		ID:                "happyhorse-1.1-t2v",
		Capabilities:      []Capability{CapabilityT2V},
		Protocol:          ProtocolDashScope,
		VideoProfile:      VideoProfileLegacy,
		Supports:          happyhorseVideoParams,
		Resolutions:       legacyVideoResolutions,
		DefaultResolution: "720P",
		DurationMin:       3,
		DurationMax:       15,
		DefaultDuration:   5,
		Ratios:            legacyVideoRatios,
		DefaultRatio:      "16:9",
	},

	// ── wan3.0：一个 model id 覆盖全部五种视频入口 ──────────────────
	//
	// wan3.0-video 与 wan3.0-video-prime 的接口形状完全一致（同一份 API
	// 参考文档），差别只在服务端的速度/价格档位，所以除了 ID 之外两条
	// 记录逐字相同——共享同一组常量而不是各写一遍字面量，避免以后改了
	// 一条忘了另一条。
	//
	// Regions 为空（不限地域）是事实而非遗漏：万相3.0在北京、新加坡、
	// 日本、法兰克福、弗吉尼亚五个地域都提供，覆盖了本系统设置页允许
	// 选择的全部四个地域，没有可以拒绝的取值。也正因为如此，旧的
	// `len(Regions) > 0 == 是 wan 模型` 判据在这里必然失效——改用
	// VideoProfile，见其文档。
	//
	// MaxImages 取 10（reference_image 的上限）只是给沿用合计语义的旧
	// 调用方一个不至于误伤的兜底；真正生效的是 MediaLimits，service 层
	// 对 wan3.0 一律按类型分别计数。
	{
		ID: "wan3.0-video",
		Capabilities: []Capability{
			CapabilityT2V, CapabilityI2V, CapabilityR2V, CapabilityF2V, CapabilityL2V,
		},
		Protocol:          ProtocolDashScope,
		VideoProfile:      VideoProfileWan30,
		Supports:          wan30VideoParams,
		MaxImages:         10,
		MinImageEdge:      240,
		RatioMin:          0.125,
		RatioMax:          8,
		Resolutions:       wan30VideoResolutions,
		DefaultResolution: "720P",
		DurationMin:       2,
		DurationMax:       30,
		DefaultDuration:   5,
		SmartDuration:     true,
		Ratios:            wan30VideoRatios,
		DefaultRatio:      "adaptive",
		MediaLimits: MediaLimits{
			ReferenceImages: 10,
			ReferenceVideos: 5,
			ReferenceAudios: 5,
		},
	},
	{
		ID: "wan3.0-video-prime",
		Capabilities: []Capability{
			CapabilityT2V, CapabilityI2V, CapabilityR2V, CapabilityF2V, CapabilityL2V,
		},
		Protocol:          ProtocolDashScope,
		VideoProfile:      VideoProfileWan30,
		Supports:          wan30VideoParams,
		MaxImages:         10,
		MinImageEdge:      240,
		RatioMin:          0.125,
		RatioMax:          8,
		Resolutions:       wan30VideoResolutions,
		DefaultResolution: "720P",
		DurationMin:       2,
		DurationMax:       30,
		DefaultDuration:   5,
		SmartDuration:     true,
		Ratios:            wan30VideoRatios,
		DefaultRatio:      "adaptive",
		MediaLimits: MediaLimits{
			ReferenceImages: 10,
			ReferenceVideos: 5,
			ReferenceAudios: 5,
		},
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
