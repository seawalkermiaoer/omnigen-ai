// Package mimeext derives a safe file extension from a Content-Type header.
//
// It sits in internal/pkg for the same reason as urlallow: both the download
// handler (which builds a Content-Disposition filename) and the result
// archiver in internal/service (which builds an OSS object key) need it, and
// internal/handler already imports internal/service, so the dependency can
// only run downward into a shared package.
package mimeext

import (
	"mime"
	"strings"
)

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

// FromContentType returns a sanitized extension (no leading dot) for the
// given Content-Type, or "" when nothing sensible can be derived.
func FromContentType(contentType string) string {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if ext, ok := contentTypeExtensions[base]; ok {
		return ext
	}
	if exts, err := mime.ExtensionsByType(base); err == nil && len(exts) > 0 {
		return Sanitize(strings.TrimPrefix(exts[0], "."))
	}
	return ""
}

// Sanitize keeps only ASCII letters/digits (max 8 of them). This is the
// load-bearing sanitizer on both consumers: the extension is the one part of
// a Content-Disposition filename / OSS object key that comes from
// attacker-reachable data (the upstream URL path and its Content-Type
// header), so filtering it down to bare alphanumerics structurally rules out
// path separators, "..", and CR/LF ending up in a header value or a key.
func Sanitize(ext string) string {
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
