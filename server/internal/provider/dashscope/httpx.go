package dashscope

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// doRequest 是四条上游路径共用的收发逻辑，对应 server.js 的 forwardJSON：
//
//   - body 为 nil 时不发送请求体，也不设置 Content-Type（任务轮询走这条路，
//     GET 请求应当既没有 body 也没有 Content-Type 头）；body 非 nil 时序列化
//     成 JSON 并设置 Content-Type: application/json（forwardJSON 里
//     `if (body) { reqHeaders['Content-Type'] = 'application/json' }`）。
//   - timeout 通过 context.WithTimeout 施加在传入的 ctx 上：若调用方的 ctx
//     本身带有更短的 deadline，Go 的 context 会取两者中更早的那个生效，
//     天然支持"调用方可以用一个更短的 context 抢先超时"这种测试场景。
//   - 上游返回的响应体尝试解码成 map[string]any；解码失败（不是合法 JSON）
//     时不 panic、不整体报错，而是退化成 {"raw": "<原始文本>"}，与
//     forwardJSON 里 `try { data = JSON.parse(text) } catch { data = { raw:
//     text } }` 的兜底行为一致。
func (c *Client) doRequest(ctx context.Context, method, url string, headers map[string]string, body any, timeout time.Duration) (int, map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var reader io.Reader
	var hasBody bool
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("dashscope: 编码请求体失败: %w", err)
		}
		reader = bytes.NewReader(encoded)
		hasBody = true
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("dashscope: 构造请求失败: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("dashscope: 请求上游失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("dashscope: 读取上游响应失败: %w", err)
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		data = map[string]any{"raw": string(raw)}
	}
	return resp.StatusCode, data, nil
}
