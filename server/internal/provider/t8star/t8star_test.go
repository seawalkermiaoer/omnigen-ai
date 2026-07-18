package t8star_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/provider/t8star"
)

// realFixture is the actual captured upstream response quoted verbatim from
// docs/superpowers/plans/2026-07-18-t8star-gpt-image-2.md. Protocol parsing
// is the part of this port most likely to go wrong, so tests assert against
// this real payload rather than a hand-written sample.
const realFixture = `{
  "id": "chatcmpl-11e15ef4-b616-412a-9e5a-d9e8e44497b5",
  "object": "chat.completion",
  "created": 1784370851,
  "model": "gpt-image-2-pro",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "![image](https://webstatic.aiproxy.vip/output/0276be3f.png)\n\n给你画好了，一只可爱的小猫。"
    },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 3780, "completion_tokens": 883, "total_tokens": 4663 }
}`

func decodeFixture(t *testing.T) t8star.Response {
	t.Helper()
	var resp t8star.Response
	require.NoError(t, json.Unmarshal([]byte(realFixture), &resp))
	return resp
}

// ─── BuildPayload ──────────────────────────────────────────────────────

func TestBuildPayload_NoImages_ContentIsBareString(t *testing.T) {
	payload := t8star.BuildPayload("gpt-image-2", "画只猫", nil)

	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	var generic map[string]any
	require.NoError(t, json.Unmarshal(raw, &generic))

	messages, ok := generic["messages"].([]any)
	require.True(t, ok)
	require.Len(t, messages, 1)
	msg := messages[0].(map[string]any)

	// The load-bearing assertion: content must decode back as a JSON
	// string, not a one-element array. json.Unmarshal into interface{}
	// yields a Go string only for a bare JSON string.
	content, ok := msg["content"].(string)
	require.True(t, ok, "content must be a bare string, got %T: %v", msg["content"], msg["content"])
	assert.Equal(t, "画只猫", content)

	assert.Equal(t, "gpt-image-2", generic["model"])
	assert.Equal(t, false, generic["stream"])
	assert.JSONEq(t, `{
		"model": "gpt-image-2",
		"stream": false,
		"messages": [{"role": "user", "content": "画只猫"}]
	}`, string(raw))
}

func TestBuildPayload_NoImages_EmptyPrompt_ContentIsBareEmptyString(t *testing.T) {
	payload := t8star.BuildPayload("gpt-image-2", "", nil)
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model": "gpt-image-2",
		"stream": false,
		"messages": [{"role": "user", "content": ""}]
	}`, string(raw))
}

func TestBuildPayload_WithImages_TextBlockFirstThenImageBlocksInOrder(t *testing.T) {
	payload := t8star.BuildPayload("gpt-image-2", "修改这个图片", []string{"https://a/1.png", "https://a/2.png"})
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	assert.JSONEq(t, `{
		"model": "gpt-image-2",
		"stream": false,
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "修改这个图片"},
				{"type": "image_url", "image_url": {"url": "https://a/1.png"}},
				{"type": "image_url", "image_url": {"url": "https://a/2.png"}}
			]
		}]
	}`, string(raw))
}

func TestBuildPayload_WithImages_EmptyPrompt_NoTextBlockAtAll(t *testing.T) {
	payload := t8star.BuildPayload("gpt-image-2", "", []string{"https://a/1.png"})
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	var generic map[string]any
	require.NoError(t, json.Unmarshal(raw, &generic))
	messages := generic["messages"].([]any)
	msg := messages[0].(map[string]any)
	content := msg["content"].([]any)

	require.Len(t, content, 1, "empty prompt must omit the text block entirely, not just its text")
	block := content[0].(map[string]any)
	assert.Equal(t, "image_url", block["type"])
	_, hasText := block["text"]
	assert.False(t, hasText, "no text field should be present when prompt is empty")
}

func TestBuildPayload_ImagesFilterOutEmptyEntries(t *testing.T) {
	payload := t8star.BuildPayload("gpt-image-2", "p", []string{"", "https://a/1.png", ""})
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model": "gpt-image-2",
		"stream": false,
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "p"},
				{"type": "image_url", "image_url": {"url": "https://a/1.png"}}
			]
		}]
	}`, string(raw))
}

