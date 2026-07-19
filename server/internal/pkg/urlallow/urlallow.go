// Package urlallow holds the single host allowlist every "the server fetches
// a URL for you" path in this system must pass through.
//
// It lives in internal/pkg rather than internal/handler because there are now
// two such paths — GET /api/download (handler/download.go) and result
// archiving (service/result_archive.go) — and internal/handler already
// imports internal/service, so the service layer cannot import the handler
// package back. The alternative to this package was a second copy of the
// allowlist inside service; two copies of an SSRF guard drift, and the copy
// that drifts is the one nobody remembers to update.
package urlallow

import "strings"

// allowedResultHostSuffixes lists exactly the upstream domains this system
// ever produces generation results on:
//   - ".aliyuncs.com" — DashScope result URLs, and also every OSS bucket URL
//     this codebase can produce: ossx.Client always addresses
//     "<bucket>.<region>.aliyuncs.com" (see internal/pkg/ossx/client.go,
//     Config.endpoint/publicURL), so "the configured OSS bucket host" — which
//     is what result_urls holds once archiving is on — is already a subdomain
//     of this suffix; no separate entry is needed for it.
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

// MaxRedirects is the hop limit server.js never had. Following N redirects
// means CheckRedirect is invoked N times (once per hop, with `via` holding
// the requests made so far — length 1 on the first hop, length N on the Nth);
// rejecting once len(via) exceeds this constant means exactly MaxRedirects
// hops are followed and the next one fails closed.
const MaxRedirects = 3

// IsAllowedResultHost is the production host filter. Callers substitute their
// own filter in tests (see handler.NewDownloadHandlerWithHostFilter and
// service.newResultArchiverWithDeps) so an httptest.Server's loopback host
// can stand in for a real result host, while still exercising the exact same
// enforcement code path (initial URL + every redirect hop) that runs in
// production.
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
