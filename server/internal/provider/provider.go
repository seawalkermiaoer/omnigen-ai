// Package provider 定义生成能力的上游客户端接口，供 service 层依赖。
//
// 这是一次移植：旧系统（server.js）里同样的三种能力——图片生成、视频任务
// （创建 + 轮询）、prompt 优化——分别对应 DashScope 与 t8star 两种上游协议。
// 具体协议实现放在子包 internal/provider/dashscope 与
// internal/provider/t8star 里；本文件只定义 service 层能依赖的接口与
// 请求/响应形状，不含任何网络 IO。
//
// 凭证（apiKey、region、workspaceId、endpoint）不出现在这些请求结构体里：
// 它们在构造具体 provider 实例时（如 dashscope.New(apiKey, region,
// workspaceID, endpoint)）就固定下来，同一实例此后的每次调用都复用同一份
// 凭证——这样 VideoProvider.PollTask(ctx, taskID) 才能保持与旧系统里
// GET /api/task/:taskId 同样干净的签名，不必在每次轮询时重新传一遍凭证。
package provider

import (
	"context"
	"errors"
)

// ErrAccessDenied 是 OptimizeProvider.Optimize 在上游响应判定为
// "AccessDenied"（isAccessDeniedResponse 语义，见 dashscope 包）时返回的
// 错误里会 errors.Is 命中的哨兵。
//
// 旧系统 server.js:614-625 的降级链——模型外层、endpoint 内层，仅
// AccessDenied 触发重试——发生在调用方（Task 8 的 service 层），而不是这里：
// provider 层每次 Optimize 只做一次上游调用。调用方要知道"要不要换个模型/
// endpoint 重试"，就必须能从返回的 error 里判断出"这是不是 AccessDenied"，
// 而不能靠解析错误文案字符串（脆弱、协议一换就碎）。这个哨兵就是那个信号：
//
//	text, model, err := p.Optimize(ctx, req)
//	if err != nil && errors.Is(err, provider.ErrAccessDenied) && canRetry {
//	    // 换下一个模型 / endpoint 重试
//	}
//
// 只有 dashscope 实现会返回它（t8star 不提供优化能力）；把它放在 provider
// 包而不是 dashscope 子包，是为了让 service 层只依赖接口包，不必反过来
// import 某个具体协议实现。
var ErrAccessDenied = errors.New("provider: upstream access denied")

// ImageParams 是 DashScope 原生图片生成接口 `parameters` 字段里可选参数的
// 直接映射，字段命名与判空方式都照抄 server.js:391-398：
//
//   - Size / NegativePrompt 用真值判断（旧代码 `if (p.size)` /
//     `if (p.negative_prompt)`）：空字符串等价于"没传"，不写入 parameters。
//   - N / Watermark / Seed / ThinkingMode / EnableSequential / PromptExtend
//     用 `!= null` 判断，因此 `n=0`、`watermark=false`、`seed=0` 都要发送。
//     Go 没有 null，用指针表达"调用方是否显式提供了这个字段"——绝不能改成
//     直接用值类型 + 零值判断，那样会把 `n:0` 和 `watermark:false` 静默丢掉，
//     这正是这次移植里最容易犯的错。
type ImageParams struct {
	Size           string
	NegativePrompt string

	N                *int
	Watermark        *bool
	Seed             *int64
	ThinkingMode     *bool
	EnableSequential *bool
	PromptExtend     *bool
}

// ImageRequest 是一次图片生成/编辑调用的入参。
type ImageRequest struct {
	Model  string
	Prompt string
	// Images 是参考图 URL（或 data URI），生成 content 数组时的顺序由具体
	// provider 决定——DashScope 是图在前文在后，t8star 是文在前图在后，
	// 两者刻意不统一（详见 dashscope 包 GenerateImage 的注释）。
	Images []string
	Params ImageParams
}

// ImageResult 是一次图片生成/编辑调用的出参。
type ImageResult struct {
	Images []string
	Usage  map[string]any
	// Model 是上游实际使用的模型：DashScope 分支就是请求里的 model 原样回填
	// （server.js:437 `res.json({ images, usage, model })`）；t8star 分支会
	// 用上游回显的具体子模型覆盖（如 gpt-image-2-pro），由 t8star 包自行处理。
	Model string
	// Note 是模型随图片一起返回的散文说明。只有 t8star 协议有——它把图片 URL
	// 以 markdown 形式嵌在对话内容里，剥掉链接后剩下的文字就是 Note，
	// 旧系统会原样透传给前端展示（server.js 的 t8star 分支）。
	// DashScope 分支恒为空。
	Note string
}

// VideoRequest 是创建异步视频任务的入参。Payload 是发往
// `video-synthesis` 的完整请求体（model/input/parameters），由 service 层
// 按目录里的参数约束组装——旧系统 `/api/create-task` 本就是对客户端传入的
// payload 原样透传（server.js:306-323），这里保留同样的"调用方负责整个
// payload 形状"的分工，provider 层只负责把它送到正确的 URL 并带上正确的
// 异步 header。
type VideoRequest struct {
	Payload map[string]any
}

// TaskResult 是一次任务轮询的结果。
type TaskResult struct {
	TaskID string
	// Status 是 DashScope 的 task_status 原文
	// （PENDING/RUNNING/SUCCEEDED/FAILED/CANCELED，或旧系统里出现过的
	// UNKNOWN）——是否终态、UNKNOWN 要不要连续计数由 Task 12 的轮询 worker
	// 判断，不是 provider 层的职责。
	Status string
	// Raw 是解码后的完整响应体，供调用方按需抠出 output.results[].url 等
	// 字段——不同协议/模型的结果形状不完全一致，在这里强行定义一个"通用
	// 结果 URL 字段"只会丢信息。
	Raw map[string]any
}

// OptimizeRequest 是一次 prompt 优化调用的入参。
//
// SystemPrompt / UserText 是已经拼装好的最终文本——7 套系统提示词的选择、
// sourceDesc/draftText 的三种拼装分支，都是 Task 8（service 层）的职责
// （见 server.js:561-577），provider 层不关心"这是哪个 mode"，只管照单
// 发送。
type OptimizeRequest struct {
	// Model 是这一次调用要用的模型；模型间的降级顺序（TEXT_OPTIMIZE →
	// TEXT_OPTIMIZE_FALLBACKS，或 VISION_OPTIMIZE → VISION_OPTIMIZE_FALLBACKS）
	// 由调用方驱动，provider 每次调用只认一个 model。
	Model        string
	SystemPrompt string
	UserText     string
	// Images 非空时走 DashScope 原生 multimodal-generation 协议
	// （content 数组，图在前文在后，与 GenerateImage 一致）；为空时走
	// compatible-mode/chat completions（server.js:579 `isCompat = !hasImages`）。
	Images []string
}

// ImageProvider 是同步图片生成/编辑能力。
type ImageProvider interface {
	GenerateImage(ctx context.Context, req ImageRequest) (*ImageResult, error)
}

// VideoProvider 是异步视频任务的创建与轮询能力。t8star 不实现这个接口——
// 旧系统里视频生成只走 DashScope，这个限制由模型目录（catalog 包）表达，
// 不需要在接口层面强行为 t8star 拼一个空实现。
type VideoProvider interface {
	CreateVideoTask(ctx context.Context, req VideoRequest) (string, error)
	PollTask(ctx context.Context, taskID string) (*TaskResult, error)
}

// OptimizeProvider 是 prompt 智能优化能力。
type OptimizeProvider interface {
	Optimize(ctx context.Context, req OptimizeRequest) (text string, model string, err error)
}
