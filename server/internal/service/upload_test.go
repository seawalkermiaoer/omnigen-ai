package service_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/ossx"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// ── fakes ────────────────────────────────────────────────────────────────
//
// UploadService depends on service.OSSResolver -> ossx.Store, a narrow
// put+sign interface (see upload.go's doc comment on OSSResolver). Both
// fakes below let every test in this file exercise the threshold/MIME/size
// rules without ever constructing a real ossx.Client, let alone touching
// real OSS — exactly what the task asks for ("Define a narrow interface for
// the object-store operations... inject a fake").

type fakePut struct {
	key         string
	body        []byte
	contentType string
}

type fakeOSSStore struct {
	mu        sync.Mutex
	puts      []fakePut
	putErr    error
	signErr   error
	signedURL string
}

func (f *fakeOSSStore) Put(_ context.Context, key string, body []byte, contentType string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return f.putErr
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	f.puts = append(f.puts, fakePut{key: key, body: cp, contentType: contentType})
	return nil
}

func (f *fakeOSSStore) SignedURL(_ context.Context, key string) (string, error) {
	if f.signErr != nil {
		return "", f.signErr
	}
	if f.signedURL != "" {
		return f.signedURL, nil
	}
	return "https://signed.example.com/" + key, nil
}

var _ ossx.Store = (*fakeOSSStore)(nil)

type fakeOSSResolver struct {
	store        ossx.Store
	err          error
	resolveCalls int
}

func (f *fakeOSSResolver) Resolve(context.Context) (ossx.Store, error) {
	f.resolveCalls++
	if f.err != nil {
		return nil, f.err
	}
	return f.store, nil
}

var _ service.OSSResolver = (*fakeOSSResolver)(nil)

func repeatBytes(n int) []byte { return bytes.Repeat([]byte{0xAB}, n) }

// ── 12MB 阈值边界 ────────────────────────────────────────────────────────

func TestUploadService_ExactlyAtThreshold_UsesBase64(t *testing.T) {
	resolver := &fakeOSSResolver{store: &fakeOSSStore{}}
	svc := service.NewUploadServiceWithResolver(resolver)

	body := repeatBytes(service.UploadSizeThresholdBytes) // 恰好 12MB
	result, err := svc.Upload(context.Background(), service.UploadInput{ContentType: "image/png", Body: body})
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(result.URL, "data:image/png;base64,"))
	assert.Empty(t, result.Key)
	assert.EqualValues(t, len(body), result.Size)
	assert.Zero(t, resolver.resolveCalls, "阈值内的上传不应触碰 OSS resolver")
}

func TestUploadService_ThresholdPlusOneByte_UsesOSS(t *testing.T) {
	store := &fakeOSSStore{}
	svc := service.NewUploadServiceWithResolver(&fakeOSSResolver{store: store})

	body := repeatBytes(service.UploadSizeThresholdBytes + 1) // 12MB + 1 字节
	result, err := svc.Upload(context.Background(), service.UploadInput{ContentType: "image/jpeg", Body: body})
	require.NoError(t, err)

	assert.False(t, strings.HasPrefix(result.URL, "data:"), "超过阈值 1 字节也必须走 OSS，而不是 base64")
	assert.NotEmpty(t, result.Key)
	require.Len(t, store.puts, 1)
	assert.Equal(t, body, store.puts[0].body)
	assert.Equal(t, "image/jpeg", store.puts[0].contentType)
	assert.EqualValues(t, len(body), result.Size)
}

// ── 50MB 硬上限 ──────────────────────────────────────────────────────────

func TestUploadService_OverHardLimit_Returns413(t *testing.T) {
	svc := service.NewUploadServiceWithResolver(&fakeOSSResolver{})

	body := repeatBytes(service.UploadHardLimitBytes + 1)
	_, err := svc.Upload(context.Background(), service.UploadInput{ContentType: "image/png", Body: body})
	require.Error(t, err)

	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.ErrUploadTooLarge.Code(), appErr.Code())
	assert.Equal(t, http.StatusRequestEntityTooLarge, appErr.HTTPStatus())
}

func TestUploadService_ExactlyAtHardLimit_Allowed(t *testing.T) {
	store := &fakeOSSStore{}
	svc := service.NewUploadServiceWithResolver(&fakeOSSResolver{store: store})

	body := repeatBytes(service.UploadHardLimitBytes) // 恰好 50MB，必须放行
	_, err := svc.Upload(context.Background(), service.UploadInput{ContentType: "image/png", Body: body})
	require.NoError(t, err)
}

// ── MIME 白名单 ──────────────────────────────────────────────────────────

func TestUploadService_AllowedMimeTypes_Accepted(t *testing.T) {
	cases := map[string]string{
		"image/jpeg": "jpg",
		"image/png":  "png",
		"image/webp": "webp",
		"image/bmp":  "bmp",
	}
	for mime, ext := range cases {
		t.Run(mime, func(t *testing.T) {
			store := &fakeOSSStore{}
			svc := service.NewUploadServiceWithResolver(&fakeOSSResolver{store: store})

			body := repeatBytes(service.UploadSizeThresholdBytes + 10)
			result, err := svc.Upload(context.Background(), service.UploadInput{ContentType: mime, Body: body})
			require.NoError(t, err)
			require.Len(t, store.puts, 1)
			assert.True(t, strings.HasSuffix(result.Key, "."+ext), "key=%q 应以 .%s 结尾", result.Key, ext)
		})
	}
}

