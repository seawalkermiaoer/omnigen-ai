package router_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/router"
)

// preflight 发一个真实的 CORS 预检请求，返回响应头。
// 浏览器就是靠这一次 OPTIONS 的响应头来决定放不放行真实请求的，
// 所以断言必须打在响应头上，而不是打在配置结构体上。
func preflight(t *testing.T, r *gin.Engine, origin string) http.Header {
	t.Helper()
	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Result().Header
}

func newRouter(t *testing.T, origins []string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := router.New(router.Handlers{}, nil, nil, origins)
	require.NotNil(t, r)
	return r
}

// 白名单模式的既有行为不能被通配改动破坏。
func TestCORS_Allowlist_OnlyListedOriginAllowed(t *testing.T) {
	r := newRouter(t, []string{"http://localhost:5173"})

	assert.Equal(t, "http://localhost:5173",
		preflight(t, r, "http://localhost:5173").Get("Access-Control-Allow-Origin"))

	assert.Empty(t, preflight(t, r, "https://evil.example").Get("Access-Control-Allow-Origin"),
		"未列入白名单的 origin 不应拿到放行头")
}

// 这是本次要加的能力：cors.origins 配成 ["*"] 时任何 origin 都放行。
func TestCORS_Wildcard_AllowsAnyOrigin(t *testing.T) {
	r := newRouter(t, []string{"*"})

	for _, origin := range []string{
		"http://192.168.1.10:8000",
		"https://some.random.host",
		"http://localhost:9999",
	} {
		assert.Equal(t, origin, preflight(t, r, origin).Get("Access-Control-Allow-Origin"),
			"通配模式下应把请求自带的 origin 原样回显")
	}
}

// 这条是通配实现最容易写错的地方，必须单独钉住。
//
// CORS 规范禁止 `Access-Control-Allow-Origin: *` 与
// `Access-Control-Allow-Credentials: true` 同时出现——浏览器会直接拒绝整个
// 响应。所以"放行所有来源"不能真的回字面量 `*`，只能回显请求自带的 Origin。
// 如果哪天有人图省事把实现改成回 `*`，跨域请求会在浏览器侧全部失败，
// 而服务端日志一切正常，非常难查。
func TestCORS_Wildcard_NeverEmitsLiteralStarWithCredentials(t *testing.T) {
	h := preflight(t, newRouter(t, []string{"*"}), "https://some.random.host")

	if h.Get("Access-Control-Allow-Credentials") == "true" {
		assert.NotEqual(t, "*", h.Get("Access-Control-Allow-Origin"),
			"允许携带凭证时回字面量 * 会被浏览器整体拒绝")
	}
}
