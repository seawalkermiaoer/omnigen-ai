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

// t8starProbeModel is the only model t8star's chat-completions endpoint
// understands — there is no cheaper probe target (see the design doc's
// "为什么这不对称" section).
const t8starProbeModel = "gpt-image-2"

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

// t8starConnectionTester probes t8star credentials by sending a minimal
// (empty-prompt) GenerateImage request and classifying the outcome by HTTP
// status rather than by whether an image came back — see
// docs/superpowers/specs/2026-07-19-t8star-connection-test-design.md for why
// t8star can't be probed the cheap way DashScope is.
//
//   - 401 / 403                                  -> apperr.ErrUpstreamAuthFailed
//   - any other response (4xx business error,
//     200 with no image, or a real generated
//     image)                                     -> nil (credentials valid)
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
		// A real response came back and even parsed into an image — that
		// spends real money, but it unambiguously proves the credential
		// works. See the design doc: the test verifies the credential, not
		// the ability to generate for free.
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
