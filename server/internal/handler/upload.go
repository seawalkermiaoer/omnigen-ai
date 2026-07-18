package handler

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// uploadFormField is the multipart field name the frontend sends the file
// under, matching server.js's `upload.single('file')` (server.js:254).
const uploadFormField = "file"

// maxSkippedPartBytes bounds how much of a *non*-file part findFilePart is
// willing to discard while scanning for the "file" field. Legitimate extra
// form fields are always tiny; the cap exists purely so a request that
// smuggles an oversized/never-terminating part ahead of "file" can't force
// an unbounded read — see drainBoundedly's doc comment for the mechanism
// this closes off.
const maxSkippedPartBytes = 1 << 20 // 1MiB

// UploadHandler binds POST /api/upload (any logged-in user) to
// service.UploadService.
type UploadHandler struct {
	uploads *service.UploadService
}

func NewUploadHandler(uploads *service.UploadService) *UploadHandler {
	return &UploadHandler{uploads: uploads}
}

// Upload streams the multipart "file" field into memory itself, deliberately
// not using gin's c.FormFile/c.Request.FormFile — those call
// ParseMultipartForm, which has no size cap of its own and would let an
// attacker force the process to buffer an arbitrarily large body before any
// size check runs. Instead this reads the target part through
// io.LimitReader(part, UploadHardLimitBytes+1): reading stops the instant
// the hard limit is crossed, so memory usage for the file is bounded by
// UploadHardLimitBytes+1 regardless of how much data the client tries to
// send — the old system's multer had the same guarantee via
// limits.fileSize aborting mid-stream (server.js:76), not by buffering
// everything and checking .length afterward.
func (h *UploadHandler) Upload(c *gin.Context) {
	// 上传者身份目前只用于鉴权（路由层要求已登录）——上传结果本身不落库，
	// 与具体用户绑定要等到 Task 10 之后 generation_tasks 落地才有意义。
	if _, ok := middleware.UserIDFrom(c); !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}

	reader, err := c.Request.MultipartReader()
	if err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}

	part, err := findFilePart(reader)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	// Deliberately no `defer part.Close()` here. mime/multipart.Part.Close
	// is `io.Copy(io.Discard, p)` — it drains whatever of the part is still
	// unread, with no bound of its own. For a well-behaved upload that's
	// fine (there's nothing left to drain once ReadAll below hits real
	// EOF), but for an oversized/adversarial upload that hit the
	// LimitReader cap, calling Close() would happily keep pulling bytes
	// from the client forever — silently reintroducing the exact unbounded
	// read this handler exists to prevent. Neither this function nor
	// findFilePart ever calls reader.NextPart() again after this point, so
	// there is nothing that actually needs `part` closed.

	contentType := part.Header.Get("Content-Type")

	limited := io.LimitReader(part, service.UploadHardLimitBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	if len(body) > service.UploadHardLimitBytes {
		middleware.Fail(c, apperr.ErrUploadTooLarge)
		return
	}

	result, err := h.uploads.Upload(c.Request.Context(), service.UploadInput{
		Filename:    part.FileName(),
		ContentType: contentType,
		Body:        body,
	})
	if err != nil {
		middleware.Fail(c, err)
		return
	}

	c.JSON(http.StatusOK, common.OK(uploadResponse{
		URL:  result.URL,
		Key:  result.Key,
		Size: result.Size,
	}))
}

// findFilePart scans the multipart stream for the first part named
// uploadFormField with a filename set (i.e. an actual file field, not a
// plain form value). Every other part is discarded via drainBoundedly
// before looping to the next NextPart() call.
//
// This does NOT rely on mime/multipart.Reader's own part-skipping: calling
// NextPart() again internally calls the *previous* part's Close(), which is
// the same unbounded io.Copy(io.Discard, p) discussed in Upload's comment —
// a stray non-file part with unbounded content would hang this loop just as
// surely as the actual file part would. drainBoundedly consumes (and caps)
// the skipped part itself first, so by the time NextPart() runs its own
// Close() there's nothing left for it to read.
func findFilePart(reader *multipart.Reader) (*multipart.Part, error) {
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return nil, apperr.ErrUploadFileMissing
		}
		if err != nil {
			return nil, apperr.ErrValidation.Wrap(err)
		}
		if part.FormName() == uploadFormField && part.FileName() != "" {
			return part, nil
		}
		if err := drainBoundedly(part); err != nil {
			return nil, err
		}
	}
}

// drainBoundedly discards a part we're not interested in, capped at
// maxSkippedPartBytes so a part that never reaches its terminating boundary
// can't force an unbounded read.
func drainBoundedly(part *multipart.Part) error {
	n, err := io.CopyN(io.Discard, part, maxSkippedPartBytes+1)
	if err != nil && err != io.EOF {
		return apperr.ErrValidation.Wrap(err)
	}
	if n > maxSkippedPartBytes {
		return apperr.ErrValidation.Wrap(fmt.Errorf("表单字段 %q 超出预期大小", part.FormName()))
	}
	return nil
}

// uploadResponse is POST /api/upload's success payload.
type uploadResponse struct {
	URL  string `json:"url"`
	Key  string `json:"key,omitempty"`
	Size int64  `json:"size"`
}
