package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/ossx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// fakeUploadStore/fakeUploadResolver let these handler tests exercise the
// full HTTP path (multipart parsing, streaming bound, auth) without ever
// building a real ossx.Client — same seam upload_test.go uses at the
// service layer, redeclared here because the fakes in package service_test
// aren't visible from package handler_test.

type fakeUploadStore struct{ putCount int }

func (f *fakeUploadStore) Put(context.Context, string, []byte, string) error {
	f.putCount++
	return nil
}
func (f *fakeUploadStore) SignedURL(_ context.Context, key string) (string, error) {
	return "https://signed.example.com/" + key, nil
}

// PutPublic 必须在这里炸：参考图上传走的是「私有对象 + 24 小时签名 URL」，
// 一旦哪次重构让它改走 public-read，用户上传的参考图就变成了公网可访问，
// 这个 fake 是那条防线上的告警。
func (f *fakeUploadStore) PutPublic(context.Context, string, io.Reader, string) (string, error) {
	panic("参考图上传不得走 public-read 路径")
}

var _ ossx.Store = (*fakeUploadStore)(nil)

type fakeUploadResolver struct {
	store ossx.Store
	err   error
}

func (f *fakeUploadResolver) Resolve(context.Context) (ossx.Store, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.store, nil
}

var _ service.OSSResolver = (*fakeUploadResolver)(nil)

// newUploadTestEnv wires just enough of the real middleware chain (auth +
// error handling) around UploadHandler to test it as an HTTP endpoint,
// reusing memRepo/newMemRepo from auth_test.go (same package).
func newUploadTestEnv(t *testing.T, resolver service.OSSResolver) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := newMemRepo()
	jwtMgr := jwtx.NewManager("upload-handler-test-secret", time.Hour)

	hash, err := password.Hash("password123")
	require.NoError(t, err)
	u := &usermodel.User{
		Username: "alice", PasswordHash: hash, DisplayName: "alice",
		Role: usermodel.RoleUser, Status: usermodel.StatusActive,
	}
	require.NoError(t, repo.Create(context.Background(), u))
	token, err := jwtMgr.Generate(u.ID, u.Role)
	require.NoError(t, err)

	uploadSvc := service.NewUploadServiceWithResolver(resolver)
	uploadH := handler.NewUploadHandler(uploadSvc)

	r := gin.New()
	r.Use(middleware.Recovery(), middleware.ErrorHandler())
	authed := r.Group("/api", middleware.Auth(jwtMgr, repo))
	authed.POST("/upload", uploadH.Upload)

	return r, token
}