func TestUploadService_DisallowedMime_RejectedWithClearCode(t *testing.T) {
	svc := service.NewUploadServiceWithResolver(&fakeOSSResolver{})

	_, err := svc.Upload(context.Background(), service.UploadInput{ContentType: "application/pdf", Body: []byte("not an image")})
	require.Error(t, err)

	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.ErrUploadUnsupportedType.Code(), appErr.Code())
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus())
}

func TestUploadService_DisallowedMime_TakesPriorityOverHardLimit(t *testing.T) {
	// 一个既超过 50MB、MIME 又不合法的请求：MIME 检查在最前面，
	// 不应该因为文件很大就先报"太大"掩盖掉"类型不支持"。
	svc := service.NewUploadServiceWithResolver(&fakeOSSResolver{})

	body := repeatBytes(service.UploadHardLimitBytes + 1)
	_, err := svc.Upload(context.Background(), service.UploadInput{ContentType: "video/mp4", Body: body})
	require.Error(t, err)

	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.ErrUploadUnsupportedType.Code(), appErr.Code())
}

// ── OSS object key 形状 ──────────────────────────────────────────────────

var objectKeyPattern = regexp.MustCompile(`^omnigen-uploads/[0-9]+-[0-9a-f]{16}\.(jpg|png|webp|bmp)$`)

func TestUploadService_ObjectKey_MatchesExpectedShape(t *testing.T) {
	store := &fakeOSSStore{}
	svc := service.NewUploadServiceWithResolver(&fakeOSSResolver{store: store})

	body := repeatBytes(service.UploadSizeThresholdBytes + 1)
	result, err := svc.Upload(context.Background(), service.UploadInput{ContentType: "image/png", Body: body})
	require.NoError(t, err)

	assert.Regexp(t, objectKeyPattern, result.Key)
}

func TestUploadService_ObjectKeys_AreUnique(t *testing.T) {
	store := &fakeOSSStore{}
	svc := service.NewUploadServiceWithResolver(&fakeOSSResolver{store: store})

	body := repeatBytes(service.UploadSizeThresholdBytes + 1)
	r1, err := svc.Upload(context.Background(), service.UploadInput{ContentType: "image/png", Body: body})
	require.NoError(t, err)
	r2, err := svc.Upload(context.Background(), service.UploadInput{ContentType: "image/png", Body: body})
	require.NoError(t, err)

	assert.NotEqual(t, r1.Key, r2.Key)
}

// ── OSS 未配置 ───────────────────────────────────────────────────────────

func TestUploadService_OSSNotConfigured_OversizedFile_ReturnsClearErrorNot500(t *testing.T) {
	resolver := &fakeOSSResolver{err: apperr.ErrUploadOSSNotConfigured.Wrap(errors.New("缺少 access key"))}
	svc := service.NewUploadServiceWithResolver(resolver)

	body := repeatBytes(service.UploadSizeThresholdBytes + 1)
	_, err := svc.Upload(context.Background(), service.UploadInput{ContentType: "image/png", Body: body})
	require.Error(t, err)

	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.ErrUploadOSSNotConfigured.Code(), appErr.Code())
	assert.NotEqual(t, apperr.ErrInternal.Code(), appErr.Code(), "必须是明确的错误码，不是泛化的 500")
	assert.Equal(t, http.StatusRequestEntityTooLarge, appErr.HTTPStatus())
}

func TestUploadService_SmallFile_NeverConsultsOSSResolver_EvenIfUnconfigured(t *testing.T) {
	resolver := &fakeOSSResolver{err: apperr.ErrUploadOSSNotConfigured}
	svc := service.NewUploadServiceWithResolver(resolver)

	body := repeatBytes(service.UploadSizeThresholdBytes) // 恰好在阈值内
	_, err := svc.Upload(context.Background(), service.UploadInput{ContentType: "image/png", Body: body})
	require.NoError(t, err, "阈值内的上传即便 OSS 未配置也必须正常工作")
	assert.Zero(t, resolver.resolveCalls)
}

// ── OSS Put/SignedURL 失败的透传 ─────────────────────────────────────────

func TestUploadService_OSSPutFailure_PropagatesAsInternalError(t *testing.T) {
	store := &fakeOSSStore{putErr: errors.New("connection reset")}
	svc := service.NewUploadServiceWithResolver(&fakeOSSResolver{store: store})

	body := repeatBytes(service.UploadSizeThresholdBytes + 1)
	_, err := svc.Upload(context.Background(), service.UploadInput{ContentType: "image/png", Body: body})
	require.Error(t, err)

	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.ErrInternal.Code(), appErr.Code())
}
