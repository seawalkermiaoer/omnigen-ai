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

// 探测请求发的是哨兵模型名 t8starProbeModel，不是真实模型 gpt-image-2。
// 这是本次改动的核心：用真实模型探测时空 prompt 不会被提前拒绝，会真的
// 生成一张图并计费（见 t8starProbeModel 定义处记录的实测数据：29.2s、
// HTTP 200、扣费 1155 tokens）。哨兵模型名让上游在鉴权之后、真正路由到
// 某个模型之前就返回错误——快、免费、不出图。
func TestT8starTester_SendsSentinelModelNotRealModel(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "invalid_request",
				"message": "所有分组对于模型 __omnigen_credential_probe__ 无可用渠道，请检查模型是否存在或联系管理员",
				"type":    "new_api_error",
			},
		})
	}))
	defer server.Close()

	tester := newT8starConnectionTester()
	err := tester.Test(context.Background(), UpstreamCredentials{APIKey: "sk-valid", Endpoint: server.URL})

	require.NoError(t, err)
	require.NotNil(t, gotBody["model"])
	assert.Equal(t, t8starProbeModel, gotBody["model"])
	assert.NotEqual(t, "gpt-image-2", gotBody["model"], "must never probe with the real billable model")
}

// 上游对哨兵模型名返回 503 业务错误（"无可用渠道"）→ 判定为凭证有效。
// 这正是用真实 Key 实测时观测到的响应——鉴权先于模型路由完成，一个不存在
// 的模型名不会被拒在鉴权之前。这条最容易写反：直觉上"上游报错了"应该判
// 失败，但测试连接验证的是凭证能不能通过鉴权，不是这次调用本身成不成功。
func TestT8starTester_503BusinessErrorMeansCredentialValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "invalid_request",
				"message": "所有分组对于模型 __omnigen_credential_probe__ 无可用渠道，请检查模型是否存在或联系管理员",
				"type":    "new_api_error",
			},
		})
	}))
	defer server.Close()

	tester := newT8starConnectionTester()
	err := tester.Test(context.Background(), UpstreamCredentials{APIKey: "sk-valid", Endpoint: server.URL})

	require.NoError(t, err, "503 业务错误（无可用渠道）应当被判定为凭证有效（成功），不是失败")
}

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

// 200 OK、解析干净、但没有任何图片链接——依然是凭证有效：拿到了一个可信
// 的响应。（用哨兵模型名探测时这个分支实际上打不到——上游会在鉴权之后就
// 因为模型不存在而报错，走不到 200——但分类逻辑本身仍要覆盖这个响应形状。）
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

// 200 OK 且真的解析出一张图——同样只证明凭证有效，不代表测试连接应该以
// "图生成成功"为目标（这正是本方案要避免的语义）。用哨兵模型名探测时这
// 个分支不会在真实上游出现，但分类逻辑不应该假设它不会出现。
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
