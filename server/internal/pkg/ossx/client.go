package ossx

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// Config identifies the bucket/region an OSS Client talks to. Region is the
// short form used throughout the app (e.g. "oss-cn-chengdu", see
// settingmodel.KeyOSSRegion) — Client derives the full public endpoint from
// it, the same shape the old ali-oss JS SDK builds from its own `region`
// option.
type Config struct {
	Bucket string
	Region string
}

func (c Config) endpoint() string {
	return fmt.Sprintf("https://%s.aliyuncs.com", c.Region)
}

// publicURL is the永久可访问地址 of an object, valid only for objects that
// were uploaded public-read (PutPublic). It is deliberately not derived from
// endpoint(): the endpoint is region-scoped ("https://oss-cn-chengdu.aliyuncs.com")
// while the object URL is bucket-scoped ("https://<bucket>.oss-cn-chengdu.aliyuncs.com/<key>"),
// and gluing the bucket into the path instead of the host yields a URL that
// silently 404s.
func (c Config) publicURL(key string) string {
	return fmt.Sprintf("https://%s.%s.aliyuncs.com/%s", c.Bucket, c.Region, key)
}

// cacheControl is applied to every object Put uploads. Its max-age matches
// SignedURLTTL — the object's cache lifetime tracks the signed URL's
// validity window (server.js:294 `Cache-Control: public, max-age=86400`).
const cacheControl = "public, max-age=86400"

// credentialsFunc resolves the credentials to use for the next OSS call.
// Direct mode (see NewDirectClient) returns a fixed pair; STS mode (see
// NewSTSClient) delegates to a CredentialCache.
type credentialsFunc func(ctx context.Context) (Credentials, error)

// Client is the production Store implementation, wrapping
// github.com/aliyun/aliyun-oss-go-sdk. It rebuilds the underlying SDK bucket
// handle on every call from whatever credentialsFunc currently returns —
// mirroring server.js's getOSSClient(), which constructs a fresh `new
// OSS(...)` per request rather than caching the OSS client instance (only
// the STS credentials themselves are cached, in CredentialCache).
type Client struct {
	cfg   Config
	creds credentialsFunc

	// putter is a test-only seam (nil in production, in which case the real
	// SDK bucket handle is used). It exists because PutPublic's load-bearing
	// property — the caller's io.Reader is handed to the SDK untouched, so a
	// 40MB video never lands in memory — is otherwise only observable behind
	// a real network call. See client_public_test.go.
	putter func(ctx context.Context) (objectPutter, error)
}

// objectPutter is the one SDK operation PutPublic needs, narrowed to an
// interface so a test can stand in for *oss.Bucket.
type objectPutter interface {
	PutObject(objectKey string, reader io.Reader, options ...oss.Option) error
}

// NewDirectClient builds a Client using a fixed, non-expiring AccessKeyID/
// Secret pair — server.js's "direct AK/SK mode" (server.js:145-153),
// used when no RAM role ARN is configured.
func NewDirectClient(cfg Config, accessKeyID, accessKeySecret string) *Client {
	return &Client{
		cfg: cfg,
		creds: func(context.Context) (Credentials, error) {
			return Credentials{AccessKeyID: accessKeyID, AccessKeySecret: accessKeySecret}, nil
		},
	}
}

// NewSTSClient builds a Client that fetches (and, via the cache, reuses or
// refreshes) temporary credentials on every call — server.js's STS mode
// (server.js:132-144).
func NewSTSClient(cfg Config, cache *CredentialCache) *Client {
	return &Client{cfg: cfg, creds: cache.Get}
}

func (c *Client) bucket(ctx context.Context) (*oss.Bucket, error) {
	creds, err := c.creds(ctx)
	if err != nil {
		return nil, err
	}
	var opts []oss.ClientOption
	if creds.SecurityToken != "" {
		opts = append(opts, oss.SecurityToken(creds.SecurityToken))
	}
	client, err := oss.New(c.cfg.endpoint(), creds.AccessKeyID, creds.AccessKeySecret, opts...)
	if err != nil {
		return nil, err
	}
	return client.Bucket(c.cfg.Bucket)
}

// Put implements Store.
func (c *Client) Put(ctx context.Context, key string, body []byte, contentType string) error {
	bucket, err := c.bucket(ctx)
	if err != nil {
		return err
	}
	return bucket.PutObject(key, bytes.NewReader(body), oss.ContentType(contentType), oss.CacheControl(cacheControl))
}

// objectPutter resolves what PutPublic uploads through: the injected test
// seam if present, otherwise a freshly built SDK bucket handle.
func (c *Client) objectPutter(ctx context.Context) (objectPutter, error) {
	if c.putter != nil {
		return c.putter(ctx)
	}
	return c.bucket(ctx)
}

// PutPublic implements Store. Unlike Put it takes an io.Reader, which is
// passed straight through to the SDK (bucket.PutObject streams it into the
// request body) — generation results include videos of tens of MB, and
// buffering those into a []byte the way Put does would put whole-file copies
// on the heap for every concurrent archive.
//
// The public-read ACL is what makes the returned URL usable at all: it is
// stored in generation_tasks.result_urls and handed to users as a shareable
// link, so it must resolve without credentials. That trade-off is scoped to
// this method — Put/SignedURL (reference-image uploads) stay private +
// 24h-signed.
func (c *Client) PutPublic(ctx context.Context, key string, body io.Reader, contentType string) (string, error) {
	putter, err := c.objectPutter(ctx)
	if err != nil {
		return "", err
	}
	err = putter.PutObject(key, body,
		oss.ObjectACL(oss.ACLPublicRead),
		oss.ContentType(contentType),
		oss.CacheControl(cacheControl),
	)
	if err != nil {
		// 空字符串而不是「算得出来的 URL」：上传失败时对象根本不存在，
		// 返回地址只会让调用方把坏链写进库。
		return "", err
	}
	return c.cfg.publicURL(key), nil
}

// SignedURL implements Store. Signing is a pure local HMAC computation (no
// network call), so it can never fail because OSS is unreachable — only
// because the credentials/config used to build the bucket handle are bad.
func (c *Client) SignedURL(ctx context.Context, key string) (string, error) {
	bucket, err := c.bucket(ctx)
	if err != nil {
		return "", err
	}
	return bucket.SignURL(key, oss.HTTPGet, int64(SignedURLTTL.Seconds()))
}

var _ Store = (*Client)(nil)
