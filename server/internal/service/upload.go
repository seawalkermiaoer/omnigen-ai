// Package service — this file is the upload service: port of server.js's
// POST /api/upload-image (server.js:263-304). See
// docs/superpowers/plans/2026-07-19-generation-core.md, Task 9.
package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/ossx"
)

// UploadSizeThresholdBytes is the boundary between the two response shapes:
// <= this many bytes of the *original* upload returns an embedded base64
// data URI; more than this uploads to OSS and returns a signed URL.
// server.js:269 `const SIZE_THRESHOLD = 12 * 1024 * 1024` — comparison is
// `<=`, not `<` (server.js:280 `if (originalSize <= SIZE_THRESHOLD)`).
const UploadSizeThresholdBytes = 12 * 1024 * 1024

// UploadHardLimitBytes is the absolute cap on the original upload size,
// mirroring multer's `limits.fileSize` (server.js:76). Uploads over this
// are rejected outright, never partially processed.
const UploadHardLimitBytes = 50 * 1024 * 1024

// uploadMimeExt maps allowed MIME types to the file extension used when
// building an OSS object key, mirroring server.js:272 `extMap`. This map is
// also the MIME whitelist: any type not present here is rejected in
// Upload() before anything else runs (server.js's multer `fileFilter`,
// server.js:77-81).
var uploadMimeExt = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
	"image/bmp":  "bmp",
}

// UploadInput is a fully-buffered file ready for the size/MIME rules.
// Reading the multipart body and bounding it to UploadHardLimitBytes while
// still streaming is the handler's job (see internal/handler/upload.go) —
// by the time it reaches here Body is already the complete original bytes,
// so len(Body) is exactly the "original upload size" the 12MB/50MB
// thresholds compare against.
type UploadInput struct {
	Filename    string
	ContentType string
	Body        []byte
}

// UploadResult is what POST /api/upload returns. Key is empty for the
// base64 path (there is no OSS object).
type UploadResult struct {
	URL  string
	Key  string
	Size int64
}

// OSSResolver resolves the object store to use for an OSS upload, based on
// the live app_settings OSS configuration. Implementations return
// apperr.ErrUploadOSSNotConfigured when required settings are missing.
// UploadService only asks for a store when a file exceeds
// UploadSizeThresholdBytes, so a deployment with zero OSS setup still works
// fine for everyday small uploads — mirrors server.js's OSS_ENABLED gate
// (server.js:94-96, 286-288).
//
// This is the seam the task description asks for ("narrow interface for the
// object-store operations... inject a fake"): tests construct UploadService
// with a fakeOSSResolver that returns a fixed ossx.Store (or a fixed error)
// directly, bypassing app_settings/STS/singleflight entirely so the
// threshold/MIME/size logic below can be tested in isolation. The
// production resolver (settingsOSSResolver, upload_oss.go) is exercised
// separately in upload_oss_test.go.
type OSSResolver interface {
	Resolve(ctx context.Context) (ossx.Store, error)
}

// UploadService implements the upload rules: MIME whitelist, 50MB hard
// limit, 12MB base64/OSS split, OSS object key format, 24h signed URL.
type UploadService struct {
	resolver OSSResolver
}

// NewUploadService constructs the production UploadService: OSS
// credentials are resolved from app_settings on every >12MB upload (so an
// admin changing OSS settings takes effect without a restart), with STS
// credentials cached and singleflight-guarded across requests — see
// upload_oss.go.
func NewUploadService(settings SettingReader) *UploadService {
	return NewUploadServiceWithResolver(newSettingsOSSResolver(settings))
}

// NewUploadServiceWithResolver allows tests (and upload_oss_test.go's own
// resolver tests) to substitute the OSSResolver — mirrors
// NewSettingServiceWithTesters's fake-injection pattern (setting.go).
func NewUploadServiceWithResolver(resolver OSSResolver) *UploadService {
	return &UploadService{resolver: resolver}
}

// Upload implements the rules in this exact order:
//  1. MIME whitelist (400) — cheapest check, and matches server.js running
//     multer's fileFilter before anything else touches the file.
//  2. 50MB hard limit (413).
//  3. <=12MB -> base64 data URI.
//  4. >12MB -> resolve an OSS store (may fail with
//     ErrUploadOSSNotConfigured), build an object key, Put, then SignedURL.
func (s *UploadService) Upload(ctx context.Context, in UploadInput) (*UploadResult, error) {
	ext, ok := uploadMimeExt[in.ContentType]
	if !ok {
		return nil, apperr.ErrUploadUnsupportedType.Wrap(
			fmt.Errorf("不支持的文件类型: %q，仅支持 image/jpeg、image/png、image/webp、image/bmp", in.ContentType))
	}
	if ext == "" {
		// Defensive fallback mirroring server.js:273 `extMap[mimetype] ||
		// 'jpg'` — unreachable today since every key in uploadMimeExt maps
		// to a non-empty extension, kept for parity with the old system.
		ext = "jpg"
	}

	originalSize := int64(len(in.Body))
	if originalSize > UploadHardLimitBytes {
		return nil, apperr.ErrUploadTooLarge.Wrap(
			fmt.Errorf("文件大小 %d 字节超过 %d 字节上限", originalSize, UploadHardLimitBytes))
	}

	if originalSize <= UploadSizeThresholdBytes {
		b64 := base64.StdEncoding.EncodeToString(in.Body)
		return &UploadResult{
			URL:  fmt.Sprintf("data:%s;base64,%s", in.ContentType, b64),
			Size: originalSize,
		}, nil
	}

	store, err := s.resolver.Resolve(ctx)
	if err != nil {
		return nil, err
	}

	key, err := newObjectKey(ext)
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("生成 OSS object key 失败: %w", err))
	}

	if err := store.Put(ctx, key, in.Body, in.ContentType); err != nil {
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("OSS 上传失败: %w", err))
	}
	url, err := store.SignedURL(ctx, key)
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("OSS 签名 URL 失败: %w", err))
	}

	return &UploadResult{URL: url, Key: key, Size: originalSize}, nil
}

// newObjectKey builds "omnigen-uploads/<epoch-ms>-<16 hex chars>.<ext>",
// matching server.js:291 exactly:
// `omnigen-uploads/${Date.now()}-${crypto.randomBytes(8).toString('hex')}.${ext}`
// — 8 random bytes hex-encode to exactly 16 characters.
func newObjectKey(ext string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("omnigen-uploads/%d-%s.%s", time.Now().UnixMilli(), hex.EncodeToString(buf), ext), nil
}
