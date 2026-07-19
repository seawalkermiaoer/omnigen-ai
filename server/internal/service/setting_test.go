package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/crypto"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// testEncryptionKey 是与 crypto_test.go 同一把合法测试密钥：32 字节原始数据，
// 直接传给 NewSettingServiceWithTester，不再经由环境变量。
var testEncryptionKey = []byte("01234567890123456789012345678901")[:32]

func newSettingService(t *testing.T) (*service.SettingService, *fakeSettingRepo, *fakeConnectionTester) {
	t.Helper()
	repo := newFakeSettingRepo()
	tester := &fakeConnectionTester{t: t}
	return service.NewSettingServiceWithTester(repo, testEncryptionKey, tester), repo, tester
}

func settingByKey(t *testing.T, resp *settingmodel.SettingsResponse, key settingmodel.Key) settingmodel.SettingResponse {
	t.Helper()
	for _, item := range resp.Items {
		if item.Key == string(key) {
			return item
		}
	}
	t.Fatalf("响应里没有找到配置项 %q", key)
	return settingmodel.SettingResponse{}
}

// ── 往返：更新一个密钥、读回脱敏值、GetDecrypted 拿到原始明文 ──────────

func TestSettingService_UpdateThenGet_SecretRoundTrips(t *testing.T) {
	svc, _, _ := newSettingService(t)
	ctx := context.Background()

	const plaintext = "sk-SUPERSECRETPLAINTEXTVALUE1234567890"
	resp, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyDashscopeAPIKey, Value: plaintext}},
	})
	require.NoError(t, err)

	item := settingByKey(t, resp, settingmodel.KeyDashscopeAPIKey)
	assert.True(t, item.Configured)
	assert.Equal(t, crypto.Mask(plaintext), item.Masked)
	assert.Empty(t, item.Value, "响应体不得携带明文")

	got, err := svc.GetDecrypted(ctx, settingmodel.KeyDashscopeAPIKey)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)
}

func TestSettingService_UpdateThenGet_PlainKeyRoundTrips(t *testing.T) {
	svc, _, _ := newSettingService(t)
	ctx := context.Background()

	resp, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyRegion, Value: "cn-beijing"}},
	})
	require.NoError(t, err)

	item := settingByKey(t, resp, settingmodel.KeyRegion)
	assert.Equal(t, "cn-beijing", item.Value)

	got, err := svc.GetDecrypted(ctx, settingmodel.KeyRegion)
	require.NoError(t, err)
	assert.Equal(t, "cn-beijing", got)
}

// ── AAD 绑定：把 dashscope 密钥的密文搬到 t8star 密钥那一行必须解密失败 ──

func TestSettingService_AADBindsCiphertextToItsOwnKey(t *testing.T) {
	svc, repo, _ := newSettingService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyDashscopeAPIKey, Value: "sk-dashscope-real-key"}},
	})
	require.NoError(t, err)

	stored, err := repo.Get(ctx, string(settingmodel.KeyDashscopeAPIKey))
	require.NoError(t, err)
	require.NotEmpty(t, stored.Value)

	// 直接用同一份密文冒充 t8star 的密文，AAD 用错了 key，必须解密失败。
	_, err = crypto.Decrypt(testEncryptionKey, stored.Value, string(settingmodel.KeyT8starAPIKey))
	require.Error(t, err)

	// 用正确的 AAD 解密仍然成功——证明上面失败确实是 AAD 不匹配导致的，
	// 不是密文本身就坏了。
	plain, err := crypto.Decrypt(testEncryptionKey, stored.Value, string(settingmodel.KeyDashscopeAPIKey))
	require.NoError(t, err)
	assert.Equal(t, "sk-dashscope-real-key", plain)
}

