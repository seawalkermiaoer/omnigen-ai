package stats

import "time"

// Overview 是总览。TokensAvailable 区分「上游没给 token 数据」与「token 为 0」——
// 视频三种模式的上游根本不返回 token，界面必须显示「—」而不是 0，
// 否则会让人误以为视频免费。判断依据是 JSONB 键是否存在
// （usage ? 'total_tokens'），不是值是否为 0。
type Overview struct {
	TotalCalls      int64 `json:"totalCalls"`
	SucceededCalls  int64 `json:"succeededCalls"`
	FailedCalls     int64 `json:"failedCalls"`
	TotalTokens     int64 `json:"totalTokens"`
	TokensAvailable bool  `json:"tokensAvailable"`
	VideoSeconds    int64 `json:"videoSeconds"`
}

// ByModel 是按 (model, mode) 分组的一行，按 calls 降序排列。
type ByModel struct {
	Model           string `json:"model"`
	Mode            string `json:"mode"`
	Calls           int64  `json:"calls"`
	Succeeded       int64  `json:"succeeded"`
	Failed          int64  `json:"failed"`
	Tokens          int64  `json:"tokens"`
	TokensAvailable bool   `json:"tokensAvailable"`
	VideoSeconds    int64  `json:"videoSeconds"`
}

// ByDay 是按天（date_trunc('day', created_at)）分组的一行，按天升序排列。
type ByDay struct {
	Day       time.Time `json:"day"`
	Calls     int64     `json:"calls"`
	Succeeded int64     `json:"succeeded"`
	Failed    int64     `json:"failed"`
}

// Report 是 GET /api/stats 的完整响应体。三块共享同一个 Query 过滤条件，
// 但各自独立聚合——不是从 Overview 或 ByModel 派生出 ByDay，三者都直接
// 对 generation_tasks 求值，因此测试需要断言三者互相自洽。
//
// 空结果集必须返回结构完整的零值（Overview 全 0、ByModel/ByDay 是空切片
// 而非 nil），前端才能不做 null 检查直接渲染。
type Report struct {
	Overview Overview  `json:"overview"`
	ByModel  []ByModel `json:"byModel"`
	ByDay    []ByDay   `json:"byDay"`
}
