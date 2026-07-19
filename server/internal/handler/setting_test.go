package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	catalogmodel "github.com/chenhao/omnigen-ai/server/internal/model/catalog"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// memSettingRepo 是 handler 测试用的内存 SettingRepository，与 memRepo
// （auth_test.go 里的内存 UserRepository）同样的理由：让 httptest 打完整
// 中间件链的测试不需要启动数据库。
type memSettingRepo struct {
	rows map[string]settingmodel.Setting
}

func newMemSettingRepo() *memSettingRepo {
	return &memSettingRepo{rows: map[string]settingmodel.Setting{}}
}

func (m *memSettingRepo) Get(_ context.Context, key string) (*settingmodel.Setting, error) {
	row, ok := m.rows[key]
	if !ok {
		return nil, apperr.ErrSettingNotFound
	}
	clone := row
	return &clone, nil
}

func (m *memSettingRepo) GetAll(_ context.Context) ([]settingmodel.Setting, error) {
	out := make([]settingmodel.Setting, 0, len(m.rows))
	for _, row := range m.rows {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *memSettingRepo) Upsert(_ context.Context, s *settingmodel.Setting) error {
	s.UpdatedAt = time.Now()
	m.rows[s.Key] = *s
	return nil
}

func (m *memSettingRepo) UpsertMany(_ context.Context, items []settingmodel.Setting) error {
	now := time.Now()
	for _, it := range items {
		it.UpdatedAt = now
		m.rows[it.Key] = it
	}
	return nil
}

var _ repository.SettingRepository = (*memSettingRepo)(nil)

func decodeSettingsResponse(t *testing.T, body []byte) []settingmodel.SettingResponse {
	t.Helper()
	var resp struct {
		Data struct {
			Items []settingmodel.SettingResponse `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp.Data.Items
}

// ── 权限 ────────────────────────────────────────────────────────────

func TestSettings_PlainUserCanRead(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	e.seed(t, "plain", "password123", usermodel.RoleUser)
	token := e.login(t, "plain", "password123")

	w := e.request(t, http.MethodGet, "/api/settings", token, nil)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSettings_PlainUserCannotWrite(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	e.seed(t, "plain", "password123", usermodel.RoleUser)
	token := e.login(t, "plain", "password123")

	w := e.request(t, http.MethodPut, "/api/settings", token, gin.H{
		"items": []gin.H{{"key": "region", "value": "cn-beijing"}},
	})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "AUTH_FORBIDDEN", respCode(t, w))
}

func TestSettings_PlainUserCannotTestConnection(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	e.seed(t, "plain", "password123", usermodel.RoleUser)
	token := e.login(t, "plain", "password123")

	w := e.request(t, http.MethodPost, "/api/settings/test", token, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// ── POST /api/settings/test：provider 分派 ──────────────────────────────
//
// These exercise only the validation/short-circuit paths (unknown provider,
// unconfigured key) — both return before newTestEnv's real
// dashscope/t8star testers ever get a chance to make a network call, so
// they're safe to run without a fake ConnectionTester.

// 完全不带请求体（nil body）时，provider 应当按空字符串处理——等价于
// "dashscope"——而不是让 JSON 绑定报错。没配 dashscope_api_key 时应短路成
// SETTING_INCOMPLETE，证明请求确实按 dashscope 走了，而不是卡在绑定阶段。
func TestSettings_TestConnection_NoBodyDefaultsToDashscope(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	w := e.request(t, http.MethodPost, "/api/settings/test", token, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
	assert.Equal(t, "SETTING_INCOMPLETE", respCode(t, w))
}

// 非法 provider 值 → VALIDATION_FAILED，不是 500、也不是被悄悄当成
// dashscope 处理掉。
func TestSettings_TestConnection_InvalidProviderRejected(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	w := e.request(t, http.MethodPost, "/api/settings/test", token, gin.H{"provider": "openai"})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
	assert.Equal(t, "VALIDATION_FAILED", respCode(t, w))
}

// provider=t8star 但 t8star_api_key 未配置 → SETTING_INCOMPLETE，
// 且必须是这个码而不是 dashscope 那条路径可能报的任何错误——证明
// provider=t8star 确实路由到了 t8star 的凭证检查，不是被 provider=dashscope
// 的检查悄悄接管。
func TestSettings_TestConnection_T8starKeyMissing(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	w := e.request(t, http.MethodPost, "/api/settings/test", token, gin.H{"provider": "t8star"})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
	assert.Equal(t, "SETTING_INCOMPLETE", respCode(t, w))
}

func TestSettings_UnauthenticatedGetReturns401(t *testing.T) {
	e := newTestEnv(t)
	w := e.request(t, http.MethodGet, "/api/settings", "", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSettings_UnauthenticatedPutReturns401(t *testing.T) {
	e := newTestEnv(t)
	w := e.request(t, http.MethodPut, "/api/settings", "", gin.H{"items": []gin.H{}})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSettings_AdminCanWrite(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	w := e.request(t, http.MethodPut, "/api/settings", token, gin.H{
		"items": []gin.H{{"key": "region", "value": "cn-beijing"}},
	})
	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())

	items := decodeSettingsResponse(t, w.Body.Bytes())
	found := false
	for _, it := range items {
		if it.Key == "region" {
			found = true
			assert.Equal(t, "cn-beijing", it.Value)
		}
	}
	assert.True(t, found)
}

// ── 响应体绝不能带出明文密钥 ─────────────────────────────────────────

func TestSettings_ResponseNeverContainsPlaintextSecret(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	const plaintext = "sk-REALSECRETVALUE1234567890"
	w := e.request(t, http.MethodPut, "/api/settings", token, gin.H{
		"items": []gin.H{{"key": "dashscope_api_key", "value": plaintext}},
	})
	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())
	assert.NotContains(t, w.Body.String(), plaintext)
	assert.NotContains(t, w.Body.String(), "sk-REALSECRET")

	// 再单独 GET 一次，双重确认读路径也不带出明文。
	// 注意：masked 值本身合法地以 "sk-" 开头（如 "sk-REA...7890"），
	// 所以这里不能断言响应体完全不含 "sk-"，只能断言完整明文/明文中段
	// 不出现。
	get := e.request(t, http.MethodGet, "/api/settings", token, nil)
	require.Equal(t, http.StatusOK, get.Code)
	assert.NotContains(t, get.Body.String(), plaintext)
	assert.NotContains(t, get.Body.String(), "REALSECRETVALUE")

	items := decodeSettingsResponse(t, get.Body.Bytes())
	for _, it := range items {
		if it.Key == "dashscope_api_key" {
			assert.True(t, it.Configured)
			assert.NotContains(t, it.Masked, plaintext)
		}
	}
}

// ── 空值 = 不修改（通过 HTTP 层再验证一次这条语义） ─────────────────

func TestSettings_EmptyValueInPutLeavesSecretUnchanged(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	const plaintext = "sk-DO-NOT-WIPE-ME"
	require.Equal(t, http.StatusOK,
		e.request(t, http.MethodPut, "/api/settings", token, gin.H{
			"items": []gin.H{{"key": "dashscope_api_key", "value": plaintext}},
		}).Code)

	// 模拟前端表单：region 改了，密钥字段照旧留空一起提交。
	w := e.request(t, http.MethodPut, "/api/settings", token, gin.H{
		"items": []gin.H{
			{"key": "region", "value": "ap-southeast-1"},
			{"key": "dashscope_api_key", "value": ""},
		},
	})
	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())

	items := decodeSettingsResponse(t, w.Body.Bytes())
	for _, it := range items {
		if it.Key == "dashscope_api_key" {
			assert.True(t, it.Configured, "空值提交不应清空原有密钥")
		}
	}
}

func TestSettings_ClearTrueClearsSecret(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	require.Equal(t, http.StatusOK,
		e.request(t, http.MethodPut, "/api/settings", token, gin.H{
			"items": []gin.H{{"key": "dashscope_api_key", "value": "sk-to-clear"}},
		}).Code)

	w := e.request(t, http.MethodPut, "/api/settings", token, gin.H{
		"items": []gin.H{{"key": "dashscope_api_key", "clear": true}},
	})
	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())

	items := decodeSettingsResponse(t, w.Body.Bytes())
	for _, it := range items {
		if it.Key == "dashscope_api_key" {
			assert.False(t, it.Configured, "clear=true 之后应报告未配置")
		}
	}
}

// ── catalog ───────────────────────────────────────────────────────────

func TestCatalog_RequiresAuth(t *testing.T) {
	e := newTestEnv(t)
	w := e.request(t, http.MethodGet, "/api/catalog", "", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCatalog_ReturnsEveryModelID(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "plain", "password123", usermodel.RoleUser)
	token := e.login(t, "plain", "password123")

	w := e.request(t, http.MethodGet, "/api/catalog", token, nil)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Data struct {
			Models []catalogmodel.Model `json:"models"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	got := make(map[string]bool, len(resp.Data.Models))
	for _, m := range resp.Data.Models {
		got[m.ID] = true
	}
	for _, want := range catalogmodel.All() {
		assert.True(t, got[want.ID], "目录响应里缺少模型 %s", want.ID)
	}
}
