package dashscope

import (
	"context"
	"net/http"
	"strings"

	"github.com/chenhao/omnigen-ai/server/internal/provider"
)

// optimizeCompatPath 是无参考图时使用的 OpenAI 兼容协议路径
// （server.js:594 的 `!hasImages` 分支）。
const optimizeCompatPath = "/compatible-mode/v1/chat/completions"

// Optimize 照抄 server.js `/api/optimize-prompt` 单次上游调用的请求/响应
// 处理（server.js:591-644），但只做"一次调用"，不做模型/endpoint 的降级
// 重试循环——降级链（模型外层、endpoint 内层，仅 AccessDenied 触发）是
// Task 8 service 层的职责（见 provider.go 里 ErrAccessDenied 的注释）。
//
// 与 GenerateImage 共用同一条路径判断：有参考图时走 DashScope 原生
// multimodal-generation（content 数组，图在前文在后，system 消息单独一条），
// 无参考图时走 compatible-mode/chat completions（messages 是
// {role, content: 字符串} 的简单形式）。这与 server.js:579
// `const isCompat = !hasImages;` 完全对应。
//
// 注意：这里刻意不检查 HTTP 状态码是否 >= 400——server.js 的
// optimize-prompt 分支自始至终只用 `r.data && (r.data.code || r.data.error)`
// 判断失败（server.js:628），不像 generate-image 那样先做一次单独的
// status>=400 检查。这个差异是旧代码本来的样子，照抄。
func (c *Client) Optimize(ctx context.Context, req provider.OptimizeRequest) (string, string, error) {
	base, err := c.baseURL()
	if err != nil {
		return "", "", err
	}

	hasImages := len(req.Images) > 0
	var url string
	var payload map[string]any

	if hasImages {
		url = base + multimodalGenerationPath
		content := buildImageContent(req.Images, req.UserText)
		payload = map[string]any{
			"model": req.Model,
			"input": map[string]any{
				"messages": []map[string]any{
					{"role": "system", "content": []map[string]any{{"text": req.SystemPrompt}}},
					{"role": "user", "content": content},
				},
			},
			"parameters": map[string]any{"result_format": "message"},
		}
	} else {
		url = base + optimizeCompatPath
		payload = map[string]any{
			"model": req.Model,
			"messages": []map[string]any{
				{"role": "system", "content": req.SystemPrompt},
				{"role": "user", "content": req.UserText},
			},
		}
	}

	// HTTP 状态码有意不参与错误判断，见函数注释。
	_, data, err := c.doRequest(ctx, http.MethodPost, url, c.authHeader(), payload, defaultTimeout)
	if err != nil {
		return "", "", ErrRequestFailed.Wrap(err)
	}

	if hasNativeError(data) {
		return "", "", newUpstreamError(data)
	}

	text := extractOptimizedText(data, hasImages)
	if text == "" {
		return "", "", ErrNoOptimizeResult
	}
	return text, req.Model, nil
}

// extractOptimizedText 照抄 server.js:634-641：
//
//	const choices = isCompat ? r.data?.choices : r.data?.output?.choices;
//	...
//	const content = choices[0]?.message?.content;
//	let text = '';
//	if (typeof content === 'string') text = content.trim();
//	else if (Array.isArray(content)) text = content.map(c => c.text || '').join('').trim();
func extractOptimizedText(data map[string]any, hasImages bool) string {
	var choicesRaw []any
	if hasImages {
		output := nestedMap(data, "output")
		if output != nil {
			choicesRaw, _ = output["choices"].([]any)
		}
	} else {
		choicesRaw, _ = data["choices"].([]any)
	}
	if len(choicesRaw) == 0 {
		return ""
	}
	choice0, ok := choicesRaw[0].(map[string]any)
	if !ok {
		return ""
	}
	message := nestedMap(choice0, "message")
	if message == nil {
		return ""
	}

	switch content := message["content"].(type) {
	case string:
		return strings.TrimSpace(content)
	case []any:
		var sb strings.Builder
		for _, item := range content {
			if m, ok := item.(map[string]any); ok {
				sb.WriteString(stringField(m, "text"))
			}
		}
		return strings.TrimSpace(sb.String())
	default:
		return ""
	}
}
