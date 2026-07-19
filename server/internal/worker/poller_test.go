package worker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/worker"
)

func seedPendingTask(repo *fakeTaskRepo, upstreamID string, createdAt time.Time) int64 {
	uid := upstreamID
	return repo.seed(generationmodel.Task{
		UserID:         1,
		Mode:           generationmodel.TaskModeT2V,
		Model:          "happyhorse-1.1-t2v",
		Status:         generationmodel.StatusPending,
		UpstreamTaskID: &uid,
		Prompt:         "x",
		// 真实的 CreateVideoTask 提交时总是先扣费再落 PENDING 行（见
		// generation_video.go），所以任何测试都不应该假装一个"从未被计费"
		// 的在飞任务——这里默认 true，与生产行为的不变量保持一致。
		QuotaCharged: true,
	}, createdAt)
}

func ok(status string) scriptedPoll {
	return scriptedPoll{result: &provider.TaskResult{Status: status, Raw: map[string]any{
		"output": map[string]any{"task_status": status},
	}}}
}

func succeeded(url string) scriptedPoll {
	return scriptedPoll{result: &provider.TaskResult{
		Status: string(generationmodel.StatusSucceeded),
		Raw: map[string]any{
			"output": map[string]any{"task_status": "SUCCEEDED", "video_url": url},
			"usage":  map[string]any{"output_video_duration": 5},
		},
	}}
}

// ── PENDING → RUNNING → SUCCEEDED ────────────────────────────────────

func TestPoller_PendingRunningSucceeded_UpdatesRowAtEachStep(t *testing.T) {
	repo := newFakeTaskRepo()
	id := seedPendingTask(repo, "up-1", time.Now())

	pv := newScriptedVideoProvider(map[string][]scriptedPoll{
		"up-1": {ok("RUNNING"), succeeded("https://cdn.example.com/v.mp4")},
	})
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	p.PollOnce(context.Background())
	got := repo.get(id)
	assert.Equal(t, generationmodel.StatusRunning, got.Status)

	p.PollOnce(context.Background())
	got = repo.get(id)
	assert.Equal(t, generationmodel.StatusSucceeded, got.Status)
	assert.Equal(t, []string{"https://cdn.example.com/v.mp4"}, got.ResultURLs)
}

// ── 4 UNKNOWNs 保持轮询；第 5 次判失败 ───────────────────────────────

func TestPoller_FourConsecutiveUnknowns_KeepPolling_FifthFails(t *testing.T) {
	repo := newFakeTaskRepo()
	id := seedPendingTask(repo, "up-1", time.Now())

	pv := newScriptedVideoProvider(map[string][]scriptedPoll{
		"up-1": {ok("UNKNOWN"), ok("UNKNOWN"), ok("UNKNOWN"), ok("UNKNOWN"), ok("UNKNOWN")},
	})
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	for i := 0; i < 4; i++ {
		p.PollOnce(context.Background())
		got := repo.get(id)
		require.Equal(t, generationmodel.StatusPending, got.Status, "第 %d 次 UNKNOWN 不应该终止任务", i+1)
	}

	p.PollOnce(context.Background())
	got := repo.get(id)
	assert.Equal(t, generationmodel.StatusFailed, got.Status, "第 5 次连续 UNKNOWN 必须判失败")
	require.NotNil(t, got.ErrorCode)
	assert.Equal(t, apperr.ErrTaskPollFailed.Code(), *got.ErrorCode)
}

// ── 好响应重置计数器 ──────────────────────────────────────────────────

func TestPoller_GoodResponse_ResetsCounter(t *testing.T) {
	repo := newFakeTaskRepo()
	id := seedPendingTask(repo, "up-1", time.Now())

	script := []scriptedPoll{
		ok("UNKNOWN"), ok("UNKNOWN"), ok("UNKNOWN"), ok("UNKNOWN"), // 4 UNKNOWN
		ok("RUNNING"),                                              // 1 好响应，重置计数器
		ok("UNKNOWN"), ok("UNKNOWN"), ok("UNKNOWN"), ok("UNKNOWN"), // 再 4 个 UNKNOWN
	}
	pv := newScriptedVideoProvider(map[string][]scriptedPoll{"up-1": script})
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	for i := 0; i < len(script); i++ {
		p.PollOnce(context.Background())
	}

	got := repo.get(id)
	assert.NotEqual(t, generationmodel.StatusFailed, got.Status,
		"4 UNKNOWN + 1 好响应 + 4 UNKNOWN 一共只攒到 4 连续失败，不应该判失败")
}

// ── 计数器按任务隔离，不是全局的 ─────────────────────────────────────

