// Package generation 定义生成任务（generation_tasks）域的实体、请求体与响应体。
// 导入时统一使用 generationmodel 别名，避免与 service/generation、repository 包名冲突。
//
// 这张表同时是旧系统「历史记录」功能的落地形态（原计划里的子项目 4）——
// 图片生成同步落库为终态，视频生成异步落库后由后端 worker 轮询更新，
// 二者共用同一张表、同一套状态机，因此没有再单独设计一张 history 表。
package generation

import "time"

// TaskMode 对应旧系统五个生成页面各自的模式。
//
// 命名刻意不叫 Mode / 常量不叫 Mode*——本包（Task 8）已经有一个语义不同的
// OptimizeMode 及其 ModeT2V/ModeI2V/ModeR2V 常量（prompts.go），二者虽然
// 取值大多重叠但不是同一个概念：TaskMode 是"这条 generation_tasks 记录是
// 哪种生成"，OptimizeMode 是"提示词优化用哪套系统提示词"，加前缀
// TaskMode/TaskMode* 避免常量重名，也避免调用方把两者混用。
type TaskMode string

const (
	TaskModeImgGen  TaskMode = "imggen"
	TaskModeImgEdit TaskMode = "imgedit"
	TaskModeT2V     TaskMode = "t2v"
	TaskModeI2V     TaskMode = "i2v"
	TaskModeR2V     TaskMode = "r2v"
)

// Status 是任务状态机。PENDING/RUNNING 是「在飞」状态——ClaimPending 只取
// 这两种；SUCCEEDED/FAILED/CANCELED 是终态。图片生成是同步调用，落库时
// 直接是终态（SUCCEEDED/FAILED），不会经过 PENDING/RUNNING。
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusRunning   Status = "RUNNING"
	StatusSucceeded Status = "SUCCEEDED"
	StatusFailed    Status = "FAILED"
	StatusCanceled  Status = "CANCELED"
)

// InFlightStatuses 是 ClaimPending 关心的状态集合，与迁移里
// idx_tasks_polling 部分索引的 WHERE 条件（status IN ('PENDING','RUNNING')）
// 必须保持一致——两处任何一处漏改，索引就不再覆盖查询。
var InFlightStatuses = []Status{StatusPending, StatusRunning}

// Task 是数据库实体，对应 generation_tasks 表。
//
// Params/InputURLs/ResultURLs/Usage 是 JSONB 列，映射为 Go 侧的
// map[string]any / []string，由 repository 层负责与数据库字节的编解码
// （Task 10 职责）。UpstreamTaskID/Note/ErrorCode/ErrorMessage 数据库里允许
// NULL，图片生成（同步、无上游任务 id）与尚未失败的任务不会写这些列，
// 因此用指针表达「未设置」而非空字符串——空字符串是一个合法值
// （比如上游确实返回了空 note），不能拿来兼职表示 NULL。
type Task struct {
	ID             int64
	UserID         int64
	Mode           TaskMode
	Model          string
	Status         Status
	UpstreamTaskID *string
	Prompt         string
	Params         map[string]any
	InputURLs      []string
	ResultURLs     []string
	Usage          map[string]any
	Note           *string
	ErrorCode      *string
	ErrorMessage   *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// IsInFlight 报告任务是否处于 PENDING/RUNNING——供 worker（Task 12）判断是否
// 还需要继续轮询，避免在调用处到处手写状态字符串比较。
func (t Task) IsInFlight() bool {
	return t.Status == StatusPending || t.Status == StatusRunning
}
