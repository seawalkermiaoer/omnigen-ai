package dashscope

import (
	"fmt"
	"net/http"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
)

// 错误码：每个 code 对应旧系统 server.js 里一条不同的失败分支。拆开而不是
// 全部塞进 apperr.ErrInternal，是为了让 service/handler 层能按错误码区分
// "上游 HTTP 错误"、"上游业务错误"、"解析不出结果" 这几类完全不同的情况，
// 而不必解析错误字符串。
const (
	// CodeRequestFailed：网络层失败（连不上、超时、TLS 错误等），forwardJSON
	// 里 req.on('error')/req.on('timeout') 对应的情形。
	CodeRequestFailed = "DASHSCOPE_REQUEST_FAILED"
	// CodeUpstreamHTTP：HTTP 状态码 >= 400（server.js:407/321）。
	CodeUpstreamHTTP = "DASHSCOPE_UPSTREAM_HTTP_ERROR"
	// CodeUpstreamError：HTTP 200 但响应体带 code/error（DashScope 原生错误
	// 格式，server.js:413/619/628）。
	CodeUpstreamError = "DASHSCOPE_UPSTREAM_ERROR"
	// CodeNoImageResult：响应解析成功但抠不出图片 URL（server.js:433）。
	CodeNoImageResult = "DASHSCOPE_NO_IMAGE_RESULT"
	// CodeNoTaskID：视频创建响应里没有 output.task_id。旧系统 /api/create-task
	// 只是把响应原样转发给前端，不解析 task_id；新架构需要在服务端持久化
	// 任务，所以这里比旧系统多做了一步解析，解析失败是新增的失败模式。
	CodeNoTaskID = "DASHSCOPE_NO_TASK_ID"
	// CodeNoOptimizeResult：优化响应解析成功但抠不出非空文本
	// （server.js:635/642，对应 noModelResult 与 parseFail 两种情形，这里
	// 合并成一个码——两者在旧系统里也是同样的 502，区别只是文案）。
	CodeNoOptimizeResult = "DASHSCOPE_NO_OPTIMIZE_RESULT"
)

var (
	ErrRequestFailed    = apperr.New(CodeRequestFailed, http.StatusBadGateway)
	ErrUpstreamHTTP     = apperr.New(CodeUpstreamHTTP, http.StatusBadGateway)
	ErrUpstreamError    = apperr.New(CodeUpstreamError, http.StatusBadRequest)
	ErrNoImageResult    = apperr.New(CodeNoImageResult, http.StatusBadGateway)
	ErrNoTaskID         = apperr.New(CodeNoTaskID, http.StatusBadGateway)
	ErrNoOptimizeResult = apperr.New(CodeNoOptimizeResult, http.StatusBadGateway)
)

// newUpstreamHTTPError 保留旧系统里"HTTP 层错误"與"状态码"的对应关系，同
// t8star 包 newUpstreamHTTPError 的做法一致：用真实的上游状态码而不是固定
// 的 apperr 状态。
func newUpstreamHTTPError(status int, data map[string]any) error {
	msg := extractHTTPError(data)
	if msg == "" {
		msg = fmt.Sprintf("上游返回 HTTP %d", status)
	}
	return apperr.New(CodeUpstreamHTTP, status).Wrap(fmt.Errorf("dashscope: %s", msg))
}

// newUpstreamError 对应 hasNativeError 命中时的错误构造。当 data 判定为
// AccessDenied 时，额外用 provider.ErrAccessDenied 包一层，供 Optimize 的
// 调用方（Task 8 的降级链）用 errors.Is 判断是否要重试——只有这一条路径会
// 附带这个哨兵，其它错误分支（HTTP 层错误、解析失败）都不触发降级重试，
// 与 server.js 只在 isAccessDeniedResponse 命中时才 continue 的行为一致。
func newUpstreamError(data map[string]any) error {
	msg := extractNativeError(data)
	if msg == "" {
		msg = "调用失败"
	}
	cause := fmt.Errorf("dashscope: %s", msg)
	if isAccessDeniedResponse(data) {
		cause = fmt.Errorf("%w: %s", provider.ErrAccessDenied, cause)
	}
	return apperr.New(CodeUpstreamError, http.StatusBadRequest).Wrap(cause)
}
