// Package handler: download.go implements GET /api/download/:taskId/:index.
//
// This replaces server.js's `/api/download?url=...&filename=...`, which was
// an unauthenticated open proxy: it accepted any URL from anyone, followed
// redirects with no hop limit, and streamed back whatever it found — a
// textbook SSRF (point it at an internal address and it fetches it for you).
//
// The fix here isn't a patch on top of that shape, it's a different shape:
// the client never names a URL at all. It names a task and a result index;
// this handler is the only thing that ever reads task.ResultURLs and turns
// one of them into an outbound request. On top of that:
//   - login required, and ownership is checked via GetByIDForUser, which
//     folds "task belongs to someone else" into the same NOT_FOUND as
//     "task doesn't exist" — never FORBIDDEN, which would itself leak
//     whether the id exists (see repository/task.go's doc comment).
//   - redirects are capped at maxDownloadRedirects, and the host allowlist
//     is enforced on the initial URL *and* on every redirect hop via
//     http.Client.CheckRedirect — checking only the first URL is the classic
//     way this kind of guard gets bypassed.
//   - the filename handed back in Content-Disposition is assembled from
//     server-controlled fields (task mode/id/index) plus an extension that
//     is filtered down to bare ASCII alnum, so nothing derived from the
//     upstream URL or its Content-Type header can inject a path separator,
//     "..", or a CR/LF byte into the header value.
package handler

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// allowedResultHostSuffixes lists exactly the upstream domains this system
// ever produces generation results on:
//   - ".aliyuncs.com" — DashScope result URLs, and also every OSS bucket URL
//     this codebase can produce: ossx.Client always signs against
//     "<bucket>.<region>.aliyuncs.com" (see internal/pkg/ossx/client.go,
//     Config.endpoint), so "the configured OSS bucket host" is already a
//     subdomain of this suffix — no separate entry is needed for it.
//   - ".alicdn.com" — Aliyun's CDN fronting for the same result assets.
//   - ".aiproxy.vip" — t8star's *result* host, taken from the real fixture
//     in docs/superpowers/plans/2026-07-18-t8star-gpt-image-2.md
//     ("https://webstatic.aiproxy.vip/output/...png"). This is deliberately
//     NOT "ai.t8star.org", which is t8star's *API* host, not where it hosts
//     the generated images the client is asked to download.
//
// Each entry carries its own leading dot: matching is a suffix check that
// requires the dot to be present in the candidate host too, so
// "evil-aliyuncs.com" (which merely ends with the letters "aliyuncs.com",
// with no dot boundary before them) does not pass — a naive
// strings.Contains/HasSuffix-without-a-boundary check would let it through.
var allowedResultHostSuffixes = []string{
	".aliyuncs.com",
	".alicdn.com",
	".aiproxy.vip",
}

// IsAllowedResultHost is the production host filter. Tests substitute their
// own filter (see NewDownloadHandlerWithHostFilter) so an httptest.Server's
// loopback host can stand in for a real result host, while still exercising
// the exact same enforcement code path (initial URL + every redirect hop)
// that runs in production.
func IsAllowedResultHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	for _, suffix := range allowedResultHostSuffixes {
		if host == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// maxDownloadRedirects is the hop limit server.js never had. Following N
// redirects means CheckRedirect is invoked N times (once per hop, with `via`
// holding the requests made so far — length 1 on the first hop, length N on
// the Nth); rejecting once len(via) exceeds this constant means exactly
// maxDownloadRedirects hops are followed and the next one fails closed.
const maxDownloadRedirects = 3

const downloadTimeout = 60 * time.Second

// errTooManyRedirects / errRedirectHostNotAllowed are the sentinels
// CheckRedirect returns. They never reach the client directly — Download
// below folds any client.Do failure (network error, these two included)
// into apperr.ErrDownloadFailed — but keeping them as distinct errors makes
// the two failure modes discoverable in logs via errors.Is, and gives the
// unit tests something precise to assert on the client boundary if needed.
var (
	errTooManyRedirects       = errors.New("重定向跳数超过上限")
	errRedirectHostNotAllowed = errors.New("重定向目标主机不在白名单内")
)

// DownloadHandler wires GET /api/download/:taskId/:index. It does auth,
// path parsing, ownership lookup, host-allowlist enforcement, and streaming
// — there is no service layer beneath it because there is no business rule
// here beyond "fetch this one server-known URL and nothing else", which is
// exactly what makes the old open-proxy design replaceable by construction
// rather than by patching.
type DownloadHandler struct {
	tasks       repository.TaskRepository
	client      *http.Client
	hostAllowed func(host string) bool
}

// NewDownloadHandler builds the production handler, using
// IsAllowedResultHost as the only host filter.
func NewDownloadHandler(tasks repository.TaskRepository) *DownloadHandler {
	return NewDownloadHandlerWithHostFilter(tasks, IsAllowedResultHost)
}

// NewDownloadHandlerWithHostFilter is the test seam: it lets tests supply a
// filter that also accepts an httptest.Server's loopback host, so the SAME
// CheckRedirect closure built here — not a re-implementation of it — is what
// tests exercise for both the initial URL and every redirect hop.
func NewDownloadHandlerWithHostFilter(tasks repository.TaskRepository, hostAllowed func(host string) bool) *DownloadHandler {
	h := &DownloadHandler{tasks: tasks, hostAllowed: hostAllowed}
	h.client = &http.Client{
		Timeout: downloadTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxDownloadRedirects {
				return errTooManyRedirects
			}
			if !h.hostAllowed(req.URL.Hostname()) {
				return errRedirectHostNotAllowed
			}
			return nil
		},
	}
	return h
}