// ─── ParseResponse ─────────────────────────────────────────────────────

func TestParseResponse_RealFixture_ExtractsEveryImageURL(t *testing.T) {
	resp := decodeFixture(t)
	images, _ := t8star.ParseResponse(resp)
	assert.Equal(t, []string{"https://webstatic.aiproxy.vip/output/0276be3f.png"}, images)
}

func TestParseResponse_RealFixture_NoteIsProseWithImageLinksStrippedAndTrimmed(t *testing.T) {
	resp := decodeFixture(t)
	_, note := t8star.ParseResponse(resp)
	assert.Equal(t, "给你画好了，一只可爱的小猫。", note)
}

func TestParseResponse_RepeatedCallsReturnIdenticalResults(t *testing.T) {
	// Regression test for the JS lastIndex bug: a shared /g RegExp carries
	// state (lastIndex) between calls, so calling exec() repeatedly on the
	// same pattern without resetting it silently returns fewer/no matches
	// on the second call. Go's regexp.Regexp has no such state, but this
	// guards against ever introducing a shared, stateful matcher.
	resp := decodeFixture(t)

	images1, note1 := t8star.ParseResponse(resp)
	images2, note2 := t8star.ParseResponse(resp)
	images3, note3 := t8star.ParseResponse(resp)

	assert.Equal(t, images1, images2)
	assert.Equal(t, images2, images3)
	assert.Equal(t, note1, note2)
	assert.Equal(t, note2, note3)
	assert.NotEmpty(t, images1, "sanity: the fixture does contain an image")
}

func TestParseResponse_NonStringContent_ZeroImagesNoPanic(t *testing.T) {
	raw := `{
		"model": "gpt-image-2",
		"choices": [{
			"message": {
				"content": [{"type": "text", "text": "not a string"}]
			}
		}]
	}`
	var resp t8star.Response
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))

	assert.NotPanics(t, func() {
		images, note := t8star.ParseResponse(resp)
		assert.Equal(t, []string{}, images)
		assert.Equal(t, "", note)
	})
}

func TestParseResponse_NoChoices_ZeroImagesNoPanic(t *testing.T) {
	var resp t8star.Response
	require.NoError(t, json.Unmarshal([]byte(`{}`), &resp))

	assert.NotPanics(t, func() {
		images, note := t8star.ParseResponse(resp)
		assert.Equal(t, []string{}, images)
		assert.Equal(t, "", note)
	})
}

func TestParseResponse_MultipleImageLinks_ExtractsAllInOrder(t *testing.T) {
	raw := `{
		"choices": [{
			"message": {
				"content": "![image](https://a/1.png) some text ![image](https://a/2.png) more text"
			}
		}]
	}`
	var resp t8star.Response
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))

	images, note := t8star.ParseResponse(resp)
	assert.Equal(t, []string{"https://a/1.png", "https://a/2.png"}, images)
	assert.Equal(t, "some text  more text", note)
}

// ─── ResolveBaseURL ────────────────────────────────────────────────────

func TestResolveBaseURL_Empty_DefaultsToOfficialHost(t *testing.T) {
	assert.Equal(t, t8star.DefaultBaseURL, t8star.ResolveBaseURL(""))
}

func TestResolveBaseURL_NonURLString_DefaultsToOfficialHost(t *testing.T) {
	assert.Equal(t, t8star.DefaultBaseURL, t8star.ResolveBaseURL("custom"))
}

func TestResolveBaseURL_RealURL_StripsTrailingSlashes(t *testing.T) {
	assert.Equal(t, "https://relay.example.com", t8star.ResolveBaseURL("https://relay.example.com///"))
}

func TestResolveBaseURL_RealURL_NoTrailingSlash_Unchanged(t *testing.T) {
	assert.Equal(t, "http://relay.example.com", t8star.ResolveBaseURL("http://relay.example.com"))
}
