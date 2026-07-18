package t8star_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/t8star"
)

func TestGenerateImage_RequestShape_MethodPathAuthHeader(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(realFixture))
	}))
	defer srv.Close()

	client := t8star.New("sk-test-key", srv.URL)
	_, err := client.GenerateImage(context.Background(), provider.ImageRequest{
		Model:  "gpt-image-2",
		Prompt: "画只猫",
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/v1/chat/completions", gotPath)
	assert.Equal(t, "Bearer sk-test-key", gotAuth)
	assert.Equal(t, "application/json", gotContentType)
}

func TestGenerateImageWithNote_Success_RealFixture_ReturnsParsedResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(realFixture))
	}))
	defer srv.Close()

	client := t8star.New("sk-test", srv.URL)
	result, err := client.GenerateImageWithNote(context.Background(), provider.ImageRequest{
		Model:  "gpt-image-2",
		Prompt: "画只猫",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, []string{"https://webstatic.aiproxy.vip/output/0276be3f.png"}, result.Images)
	assert.Equal(t, "给你画好了，一只可爱的小猫。", result.Note)
	assert.Equal(t, float64(3780), result.Usage["prompt_tokens"])
	assert.Equal(t, float64(4663), result.Usage["total_tokens"])
}

func TestGenerateImage_Success_RealFixture_DropsNoteButKeepsImagesUsageModel(t *testing.T) {
	// GenerateImage is the provider.ImageProvider-satisfying entry point;
	// provider.ImageResult has no Note field (see client.go's doc comment
	// on GenerateImage), so this only asserts what does carry through.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(realFixture))
	}))
	defer srv.Close()

	client := t8star.New("sk-test", srv.URL)
	result, err := client.GenerateImage(context.Background(), provider.ImageRequest{
		Model:  "gpt-image-2",
		Prompt: "画只猫",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, []string{"https://webstatic.aiproxy.vip/output/0276be3f.png"}, result.Images)
	assert.Equal(t, "gpt-image-2-pro", result.Model)
	assert.Equal(t, float64(3780), result.Usage["prompt_tokens"])
}

func TestGenerateImage_EchoedUpstreamModel_PreferredOverRequested(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(realFixture))
	}))
	defer srv.Close()

	client := t8star.New("sk-test", srv.URL)
	// Fixture echoes "gpt-image-2-pro" even though we request "gpt-image-2".
	result, err := client.GenerateImage(context.Background(), provider.ImageRequest{
		Model:  "gpt-image-2",
		Prompt: "画只猫",
	})
	require.NoError(t, err)
	assert.Equal(t, "gpt-image-2-pro", result.Model)
}

func TestGenerateImage_ModelNotEchoed_FallsBackToRequested(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"content": "![image](https://a/1.png)"}}],
			"usage": {}
		}`))
	}))
	defer srv.Close()

	client := t8star.New("sk-test", srv.URL)
	result, err := client.GenerateImage(context.Background(), provider.ImageRequest{
		Model:  "gpt-image-2",
		Prompt: "x",
	})
	require.NoError(t, err)
	assert.Equal(t, "gpt-image-2", result.Model)
}

func TestGenerateImage_Upstream4xx_ReturnsDistinctUpstreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "invalid api key", "type": "invalid_request_error"}}`))
	}))
	defer srv.Close()

	client := t8star.New("bad-key", srv.URL)
	result, err := client.GenerateImage(context.Background(), provider.ImageRequest{
		Model:  "gpt-image-2",
		Prompt: "x",
	})
	require.Error(t, err)
	assert.Nil(t, result)

	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, t8star.CodeUpstreamHTTP, appErr.Code())
	// Old server.js forwards t8r.status verbatim (res.status(t8r.status)),
	// not a fixed collapsed status.
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus())
	assert.ErrorContains(t, err, "invalid api key")
	assert.True(t, errors.Is(err, t8star.ErrUpstreamHTTP))
}

func TestGenerateImage_200WithErrorBody_ReturnsUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error": {"message": "content policy violation"}}`))
	}))
	defer srv.Close()

	client := t8star.New("sk-test", srv.URL)
	result, err := client.GenerateImage(context.Background(), provider.ImageRequest{
		Model:  "gpt-image-2",
		Prompt: "x",
	})
	require.Error(t, err)
	assert.Nil(t, result)

	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, t8star.CodeUpstreamError, appErr.Code())
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus())
	assert.ErrorContains(t, err, "content policy violation")
	assert.True(t, errors.Is(err, t8star.ErrUpstreamError))
}

func TestGenerateImage_ZeroImagesParsed_ReturnsNoImageResultError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"model": "gpt-image-2-pro",
			"choices": [{"message": {"content": "sorry, I can't help with that."}}],
			"usage": {}
		}`))
	}))
	defer srv.Close()

	client := t8star.New("sk-test", srv.URL)
	result, err := client.GenerateImage(context.Background(), provider.ImageRequest{
		Model:  "gpt-image-2",
		Prompt: "x",
	})
	require.Error(t, err)
	assert.Nil(t, result)

	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, t8star.CodeNoImageResult, appErr.Code())
	assert.Equal(t, http.StatusBadGateway, appErr.HTTPStatus())
	assert.True(t, errors.Is(err, t8star.ErrNoImageResult))
}

func TestGenerateImage_ResolvesEndpointConfiguredAtConstruction(t *testing.T) {
	// endpoint/apiKey are fixed at New(), not per-request (matches
	// dashscope.Client's convention) — GenerateImage still hits the
	// constructed base URL without needing it repeated in the request.
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(realFixture))
	}))
	defer srv.Close()

	client := t8star.New("sk-test", srv.URL)
	_, err := client.GenerateImage(context.Background(), provider.ImageRequest{Model: "gpt-image-2", Prompt: "x"})
	require.NoError(t, err)
	assert.Equal(t, "/v1/chat/completions", gotPath)
}
