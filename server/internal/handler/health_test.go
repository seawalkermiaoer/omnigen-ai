package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
)

// fakePinger 是测试用的 Pinger 实现，通过 err 字段控制 Ping 的返回值。
type fakePinger struct {
	err error
}

func (f fakePinger) Ping(ctx context.Context) error {
	return f.err
}

func TestHealth_ReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handler.NewHealthHandler(nil)
	r.GET("/api/health", h.Check)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, common.CodeOK, resp.Code)
}

func TestHealth_DatabaseUp_ReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handler.NewHealthHandler(fakePinger{err: nil})
	r.GET("/api/health", h.Check)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, common.CodeOK, resp.Code)

	data, ok := resp.Data.(map[string]any)
	require.True(t, ok, "expected Data to be an object")
	assert.Equal(t, "up", data["database"])
	assert.Equal(t, "up", data["service"])
}

func TestHealth_DatabaseDown_ReturnsServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handler.NewHealthHandler(fakePinger{err: errors.New("connection refused")})
	r.GET("/api/health", h.Check)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "HEALTH_DB_UNREACHABLE", resp.Code)

	data, ok := resp.Data.(map[string]any)
	require.True(t, ok, "expected Data to be an object")
	assert.Equal(t, "down", data["database"])
	assert.Equal(t, "up", data["service"])
}