// multipartUpload builds a POST /api/upload request with a single "file"
// field, mirroring how the frontend's FormData does it.
func multipartUpload(t *testing.T, filename, contentType string, content []byte) (*http.Request, error) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="file"; filename="` + filename + `"`}
	if contentType != "" {
		h["Content-Type"] = []string{contentType}
	}
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(content); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req, nil
}

func decodeUploadResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok, "响应 data 应该是个对象，实际是 %#v", resp.Data)
	return data
}

func TestUploadHandler_SmallFile_ReturnsBase64DataURI(t *testing.T) {
	r, token := newUploadTestEnv(t, &fakeUploadResolver{store: &fakeUploadStore{}})

	content := bytes.Repeat([]byte{0xAB}, 1024)
	req, err := multipartUpload(t, "photo.png", "image/png", content)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())
	data := decodeUploadResponse(t, w)
	url, _ := data["url"].(string)
	assert.True(t, strings.HasPrefix(url, "data:image/png;base64,"))
}

func TestUploadHandler_LargeFile_UsesOSSStore(t *testing.T) {
	store := &fakeUploadStore{}
	r, token := newUploadTestEnv(t, &fakeUploadResolver{store: store})

	content := bytes.Repeat([]byte{0xCD}, service.UploadSizeThresholdBytes+1)
	req, err := multipartUpload(t, "big.jpg", "image/jpeg", content)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())
	data := decodeUploadResponse(t, w)
	url, _ := data["url"].(string)
	assert.True(t, strings.HasPrefix(url, "https://signed.example.com/"))
	assert.Equal(t, 1, store.putCount)
}

func TestUploadHandler_NoAuth_401(t *testing.T) {
	r, _ := newUploadTestEnv(t, &fakeUploadResolver{store: &fakeUploadStore{}})

	req, err := multipartUpload(t, "a.png", "image/png", []byte("x"))
	require.NoError(t, err)
	// 故意不带 Authorization header。

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUploadHandler_NoFilePart_400(t *testing.T) {
	r, token := newUploadTestEnv(t, &fakeUploadResolver{store: &fakeUploadStore{}})

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	require.NoError(t, w.WriteField("note", "no file here"))
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp common.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, apperr.ErrUploadFileMissing.Code(), resp.Code)
}

func TestUploadHandler_DisallowedMime_400(t *testing.T) {
	r, token := newUploadTestEnv(t, &fakeUploadResolver{store: &fakeUploadStore{}})

	req, err := multipartUpload(t, "doc.pdf", "application/pdf", []byte("%PDF-1.4"))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, apperr.ErrUploadUnsupportedType.Code(), resp.Code)
}

// infiniteReader emits an endless stream of 'A' bytes, counting how many
// have actually been pulled out of it. It never spawns a goroutine and does
// no buffering of its own — Read only ever produces what its caller asks
// for, synchronously, so if nobody keeps calling Read the stream just idles
// forever without leaking anything. That's exactly what lets
// TestUploadHandler_UnboundedAttackerStream_NeverBuffersPastHardLimit below
// simulate an attacker willing to send gigabytes without the test itself
// allocating gigabytes.
type infiniteReader struct{ n atomic.Int64 }

func (r *infiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'A'
	}
	r.n.Add(int64(len(p)))
	return len(p), nil
}

func (r *infiniteReader) produced() int64 { return r.n.Load() }

// TestUploadHandler_UnboundedAttackerStream_NeverBuffersPastHardLimit is the
// concrete demonstration of the self-review question "does the 50MB limit
// actually bound memory, or is it checked after buffering?": the client
// here is willing to send an unbounded number of bytes (infiniteReader never
// terminates), yet the handler's io.LimitReader(part,
// UploadHardLimitBytes+1) guarantees it only ever pulls a little over the
// hard limit before giving up and returning 413 — memory is bounded by the
// limit itself, not by how much the client tries to send.
func TestUploadHandler_UnboundedAttackerStream_NeverBuffersPastHardLimit(t *testing.T) {
	r, token := newUploadTestEnv(t, &fakeUploadResolver{store: &fakeUploadStore{}})

	const boundary = "omnigenuploadtestboundary"
	header := "--" + boundary + "\r\n" +
		`Content-Disposition: form-data; name="file"; filename="huge.png"` + "\r\n" +
		"Content-Type: image/png\r\n\r\n"

	src := &infiniteReader{}
	body := io.MultiReader(strings.NewReader(header), src)

	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code, "响应=%s", w.Body.String())
	// 64KB 余量覆盖 mime/multipart 内部扫描边界串时的 bufio 预读，
	// 核心断言是：实际拉取的字节数停在硬上限附近的一个常数范围内，
	// 而不是随攻击者愿意发送的数据量无限增长。
	assert.LessOrEqual(t, src.produced(), int64(service.UploadHardLimitBytes+65536),
		"handler 应该在硬上限附近停止读取，而不是被拖着无限缓冲")
}

// TestUploadHandler_UnboundedNonFilePart_RejectedInsteadOfHanging targets
// the other unbounded-drain path this handler has to guard against:
// mime/multipart.Reader.NextPart() closes (== unboundedly drains) the
// *previous* part internally whenever you call it again. A malicious
// request that puts a never-terminating non-"file" part ahead of the real
// file field would hang findFilePart's scanning loop forever if it relied
// on that automatic behavior (or on our own unbounded part.Close()) to skip
// past it — see drainBoundedly's doc comment. This proves the bounded skip
// actually kicks in: the request is rejected quickly instead of hanging.
func TestUploadHandler_UnboundedNonFilePart_RejectedInsteadOfHanging(t *testing.T) {
	r, token := newUploadTestEnv(t, &fakeUploadResolver{store: &fakeUploadStore{}})

	const boundary = "omnigenuploadtestboundary2"
	// 第一个 part 是一个从不终结的非 "file" 字段。
	junkHeader := "--" + boundary + "\r\n" +
		`Content-Disposition: form-data; name="junk"` + "\r\n\r\n"
	src := &infiniteReader{}

	req := httptest.NewRequest(http.MethodPost, "/api/upload", io.MultiReader(strings.NewReader(junkHeader), src))
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
	assert.LessOrEqual(t, src.produced(), int64(1<<20+65536),
		"扫描非 file 字段时也必须停在一个常数范围内，而不是无限排空")
}

func TestUploadHandler_OverHardLimit_413_WithoutBufferingWholeBody(t *testing.T) {
	r, token := newUploadTestEnv(t, &fakeUploadResolver{store: &fakeUploadStore{}})

	// 只需略超过硬上限即可触发 413；不构造一个巨大的请求体（那会让测试本身
	// 变慢/占内存），这里验证的是"超过上限即报错"的行为。
	// TestUploadHandler_UnboundedAttackerStream_NeverBuffersPastHardLimit
	// 单独验证了"读取本身不会无限缓冲"这条更强的保证。
	content := bytes.Repeat([]byte{0xEF}, service.UploadHardLimitBytes+1)
	req, err := multipartUpload(t, "huge.png", "image/png", content)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, apperr.ErrUploadTooLarge.Code(), resp.Code)
}

func TestUploadHandler_OSSNotConfigured_413WithClearCode(t *testing.T) {
	resolver := &fakeUploadResolver{err: apperr.ErrUploadOSSNotConfigured}
	r, token := newUploadTestEnv(t, resolver)

	content := bytes.Repeat([]byte{0x11}, service.UploadSizeThresholdBytes+1)
	req, err := multipartUpload(t, "big.webp", "image/webp", content)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, apperr.ErrUploadOSSNotConfigured.Code(), resp.Code)
}
