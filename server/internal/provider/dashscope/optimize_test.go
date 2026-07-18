package dashscope_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/dashscope"
)

func TestOptimize_NoImagesUsesCompatModeWithPlainStringMessages(t *testing.T) {
	captured := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured.Path = r.URL.Path
		captured.Body = body
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "optimized prompt text"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	text, model, err := c.Optimize(context.Background(), provider.OptimizeRequest{
		Model:        "qwen3.7-plus",
		SystemPrompt: "you are a prompt engineer",
		UserText:     "make it better",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "optimized prompt text" {
		t.Errorf("text = %q", text)
	}
	if model != "qwen3.7-plus" {
		t.Errorf("model = %q", model)
	}
	if captured.Path != "/compatible-mode/v1/chat/completions" {
		t.Errorf("path = %q, want compatible-mode chat completions", captured.Path)
	}

	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(captured.Body, &payload); err != nil {
		t.Fatalf("decode body: %v (body=%s)", err, captured.Body)
	}
	if len(payload.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(payload.Messages))
	}
	if payload.Messages[0].Role != "system" || payload.Messages[0].Content != "you are a prompt engineer" {
		t.Errorf("unexpected system message: %+v", payload.Messages[0])
	}
	if payload.Messages[1].Role != "user" || payload.Messages[1].Content != "make it better" {
		t.Errorf("unexpected user message: %+v", payload.Messages[1])
	}
}

func TestOptimize_WithImagesUsesNativeProtocolImagesFirstThenText(t *testing.T) {
	captured := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured.Path = r.URL.Path
		captured.Body = body
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"output": {"choices": [{"message": {"content": [{"text": "part1 "}, {"text": "part2"}]}}]}}`))
	}))
	t.Cleanup(srv.Close)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	text, model, err := c.Optimize(context.Background(), provider.OptimizeRequest{
		Model:        "qwen-vl-max-latest",
		SystemPrompt: "sys",
		UserText:     "draft text",
		Images:       []string{"https://in.example.com/a.png", "https://in.example.com/b.png"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "part1 part2" {
		t.Errorf("text = %q, want concatenated array content", text)
	}
	if model != "qwen-vl-max-latest" {
		t.Errorf("model = %q", model)
	}
	if captured.Path != "/api/v1/services/aigc/multimodal-generation/generation" {
		t.Errorf("path = %q, want native multimodal-generation", captured.Path)
	}

	var payload struct {
		Input struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		} `json:"input"`
		Parameters map[string]any `json:"parameters"`
	}
	if err := json.Unmarshal(captured.Body, &payload); err != nil {
		t.Fatalf("decode body: %v (body=%s)", err, captured.Body)
	}
	if payload.Parameters["result_format"] != "message" {
		t.Errorf("expected result_format=message, got %+v", payload.Parameters)
	}
	if len(payload.Input.Messages) != 2 {
		t.Fatalf("expected system + user messages, got %d", len(payload.Input.Messages))
	}

	var userContent []map[string]any
	if err := json.Unmarshal(payload.Input.Messages[1].Content, &userContent); err != nil {
		t.Fatalf("decode user content: %v", err)
	}
	if len(userContent) != 3 {
		t.Fatalf("expected 2 images + 1 text, got %d: %+v", len(userContent), userContent)
	}
	if userContent[0]["image"] != "https://in.example.com/a.png" {
		t.Errorf("userContent[0] = %+v", userContent[0])
	}
	if userContent[1]["image"] != "https://in.example.com/b.png" {
		t.Errorf("userContent[1] = %+v", userContent[1])
	}
	if userContent[2]["text"] != "draft text" {
		t.Errorf("userContent[2] (last) should be text, got %+v", userContent[2])
	}
}

func TestOptimize_AccessDeniedErrorIsDetectableViaErrorsIs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code": "AccessDenied.Unauthorized", "message": "no access"}`))
	}))
	t.Cleanup(srv.Close)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	_, _, err := c.Optimize(context.Background(), provider.OptimizeRequest{Model: "m", SystemPrompt: "s", UserText: "u"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, provider.ErrAccessDenied) {
		t.Errorf("expected errors.Is(err, provider.ErrAccessDenied) to be true, got err=%v", err)
	}
}

func TestOptimize_NonAccessDeniedErrorDoesNotMatchSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code": "InvalidParameter", "message": "bad request"}`))
	}))
	t.Cleanup(srv.Close)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	_, _, err := c.Optimize(context.Background(), provider.OptimizeRequest{Model: "m", SystemPrompt: "s", UserText: "u"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, provider.ErrAccessDenied) {
		t.Errorf("non-AccessDenied error must not match the sentinel, got err=%v", err)
	}
}

func TestOptimize_StatusCodeIgnoredOnlyNativeErrorFieldsMatter(t *testing.T) {
	// server.js's optimize-prompt handler never checks HTTP status >= 400
	// separately — it only inspects r.data.code / r.data.error, regardless
	// of what the HTTP status was. A 500 with a clean success body (no code
	// / error fields) must still be treated as success.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "still works"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	text, _, err := c.Optimize(context.Background(), provider.OptimizeRequest{Model: "m", SystemPrompt: "s", UserText: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "still works" {
		t.Errorf("text = %q", text)
	}
}
