package ossx_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/ossx"
)

// fakeSTS counts AssumeRole calls and never touches the network — it's the
// fake injected through ossx.AssumeRoleClient so CredentialCache can be
// tested without real STS credentials.
type fakeSTS struct {
	calls int32
	// delay, if non-zero, is slept inside AssumeRole before returning. Used
	// to widen the window during which concurrent callers can pile onto the
	// same singleflight call, so the dedup test isn't relying on lucky
	// goroutine scheduling.
	delay time.Duration
	resp  ossx.Credentials
	err   error
}

func (f *fakeSTS) AssumeRole(_ context.Context, _, _ string, _ int64) (ossx.Credentials, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.err != nil {
		return ossx.Credentials{}, f.err
	}
	return f.resp, nil
}

func (f *fakeSTS) callCount() int32 { return atomic.LoadInt32(&f.calls) }

var _ ossx.AssumeRoleClient = (*fakeSTS)(nil)

// ── 并发去重：N 个并发请求，冷缓存只应触发一次 AssumeRole ──────────────

func TestCredentialCache_SingleflightDedupesConcurrentRefresh(t *testing.T) {
	fake := &fakeSTS{
		delay: 100 * time.Millisecond,
		resp: ossx.Credentials{
			AccessKeyID: "ak", AccessKeySecret: "sk", SecurityToken: "token",
			Expiration: time.Now().Add(time.Hour),
		},
	}
	cache := ossx.NewCredentialCache(fake, "acs:ram::123:role/upload", 3600, "omnigen-upload")

	const n = 10
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]ossx.Credentials, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			creds, err := cache.Get(context.Background())
			results[i] = creds
			errs[i] = err
		}(i)
	}
	close(start) // release all n goroutines at once to force genuine overlap
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "goroutine %d", i)
		assert.Equal(t, fake.resp.AccessKeyID, results[i].AccessKeyID)
	}
	assert.EqualValues(t, 1, fake.callCount(),
		"%d 个并发请求撞上冷缓存，singleflight 应确保只有一次真正的 AssumeRole 调用", n)
}

// ── 缓存命中：有效期内的凭证被复用，不重新请求 ──────────────────────────

func TestCredentialCache_ReusesValidCachedCredentials(t *testing.T) {
	fake := &fakeSTS{
		resp: ossx.Credentials{
			AccessKeyID: "ak-1", AccessKeySecret: "sk-1",
			Expiration: time.Now().Add(time.Hour), // 远超 60s 刷新余量
		},
	}
	cache := ossx.NewCredentialCache(fake, "role-arn", 3600, "session")

	first, err := cache.Get(context.Background())
	require.NoError(t, err)
	second, err := cache.Get(context.Background())
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.EqualValues(t, 1, fake.callCount(), "有效期内的第二次 Get 不应触发新的 AssumeRole")
}

// ── 刷新余量：过期前 60 秒内的缓存被视为过期，触发刷新 ──────────────────

func TestCredentialCache_RefreshesWithinExpiryMargin(t *testing.T) {
	fake := &fakeSTS{
		resp: ossx.Credentials{
			AccessKeyID: "ak", AccessKeySecret: "sk",
			// 30s < 60s 的刷新余量：即便还没真正过期，也必须被当作陈旧数据。
			Expiration: time.Now().Add(30 * time.Second),
		},
	}
	cache := ossx.NewCredentialCache(fake, "role-arn", 3600, "session")

	_, err := cache.Get(context.Background())
	require.NoError(t, err)
	_, err = cache.Get(context.Background())
	require.NoError(t, err)

	assert.EqualValues(t, 2, fake.callCount(),
		"缓存凭证在 60s 刷新余量内应触发重新获取，而不是被当作仍然有效")
}

// ── AssumeRole 失败要透传，且不会把失败结果当成"已缓存" ──────────────────

func TestCredentialCache_PropagatesAssumeRoleError(t *testing.T) {
	wantErr := errors.New("sts: access denied")
	fake := &fakeSTS{err: wantErr}
	cache := ossx.NewCredentialCache(fake, "role-arn", 3600, "session")

	_, err := cache.Get(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
	assert.EqualValues(t, 1, fake.callCount())
}
