package service_test

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/chenhao/omnigen-ai/server/internal/model/catalog"
	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// fakeTaskRepo is an in-memory repository.TaskRepository shared by the
// generation_image and generation_video service tests (and, via a second
// implementation of the same interface in internal/worker, the poller
// tests). Every method here has real, if simplified, semantics — ClaimPending
// only returns PENDING/RUNNING rows ordered by CreatedAt like the real SQL
// does, UpdateStatus/UpdateResult mutate in place, ListForUser paginates and
// counts — because Task 12's worker tests exercise the full
// claim/poll/update state machine against this fake, not just Create.
type fakeTaskRepo struct {
	mu        sync.Mutex
	tasks     map[int64]*generationmodel.Task
	nextID    int64
	createErr error

	// quotaUsed mirrors just enough of the real users.quota_used column to
	// let RefundQuotaForTask model both of the real CTE's guards, not just
	// the quota_charged one — see that method's doc and the worker
	// package's identical field for why a single-charge setup can't tell
	// "guard present" apart from "guard absent".
	quotaUsed map[int64]int
}

func newFakeTaskRepo() *fakeTaskRepo {
	return &fakeTaskRepo{tasks: map[int64]*generationmodel.Task{}, nextID: 1}
}

// Create assigns the next id and timestamps. It only fills in CreatedAt/
// UpdatedAt when the caller left them zero, so worker tests can pre-set an
// old CreatedAt (to simulate a task that's been in flight for a while)
// before calling Create.
func (f *fakeTaskRepo) Create(_ context.Context, t *generationmodel.Task) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t.ID = f.nextID
	f.nextID++
	now := time.Now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	clone := *t
	f.tasks[t.ID] = &clone
	if t.QuotaCharged {
		if f.quotaUsed == nil {
			f.quotaUsed = map[int64]int{}
		}
		f.quotaUsed[t.UserID]++
	}
	return nil
}

func (f *fakeTaskRepo) GetByIDForUser(_ context.Context, id, userID int64) (*generationmodel.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok || t.UserID != userID {
		return nil, apperr.ErrTaskNotFound
	}
	clone := *t
	return &clone, nil
}

func (f *fakeTaskRepo) ListForUser(_ context.Context, userID int64, offset, limit int) ([]generationmodel.Task, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var owned []*generationmodel.Task
	for _, t := range f.tasks {
		if t.UserID == userID {
			owned = append(owned, t)
		}
	}
	sort.Slice(owned, func(i, j int) bool {
		if owned[i].CreatedAt.Equal(owned[j].CreatedAt) {
			return owned[i].ID > owned[j].ID
		}
		return owned[i].CreatedAt.After(owned[j].CreatedAt)
	})

	total := int64(len(owned))
	if offset >= len(owned) {
		return []generationmodel.Task{}, total, nil
	}
	end := offset + limit
	if end > len(owned) {
		end = len(owned)
	}
	out := make([]generationmodel.Task, 0, end-offset)
	for _, t := range owned[offset:end] {
		out = append(out, *t)
	}
	return out, total, nil
}

func (f *fakeTaskRepo) UpdateStatus(_ context.Context, id int64, status generationmodel.Status, errCode, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return apperr.ErrTaskNotFound
	}
	t.Status = status
	if errCode == "" {
		t.ErrorCode = nil
	} else {
		t.ErrorCode = &errCode
	}
	if errMsg == "" {
		t.ErrorMessage = nil
	} else {
		t.ErrorMessage = &errMsg
	}
	t.UpdatedAt = time.Now()
	return nil
}

func (f *fakeTaskRepo) UpdateResult(_ context.Context, id int64, urls []string, usage map[string]any, note string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return apperr.ErrTaskNotFound
	}
	t.ResultURLs = urls
	t.Usage = usage
	if note == "" {
		t.Note = nil
	} else {
		t.Note = &note
	}
	t.Status = generationmodel.StatusSucceeded
	t.UpdatedAt = time.Now()
	return nil
}

