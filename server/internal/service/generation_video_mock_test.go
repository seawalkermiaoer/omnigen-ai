package service_test

import (
	"context"

	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// videoProviderStub is a scripted provider.VideoProvider: CreateVideoTask
// always returns the same (id, err) pair, PollTask is unused by the service
// tests in this file (the worker package has its own poll-focused fakes).
type videoProviderStub struct {
	taskID string
	err    error
}

func (s videoProviderStub) CreateVideoTask(_ context.Context, _ provider.VideoRequest) (string, error) {
	return s.taskID, s.err
}

func (s videoProviderStub) PollTask(_ context.Context, _ string) (*provider.TaskResult, error) {
	return nil, nil
}

var _ provider.VideoProvider = videoProviderStub{}

// recordingVideoFactory is a scripted service.VideoProviderFactory —
// mirrors recordingImageFactory in generation_image_mock_test.go: every
// call records the credentials it was invoked with and the payload the
// service built, so tests can assert both "what got called with" and
// "what shape got sent".
type recordingVideoFactory struct {
	calls int

	lastAPIKey      string
	lastRegion      string
	lastWorkspaceID string
	lastEndpoint    string

	lastReq provider.VideoRequest

	taskID string
	err    error
}

func (f *recordingVideoFactory) Factory() service.VideoProviderFactory {
	return func(apiKey, region, workspaceID, endpoint string) provider.VideoProvider {
		f.calls++
		f.lastAPIKey = apiKey
		f.lastRegion = region
		f.lastWorkspaceID = workspaceID
		f.lastEndpoint = endpoint
		return recordingVideoProvider{factory: f}
	}
}

// recordingVideoProvider records the request it was called with into the
// owning factory (rather than into itself) so the factory's lastReq stays
// accessible to the test after CreateVideoTask returns.
type recordingVideoProvider struct {
	factory *recordingVideoFactory
}

func (p recordingVideoProvider) CreateVideoTask(_ context.Context, req provider.VideoRequest) (string, error) {
	p.factory.lastReq = req
	if p.factory.err != nil {
		return "", p.factory.err
	}
	return p.factory.taskID, nil
}

func (p recordingVideoProvider) PollTask(_ context.Context, _ string) (*provider.TaskResult, error) {
	return nil, nil
}

var _ provider.VideoProvider = recordingVideoProvider{}

// panicVideoProvider always panics from CreateVideoTask — used by
// TestCreateVideoTask_ProviderPanics_StillRefundsQuota to prove the quota
// defer's recover()-then-repanic path, mirroring imageProviderFunc's panic
// case in generation_image_mock_test.go.
type panicVideoProvider struct{}

func (panicVideoProvider) CreateVideoTask(context.Context, provider.VideoRequest) (string, error) {
	panic("boom: provider 内部崩溃")
}

func (panicVideoProvider) PollTask(context.Context, string) (*provider.TaskResult, error) {
	return nil, nil
}

var _ provider.VideoProvider = panicVideoProvider{}
