package generation

import "time"

// TaskResponse 是任务对外的唯一表示。凭证从不落在这张表里，因此这里不需要
// 像 setting.SettingResponse 那样做脱敏——但仍然经过显式转换而非直接把
// 实体序列化出去，理由与别处一致：给"这会被发给客户端"留一个一眼可见的
// 类型，字段增删不会悄悄影响响应体。
type TaskResponse struct {
	ID             int64          `json:"id"`
	Mode           TaskMode       `json:"mode"`
	Model          string         `json:"model"`
	Status         Status         `json:"status"`
	UpstreamTaskID string         `json:"upstreamTaskId,omitempty"`
	Prompt         string         `json:"prompt"`
	Params         map[string]any `json:"params"`
	InputURLs      []string       `json:"inputUrls"`
	ResultURLs     []string       `json:"resultUrls"`
	Usage          map[string]any `json:"usage,omitempty"`
	Note           string         `json:"note,omitempty"`
	ErrorCode      string         `json:"errorCode,omitempty"`
	ErrorMessage   string         `json:"errorMessage,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type TaskListResponse struct {
	Total int64          `json:"total"`
	Items []TaskResponse `json:"items"`
}

// TaskDeleteAllResponse is DELETE /api/tasks's response — the deleted-row
// count lets the history page confirm "清空全部" actually happened (and how
// much it cleared) instead of just trusting a bare 200.
type TaskDeleteAllResponse struct {
	Deleted int64 `json:"deleted"`
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// FromEntity 把一条任务实体转换成对外响应。ResultURLs/InputURLs/Params 在
// 实体层已经保证非 nil（见 repository/task.go 的 nil-vs-empty 决策），这里
// 不再重复兜底——如果哪天实体层的保证被打破，宁可让 JSON 里出现显眼的
// null 触发前端报错，也不要在这一层悄悄吞掉信号。
func FromEntity(t Task) TaskResponse {
	return TaskResponse{
		ID:             t.ID,
		Mode:           t.Mode,
		Model:          t.Model,
		Status:         t.Status,
		UpstreamTaskID: strOrEmpty(t.UpstreamTaskID),
		Prompt:         t.Prompt,
		Params:         t.Params,
		InputURLs:      t.InputURLs,
		ResultURLs:     t.ResultURLs,
		Usage:          t.Usage,
		Note:           strOrEmpty(t.Note),
		ErrorCode:      strOrEmpty(t.ErrorCode),
		ErrorMessage:   strOrEmpty(t.ErrorMessage),
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}

func FromEntities(items []Task) []TaskResponse {
	out := make([]TaskResponse, 0, len(items))
	for _, t := range items {
		out = append(out, FromEntity(t))
	}
	return out
}