func (f *fakeTaskRepo) ClaimPending(_ context.Context, limit int) ([]generationmodel.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var pending []*generationmodel.Task
	for _, t := range f.tasks {
		if t.IsInFlight() {
			pending = append(pending, t)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].CreatedAt.Equal(pending[j].CreatedAt) {
			return pending[i].ID < pending[j].ID
		}
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})
	if limit > 0 && len(pending) > limit {
		pending = pending[:limit]
	}
	out := make([]generationmodel.Task, 0, len(pending))
	for _, t := range pending {
		out = append(out, *t)
	}
	return out, nil
}

func (f *fakeTaskRepo) DeleteForUser(_ context.Context, id, userID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok || t.UserID != userID {
		return apperr.ErrTaskNotFound
	}
	delete(f.tasks, id)
	return nil
}

func (f *fakeTaskRepo) DeleteAllForUser(_ context.Context, userID int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for id, t := range f.tasks {
		if t.UserID == userID {
			delete(f.tasks, id)
			n++
		}
	}
	return n, nil
}

// RefundQuotaForTask is unused by the generation_image/generation_video
// service tests in this file — those exercise QuotaService.Refund directly
// against *fakeUserRepo, not the task-scoped worker-side refund path (see
// internal/worker's own fakeTaskRepo for that, which the poller tests
// actually exercise). It's still implemented with the same fidelity as that
// worker fake — both the quota_charged gate on the flip and the
// quota_used > 0 floor on the decrement, matching the real CTE's two
// guards — purely so *fakeTaskRepo keeps satisfying repository.
// TaskRepository faithfully and doesn't silently pass a wrong assertion if
// a future test in this file starts relying on it for quota amounts.
func (f *fakeTaskRepo) RefundQuotaForTask(_ context.Context, taskID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[taskID]
	if !ok || !t.QuotaCharged {
		return nil
	}
	t.QuotaCharged = false
	t.UpdatedAt = time.Now()
	if f.quotaUsed[t.UserID] > 0 {
		f.quotaUsed[t.UserID]--
	}
	return nil
}

// quotaUsedFor is a test-only accessor for the per-user counter
// RefundQuotaForTask maintains — mirrors the worker package's identical
// helper.
func (f *fakeTaskRepo) quotaUsedFor(userID int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.quotaUsed[userID]
}

// get is a test-only accessor for a single task's current in-memory state
// (worker tests poll this after each cycle instead of round-tripping
// through GetByIDForUser, which requires knowing the owning userID).
func (f *fakeTaskRepo) get(id int64) (generationmodel.Task, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return generationmodel.Task{}, false
	}
	return *t, true
}

// all lists every task Create() has stored, in insertion order, for tests
// that want to assert exactly one row landed.
func (f *fakeTaskRepo) all() []*generationmodel.Task {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*generationmodel.Task, 0, len(f.tasks))
	for id := int64(1); id < f.nextID; id++ {
		if t, ok := f.tasks[id]; ok {
			out = append(out, t)
		}
	}
	return out
}

var _ repository.TaskRepository = (*fakeTaskRepo)(nil)

// legacyUnlimitedQuotaRepo exists ONLY so the ~25 pre-quota tests in this
// file — which pass arbitrary literal userIDs (42, 7, 99...) that were never
// "created" through repo.Create — keep compiling and passing unmodified. It
// wraps a *fakeUserRepo and lazily invents an unlimited-quota user for any
// id it hasn't seen before, on first ConsumeQuota call.
//
// This is a deliberate hole in fidelity: the real repository (and the plain
// *fakeUserRepo it mirrors) rejects an unseeded id with QUOTA_EXCEEDED —
// that collapsed-error behavior is pinned by
// TestFakeUserRepo_ConsumeQuota_MissingUserMatchesRealSemantics specifically
// so nothing quietly drifts away from it. This type exists to route around
// that guard for tests that predate quota and have no opinion about it, not
// to relax the guard itself.
//
// DO NOT use this for a quota-sensitive test. Any test that seeds a
// QuotaTotal, asserts on QuotaUsed, or otherwise cares whether quota
// enforcement actually ran must go through newImageServiceWithQuota, which
// uses a plain *fakeUserRepo instead — there, an unseeded userID fails
// loudly with QUOTA_EXCEEDED (or panics on a nil map lookup during a direct
// mutation) rather than silently getting unlimited credit. Routing a
// quota-sensitive test through this type would let it pass without ever
// exercising a limit — the same shape as the fakeUserRepo.Update bug this
// project already shipped once.
type legacyUnlimitedQuotaRepo struct {
	*fakeUserRepo
}

