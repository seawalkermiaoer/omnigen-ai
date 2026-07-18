package service_test

import (
	"context"
	"sync"
	"time"

	"github.com/chenhao/omnigen-ai/server/internal/model/catalog"
	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// fakeTaskRepo 是内存版 repository.TaskRepository，只有 Create/
// GetByIDForUser 有真正的行为——ImageGenerationService 只用到 Create，其余
// 方法（ListForUser/UpdateStatus/UpdateResult/ClaimPending）留给 Task 12
// 的视频轮询 worker 测试使用，这里给出满足接口的最小实现即可，不代表真实
// 语义。
type fakeTaskRepo struct {
	mu        sync.Mutex
	tasks     map[int64]*generationmodel.Task
	nextID    int64
	createErr error
}

func newFakeTaskRepo() *fakeTaskRepo {
	return &fakeTaskRepo{tasks: map[int64]*generationmodel.Task{}, nextID: 1}
}

func (f *fakeTaskRepo) Create(_ context.Context, t *generationmodel.Task) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t.ID = f.nextID
	f.nextID++
	now := time.Now()
	t.CreatedAt = now
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

func (f *fakeTaskRepo) UpdateStatus(_ context.Context, _ int64, _ generationmodel.Status, _, _ string) error {
	return nil
}

func (f *fakeTaskRepo) UpdateResult(_ context.Context, _ int64, _ []string, _ map[string]any, _ string) error {
	return nil
}

func (f *fakeTaskRepo) ClaimPending(_ context.Context, _ int) ([]generationmodel.Task, error) {
	return nil, nil
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