func TestPoller_FailureCounter_IsPerTask_NotGlobal(t *testing.T) {
	repo := newFakeTaskRepo()
	now := time.Now()
	// 交错插入两个任务，创建时间相同顺序无关紧要——ClaimPending 每轮把两个
	// 都claim 出来，poller 在同一个 PollOnce 里依次处理。
	idA := seedPendingTask(repo, "up-A", now)
	idB := seedPendingTask(repo, "up-B", now.Add(time.Millisecond))

	// A 连续 3 次 UNKNOWN 后转 RUNNING（不该失败）；
	// B 连续 5 次 UNKNOWN（该失败）。
	// 如果计数器是全局共享的，A 的 3 次会和 B 的失败合并计数，导致 B 提前
	// 在第 2 次就被判失败，或者 A 的失败被 B 的好响应错误地重置。
	pv := newScriptedVideoProvider(map[string][]scriptedPoll{
		"up-A": {ok("UNKNOWN"), ok("UNKNOWN"), ok("UNKNOWN"), ok("RUNNING"), ok("RUNNING")},
		"up-B": {ok("UNKNOWN"), ok("UNKNOWN"), ok("UNKNOWN"), ok("UNKNOWN"), ok("UNKNOWN")},
	})
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	for i := 0; i < 5; i++ {
		p.PollOnce(context.Background())
	}

	gotA := repo.get(idA)
	gotB := repo.get(idB)
	assert.Equal(t, generationmodel.StatusRunning, gotA.Status, "A 从未连续 5 次失败，不该被判失败")
	assert.Equal(t, generationmodel.StatusFailed, gotB.Status, "B 连续 5 次 UNKNOWN，必须判失败")
}

// ── 超过 30 分钟的任务判超时失败 ─────────────────────────────────────

func TestPoller_TaskOlderThan30Minutes_FailsWithTimeoutCode(t *testing.T) {
	repo := newFakeTaskRepo()
	old := time.Now().Add(-31 * time.Minute)
	id := seedPendingTask(repo, "up-1", old)

	// 空脚本：一旦轮询真的打到上游，PollTask 会因为找不到脚本而 panic——
	// 这次调用必须完全不发生，从而验证"超时任务从不轮询上游"。CreatedAt
	// 已经手工设成 31 分钟前，不需要额外注入假时钟就能触发超时判断，但
	// WithClock 仍然可用（下面这行留空走默认 time.Now，证明两条路径都对）。
	pv := newScriptedVideoProvider(map[string][]scriptedPoll{})
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	p.PollOnce(context.Background())

	got := repo.get(id)
	assert.Equal(t, generationmodel.StatusFailed, got.Status)
	require.NotNil(t, got.ErrorCode)
	assert.Equal(t, apperr.ErrTaskTimeout.Code(), *got.ErrorCode)
}

func TestPoller_TaskUnder30Minutes_NotTimedOut(t *testing.T) {
	repo := newFakeTaskRepo()
	recent := time.Now().Add(-5 * time.Minute)
	id := seedPendingTask(repo, "up-1", recent)

	pv := newScriptedVideoProvider(map[string][]scriptedPoll{"up-1": {ok("RUNNING")}})
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	p.PollOnce(context.Background())

	got := repo.get(id)
	assert.Equal(t, generationmodel.StatusRunning, got.Status)
}

// ── 优雅停机：中途取消不留脏状态 ─────────────────────────────────────

func TestPoller_GracefulShutdown_MidPoll_LeavesNoCorruptedState(t *testing.T) {
	repo := newFakeTaskRepo()
	id := seedPendingTask(repo, "up-1", time.Now())

	block := make(chan struct{}) // never closed: PollTask blocks until ctx is canceled
	pv := &scriptedVideoProvider{scripts: map[string][]scriptedPoll{}, cursor: map[string]int{}, blockUntil: block}
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.PollOnce(ctx)
		close(done)
	}()

	// Give PollOnce time to reach the blocked PollTask call, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("PollOnce 在 context 取消后没有及时返回")
	}

	got := repo.get(id)
	assert.Equal(t, generationmodel.StatusPending, got.Status,
		"关停中断的轮询不应该把任务写成任何终态或半途状态")
	assert.Nil(t, got.ErrorCode)
}

// ── Run() 在 ctx 取消后退出，且不会遗留一个仍在运行的 goroutine 卡住测试 ──

func TestPoller_Run_StopsOnContextCancel(t *testing.T) {
	repo := newFakeTaskRepo()
	seedPendingTask(repo, "up-1", time.Now())

	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(alwaysRunningProvider{}), noopArchiver{}, worker.WithInterval(time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run 在 context 取消后没有及时返回")
	}
}

