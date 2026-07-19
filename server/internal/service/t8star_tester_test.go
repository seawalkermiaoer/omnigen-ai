package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

// 每个用例都用生产环境的 newT8starConnectionTester()（内部就是真正的
// t8star.Client），只是把 creds.Endpoint 指向一个 httptest.Server——
// t8star.Client 对任意合法的 http(s) URL 一视同仁（ResolveBaseURL 只在输入
// 不是合法 URL 时才回落到官方地址），不需要额外的 stub provider：这正是
// "复用 provider/t8star 的 Client" 的字面意思。

// 上游 401/403 → apperr.ErrUpstreamAuthFailed。这是唯一让测试连接判定
// "凭证无效"的两个状态码。
func TestT8starTester_AuthFailure(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"message": "invalid api key", "type": "invalid_request_error"},
				})
			}))
			defer server.Close()

			tester := newT8starConnectionTester()
			err := tester.Test(context.Background(), UpstreamCredentials{APIKey: "sk-bad", Endpoint: server.URL})

			require.Error(t, err)
			var appErr *apperr.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, "UPSTREAM_AUTH_FAILED", appErr.Code())
		})
	}
}

// 上游返回一个 4xx 业务错误（不是 401/403）→ 判定为成功（凭证有效）。
// 这条极容易写反：直觉上"出错了"应该判失败，但测试连接验证的是凭证，
// 不是验证这次调用本身成不成功——收到任何响应，就说明鉴权通过了。
// 必须显式断言 err 为 nil，不能只断言"不是 UPSTREAM_AUTH_FAILED"。
func TestT8starTester_BusinessErrorMeansCredentialValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest) // 400，不是 401/403
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "model overloaded, try again later", "type": "server_error"},
		})
	}))
	defer server.Close()

	tester := newT8starConnectionTester()
	err := tester.Test(context.Background(), UpstreamCredentials{APIKey: "sk-valid", Endpoint: server.URL})

	require.NoError(t, err, "4xx 业务错误应当被判定为凭证有效（成功），不是失败")
}

// 200 OK 但响应体里嵌了 error 字段（t8star 的另一种"业务失败"形状）—
// 同样意味着凭证有效：请求确实被上游处理了。
func TestT8starTester_200WithEmbeddedError_MeansCredentialValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "prompt rejected by content policy"},
		})
	}))
	defer server.Close()

	tester := newT8starConnectionTester()
	err := tester.Test(context.Background(), UpstreamCredentials{APIKey: "sk-valid", Endpoint: server.URL})

	require.NoError(t, err)
}

// 200 OK、解析干净、但没有任何图片链接（探测用的空 prompt 大概率走到这
// 里）——依然是凭证有效：拿到了一个可信的响应。
func TestT8starTester_200NoImage_MeansCredentialValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "gpt-image-2",
			"choices": []map[string]any{
				{"message": map[string]any{"content": "抱歉，我没有理解你的请求。"}},
			},
		})
	}))
	defer server.Close()

	tester := newT8starConnectionTester()
	err := tester.Test(context.Background(), UpstreamCredentials{APIKey: "sk-valid", Endpoint: server.URL})

	require.NoError(t, err)
}

// 200 OK 且真的解析出一张图——最贵的分支，但同样只证明凭证有效，不代表
// 测试连接应该以"图生成成功"为目标（这正是本方案要避免的语义）。
func TestT8starTester_200WithImage_MeansCredentialValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "gpt-image-2",
			"choices": []map[string]any{
				{"message": map[string]any{"content": "给你画好了。![result](https://example.com/out.png)"}},
			},
		})
	}))
	defer server.Close()

	tester := newT8starConnectionTester()
	err := tester.Test(context.Background(), UpstreamCredentials{APIKey: "sk-valid", Endpoint: server.URL})

	require.NoError(t, err)
}

// 连接失败（服务端在请求发出前就已关闭，握手都建立不起来）→
// apperr.ErrUpstreamFailed。这是唯一的"没有任何响应"分支，必须与上面
// "收到了 4xx/5xx 响应"的分支区分开——网络层失败不能被误判成"凭证有效"。
func TestT8starTester_NetworkFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	closedURL := server.URL
	server.Close() // 关闭后这个地址上没有任何东西在监听

	tester := newT8starConnectionTester()
	err := tester.Test(context.Background(), UpstreamCredentials{APIKey: "sk-valid", Endpoint: closedURL})

	require.Error(t, err)
	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, "UPSTREAM_FAILED", appErr.Code())
}
