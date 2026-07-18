// Package t8star implements the OpenAI chat-completions protocol client for
// the t8star relay (gpt-image-2). This is a straight port of the old Node.js
// implementation in lib/providers/t8star.js — see that file and
// docs/superpowers/specs/2026-07-18-t8star-gpt-image-2-design.md for the
// upstream contract this code depends on.
//
// BuildPayload, ParseResponse and ResolveBaseURL are pure functions with no
// network IO, mirroring the JS module so they can be unit tested against the
// real captured fixture in docs/superpowers/plans/2026-07-18-t8star-gpt-image-2.md.
package t8star

import (
	"regexp"
	"strings"
)

// DefaultBaseURL is used whenever the configured endpoint isn't a valid
// http(s) URL.
const DefaultBaseURL = "https://ai.t8star.org"

// imageLinkPattern matches a markdown image link and captures its URL.
// Go's regexp.Regexp is safe for concurrent, repeated use (no lastIndex
// state like the JS RegExp with the /g flag), so unlike the JS port this is
// compiled once at package init rather than rebuilt on every call — but see
// TestParseResponse_RepeatedCallsAreStable, which regression-tests exactly
// the bug that rebuilding-per-call worked around in JS.
var imageLinkPattern = regexp.MustCompile(`!\[[^\]]*\]\((https?://[^)\s]+)\)`)

// chatMessage is a single OpenAI chat-completions message. Content is either
// a bare string (no images) or a []contentBlock (images present) — encoding
// this as interface{} is what lets encoding/json pick the right JSON shape.
type chatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// contentBlock is one element of a multi-part message content array.
type contentBlock struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

// ChatPayload is the request body posted to POST {base}/v1/chat/completions.
type ChatPayload struct {
	Model    string        `json:"model"`
	Stream   bool          `json:"stream"`
	Messages []chatMessage `json:"messages"`
}

// UpstreamError is the OpenAI-shaped error object t8star returns either as
// an HTTP error body or embedded in a 200 response.
type UpstreamError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// Message is the assistant message inside a choice. Content is typed as
// interface{} because the protocol only ever sends a string here in
// practice, but decoding leaves it as-is (string vs []interface{}) so
// ParseResponse can tell the two apart the same way the JS `typeof` check did.
type Message struct {
	Content interface{} `json:"content"`
}

// Choice is one entry of the response's choices array.
type Choice struct {
	Message Message `json:"message"`
}

// Response is the full decoded chat-completions response body.
type Response struct {
	Model   string         `json:"model"`
	Choices []Choice       `json:"choices"`
	Usage   map[string]any `json:"usage"`
	Error   *UpstreamError `json:"error"`
	Message string         `json:"message"`
}

// BuildPayload builds the chat-completions request body.
//
// With no images, content is a bare string (prompt, or "" if prompt is
// empty) — NOT a one-element array. With images, content is an array with
// the text block FIRST (omitted entirely if prompt is empty) followed by the
// image_url blocks in order. This ordering is deliberately the reverse of
// the DashScope provider's images-first, text-last shape — they are
// different upstream contracts and must not be harmonized.
func BuildPayload(model, prompt string, images []string) ChatPayload {
	list := make([]string, 0, len(images))
	for _, img := range images {
		if img != "" {
			list = append(list, img)
		}
	}

	var content interface{}
	if len(list) == 0 {
		content = prompt
	} else {
		blocks := make([]contentBlock, 0, len(list)+1)
		if prompt != "" {
			blocks = append(blocks, contentBlock{Type: "text", Text: prompt})
		}
		for _, url := range list {
			blocks = append(blocks, contentBlock{Type: "image_url", ImageURL: &imageURL{URL: url}})
		}
		content = blocks
	}

	return ChatPayload{
		Model:  model,
		Stream: false,
		Messages: []chatMessage{
			{Role: "user", Content: content},
		},
	}
}

// ParseResponse extracts image URLs and the leftover prose from the
// assistant reply embedded in resp.Choices[0].Message.Content. Only
// string-typed content is inspected; array-form content (which this
// protocol never actually returns in practice) yields zero images rather
// than panicking.
func ParseResponse(resp Response) (images []string, note string) {
	if len(resp.Choices) == 0 {
		return []string{}, ""
	}

	s, ok := resp.Choices[0].Message.Content.(string)
	if !ok {
		return []string{}, ""
	}

	matches := imageLinkPattern.FindAllStringSubmatch(s, -1)
	images = make([]string, 0, len(matches))
	for _, m := range matches {
		images = append(images, m[1])
	}

	note = strings.TrimSpace(imageLinkPattern.ReplaceAllString(s, ""))
	return images, note
}

// ResolveBaseURL normalizes a user-supplied base URL, falling back to the
// official host when the input isn't a valid http(s) URL.
func ResolveBaseURL(input string) string {
	v := strings.TrimSpace(input)
	lower := strings.ToLower(v)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return DefaultBaseURL
	}
	return strings.TrimRight(v, "/")
}
