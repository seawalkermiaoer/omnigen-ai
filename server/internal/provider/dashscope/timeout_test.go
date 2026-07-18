package dashscope_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/dashscope"
)

// TestGenerateImage_CallerContextDeadlinePreemptsTheBuiltInTimeout verifies
// that doRequest's context.WithTimeout takes the earlier of the caller's
// deadline and the built-in 120s/180s timeout: a short-lived caller context
// must abort well before a deliberately slow upstream ever responds, instead
// of blocking for the full 180s image-generation timeout.
func TestGenerateImage_CallerContextDeadlinePreemptsTheBuiltInTimeout(t *testing.T) {
	blockFor := 2 * time.Second
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(blockFor):
		case <-r.Context().Done():
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"output": {"choices": [{"message": {"content": [{"image": "https://x/y.png"}]}}]}}`))
	}))
	t.Cleanup(srv.Close)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.GenerateImage(ctx, provider.ImageRequest{Model: "qwen-image", Prompt: "x"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if elapsed >= blockFor {
		t.Errorf("request took %v, expected it to abort near the 100ms caller deadline, well before the %v upstream delay", elapsed, blockFor)
	}
}

// TestPollTask_CallerContextDeadlinePreemptsTheBuiltInTimeout mirrors the
// above for the GET poll path, which uses the 120s default timeout.
func TestPollTask_CallerContextDeadlinePreemptsTheBuiltInTimeout(t *testing.T) {
	blockFor := 2 * time.Second
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(blockFor):
		case <-r.Context().Done():
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"output": {"task_id": "t", "task_status": "RUNNING"}}`))
	}))
	t.Cleanup(srv.Close)

	c := dashscope.New("test-key", "cn-beijing", "", srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.PollTask(ctx, "t")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if elapsed >= blockFor {
		t.Errorf("request took %v, expected it to abort near the 100ms caller deadline, well before the %v upstream delay", elapsed, blockFor)
	}
}