func (r *legacyUnlimitedQuotaRepo) ConsumeQuota(ctx context.Context, id int64) error {
	if _, ok := r.users[id]; !ok {
		r.users[id] = &usermodel.User{ID: id, Username: fmt.Sprintf("auto-%d", id)}
	}
	return r.fakeUserRepo.ConsumeQuota(ctx, id)
}

var _ repository.UserRepository = (*legacyUnlimitedQuotaRepo)(nil)

// imageProviderFunc lets a plain function satisfy provider.ImageProvider,
// mirroring optimizeProviderFunc in optimize_mock_test.go.
type imageProviderFunc func(ctx context.Context, req provider.ImageRequest) (*provider.ImageResult, error)

func (f imageProviderFunc) GenerateImage(ctx context.Context, req provider.ImageRequest) (*provider.ImageResult, error) {
	return f(ctx, req)
}

var _ provider.ImageProvider = imageProviderFunc(nil)

// recordingImageFactory is a scripted service.ImageProviderFactory: every
// call records the (protocol, credentials) it was invoked with and hands
// back a provider that returns a single fixed result/error, so tests can
// assert both "what got called" (protocol routing, credential plumbing)
// and "what came back" (persisted task shape) in one place.
type recordingImageFactory struct {
	calls int

	lastProtocol    catalog.Protocol
	lastAPIKey      string
	lastRegion      string
	lastWorkspaceID string
	lastEndpoint    string

	lastReq provider.ImageRequest

	result *provider.ImageResult
	err    error
}

func (f *recordingImageFactory) Factory() service.ImageProviderFactory {
	return func(protocol catalog.Protocol, apiKey, region, workspaceID, endpoint string) provider.ImageProvider {
		f.calls++
		f.lastProtocol = protocol
		f.lastAPIKey = apiKey
		f.lastRegion = region
		f.lastWorkspaceID = workspaceID
		f.lastEndpoint = endpoint
		return imageProviderFunc(func(_ context.Context, req provider.ImageRequest) (*provider.ImageResult, error) {
			f.lastReq = req
			if f.err != nil {
				return nil, f.err
			}
			return f.result, nil
		})
	}
}

// ── 结果归档 ──────────────────────────────────────────────────────────

// scriptedArchiver 是一个假的 service.ResultArchiver。
//
// 它刻意复刻了真实 ResultArchiveService 最关键的语义：Archive 不返回 error，
// 归档不了的那一条原样退回上游 URL，返回切片长度恒等于入参长度、顺序一一
// 对应。fail 让测试模拟「OSS 没配 / 网络抖 / 写入被拒」——那时的正确行为是
// 生成照样成功、库里存上游 URL，而不是把一次已经扣过配额的成功变成失败。
type scriptedArchiver struct {
	calls int
	// lastTaskID / lastURLs 记录调用现场，用来断言归档确实发生在写库之前
	// （拿到的是上游 URL）而不是之后。
	lastTaskID int64
	lastURLs   []string

	// fail 为 true 时整批归档失败，全部退回上游 URL。
	fail bool
	// failIndex 只让某一条失败，用来验证「逐条降级」不是「整批降级」。
	failIndex *int
	// prefix 是归档成功时替换出来的 URL 前缀，形状对齐 ossx 的 publicURL。
	prefix string
}

func newScriptedArchiver() *scriptedArchiver {
	return &scriptedArchiver{prefix: "https://test-bucket.oss-cn-chengdu.aliyuncs.com/results/"}
}

func (a *scriptedArchiver) Archive(_ context.Context, taskID int64, urls []string) []string {
	a.calls++
	a.lastTaskID = taskID
	a.lastURLs = append([]string(nil), urls...)

	out := make([]string, len(urls))
	copy(out, urls)
	if a.fail {
		return out
	}
	for i := range out {
		if a.failIndex != nil && *a.failIndex == i {
			continue
		}
		out[i] = fmt.Sprintf("%s%d/%d-deadbeef.png", a.prefix, taskID, i)
	}
	return out
}

var _ service.ResultArchiver = (*scriptedArchiver)(nil)
