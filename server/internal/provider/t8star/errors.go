package t8star

import (
	"net/http"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

// Error codes returned by Client.GenerateImage. All three mirror a distinct
// failure branch in the old server.js t8star handler.
const (
	// CodeUpstreamHTTP: t8star answered with an HTTP status >= 400.
	CodeUpstreamHTTP = "T8STAR_UPSTREAM_HTTP_ERROR"
	// CodeUpstreamError: t8star answered 200 OK but the body carries an
	// OpenAI-shaped `error` object.
	CodeUpstreamError = "T8STAR_UPSTREAM_ERROR"
	// CodeNoImageResult: the response parsed cleanly but no markdown image
	// link could be found in the assistant's reply.
	CodeNoImageResult = "T8STAR_NO_IMAGE_RESULT"
)

var (
	// ErrUpstreamHTTP is the sentinel for CodeUpstreamHTTP. Actual errors
	// returned from GenerateImage carry the real upstream status code (via
	// newUpstreamHTTPError) — old server.js forwards t8r.status verbatim
	// rather than collapsing it to a fixed code. errors.Is still matches
	// against this sentinel because *apperr.AppError.Is compares by code,
	// not by HTTP status.
	ErrUpstreamHTTP = apperr.New(CodeUpstreamHTTP, http.StatusBadGateway)

	// ErrUpstreamError mirrors the old code's fixed `res.status(400)` for a
	// 200-with-error-body response.
	ErrUpstreamError = apperr.New(CodeUpstreamError, http.StatusBadRequest)

	// ErrNoImageResult mirrors the old code's fixed `res.status(502)` when
	// zero images could be parsed out of an otherwise-successful response.
	ErrNoImageResult = apperr.New(CodeNoImageResult, http.StatusBadGateway)
)

// newUpstreamHTTPError builds a CodeUpstreamHTTP error carrying the real
// upstream HTTP status, forwarded verbatim as old server.js did
// (`res.status(t8r.status)`), not collapsed to ErrUpstreamHTTP's status.
func newUpstreamHTTPError(status int, cause error) *apperr.AppError {
	return apperr.New(CodeUpstreamHTTP, status).Wrap(cause)
}