// ── 上游 provider 构造失败（凭证未配置）时整轮跳过，不牵连任何任务 ──────

func TestPoller_CredentialsMissing_SkipsCycle_DoesNotFailTasks(t *testing.T) {
	repo := newFakeTaskRepo()
	id := seedPendingTask(repo, "up-1", time.Now())

	pv := newScriptedVideoProvider(map[string][]scriptedPoll{})
	p := worker.New(repo, fakeSettings{}, fixedVideoFactory(pv), noopArchiver{}) // 空设置：apiKey 为空

	p.PollOnce(context.Background())

	got := repo.get(id)
	assert.Equal(t, generationmodel.StatusPending, got.Status, "凭证缺失不应该牵连任何具体任务")
}

// ── 网络失败按 UNKNOWN 同样的规则计数 ──────────────────────────────────

func TestPoller_NetworkFailure_CountsTowardSameThreshold(t *testing.T) {
	repo := newFakeTaskRepo()
	id := seedPendingTask(repo, "up-1", time.Now())

	netErr := errors.New("dial tcp: connection refused")
	pv := newScriptedVideoProvider(map[string][]scriptedPoll{
		"up-1": {
			{err: netErr}, {err: netErr}, {err: netErr}, {err: netErr}, {err: netErr},
		},
	})
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	for i := 0; i < 4; i++ {
		p.PollOnce(context.Background())
		got := repo.get(id)
		require.Equal(t, generationmodel.StatusPending, got.Status)
	}
	p.PollOnce(context.Background())
	got := repo.get(id)
	assert.Equal(t, generationmodel.StatusFailed, got.Status)
	require.NotNil(t, got.ErrorCode)
	assert.Equal(t, apperr.ErrTaskPollFailed.Code(), *got.ErrorCode)
}

// ── 任务在轮询过程中被用户删除 ─────────────────────────────────────────
//
// Design decision (see service.VideoGenerationService.DeleteTask's doc):
// deleting a task is allowed regardless of status, including PENDING/
// RUNNING ones the poller may be mid-flight on. This test simulates the
// worst-case ordering — the row vanishes between ClaimPending returning it
// and pollTask writing its result back — by having the scripted provider
// delete the row as a side effect of the upstream PollTask call itself.
// TaskRepository.UpdateResult/UpdateStatus both return apperr.ErrTaskNotFound
// when zero rows match (task.go); the assertion here is that PollOnce
// neither panics on that nor resurrects the row.

// deleteOnPollProvider deletes a task out from under the poller the moment
// PollTask is called for it, then answers as scripted — modeling "the user
// clicked delete while this exact upstream round trip was in flight".
type deleteOnPollProvider struct {
	repo   *fakeTaskRepo
	id     int64
	result *provider.TaskResult
}

func (p *deleteOnPollProvider) CreateVideoTask(context.Context, provider.VideoRequest) (string, error) {
	panic("deleteOnPollProvider: CreateVideoTask should never be called by the poller")
}

func (p *deleteOnPollProvider) PollTask(_ context.Context, _ string) (*provider.TaskResult, error) {
	p.repo.delete(p.id)
	return p.result, nil
}

var _ provider.VideoProvider = (*deleteOnPollProvider)(nil)

func TestPoller_TaskDeletedMidPoll_SucceededResponse_DoesNotCrash_DoesNotResurrectRow(t *testing.T) {
	repo := newFakeTaskRepo()
	id := seedPendingTask(repo, "up-1", time.Now())

	pv := &deleteOnPollProvider{repo: repo, id: id, result: &provider.TaskResult{
		Status: string(generationmodel.StatusSucceeded),
		Raw: map[string]any{
			"output": map[string]any{"task_status": "SUCCEEDED", "video_url": "https://cdn.example.com/v.mp4"},
		},
	}}
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	assert.NotPanics(t, func() {
		p.PollOnce(context.Background())
	})

	repo.mu.Lock()
	_, exists := repo.tasks[id]
	repo.mu.Unlock()
	assert.False(t, exists, "worker 不应该把已删除的任务重新插入回去")

	// 下一轮 ClaimPending 也不应该再看到这个 id——它已经不在表里了。
	claimed, err := repo.ClaimPending(context.Background(), 10)
	require.NoError(t, err)
	for _, ct := range claimed {
		assert.NotEqual(t, id, ct.ID)
	}
}

