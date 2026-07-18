// Package dashscope 实现阿里云百炼（DashScope）原生协议客户端，是
// server.js 里 forwardJSON + getEndpoint + REGION_ENDPOINTS 那部分转发逻辑
// 的移植目标。四条上游路径：
//
//   - 视频创建（异步）：POST {base}/api/v1/services/aigc/video-generation/video-synthesis
//     带 X-DashScope-Async: enable
//   - 任务轮询：       GET  {base}/api/v1/tasks/{taskID}
//   - 图片生成/编辑（同步）：POST {base}/api/v1/services/aigc/multimodal-generation/generation
//   - prompt 优化（文本）：  POST {base}/compatible-mode/v1/chat/completions
//
// 全部请求带 `Authorization: Bearer {apiKey}`；有请求体的都带
// `Content-Type: application/json`（任务轮询是 GET 且无请求体，因此也没有
// Content-Type——这是对 server.js forwardJSON 里"只有 body 非空才设置
// Content-Type/Content-Length"这条规则的忠实移植，不是遗漏）。
package dashscope

import (
	"net/http"
	"time"

	"github.com/chenhao/omnigen-ai/server/internal/provider"
)

// 编译期断言：Client 必须同时满足全部三个 provider 接口。放在这里而不是
// 某一个方法所在的文件里，是因为它检验的是整个类型，不是某个方法。
var (
	_ provider.ImageProvider    = (*Client)(nil)
	_ provider.VideoProvider    = (*Client)(nil)
	_ provider.OptimizeProvider = (*Client)(nil)
)

// defaultTimeout 对应 server.js forwardJSON 的默认 timeoutMs=120000。
const defaultTimeout = 120 * time.Second

// imageGenTimeout 对应 server.js `/api/generate-image` 调用 forwardJSON 时
// 显式传入的 180000（server.js:403）。
const imageGenTimeout = 180 * time.Second

// Client 持有一次上游调用所需的全部凭证与路由信息，构造后不可变。
// 凭证不通过每次调用的参数传入（对照 provider.VideoProvider.PollTask(ctx,
// taskID) 这种干净签名），而是在 New() 时固定——同一份配置（一个 region/
// endpoint/apiKey 组合）对应一个 Client 实例，与旧系统"每次生成动作都读一遍
// 当前配置"的效果一致，只是把读配置的时机挪到了构造时。
type Client struct {
	apiKey      string
	region      string
	workspaceID string
	// endpoint 是可选的显式 base URL 覆盖，语义与 server.js resolveEndpoint
	// 里的 `endpoint` 参数一致：非空且以 "http" 开头时整段替换 region 推导
	// 出的默认地址。新架构里这个值只能来自服务端 app_settings（管理员配置），
	// 不再像旧系统那样接受客户端每次请求携带任意 URL（那是 SSRF 的源头，
	// 见设计文档「安全问题 3」）——本包不区分这两种来源，调用方（service 层）
	// 负责保证传进来的 endpoint 只可能是服务端配置。
	endpoint   string
	httpClient *http.Client
}

// New 构造一个绑定了固定凭证的 Client。
func New(apiKey, region, workspaceID, endpoint string) *Client {
	return &Client{
		apiKey:      apiKey,
		region:      region,
		workspaceID: workspaceID,
		endpoint:    endpoint,
		httpClient:  &http.Client{},
	}
}

// authHeader 是全部四条路径共用的鉴权头。
func (c *Client) authHeader() map[string]string {
	return map[string]string{"Authorization": "Bearer " + c.apiKey}
}
