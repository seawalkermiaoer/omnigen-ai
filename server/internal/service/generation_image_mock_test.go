package service_test

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/chenhao/omnigen-ai/server/internal/model/catalog"
	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
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
