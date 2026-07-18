package worker_test

import (
	"context"
	"sort"
	"sync"
	"time"

	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// fakeTaskRepo is an in-memory repository.TaskRepository — same real
// semantics as the one in internal/service (ClaimPending only returns
// in-flight rows, UpdateStatus/UpdateResult mutate in place) but redeclared
// here because that one lives in an internal test package the worker tests
// can't import.
type fakeTaskRepo struct {
	mu     sync.Mutex
	tasks  map[int64]*generationmodel.Task
	nextID int64

	// claimCalls records every ClaimPending invocation's returned task ids,
	// letting shutdown tests assert exactly how many batches were claimed.
	claimCalls [][]int64
}

func newFakeTaskRepo() *fakeTaskRepo {
	return &fakeTaskRepo{tasks: map[int64]*generationmodel.Task{}, nextID: 1}
}

// seed inserts a task directly (bypassing any service-layer validation,
// since these are worker tests, not service tests) and returns its id.
// createdAt lets a test plant an already-old task without sleeping.
func (f *fakeTaskRepo) seed(t generationmodel.Task, createdAt time.Time) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextID
	f.nextID++
	t.ID = id
	t.CreatedAt = createdAt
	t.UpdatedAt = createdAt
	f.tasks[id] = &t
	return id
}

func (f *fakeTaskRepo) Create(_ context.Context, t *generationmodel.Task) error {
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

func (f *fakeTaskRepo) ListForUser(_ context.Context, _ int64, _, _ int) ([]generationmodel.Task, int64, error) {
	return nil, 0, nil
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

	ids := make([]int64, 0, len(pending))
	out := make([]generationmodel.Task, 0, len(pending))
	for _, t := range pending {
		out = append(out, *t)
		ids = append(ids, t.ID)
	}
	f.claimCalls = append(f.claimCalls, ids)
	return out, nil
}

// delete removes a task directly, bypassing any user-scoping check (worker
// tests use this to simulate a user deleting a task that's still in flight,
// mid-poll — see poller_test.go's deletion test).
func (f *fakeTaskRepo) delete(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tasks, id)
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

func (f *fakeTaskRepo) get(id int64) generationmodel.Task {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.tasks[id]
}

func (f *fakeTaskRepo) claimCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.claimCalls)
}

var _ repository.TaskRepository = (*fakeTaskRepo)(nil)

// fakeSettings is a trivial service.SettingReader — always returns fixed
// values, no crypto, no real database.
type fakeSettings map[settingmodel.Key]string

func (s fakeSettings) GetDecrypted(_ context.Context, key settingmodel.Key) (string, error) {
	return s[key], nil
}

var _ service.SettingReader = fakeSettings(nil)

func defaultFakeSettings() fakeSettings {
	return fakeSettings{
		settingmodel.KeyDashscopeAPIKey: "sk-worker-test-key",
		settingmodel.KeyRegion:          "cn-beijing",
	}
}

// scriptedVideoProvider hands back pre-programmed PollTask results/errors,
// keyed by upstream task id — each task id gets its own sequential script
// and its own cursor into it, so tests with two tasks in flight at once
// (the "counter is per task" test) can script each independently rather
// than sharing one call-ordered queue. CreateVideoTask is unused by worker
// tests (the worker only ever polls, never creates) and panics if called,
// so a test would fail loudly if the poller ever tried to create a task.
type scriptedVideoProvider struct {
	mu      sync.Mutex
	scripts map[string][]scriptedPoll
	cursor  map[string]int

	// blockUntil, when non-nil, makes every PollTask call block until the
	// channel is closed before consulting the script — used by the
	// graceful-shutdown test to hold a poll "in flight" while the context
	// gets canceled.
	blockUntil <-chan struct{}
}

type scriptedPoll struct {
	result *provider.TaskResult
	err    error
}

func newScriptedVideoProvider(scripts map[string][]scriptedPoll) *scriptedVideoProvider {
	return &scriptedVideoProvider{scripts: scripts, cursor: map[string]int{}}
}

func (p *scriptedVideoProvider) CreateVideoTask(context.Context, provider.VideoRequest) (string, error) {
	panic("scriptedVideoProvider: CreateVideoTask should never be called by the poller")
}

func (p *scriptedVideoProvider) PollTask(ctx context.Context, taskID string) (*provider.TaskResult, error) {
	if p.blockUntil != nil {
		select {
		case <-p.blockUntil:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	script := p.scripts[taskID]
	i := p.cursor[taskID]
	if i >= len(script) {
		panic("scriptedVideoProvider: ran out of scripted results for " + taskID)
	}
	p.cursor[taskID] = i + 1
	r := script[i]
	if r.result != nil && r.result.TaskID == "" {
		r.result.TaskID = taskID
	}
	return r.result, r.err
}

var _ provider.VideoProvider = (*scriptedVideoProvider)(nil)

func fixedVideoFactory(pv provider.VideoProvider) service.VideoProviderFactory {
	return func(string, string, string, string) provider.VideoProvider { return pv }
}

// alwaysRunningProvider answers every PollTask call with RUNNING,
// unconditionally and indefinitely — used by tests that exercise Run()'s
// ticking behavior itself (not the state machine), where the exact number
// of poll cycles that happen before the test cancels the context isn't
// deterministic.
type alwaysRunningProvider struct{}

func (alwaysRunningProvider) CreateVideoTask(context.Context, provider.VideoRequest) (string, error) {
	panic("alwaysRunningProvider: CreateVideoTask should never be called by the poller")
}

func (alwaysRunningProvider) PollTask(_ context.Context, taskID string) (*provider.TaskResult, error) {
	return &provider.TaskResult{
		TaskID: taskID,
		Status: "RUNNING",
		Raw:    map[string]any{"output": map[string]any{"task_status": "RUNNING"}},
	}, nil
}

var _ provider.VideoProvider = alwaysRunningProvider{}