// 同一场景在"仍是 RUNNING"（非终态）响应下必须一样安全——default 分支里
// 那次可能触发的 UpdateStatus 同样要能吞下 ErrTaskNotFound。
// ── 配额退回 ────────────────────────────────────────────────────────

// 任务判定失败时退回额度，并把 quota_charged 置 false。
func TestPoller_TaskFailed_RefundsQuota(t *testing.T) {
	repo := newFakeTaskRepo()
	id := seedPendingTask(repo, "up-1", time.Now())
	require.Equal(t, 1, repo.quotaUsedFor(1), "种子任务应该已经计费")

	pv := newScriptedVideoProvider(map[string][]scriptedPoll{
		"up-1": {{result: &provider.TaskResult{
			Status: string(generationmodel.StatusFailed),
			Raw:    map[string]any{"output": map[string]any{"task_status": "FAILED", "message": "upstream rejected"}},
		}}},
	})
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	p.PollOnce(context.Background())

	got := repo.get(id)
	assert.Equal(t, generationmodel.StatusFailed, got.Status)
	assert.False(t, got.QuotaCharged, "判定失败必须把 quota_charged 置 false")
	assert.Equal(t, 0, repo.quotaUsedFor(1), "判定失败必须退回额度")
}

// 已退过的任务（quota_charged=false）再次被处理不会重复退款。这是
// quota_charged 存在的唯一理由，必须直接测——不经过 pollTask 的状态机，
// 直接调用两次 RefundQuotaForTask 本身，模拟 worker 因重试/重启对同一个
// 任务重复处理。
func TestPoller_AlreadyRefunded_DoesNotRefundTwice(t *testing.T) {
	repo := newFakeTaskRepo()
	// 同一个用户有两个已计费的在飞任务（quotaUsed 从 2 起步）——这一点
	// 是必须的：如果这个用户全程只欠了 1 份额度，"退两次"和"退一次"在
	// quota_used 上都会落在 0（第二次会被 users.quota_used > 0 那道独立
	// 守卫挡住），guard 是否生效就无法从 quotaUsed 的数值上分辨出来。
	// 两份在飞任务时，只退第一个任务两次，必须只影响它自己那一份，
	// 第二个任务的额度必须原封不动地还欠着——这才是 quota_charged 守卫
	// 真正在防的事情：不能因为重复处理任务 A，就把任务 B 尚未退的额度
	// 也搭进去退掉。
	idA := seedPendingTask(repo, "up-A", time.Now())
	seedPendingTask(repo, "up-B", time.Now())
	require.Equal(t, 2, repo.quotaUsedFor(1), "两个已计费任务，quotaUsed 应该是 2")

	require.NoError(t, repo.RefundQuotaForTask(context.Background(), idA))
	assert.Equal(t, 1, repo.quotaUsedFor(1), "第一次退款只应该退掉任务 A 自己的那一份")
	assert.False(t, repo.get(idA).QuotaCharged)

	require.NoError(t, repo.RefundQuotaForTask(context.Background(), idA))
	assert.Equal(t, 1, repo.quotaUsedFor(1), "第二次对同一个任务的退款必须是无操作——不能连带把任务 B 尚未退的额度也退掉")
}

// 超时终止也要退款。
func TestPoller_Timeout_RefundsQuota(t *testing.T) {
	repo := newFakeTaskRepo()
	old := time.Now().Add(-31 * time.Minute)
	id := seedPendingTask(repo, "up-1", old)
	require.Equal(t, 1, repo.quotaUsedFor(1))

	pv := newScriptedVideoProvider(map[string][]scriptedPoll{})
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	p.PollOnce(context.Background())

	got := repo.get(id)
	assert.Equal(t, generationmodel.StatusFailed, got.Status)
	require.NotNil(t, got.ErrorCode)
	assert.Equal(t, apperr.ErrTaskTimeout.Code(), *got.ErrorCode)
	assert.False(t, got.QuotaCharged, "超时判定也必须把 quota_charged 置 false")
	assert.Equal(t, 0, repo.quotaUsedFor(1), "超时判定必须退回额度")
}

// 成功的任务不退款。
func TestPoller_Succeeded_DoesNotRefund(t *testing.T) {
	repo := newFakeTaskRepo()
	id := seedPendingTask(repo, "up-1", time.Now())
	require.Equal(t, 1, repo.quotaUsedFor(1))

	pv := newScriptedVideoProvider(map[string][]scriptedPoll{
		"up-1": {succeeded("https://cdn.example.com/v.mp4")},
	})
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	p.PollOnce(context.Background())

	got := repo.get(id)
	assert.Equal(t, generationmodel.StatusSucceeded, got.Status)
	assert.True(t, got.QuotaCharged, "成功的任务不应该被退款，quota_charged 应保持 true")
	assert.Equal(t, 1, repo.quotaUsedFor(1), "成功的任务不应该退回额度")
}

