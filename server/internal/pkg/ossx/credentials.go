package ossx

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// refreshMargin is how far ahead of the real expiry a cached STS credential
// is treated as stale, so a request doesn't start an upload with a token
// that dies mid-flight. Matches server.js:109
// (`now < stsCache.expiresAt - 60_000`).
const refreshMargin = 60 * time.Second

// AssumeRoleClient is the narrow STS surface CredentialCache needs — not the
// full generated alibabacloud STS SDK client — so tests can inject a fake
// that counts calls without any network I/O. The production implementation
// is sts.go's stsAssumeRoleClient.
type AssumeRoleClient interface {
	AssumeRole(ctx context.Context, roleArn, sessionName string, durationSeconds int64) (Credentials, error)
}

// CredentialCache caches STS credentials obtained via AssumeRole, treating
// them as stale refreshMargin before their real expiry, and de-duplicating
// concurrent refreshes with singleflight.
//
// This directly fixes a bug in the old system: server.js's stsCache
// (server.js:105) was a bare struct with no concurrency guard — two
// simultaneous >12MB uploads landing on a cold cache would both call
// AssumeRole. Here, concurrent callers that all miss the cache share exactly
// one in-flight AssumeRole call.
type CredentialCache struct {
	client          AssumeRoleClient
	roleArn         string
	sessionPrefix   string
	durationSeconds int64

	mu     sync.Mutex
	cached Credentials
	valid  bool

	group singleflight.Group
}

// NewCredentialCache constructs a cache bound to a single roleArn.
// durationSeconds is the STS token lifetime requested on every AssumeRole
// call (server.js's OSS_TOKEN_EXPIRE_SECONDS, default 3600). Session names
// are derived from sessionPrefix plus a timestamp, mirroring server.js's
// `omnigen-upload-${Date.now()}` (server.js:121).
func NewCredentialCache(client AssumeRoleClient, roleArn string, durationSeconds int64, sessionPrefix string) *CredentialCache {
	return &CredentialCache{
		client:          client,
		roleArn:         roleArn,
		sessionPrefix:   sessionPrefix,
		durationSeconds: durationSeconds,
	}
}

// Get returns cached credentials if they are still valid outside the
// refresh margin, otherwise fetches fresh ones via AssumeRole. Concurrent
// callers that all miss the cache share exactly one AssumeRole call.
func (c *CredentialCache) Get(ctx context.Context) (Credentials, error) {
	if creds, ok := c.peek(); ok {
		return creds, nil
	}

	v, err, _ := c.group.Do("assume-role", func() (any, error) {
		// Re-check under the singleflight key: the goroutine that lost the
		// race to become the leader here might have been queued behind a
		// leader that already refreshed the cache while this one was still
		// waiting to enter Do — no need to hit STS again in that case.
		if creds, ok := c.peek(); ok {
			return creds, nil
		}
		sessionName := fmt.Sprintf("%s-%d", c.sessionPrefix, time.Now().UnixMilli())
		creds, err := c.client.AssumeRole(ctx, c.roleArn, sessionName, c.durationSeconds)
		if err != nil {
			return Credentials{}, err
		}
		c.mu.Lock()
		c.cached = creds
		c.valid = true
		c.mu.Unlock()
		return creds, nil
	})
	if err != nil {
		return Credentials{}, err
	}
	return v.(Credentials), nil
}

// peek returns the cached credentials without triggering a refresh, and
// whether they're still usable (present and outside the refresh margin).
func (c *CredentialCache) peek() (Credentials, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.valid {
		return Credentials{}, false
	}
	if !time.Now().Before(c.cached.Expiration.Add(-refreshMargin)) {
		return Credentials{}, false
	}
	return c.cached, true
}
