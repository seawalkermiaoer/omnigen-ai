package dashscope_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/dashscope"
)

// intPtr / boolPtr / int64Ptr 是构造 provider.ImageParams 指针字段的小工具。
func intPtr(v int) *int       { return &v }
func boolPtr(v bool) *bool    { return &v }
func int64Ptr(v int64) *int64 { return &v }

// capturingImageServer 起一个记录原始请求字节与关键头字段的 httptest.Server，
// 并用给定的 JSON 响应体应答。调用方通过返回的指针读取被记录下来的请求。
type capturedRequest struct {
	Method      string
	Path        string
	Header      http.Header
	Body        []byte
	ContentType string
}

func newCapturingServer(t *testing.T, status int, respBody string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured.Method = r.Method
		captured.Path = r.URL.Path
		captured.Header = r.Header.Clone()
		captured.Body = body
		captured.ContentType = r.Header.Get("Content-Type")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

// successImageResponse is a minimal well-formed DashScope native
// multimodal-generation success body.
const successImageResponse = `{
  "output": {
    "choices": [
      {"message": {"content": [{"image": "https://example.com/out1.png"}, {"image": "https://example.com/out2.png"}]}}
    ]
  },
  "usage": {"image_count": 2}
}`

func TestGenerateImage_ContentOrderIsImagesFirstThenText(t *testing.T) {
	srv, captured := newCapturingServer(t, 200, successImageResponse)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	req := provider.ImageRequest{
		Model:  "qwen-image",
		Prompt: "a cat wearing sunglasses",
		Images: []string{"https://in.example.com/1.png", "https://in.example.com/2.png"},
	}
	_, err := c.GenerateImage(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload struct {
		Input struct {
			Messages []struct {
				Content []map[string]any `json:"content"`
			} `json:"messages"`
		} `json:"input"`
	}
	if err := json.Unmarshal(captured.Body, &payload); err != nil {
		t.Fatalf("failed to decode captured body: %v (body=%s)", err, captured.Body)
	}
	content := payload.Input.Messages[0].Content
	if len(content) != 3 {
		t.Fatalf("expected 3 content items (2 images + 1 text), got %d: %+v", len(content), content)
	}
	if _, ok := content[0]["image"]; !ok {
		t.Errorf("content[0] should be an image block, got %+v", content[0])
	}
	if content[0]["image"] != "https://in.example.com/1.png" {
		t.Errorf("content[0].image = %v, want first image in order", content[0]["image"])
	}
	if _, ok := content[1]["image"]; !ok {
		t.Errorf("content[1] should be an image block, got %+v", content[1])
	}
	if content[1]["image"] != "https://in.example.com/2.png" {
		t.Errorf("content[1].image = %v, want second image in order", content[1]["image"])
	}
	if _, ok := content[2]["text"]; !ok {
		t.Errorf("content[2] (last) should be the text block, got %+v", content[2])
	}
	if content[2]["text"] != "a cat wearing sunglasses" {
		t.Errorf("content[2].text = %v", content[2]["text"])
	}
}

func TestGenerateImage_FalsyButPresentParamsAreSent(t *testing.T) {
	srv, captured := newCapturingServer(t, 200, successImageResponse)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	req := provider.ImageRequest{
		Model:  "wan2.7-image",
		Prompt: "x",
		Params: provider.ImageParams{
			N:         intPtr(0),
			Watermark: boolPtr(false),
			Seed:      int64Ptr(0),
			// Size / NegativePrompt left as zero-value "" — must be omitted.
		},
	}
	_, err := c.GenerateImage(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(captured.Body)
	for _, want := range []string{`"n":0`, `"watermark":false`, `"seed":0`} {
		if !strings.Contains(body, want) {
			t.Errorf("expected body to contain %q, got: %s", want, body)
		}
	}

	var payload struct {
		Parameters map[string]any `json:"parameters"`
	}
	if err := json.Unmarshal(captured.Body, &payload); err != nil {
		t.Fatalf("failed to decode captured body: %v", err)
	}
	if _, ok := payload.Parameters["size"]; ok {
		t.Errorf("size should be absent (empty string is falsy), got parameters=%+v", payload.Parameters)
	}
	if _, ok := payload.Parameters["negative_prompt"]; ok {
		t.Errorf("negative_prompt should be absent (empty string is falsy), got parameters=%+v", payload.Parameters)
	}
	if _, ok := payload.Parameters["n"]; !ok {
		t.Errorf("n=0 should be present (!= null check), got parameters=%+v", payload.Parameters)
	}
	if _, ok := payload.Parameters["watermark"]; !ok {
		t.Errorf("watermark=false should be present (!= null check), got parameters=%+v", payload.Parameters)
	}
	if _, ok := payload.Parameters["seed"]; !ok {
		t.Errorf("seed=0 should be present (!= null check), got parameters=%+v", payload.Parameters)
	}
}

func TestGenerateImage_SizeAndNegativePromptOmittedWhenEmpty(t *testing.T) {
	srv, captured := newCapturingServer(t, 200, successImageResponse)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	req := provider.ImageRequest{
		Model: "qwen-image",
		Params: provider.ImageParams{
			Size:           "",
			NegativePrompt: "",
		},
	}
	_, err := c.GenerateImage(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(captured.Body)
	if strings.Contains(body, `"size"`) {
		t.Errorf("expected no size key in body: %s", body)
	}
	if strings.Contains(body, `"negative_prompt"`) {
		t.Errorf("expected no negative_prompt key in body: %s", body)
	}
}

func TestGenerateImage_SizeAndNegativePromptSentWhenNonEmpty(t *testing.T) {
	srv, captured := newCapturingServer(t, 200, successImageResponse)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	req := provider.ImageRequest{
		Model: "qwen-image",
		Params: provider.ImageParams{
			Size:           "1328*1328",
			NegativePrompt: "blurry",
		},
	}
	_, err := c.GenerateImage(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(captured.Body)
	if !strings.Contains(body, `"size":"1328*1328"`) {
		t.Errorf("expected size in body: %s", body)
	}
	if !strings.Contains(body, `"negative_prompt":"blurry"`) {
		t.Errorf("expected negative_prompt in body: %s", body)
	}
}

func TestGenerateImage_ParametersAlwaysPresentEvenWhenEmpty(t *testing.T) {
	srv, captured := newCapturingServer(t, 200, successImageResponse)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	req := provider.ImageRequest{Model: "qwen-image", Prompt: "x"}
	_, err := c.GenerateImage(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(captured.Body, &payload); err != nil {
		t.Fatalf("failed to decode captured body: %v", err)
	}
	params, ok := payload["parameters"]
	if !ok {
		t.Fatalf("parameters key must always be present, got %+v", payload)
	}
	if m, ok := params.(map[string]any); !ok || len(m) != 0 {
		t.Errorf("parameters should be an empty object when no params supplied, got %+v", params)
	}
}

func TestGenerateImage_DoesNotSendAsyncHeader(t *testing.T) {
	srv, captured := newCapturingServer(t, 200, successImageResponse)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	_, err := c.GenerateImage(context.Background(), provider.ImageRequest{Model: "qwen-image", Prompt: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v := captured.Header.Get("X-Dashscope-Async"); v != "" {
		t.Errorf("image generation must NOT send X-DashScope-Async, got %q", v)
	}
	if got := captured.Path; got != "/api/v1/services/aigc/multimodal-generation/generation" {
		t.Errorf("unexpected path: %s", got)
	}
	if captured.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", captured.Method)
	}
	if want := "Bearer test-key"; captured.Header.Get("Authorization") != want {
		t.Errorf("Authorization = %q, want %q", captured.Header.Get("Authorization"), want)
	}
	if captured.ContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", captured.ContentType)
	}
}

func TestGenerateImage_ParsesImagesAndUsageOnSuccess(t *testing.T) {
	srv, _ := newCapturingServer(t, 200, successImageResponse)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	result, err := c.GenerateImage(context.Background(), provider.ImageRequest{Model: "qwen-image", Prompt: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Images) != 2 {
		t.Fatalf("expected 2 images, got %d: %+v", len(result.Images), result.Images)
	}
	if result.Images[0] != "https://example.com/out1.png" || result.Images[1] != "https://example.com/out2.png" {
		t.Errorf("unexpected images: %+v", result.Images)
	}
	if result.Model != "qwen-image" {
		t.Errorf("Model = %q, want qwen-image", result.Model)
	}
}

func TestGenerateImage_HTTPErrorStatusReturnsError(t *testing.T) {
	srv, _ := newCapturingServer(t, 403, `{"error": {"message": "insufficient balance"}}`)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	_, err := c.GenerateImage(context.Background(), provider.ImageRequest{Model: "qwen-image", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error for HTTP 403")
	}
	if !strings.Contains(err.Error(), "insufficient balance") {
		t.Errorf("expected error message to surface upstream message, got: %v", err)
	}
}

func TestGenerateImage_NativeErrorFormatWithHTTP200ReturnsError(t *testing.T) {
	srv, _ := newCapturingServer(t, 200, `{"code": "InvalidParameter", "message": "bad size"}`)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	_, err := c.GenerateImage(context.Background(), provider.ImageRequest{Model: "qwen-image", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error for native error format")
	}
	if !strings.Contains(err.Error(), "bad size") {
		t.Errorf("expected error message to surface upstream message, got: %v", err)
	}
}

func TestGenerateImage_NoImagesInResponseReturnsError(t *testing.T) {
	srv, _ := newCapturingServer(t, 200, `{"output": {"choices": [{"message": {"content": []}}]}}`)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	_, err := c.GenerateImage(context.Background(), provider.ImageRequest{Model: "qwen-image", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error when no images are returned")
	}
}
