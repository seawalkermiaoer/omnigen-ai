package service

import (
	"context"
	"errors"
	"sync"

	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/ossx"
)

// SettingReader (defined in optimize.go, shared by every service that only
// needs decrypted app_settings values) is UploadService's dependency here
// too — narrowed to an interface so tests don't have to spin up the real
// SettingService (crypto, an encryption key, a SettingRepository) just to
// exercise OSS config resolution.

// defaultOSSBucket/defaultOSSRegion mirror server.js's OSS_CONFIG fallbacks
// (server.js:88-89 `process.env.OSS_BUCKET || 'trans-ai-cn'`,
// `process.env.OSS_REGION || 'oss-cn-chengdu'`) — now applied when the
// corresponding app_settings key is unset/empty rather than when an env var
// is unset.
const (
	defaultOSSBucket = "trans-ai-cn"
	defaultOSSRegion = "oss-cn-chengdu"

	// ossTokenExpireSeconds is the STS token lifetime requested on every
	// AssumeRole call, matching server.js's OSS_TOKEN_EXPIRE_SECONDS
	// default (server.js:91). Credentials now live in app_settings instead
	// of env vars (see docs/superpowers/plans/2026-07-19-generation-core.md,
	// Task 9), and app_settings has no slot for this rarely-changed knob —
	// unlike bucket/region/roleArn/AK/SK it isn't something the product
	// exposes to admins, so it stays a constant rather than growing a new
	// settingmodel.Key.
	ossTokenExpireSeconds = 3600

	// ossSTSSessionPrefix mirrors server.js:121
	// (`omnigen-upload-${Date.now()}`).
	ossSTSSessionPrefix = "omnigen-upload"
)

// settingsOSSResolver is the production OSSResolver: it reads OSS
// configuration from app_settings on every call (so admin edits via
// PUT /api/settings take effect without a restart) and decides between
// direct AK/SK mode and STS mode exactly like server.js's OSS_ENABLED /
// OSS_USE_STS (server.js:94-96).
//
// STS credentials must NOT be re-fetched on every call — that would defeat
// caching and hammer AssumeRole — so the CredentialCache itself is built
// once (lazily, on first STS-mode use) and reused across requests. It's
// rebuilt only if the roleArn/AccessKeyID pair actually changes, so an
// admin rotating OSS credentials still takes effect on the next upload
// rather than requiring a restart.
type settingsOSSResolver struct {
	settings SettingReader

	// newSTSClient is overridable in tests to avoid constructing a real STS
	// SDK client (which itself needs no network to construct, but pulling
	// in ossx.NewSTSAssumeRoleClient here would still couple this test path
	// to the real SDK's constructor behavior for no benefit).
	newSTSClient func(accessKeyID, accessKeySecret string) (ossx.AssumeRoleClient, error)

	mu       sync.Mutex
	cacheKey string // fingerprint of (roleArn, accessKeyID) the cache below is bound to
	cache    *ossx.CredentialCache
}

func newSettingsOSSResolver(settings SettingReader) *settingsOSSResolver {
	return &settingsOSSResolver{settings: settings, newSTSClient: ossx.NewSTSAssumeRoleClient}
}

// Resolve implements OSSResolver.
func (r *settingsOSSResolver) Resolve(ctx context.Context) (ossx.Store, error) {
	bucket, err := r.settings.GetDecrypted(ctx, settingmodel.KeyOSSBucket)
	if err != nil {
		return nil, err
	}
	if bucket == "" {
		bucket = defaultOSSBucket
	}
	region, err := r.settings.GetDecrypted(ctx, settingmodel.KeyOSSRegion)
	if err != nil {
		return nil, err
	}
	if region == "" {
		region = defaultOSSRegion
	}
	accessKeyID, err := r.settings.GetDecrypted(ctx, settingmodel.KeyOSSAccessKeyID)
	if err != nil {
		return nil, err
	}
	accessKeySecret, err := r.settings.GetDecrypted(ctx, settingmodel.KeyOSSAccessKeySecret)
	if err != nil {
		return nil, err
	}
	roleArn, err := r.settings.GetDecrypted(ctx, settingmodel.KeyOSSRoleArn)
	if err != nil {
		return nil, err
	}

	// server.js:94-95: `OSS_ENABLED = !!(accessKeyId && accessKeySecret &&
	// bucket)` — bucket always has a default above, so in practice this
	// gate is really about accessKeyID/accessKeySecret, same as upstream.
	if accessKeyID == "" || accessKeySecret == "" || bucket == "" {
		return nil, apperr.ErrUploadOSSNotConfigured.Wrap(
			errors.New("OSS 未配置（缺少 access key id / secret），无法处理超过 12MB 的上传"))
	}

	cfg := ossx.Config{Bucket: bucket, Region: region}

	if roleArn == "" {
		return ossx.NewDirectClient(cfg, accessKeyID, accessKeySecret), nil
	}

	cache, err := r.stsCredentialCache(roleArn, accessKeyID, accessKeySecret)
	if err != nil {
		return nil, err
	}
	return ossx.NewSTSClient(cfg, cache), nil
}

// stsCredentialCache returns the CredentialCache bound to (roleArn,
// accessKeyID), building (or rebuilding, if those values changed since the
// last call) it under a mutex so concurrent Resolve calls in STS mode can't
// each construct their own cache — that would silently reintroduce the
// unguarded-refresh bug the CredentialCache itself fixes.
func (r *settingsOSSResolver) stsCredentialCache(roleArn, accessKeyID, accessKeySecret string) (*ossx.CredentialCache, error) {
	key := roleArn + "|" + accessKeyID
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cache != nil && r.cacheKey == key {
		return r.cache, nil
	}
	stsClient, err := r.newSTSClient(accessKeyID, accessKeySecret)
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	r.cache = ossx.NewCredentialCache(stsClient, roleArn, ossTokenExpireSeconds, ossSTSSessionPrefix)
	r.cacheKey = key
	return r.cache, nil
}

var _ OSSResolver = (*settingsOSSResolver)(nil)
