// Package ossx wraps the aliyun OSS SDK (object storage) and STS temporary
// credential machinery behind two narrow interfaces — Store (put + sign) and
// AssumeRoleClient (STS AssumeRole, see credentials.go) — so callers
// (internal/service/upload.go) and tests never depend on the concrete SDK
// types directly.
//
// This is a port of server.js's OSS_CONFIG / getSTSCredentials / getOSSClient
// (see docs/superpowers/plans/2026-07-19-generation-core.md, Task 9), with
// one deliberate fix: the STS credential cache is now guarded by
// golang.org/x/sync/singleflight so concurrent large uploads landing on a
// cold cache share a single AssumeRole call instead of each firing their
// own — the old server.js stsCache (server.js:105) had no such guard.
package ossx

import (
	"context"
	"time"
)

// SignedURLTTL is the fixed validity window for signed download URLs handed
// back to clients. It must match the object's Cache-Control (see
// cacheControl in client.go) and the UI's "24 小时内有效" promise
// (server.js:296 `client.signatureUrl(objectKey, { expires: 86400 })`).
const SignedURLTTL = 24 * time.Hour

// Credentials is a resolved set of OSS access credentials. SecurityToken is
// empty in direct AK/SK mode and non-empty when obtained via STS AssumeRole.
// Expiration is only meaningful for STS credentials — it is the zero value
// for static AK/SK, which never expires on its own.
type Credentials struct {
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	Expiration      time.Time
}

// Store is the narrow object-store surface the upload service depends on:
// put bytes under a key, then hand back a time-limited signed URL. Kept to
// exactly these two operations (mirroring server.js's `client.put` +
// `client.signatureUrl`) so tests can inject a fake instead of ever
// touching real OSS.
type Store interface {
	// Put uploads body under key with the given content type. Implementations
	// must tag the object with a Cache-Control matching SignedURLTTL (see
	// Client.Put) so the object doesn't outlive every signed URL that could
	// ever point at it, nor expire from cache before one does.
	Put(ctx context.Context, key string, body []byte, contentType string) error
	// SignedURL returns an HTTPS GET URL for key, valid for SignedURLTTL.
	SignedURL(ctx context.Context, key string) (string, error)
}