func TestPoller_TaskDeletedMidPoll_RunningResponse_DoesNotCrash(t *testing.T) {
	repo := newFakeTaskRepo()
	id := seedPendingTask(repo, "up-1", time.Now())

	pv := &deleteOnPollProvider{repo: repo, id: id, result: &provider.TaskResult{
		Status: "RUNNING",
		Raw:    map[string]any{"output": map[string]any{"task_status": "RUNNING"}},
	}}
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), noopArchiver{})

	assert.NotPanics(t, func() {
		p.PollOnce(context.Background())
	})

	repo.mu.Lock()
	_, exists := repo.tasks[id]
	repo.mu.Unlock()
	assert.False(t, exists)
}

// ── 结果归档接线 ──────────────────────────────────────────────────────

// 视频轮询到 SUCCEEDED 时，写进 generation_tasks 的必须已经是 OSS URL，
// 不能有上游 URL 残留——与图片侧同一条要求：库里从第一次写入起就只有最终
// URL，不存在「先存上游、再更新成 OSS」的中间态。
func TestPoller_ArchivesResultBeforeWritingRow(t *testing.T) {
	repo := newFakeTaskRepo()
	id := seedPendingTask(repo, "up-1", time.Now())

	const upstreamURL = "https://dashscope-result-sh.oss-cn-shanghai.aliyuncs.com/v.mp4"
	pv := newScriptedVideoProvider(map[string][]scriptedPoll{
		"up-1": {succeeded(upstreamURL)},
	})
	archiver := &scriptedArchiver{}
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), archiver)

	p.PollOnce(context.Background())

	require.Equal(t, 1, archiver.calls, "SUCCEEDED 必须触发一次归档")
	assert.Equal(t, id, archiver.taskID, "归档要拿到真实的 taskID（视频任务此刻已经有行了）")
	assert.Equal(t, []string{upstreamURL}, archiver.urls, "交给归档的是上游原始 URL")

	got := repo.get(id)
	assert.Equal(t, generationmodel.StatusSucceeded, got.Status)
	require.Len(t, got.ResultURLs, 1)
	assert.Contains(t, got.ResultURLs[0], "test-bucket.oss-cn-chengdu.aliyuncs.com")
	assert.NotContains(t, got.ResultURLs[0], "dashscope-result", "库里不允许残留上游 URL")
}

// 归档不可用（OSS 未配置 / 写入被拒）时，视频任务仍然是 SUCCEEDED、
// result_urls 退回上游 URL，且**不退配额**——上游已经真的出了视频、也真的
// 收了钱，归档失败不是生成失败。
func TestPoller_ArchiveUnavailable_StillSucceedsAndDoesNotRefund(t *testing.T) {
	repo := newFakeTaskRepo()
	id := seedPendingTask(repo, "up-1", time.Now())
	require.Equal(t, 1, repo.quotaUsedFor(1), "前置：在飞任务已经计过费")

	const upstreamURL = "https://dashscope-result-sh.oss-cn-shanghai.aliyuncs.com/v.mp4"
	pv := newScriptedVideoProvider(map[string][]scriptedPoll{
		"up-1": {succeeded(upstreamURL)},
	})
	archiver := &scriptedArchiver{fail: true}
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), archiver)

	p.PollOnce(context.Background())

	got := repo.get(id)
	assert.Equal(t, generationmodel.StatusSucceeded, got.Status, "归档失败不能把成功的视频翻成失败")
	assert.Equal(t, []string{upstreamURL}, got.ResultURLs)
	assert.True(t, got.QuotaCharged, "归档失败不得触发退款")
	assert.Equal(t, 1, repo.quotaUsedFor(1), "配额必须照扣不退")
}

// 非 SUCCEEDED 的终态（FAILED/CANCELED）不该有任何归档动作：没有结果可归档。
func TestPoller_FailedTask_DoesNotArchive(t *testing.T) {
	repo := newFakeTaskRepo()
	seedPendingTask(repo, "up-1", time.Now())

	pv := newScriptedVideoProvider(map[string][]scriptedPoll{"up-1": {ok("FAILED")}})
	archiver := &scriptedArchiver{}
	p := worker.New(repo, defaultFakeSettings(), fixedVideoFactory(pv), archiver)

	p.PollOnce(context.Background())

	assert.Equal(t, 0, archiver.calls, "没有出结果的任务不该走归档")
}
