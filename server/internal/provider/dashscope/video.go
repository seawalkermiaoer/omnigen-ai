package dashscope

import (
	"context"
	"net/http"

	"github.com/chenhao/omnigen-ai/server/internal/provider"
)

// videoSynthesisPath 是异步视频创建接口的路径（server.js:314）。
const videoSynthesisPath = "/api/v1/services/aigc/video-generation/video-synthesis"

// taskPathPrefix 是任务轮询接口的路径前缀（server.js:658）。
const taskPathPrefix = "/api/v1/tasks/"

// CreateVideoTask 照抄 server.js `/api/create-task`（server.js:306-323）的
// 请求形状：POST video-synthesis，带 X-DashScope-Async: enable，payload
// 原样透传。
//
// 与旧系统不同的是返回值：旧系统把上游响应原样转发给浏览器，由前端记住
// task_id；新架构在服务端持久化任务（Task 10/12），因此这里要解析出
// output.task_id 并作为返回值交给调用方落库——这是新架构必需的行为，
// 不是对旧系统某条分支的保留。
func (c *Client) CreateVideoTask(ctx context.Context, req provider.VideoRequest) (string, error) {
	base, err := c.baseURL()
	if err != nil {
		return "", err
	}
	url := base + videoSynthesisPath

	headers := c.authHeader()
	headers["X-DashScope-Async"] = "enable"

	status, data, err := c.doRequest(ctx, http.MethodPost, url, headers, req.Payload, defaultTimeout)
	if err != nil {
		return "", ErrRequestFailed.Wrap(err)
	}

	if status >= 400 {
		return "", newUpstreamHTTPError(status, data)
	}
	if hasNativeError(data) {
		return "", newUpstreamError(data)
	}

	output := nestedMap(data, "output")
	taskID := stringField(output, "task_id")
	if taskID == "" {
		return "", ErrNoTaskID
	}
	return taskID, nil
}

// PollTask 照抄 server.js `/api/task/:taskId`（server.js:650-666）：GET
// 请求，无请求体，只带 Authorization 头。
func (c *Client) PollTask(ctx context.Context, taskID string) (*provider.TaskResult, error) {
	base, err := c.baseURL()
	if err != nil {
		return nil, err
	}
	url := base + taskPathPrefix + taskID

	status, data, err := c.doRequest(ctx, http.MethodGet, url, c.authHeader(), nil, defaultTimeout)
	if err != nil {
		return nil, ErrRequestFailed.Wrap(err)
	}

	if status >= 400 {
		return nil, newUpstreamHTTPError(status, data)
	}
	if hasNativeError(data) {
		return nil, newUpstreamError(data)
	}

	output := nestedMap(data, "output")
	resultTaskID := stringField(output, "task_id")
	if resultTaskID == "" {
		resultTaskID = taskID
	}
	return &provider.TaskResult{
		TaskID: resultTaskID,
		Status: stringField(output, "task_status"),
		Raw:    data,
	}, nil
}
