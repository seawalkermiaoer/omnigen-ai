package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/t8star"
)

// t8starProbeModel is a deliberately nonexistent model name used only to
// probe credentials. It must never be changed to a real model name — doing
// so silently reintroduces a slow, billed call. Measured against the real
// t8star endpoint with a valid key (2026-07-19), see
// docs/superpowers/specs/2026-07-19-t8star-connection-test-design.md:
//
//   - correct key + "gpt-image-2" + empty prompt: HTTP 200 in 29.2s. The
//     empty prompt is NOT rejected early — t8star generates a real image and
//     bills 1155 tokens. connectionTestTimeout (10s) fires long before that
//     returns, so the button used to always fail with ErrUpstreamFailed
//     *and still charge the user*. This disproved the original design doc's
//     assumption that t8star has no credential-only probe.
//   - correct key + t8starProbeModel: HTTP 503 in 0.7s,
//     {"error":{"code":"invalid_request","message":"所有分组对于模型 ...
//     无可用渠道，请检查模型是否存在或联系管理员"}} — no image, no charge.
//   - bad key + t8starProbeModel: HTTP 401 in 0.2s,
//     {"error":{"code":"invalid_request","message":"令牌不合法"}}
//
// Auth is checked before model routing, so a bogus model name is a genuine,
// free, sub-second credential probe: any HTTP response (however "wrong" the
// model) proves the key was accepted; 401/403 proves it wasn't.
const t8starProbeModel = "__omnigen_credential_probe__"

// T8starImageProviderFactory constructs a provider.ImageProvider bound to a
// specific apiKey/endpoint pair. Production always passes t8star.New; tests
// pass creds.Endpoint pointing at an httptest.Server (or a closed one, to
// simulate a transport failure) and use the same production factory —
// t8star.Client already honors whatever endpoint string it's given.
type T8starImageProviderFactory func(apiKey, endpoint string) provider.ImageProvider

func newT8starImageProviderFactory() T8starImageProviderFactory {
	return func(apiKey, endpoint string) provider.ImageProvider {
		return t8star.New(apiKey, endpoint)
	}
}

// t8starConnectionTester probes t8star credentials by sending a
// GenerateImage request for the nonexistent t8starProbeModel and classifying
// the outcome by HTTP status rather than by whether an image came back — see
// docs/superpowers/specs/2026-07-19-t8star-connection-test-design.md for the
// measurements behind this (and why the original "no cheap probe exists"
// assumption in that doc was wrong).
//
//   - 401 / 403                                  -> apperr.ErrUpstreamAuthFailed
//   - any other response (the expected case is a
//     4xx/5xx "model not found" business error,
//     since the probe model never exists)        -> nil (credentials valid)
//   - transport-level failure (no response ever
//     received)                                  -> apperr.ErrUpstreamFailed
type t8starConnectionTester struct {
	factory T8starImageProviderFactory
}

func newT8starConnectionTester() t8starConnectionTester {
	return t8starConnectionTester{factory: newT8starImageProviderFactory()}
}

var _ ConnectionTester = t8starConnectionTester{}

func (t t8starConnectionTester) Test(ctx context.Context, creds UpstreamCredentials) error {
	reqCtx, cancel := context.WithTimeout(ctx, connectionTestTimeout)
	defer cancel()

	p := t.factory(creds.APIKey, creds.Endpoint)
	_, err := p.GenerateImage(reqCtx, provider.ImageRequest{Model: t8starProbeModel, Prompt: ""})
	if err == nil {
		// t8starProbeModel doesn't exist, so a nil error here would mean
		// t8star somehow parsed an image out of a response to a bogus model
		// — vanishingly unlikely, but if it ever happens that still
		// unambiguously proves the credential works. See the design doc:
		// the test verifies the credential, not the ability to generate.
		return nil
	}

	var appErr *apperr.AppError
	if !errors.As(err, &appErr) {
		// Not even a *apperr.AppError — t8star.Client never returns a bare
		// error, but fail closed (as a connectivity problem) rather than
		// silently treating an unrecognized shape as success.
		return apperr.ErrUpstreamFailed.Wrap(err)
	}

	switch appErr.Code() {
	case t8star.CodeUpstreamError, t8star.CodeNoImageResult:
		// t8star.Client only reaches these two codes after a 200 OK
		// response was received and parsed (a JSON `error` field, or zero
		// images found) — a response means the credential was accepted.
		return nil
	case t8star.CodeUpstreamHTTP:
		if status := appErr.HTTPStatus(); status == http.StatusUnauthorized || status == http.StatusForbidden {
			return apperr.ErrUpstreamAuthFailed.Wrap(err)
		}
		// t8star.Client funnels both "upstream returned a real >=400
		// response" and "http.Client.Do never got a response at all" into
		// this same CodeUpstreamHTTP bucket (the latter forced to 502 —
		// see provider/t8star/client.go). Only the latter is a genuine
		// network-layer failure; net/http always wraps that case in
		// *url.Error, which the actual-response path never produces (it
		// wraps a plain fmt.Errorf built from the response body). That's
		// the signal used to tell them apart.
		var urlErr *url.Error
		if errors.As(appErr.Internal(), &urlErr) {
			return apperr.ErrUpstreamFailed.Wrap(err)
		}
		return nil
	default:
		return apperr.ErrUpstreamFailed.Wrap(err)
	}
}
