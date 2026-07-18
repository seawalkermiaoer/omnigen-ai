package service_test

import (
	"context"
	"sort"
	"time"

	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// fakeSettingRepo 是内存版 SettingRepository，让 SettingService 测试无需
// 启动数据库。行为镜像真实 repository.settingRepository：Upsert 是覆盖式
// 写入，UpsertMany 原子写入一批（内存版天然原子，不需要额外处理）。
type fakeSettingRepo struct {
	rows map[string]settingmodel.Setting
}

func newFakeSettingRepo() *fakeSettingRepo {
	return &fakeSettingRepo{rows: map[string]settingmodel.Setting{}}
}

func (f *fakeSettingRepo) Get(_ context.Context, key string) (*settingmodel.Setting, error) {
	row, ok := f.rows[key]
	if !ok {
		return nil, apperr.ErrSettingNotFound
	}
	clone := row
	return &clone, nil
}

func (f *fakeSettingRepo) GetAll(_ context.Context) ([]settingmodel.Setting, error) {
	out := make([]settingmodel.Setting, 0, len(f.rows))
	for _, row := range f.rows {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (f *fakeSettingRepo) Upsert(_ context.Context, s *settingmodel.Setting) error {
	s.UpdatedAt = time.Now()
	f.rows[s.Key] = *s
	return nil
}

func (f *fakeSettingRepo) UpsertMany(_ context.Context, items []settingmodel.Setting) error {
	now := time.Now()
	for _, it := range items {
		it.UpdatedAt = now
		f.rows[it.Key] = it
	}
	return nil
}

var _ repository.SettingRepository = (*fakeSettingRepo)(nil)

// fakeConnectionTester 记录最近一次收到的凭证，供测试断言
// SettingService.TestConnection 确实把解密后的明文传给了它。
// 当 fail 非 nil 时把它作为 Test 的返回值；forbid=true 时一旦 Test 被调用
// 就让测试失败——用来断言"没配凭证时根本不会打网络请求"。
type fakeConnectionTester struct {
	calls []service.UpstreamCredentials
	fail  error
	t     interface {
		Fatalf(format string, args ...any)
	}
	forbid bool
}

func (f *fakeConnectionTester) Test(_ context.Context, creds service.UpstreamCredentials) error {
	if f.forbid && f.t != nil {
		f.t.Fatalf("ConnectionTester.Test 不应被调用，但收到了凭证 %+v", creds)
	}
	f.calls = append(f.calls, creds)
	return f.fail
}

var _ service.ConnectionTester = (*fakeConnectionTester)(nil)
