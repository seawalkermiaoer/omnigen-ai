// Package service — this file normalizes upstream provider errors before
// they reach a handler response, on the three generation-adjacent paths:
// ImageGenerationService.GenerateImage, VideoGenerationService.CreateVideoTask,
// OptimizeService.Optimize.
//
// Why here and not in internal/provider/{dashscope,t8star}: those two
// packages are a deliberate, faithful port of server.js's error shape,
// including forwarding the real upstream HTTP status verbatim
// (newUpstreamHTTPError in both packages — see their doc comments). That is
// exactly the behavior that created this bug: DashScope returns 401 for an
// invalid API key, the provider forwards that 401 as *our* AppError status,
// and web/src/api/client.ts's response interceptor treats any 401 as our own
// session expiring — logging the user out and discarding their form.
// /api/settings/test (service/setting.go's TestConnection) already avoids
// this by normalizing at the service boundary rather than trusting the
// tester's raw error; this file applies that same pattern to the generation
// paths. Keeping the provider packages emitting the raw upstream status is
// still valuable — it is what the setting.go connection tester and any
// future caller that genuinely wants upstream fidelity would want — so the
// normalization belongs one layer up, where "what does OUR API promise"
// actually gets decided, not baked into the provider client.
package service

import (
	"errors"
	"net/http"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/dashscope"
	"github.com/chenhao/omnigen-ai/server/internal/provider/t8star"
)

// normalizeUpstreamError maps a dashscope/t8star provider error onto one of
// apperr's three dedicated upstream codes (ErrUpstreamAuthFailed /
// ErrUpstreamRateLimited / ErrUpstreamFailed), so the HTTP status returned
// to the client reflects OUR semantics — never a raw forwarded upstream
// status — while the error CODE still tells the client what actually
// happened upstream. The original error (real status, real message) is
// preserved via Wrap so it still reaches AppError.Internal() and therefore
// the logs and the generation_tasks.error_message column; it just no longer
// dictates the HTTP status or top-level code we hand back.
//
// Anything that is not a recognized dashscope/t8star error code — most
// importantly our own apperr.ErrValidation/ErrSettingIncomplete/etc raised
// before any upstream call happens — passes through untouched. Same for a
// nil error, and for errors that aren't *apperr.AppError at all (defensive:
// every real provider call returns one, per errorCodeAndMessage's doc
// comment, but test doubles are free to hand back a plain error to exercise
// unrelated logic and must not be reclassified as an upstream failure just
// because they happen to wrap provider.ErrAccessDenied).
func normalizeUpstreamError(err error) error {
	if err == nil {
		return nil
	}

	var appErr *apperr.AppError
	if !errors.As(err, &appErr) {
		return err
	}

	// DashScope's AccessDenied sentinel is a stronger, protocol-independent
	// signal than any particular HTTP status: server.js's own downgrade
	// chain (ported in OptimizeService.Optimize) treats it as "this
	// credential/model pairing is not permitted", which is squarely an
	// auth failure regardless of which fixed status the wrapping error code
	// happens to carry (dashscope's CodeUpstreamError sentinel is a fixed
	// 400, not the real upstream status).
	if errors.Is(err, provider.ErrAccessDenied) {
		return apperr.ErrUpstreamAuthFailed.Wrap(err)
	}

	switch appErr.Code() {
	case dashscope.CodeUpstreamHTTP, t8star.CodeUpstreamHTTP:
		// These two codes are the ones that carry the real forwarded
		// upstream HTTP status (see both packages' newUpstreamHTTPError) —
		// the exact status this whole normalization step exists to stop
		// from leaking out as our own.
		switch appErr.HTTPStatus() {
		case http.StatusUnauthorized, http.StatusForbidden:
			return apperr.ErrUpstreamAuthFailed.Wrap(err)
		case http.StatusTooManyRequests:
			return apperr.ErrUpstreamRateLimited.Wrap(err)
		default:
			return apperr.ErrUpstreamFailed.Wrap(err)
		}
	case dashscope.CodeUpstreamError, dashscope.CodeNoImageResult, dashscope.CodeNoTaskID,
		dashscope.CodeNoOptimizeResult, dashscope.CodeRequestFailed,
		t8star.CodeUpstreamError, t8star.CodeNoImageResult:
		// Every other upstream-originated failure mode: 200-with-error-body,
		// network-level failure, or "parsed fine but couldn't find a
		// result". None of these forward a raw status that could collide
		// with our own 401, but they're still provider-specific codes the
		// client has no i18n entry for and no reason to distinguish —
		// folded into the same generic bucket as the HTTP-error default
		// case above.
		return apperr.ErrUpstreamFailed.Wrap(err)
	default:
		return err
	}
}