// 同一断言换个角度：真的把密文搬到另一行的 repository 记录里，走
// service.GetDecrypted(t8star_api_key) 必须报错，不能返回一个"看起来有效
// 但完全错误"的凭证。
func TestSettingService_AADBindsCiphertextToItsOwnKey_ViaGetDecrypted(t *testing.T) {
	svc, repo, _ := newSettingService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyDashscopeAPIKey, Value: "sk-dashscope-real-key"}},
	})
	require.NoError(t, err)

	dashscopeCipher, err := repo.Get(ctx, string(settingmodel.KeyDashscopeAPIKey))
	require.NoError(t, err)

	// 手动把 dashscope 的密文挪到 t8star 那一行——模拟"密文被拷到错误的列"。
	repo.rows[string(settingmodel.KeyT8starAPIKey)] = settingmodel.Setting{
		Key:      string(settingmodel.KeyT8starAPIKey),
		Value:    dashscopeCipher.Value,
		IsSecret: true,
	}

	_, err = svc.GetDecrypted(ctx, settingmodel.KeyT8starAPIKey)
	require.Error(t, err, "AAD 不匹配必须报错，不能悄悄返回一个错误但看似有效的凭证")
}

// ── 空值 = 不修改；Clear=true 才真正清空 ────────────────────────────────

func TestSettingService_EmptyValueLeavesSecretUnchanged(t *testing.T) {
	svc, _, _ := newSettingService(t)
	ctx := context.Background()

	const plaintext = "sk-original-secret-value"
	_, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyDashscopeAPIKey, Value: plaintext}},
	})
	require.NoError(t, err)

	// 模拟前端表单：只改了 region，密钥字段留空提交。
	_, err = svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{
			{Key: settingmodel.KeyRegion, Value: "ap-southeast-1"},
			{Key: settingmodel.KeyDashscopeAPIKey, Value: ""},
		},
	})
	require.NoError(t, err)

	got, err := svc.GetDecrypted(ctx, settingmodel.KeyDashscopeAPIKey)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got, "空值提交不应清空原有密钥")

	region, err := svc.GetDecrypted(ctx, settingmodel.KeyRegion)
	require.NoError(t, err)
	assert.Equal(t, "ap-southeast-1", region)
}

func TestSettingService_ClearTrueActuallyClearsSecret(t *testing.T) {
	svc, _, _ := newSettingService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyDashscopeAPIKey, Value: "sk-to-be-cleared"}},
	})
	require.NoError(t, err)

	resp, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyDashscopeAPIKey, Clear: true}},
	})
	require.NoError(t, err)

	item := settingByKey(t, resp, settingmodel.KeyDashscopeAPIKey)
	assert.False(t, item.Configured, "Clear=true 之后应报告未配置")

	got, err := svc.GetDecrypted(ctx, settingmodel.KeyDashscopeAPIKey)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSettingService_ClearTrueActuallyClearsPlainKey(t *testing.T) {
	svc, _, _ := newSettingService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyRegion, Value: "cn-beijing"}},
	})
	require.NoError(t, err)

	_, err = svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyRegion, Clear: true}},
	})
	require.NoError(t, err)

	got, err := svc.GetDecrypted(ctx, settingmodel.KeyRegion)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Clear=true 且同时带了 Value：Clear 优先，Value 被忽略。
func TestSettingService_ClearTrueIgnoresAccompanyingValue(t *testing.T) {
	svc, _, _ := newSettingService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyRegion, Value: "should-be-ignored", Clear: true}},
	})
	require.NoError(t, err)

	got, err := svc.GetDecrypted(ctx, settingmodel.KeyRegion)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// ── Get 绝不返回明文 ─────────────────────────────────────────────────

func TestSettingService_Get_NeverReturnsPlaintext(t *testing.T) {
	svc, _, _ := newSettingService(t)
	ctx := context.Background()

	const plaintext = "sk-MUST-NEVER-LEAK-1234567890"
	_, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyDashscopeAPIKey, Value: plaintext}},
	})
	require.NoError(t, err)

	resp, err := svc.Get(ctx)
	require.NoError(t, err)

	raw, err := json.Marshal(resp)
	require.NoError(t, err)

	assert.NotContains(t, string(raw), plaintext)
	assert.NotContains(t, string(raw), "sk-MUST-NEVER-LEAK")
}

