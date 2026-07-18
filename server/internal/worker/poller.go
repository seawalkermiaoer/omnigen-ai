// Package worker holds the background poller that advances async video
// tasks from PENDING/RUNNING to a terminal status. This is Task 12's other
// half (see internal/service/generation_video.go for task creation) and
// ports the polling half of public/js/task.js's pollTask (task.js:102-152).
//
// Two bugs in the old loop are deliberately not carried over:
//
//  1. task.js:135 treats a single UNKNOWN response as terminal — one
//     malformed upstream reply permanently kills the poll and fails the
//     task. Here, UNKNOWN (and network failures reaching the provider) are
//     counted per task and only fail the task after 5 in a row; any
//     non-UNKNOWN response resets that task's counter to zero. The counter
//     lives in a plain map keyed by task id, touched only from the single
//     poll goroutine — see Poller.PollOnce — so "per task, not global" falls
//     out of the data structure rather than needing extra bookkeeping.
//  2. task.js's `while (true)` loop has no upper bound — a task can poll
//     forever. Here, any task whose CreatedAt is more than maxTaskAge in the
//     past is failed with apperr.ErrTaskTimeout before it's even polled.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// DefaultInterval is the production poll interval (plan: "按 15 秒间隔轮询上游").
const DefaultInterval = 15 * time.Second

// maxTaskAge is the upper bound on how long a task may stay in flight
// before the worker gives up on it and fails it with apperr.ErrTaskTimeout —
// the bound the old browser loop never had.
const maxTaskAge = 30 * time.Minute

// maxConsecutiveFailures is how many UNKNOWN/network-failure responses in a
// row a single task tolerates before the worker fails it. 4 in a row must
// not fail the task; the 5th does.
const maxConsecutiveFailures = 5

// defaultBatchSize bounds how many in-flight tasks a single PollOnce claims
// and processes.
const defaultBatchSize = 50

// statusUnknown is DashScope's task_status value for a malformed/unexpected
// response — see provider.TaskResult.Status doc. An empty Status (the zero
// value, e.g. if a provider implementation forgets to set it) is treated
// the same way: both are "we don't actually know what state this task is
// in", which is exactly what the failure counter exists for.
const statusUnknown = "UNKNOWN"

// Option configures a Poller. Interval and the clock are both injectable so
// tests never need to sleep real wall-clock time — WithClock lets a test
// simulate maxTaskAge elapsing instantly, and WithInterval lets a test that
// exercises Run() use a sub-millisecond tick instead of the real 15s.
type Option func(*Poller)

func WithInterval(d time.Duration) Option {
	return func(p *Poller) { p.interval = d }
}

// WithClock overrides time.Now for age comparisons. Tests use this to make
// a task instantly "older than 30 minutes" without a real sleep.
func WithClock(now func() time.Time) Option {
	return func(p *Poller) { p.now = now }
}

func WithBatchSize(n int) Option {
	return func(p *Poller) { p.batchSize = n }
}

// Poller periodically claims PENDING/RUNNING tasks and advances them by
// polling upstream. It assumes a single running instance — repository.
// TaskRepository.ClaimPending's doc is explicit that it does not use
// SELECT ... FOR UPDATE SKIP LOCKED, so two Poller instances against the
// same database would both claim (and both poll) the same rows. That is an
// accepted limitation of this task, not something fixed here.
type Poller struct {
	tasks    repository.TaskRepository
	settings service.SettingReader
	factory  service.VideoProviderFactory

	interval  time.Duration
	now       func() time.Time
	batchSize int

	// failureCounts tracks consecutive UNKNOWN/network-failure responses
	// per task id. It is only ever read/written from the goroutine running
	// Run/PollOnce — the type has no internal locking because it is never
	// shared across goroutines (Run is documented as not safe to call
	// concurrently with itself, matching ClaimPending's single-instance
	// assumption above).
	failureCounts map[int64]int
}

// New constructs a Poller with production defaults (15s interval, real
// clock, batch size 50); apply Option values to override any of them.
func New(tasks repository.TaskRepository, settings service.SettingReader, factory service.VideoProviderFactory, opts ...Option) *Poller {
	p := &Poller{
		tasks:         tasks,
		settings:      settings,
		factory:       factory,
		interval:      DefaultInterval,
		now:           time.Now,
		batchSize:     defaultBatchSize,
		failureCounts: map[int64]int{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Run blocks, polling every p.interval, until ctx is canceled. It is the
// production entry point (called from a goroutine at startup); tests that
// need deterministic control over individual poll cycles should call
// PollOnce directly instead.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.PollOnce(ctx)
		}
	}
}

// PollOnce runs a single claim-and-poll cycle: it claims up to batchSize
// in-flight tasks, builds one upstream provider instance from the current
// settings, and advances each task in turn. It stops early — without
// touching any remaining claimed task — as soon as ctx is canceled, so a
// shutdown mid-batch never leaves a task half-written (each task's status
// is only ever written after a complete, uncancelled upstream round trip).
func (p *Poller) PollOnce(ctx context.Context) {
	tasks, err := p.tasks.ClaimPending(ctx, p.batchSize)
	if err != nil {
		slog.Error("worker: 拉取待轮询任务失败", "err", err)
		return
	}
	if len(tasks) == 0 {
		return
	}

	pv, err := p.buildProvider(ctx)
	if err != nil {
		// 凭证暂时读不到/未配置——这是服务配置问题，不是任何一个任务自己
		// 的问题，不应该计入任何任务的失败计数器，跳过这一轮整体重试。
		slog.Error("worker: 构造上游 provider 失败，跳过本轮轮询", "err", err)
		return
	}

	for _, t := range tasks {
		if ctx.Err() != nil {
			return
		}
		p.pollTask(ctx, pv, t)
	}
}

