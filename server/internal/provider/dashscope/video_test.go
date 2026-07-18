package dashscope_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/dashscope"
)

func TestCreateVideoTask_SendsAsyncHeaderAndPayloadVerbatim(t *testing.T) {
	captured := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured.Method = r.Method
		captured.Path = r.URL.Path
		captured.Header = r.Header.Clone()
		captured.Body = body
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"output": {"task_id": "task-abc-123", "task_status": "PENDING"}}`))
	}))
	t.Cleanup(srv.Close)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	taskID, err := c.CreateVideoTask(context.Background(), provider.VideoRequest{
		Payload: map[string]any{"model": "wan2.7-r2v", "input": map[string]any{"prompt": "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskID != "task-abc-123" {
		t.Errorf("taskID = %q, want task-abc-123", taskID)
	}

	if got := captured.Header.Get("X-Dashscope-Async"); got != "enable" {
		t.Errorf("X-DashScope-Async = %q, want enable", got)
	}
	if want := "Bearer test-key"; captured.Header.Get("Authorization") != want {
		t.Errorf("Authorization = %q, want %q", captured.Header.Get("Authorization"), want)
	}
	if got := captured.Path; got != "/api/v1/services/aigc/video-generation/video-synthesis" {
		t.Errorf("unexpected path: %s", got)
	}
	if captured.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", captured.Method)
	}
	if !strings.Contains(string(captured.Body), `"model":"wan2.7-r2v"`) {
		t.Errorf("expected payload to be forwarded verbatim, got: %s", captured.Body)
	}
}

func TestCreateVideoTask_MissingTaskIDIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"output": {}}`))
	}))
	t.Cleanup(srv.Close)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	_, err := c.CreateVideoTask(context.Background(), provider.VideoRequest{Payload: map[string]any{}})
	if err == nil {
		t.Fatal("expected an error when output.task_id is missing")
	}
}

func TestCreateVideoTask_HTTPErrorSurfacesMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"message": "rate limited"}`))
	}))
	t.Cleanup(srv.Close)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	_, err := c.CreateVideoTask(context.Background(), provider.VideoRequest{Payload: map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected error containing 'rate limited', got: %v", err)
	}
}

func TestPollTask_IssuesGETWithNoBodyAndNoContentType(t *testing.T) {
	captured := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured.Method = r.Method
		captured.Path = r.URL.Path
		captured.Header = r.Header.Clone()
		captured.Body = body
		captured.ContentType = r.Header.Get("Content-Type")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"output": {"task_id": "task-xyz", "task_status": "SUCCEEDED", "results": [{"url": "https://example.com/v.mp4"}]}}`))
	}))
	t.Cleanup(srv.Close)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	result, err := c.PollTask(context.Background(), "task-xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if captured.Method != http.MethodGet {
		t.Errorf("expected GET, got %s", captured.Method)
	}
	if len(captured.Body) != 0 {
		t.Errorf("expected empty body, got: %s", captured.Body)
	}
	if captured.ContentType != "" {
		t.Errorf("expected no Content-Type header, got %q", captured.ContentType)
	}
	if got := captured.Path; got != "/api/v1/tasks/task-xyz" {
		t.Errorf("unexpected path: %s", got)
	}
	if want := "Bearer test-key"; captured.Header.Get("Authorization") != want {
		t.Errorf("Authorization = %q, want %q", captured.Header.Get("Authorization"), want)
	}
	if v := captured.Header.Get("X-Dashscope-Async"); v != "" {
		t.Errorf("poll must NOT send X-DashScope-Async, got %q", v)
	}

	if result.TaskID != "task-xyz" {
		t.Errorf("TaskID = %q, want task-xyz", result.TaskID)
	}
	if result.Status != "SUCCEEDED" {
		t.Errorf("Status = %q, want SUCCEEDED", result.Status)
	}
	if result.Raw == nil {
		t.Error("Raw should carry the full decoded response")
	}
}

func TestPollTask_HTTPErrorSurfacesMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"code": "NotFound", "message": "task not found"}`))
	}))
	t.Cleanup(srv.Close)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)
	_, err := c.PollTask(context.Background(), "does-not-exist")
	if err == nil || !strings.Contains(err.Error(), "task not found") {
		t.Fatalf("expected error containing 'task not found', got: %v", err)
	}
}