// Download handles GET /api/download/:taskId/:index.
func (h *DownloadHandler) Download(c *gin.Context) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}

	taskID, err := strconv.ParseInt(c.Param("taskId"), 10, 64)
	if err != nil || taskID <= 0 {
		middleware.Fail(c, apperr.ErrValidation.Wrap(fmt.Errorf("非法的 taskId: %q", c.Param("taskId"))))
		return
	}
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 {
		middleware.Fail(c, apperr.ErrValidation.Wrap(fmt.Errorf("非法的 index: %q", c.Param("index"))))
		return
	}

	// GetByIDForUser folds "task doesn't exist" and "task belongs to
	// someone else" into the same apperr.ErrTaskNotFound — see its doc
	// comment in repository/task.go. Nothing here re-derives a 403 from
	// some other signal; that would reintroduce the exact existence oracle
	// GetByIDForUser exists to avoid.
	task, err := h.tasks.GetByIDForUser(c.Request.Context(), taskID, userID)
	if err != nil {
		middleware.Fail(c, err)
		return
	}

	if index >= len(task.ResultURLs) {
		middleware.Fail(c, apperr.ErrValidation.Wrap(
			fmt.Errorf("index %d 超出结果范围（共 %d 个结果）", index, len(task.ResultURLs))))
		return
	}
	target := task.ResultURLs[index]

	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		middleware.Fail(c, apperr.ErrDownloadFailed.Wrap(fmt.Errorf("结果 URL 不合法: %q", target)))
		return
	}
	if !h.hostAllowed(parsed.Hostname()) {
		middleware.Fail(c, apperr.ErrDownloadFailed.Wrap(fmt.Errorf("结果主机不在白名单内: %q", parsed.Hostname())))
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, target, nil)
	if err != nil {
		middleware.Fail(c, apperr.ErrInternal.Wrap(err))
		return
	}
	req.Header.Set("User-Agent", "omnigen-web/1.0")

	resp, err := h.client.Do(req)
	if err != nil {
		middleware.Fail(c, apperr.ErrDownloadFailed.Wrap(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		middleware.Fail(c, apperr.ErrDownloadFailed.Wrap(fmt.Errorf("上游返回 HTTP %d", resp.StatusCode)))
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	contentLength := int64(-1)
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil {
			contentLength = n
		}
	}
	filename := resultFilename(task, index, target, contentType)

	c.DataFromReader(http.StatusOK, contentLength, contentType, resp.Body, map[string]string{
		"Content-Disposition": fmt.Sprintf(`attachment; filename="%s"`, filename),
	})
}

// contentTypeExtensions maps the handful of MIME types this system's
// upstreams actually return to a fixed, known-good extension — preferred
// over mime.ExtensionsByType, whose result for a type like "image/jpeg" is
// drawn from a larger, unordered candidate set (".jpe"/".jpeg"/".jpg") that
// isn't guaranteed to put the conventional extension first.
var contentTypeExtensions = map[string]string{
	"image/png":       "png",
	"image/jpeg":      "jpg",
	"image/jpg":       "jpg",
	"image/webp":      "webp",
	"image/gif":       "gif",
	"video/mp4":       "mp4",
	"video/quicktime": "mov",
	"video/webm":      "webm",
	"audio/mpeg":      "mp3",
	"audio/wav":       "wav",
}

// resultFilename derives the Content-Disposition filename from
// server-controlled data (task mode, task id, result index — never from
// user input) plus a best-effort extension. The extension is the only part
// sourced from attacker-reachable data (the upstream URL path and its
// Content-Type response header), so it alone goes through sanitizeExt,
// which keeps nothing but ASCII letters/digits — that structurally rules
// out path separators, "..", and CR/LF ending up in the header value.
// sanitizeFilename is then a second, whole-string defensive pass over the
// fully assembled name.
func resultFilename(task *generationmodel.Task, index int, sourceURL, contentType string) string {
	ext := extFromURL(sourceURL)
	if ext == "" {
		ext = extFromContentType(contentType)
	}
	if ext == "" {
		ext = "bin"
	}
	name := fmt.Sprintf("omnigen-%s-%d-%d.%s", task.Mode, task.ID, index, ext)
	return sanitizeFilename(name)
}

func extFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	base := path.Base(u.Path)
	dot := strings.LastIndexByte(base, '.')
	if dot < 0 || dot == len(base)-1 {
		return ""
	}
	return sanitizeExt(base[dot+1:])
}

func extFromContentType(contentType string) string {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if ext, ok := contentTypeExtensions[base]; ok {
		return ext
	}
	if exts, err := mime.ExtensionsByType(base); err == nil && len(exts) > 0 {
		return sanitizeExt(strings.TrimPrefix(exts[0], "."))
	}
	return ""
}

// sanitizeExt keeps only ASCII letters/digits (max 8 of them) — see
// resultFilename's doc comment for why this is the load-bearing sanitizer.
func sanitizeExt(ext string) string {
	var b strings.Builder
	for _, r := range ext {
		if b.Len() >= 8 {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}

// sanitizeFilename is the whole-string defensive pass described in
// resultFilename's doc comment.
func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"\r", "", "\n", "", "\"", "",
		"/", "_", "\\", "_", "..", "_",
	)
	name = replacer.Replace(name)
	if name == "" {
		name = "download"
	}
	return name
}