// Get 总是覆盖 settingmodel.AllKeys 的全集，即便某些键从未被写过。
func TestSettingService_Get_AlwaysListsAllKeys(t *testing.T) {
	svc, _, _ := newSettingService(t)
	ctx := context.Background()

	resp, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Len(t, resp.Items, len(settingmodel.AllKeys))

	item := settingByKey(t, resp, settingmodel.KeyDashscopeAPIKey)
	assert.False(t, item.Configured)
}

// Task 2 作者的提醒：FromEntity 假定传入的 Value 已经是明文。
// 这里断言 Masked 是根据真正的明文首尾算出来的（"sk-MUS...7890"），
// 而不是密文的首尾——如果 Get() 忘了在映射前解密，这个断言会失败，
// 因为 Masked 会变成一串看起来完全不同的乱码脱敏值。
func TestSettingService_Get_DecryptsBeforeMapping(t *testing.T) {
	svc, _, _ := newSettingService(t)
	ctx := context.Background()

	const plaintext = "sk-MUSTDECRYPTFIRST7890"
	_, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyDashscopeAPIKey, Value: plaintext}},
	})
	require.NoError(t, err)

	resp, err := svc.Get(ctx)
	require.NoError(t, err)

	item := settingByKey(t, resp, settingmodel.KeyDashscopeAPIKey)
	assert.Equal(t, crypto.Mask(plaintext), item.Masked)
	assert.NotEqual(t, strings.ToLower(item.Masked), strings.ToLower(plaintext))
}

// ── Update 校验 ─────────────────────────────────────────────────────

func TestSettingService_Update_RejectsUnknownKey(t *testing.T) {
	svc, _, _ := newSettingService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.Key("not_a_real_key"), Value: "x"}},
	})
	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, "VALIDATION_FAILED", appErr.Code())
}

// 未知 key 会让整个请求失败，已知的合法项也不能借机落地。
func TestSettingService_Update_UnknownKeyRejectsWholeBatch(t *testing.T) {
	svc, _, _ := newSettingService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{
			{Key: settingmodel.KeyRegion, Value: "cn-beijing"},
			{Key: settingmodel.Key("bogus"), Value: "x"},
		},
	})
	require.Error(t, err)

	got, err := svc.GetDecrypted(ctx, settingmodel.KeyRegion)
	require.NoError(t, err)
	assert.Empty(t, got, "校验失败时不应有任何一项落地")
}

// ── TestConnection ───────────────────────────────────────────────────

func TestSettingService_TestConnection_FailsWhenAPIKeyNotConfigured(t *testing.T) {
	svc, _, tester := newSettingService(t)
	tester.forbid = true // 没配凭证就不该打任何网络请求

	err := svc.TestConnection(context.Background())
	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, "SETTING_INCOMPLETE", appErr.Code())
}

func TestSettingService_TestConnection_PassesDecryptedCredsToTester(t *testing.T) {
	svc, _, tester := newSettingService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{
			{Key: settingmodel.KeyDashscopeAPIKey, Value: "sk-real-test-key"},
			{Key: settingmodel.KeyRegion, Value: "cn-beijing"},
			{Key: settingmodel.KeyWorkspaceID, Value: "ws-1"},
		},
	})
	require.NoError(t, err)

	require.NoError(t, svc.TestConnection(ctx))
	require.Len(t, tester.calls, 1)
	assert.Equal(t, "sk-real-test-key", tester.calls[0].APIKey, "传给探测器的必须是明文，不是密文")
	assert.Equal(t, "cn-beijing", tester.calls[0].Region)
	assert.Equal(t, "ws-1", tester.calls[0].WorkspaceID)
}

func TestSettingService_TestConnection_PropagatesTesterFailure(t *testing.T) {
	svc, _, tester := newSettingService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, 1, settingmodel.UpdateRequest{
		Items: []settingmodel.UpdateItem{{Key: settingmodel.KeyDashscopeAPIKey, Value: "sk-real-test-key"}},
	})
	require.NoError(t, err)

	tester.fail = apperr.ErrUpstreamTestFailed.Wrap(errors.New("boom"))
	err = svc.TestConnection(ctx)
	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, "SETTING_TEST_FAILED", appErr.Code())
}