func (p *Poller) buildProvider(ctx context.Context) (provider.VideoProvider, error) {
	region, err := p.settings.GetDecrypted(ctx, settingmodel.KeyRegion)
	if err != nil {
		return nil, err
	}
	apiKey, err := p.settings.GetDecrypted(ctx, settingmodel.KeyDashscopeAPIKey)
	if err != nil {
		return nil, err
	}
	if apiKey == "" {
		return nil, apperr.ErrSettingIncomplete
	}
	endpoint, err := p.settings.GetDecrypted(ctx, settingmodel.KeyEndpoint)
	if err != nil {
		return nil, err
	}
	workspaceID, err := p.settings.GetDecrypted(ctx, settingmodel.KeyWorkspaceID)
	if err != nil {
		return nil, err
	}
	return p.factory(apiKey, region, workspaceID, endpoint), nil
}

// pollTask advances a single claimed task by (at most) one step.
func (p *Poller) pollTask(ctx context.Context, pv provider.VideoProvider, t generationmodel.Task) {
	if p.now().Sub(t.CreatedAt) > maxTaskAge {
		if err := p.tasks.UpdateStatus(ctx, t.ID, generationmodel.StatusFailed, apperr.ErrTaskTimeout.Code(), "任务超过 30 分钟未完成"); err != nil {
			slog.Error("worker: 标记任务超时失败失败", "taskID", t.ID, "err", err)
		}
		delete(p.failureCounts, t.ID)
		return
	}

	if t.UpstreamTaskID == nil || *t.UpstreamTaskID == "" {
		// 不应该发生——CreateVideoTask 只在拿到上游 task id 后才会把任务
		// 落成 PENDING（见 generation_video.go）。防御性跳过，不产生任何
		// 状态变更，留给下一轮重试。
		slog.Error("worker: 待轮询任务缺少 upstream_task_id", "taskID", t.ID)
		return
	}

	result, err := pv.PollTask(ctx, *t.UpstreamTaskID)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			// 关停中——这次失败是我们主动断的，不是上游/网络的问题，不计入
			// 失败计数器，也不写任何状态，任务原样留给下次进程启动后的
			// 第一轮轮询。
			return
		}
		p.recordFailure(ctx, t)
		return
	}

	status := result.Status
	if status == "" {
		status = statusUnknown
	}

	switch status {
	case statusUnknown:
		p.recordFailure(ctx, t)

	case string(generationmodel.StatusSucceeded):
		delete(p.failureCounts, t.ID)
		urls, usage, note := extractSuccess(result.Raw)
		if err := p.tasks.UpdateResult(ctx, t.ID, urls, usage, note); err != nil {
			slog.Error("worker: 写入任务结果失败", "taskID", t.ID, "err", err)
		}

	case string(generationmodel.StatusFailed), string(generationmodel.StatusCanceled):
		delete(p.failureCounts, t.ID)
		errMsg := extractErrorMessage(result.Raw)
		if err := p.tasks.UpdateStatus(ctx, t.ID, generationmodel.Status(status), "", errMsg); err != nil {
			slog.Error("worker: 更新任务终态失败", "taskID", t.ID, "err", err)
		}

	default:
		// PENDING / RUNNING（或上游未来新增的其它在飞状态字面量）：这是一次
		// "好"的响应——即便状态没变，也要重置失败计数器（这正是"4 次
		// UNKNOWN → 1 次 RUNNING → 再 4 次 UNKNOWN 不应该失败"这条要求的
		// 落地方式）。只有状态确实变化时才写一次数据库。
		delete(p.failureCounts, t.ID)
		if generationmodel.Status(status) != t.Status {
			if err := p.tasks.UpdateStatus(ctx, t.ID, generationmodel.Status(status), "", ""); err != nil {
				slog.Error("worker: 更新任务状态失败", "taskID", t.ID, "err", err)
			}
		}
	}
}

// recordFailure increments t's consecutive-failure counter and fails the
// task once it reaches maxConsecutiveFailures — 4 consecutive failures must
// leave the task exactly as it was (still PENDING/RUNNING, still pollable
// next cycle); the 5th flips it to FAILED.
func (p *Poller) recordFailure(ctx context.Context, t generationmodel.Task) {
	p.failureCounts[t.ID]++
	if p.failureCounts[t.ID] < maxConsecutiveFailures {
		return
	}
	if err := p.tasks.UpdateStatus(ctx, t.ID, generationmodel.StatusFailed, apperr.ErrTaskPollFailed.Code(), "连续多次轮询未获得有效的上游响应"); err != nil {
		slog.Error("worker: 标记任务轮询失败失败", "taskID", t.ID, "err", err)
	}
	delete(p.failureCounts, t.ID)
}

// extractSuccess pulls the result URL(s), usage, and note out of a
// SUCCEEDED response's raw body. Shape mirrors what public/js/task.js reads
// off the same response (task.js:125,130 `out.video_url`, `d.usage`) — video
// tasks only ever produce a single output URL, unlike image generation's
// `output.results[]`.
func extractSuccess(raw map[string]any) (urls []string, usage map[string]any, note string) {
	if output, ok := raw["output"].(map[string]any); ok {
		if v, ok := output["video_url"].(string); ok && v != "" {
			urls = []string{v}
		}
	}
	if u, ok := raw["usage"].(map[string]any); ok {
		usage = u
	}
	return urls, usage, ""
}

// extractErrorMessage mirrors task.js:136 `out.message || d.message`.
func extractErrorMessage(raw map[string]any) string {
	if output, ok := raw["output"].(map[string]any); ok {
		if msg, ok := output["message"].(string); ok && msg != "" {
			return msg
		}
	}
	if msg, ok := raw["message"].(string); ok {
		return msg
	}
	return ""
}
