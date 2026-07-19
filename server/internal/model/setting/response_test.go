package setting_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
)

func TestFromEntity_SecretOmitsPlaintext(t *testing.T) {
	entity := settingmodel.Setting{
		Key:      string(settingmodel.KeyDashscopeAPIKey),
		Value:    "sk-SUPERSECRETPLAINTEXTVALUE1234567890",
		IsSecret: true,
	}

	raw, err := json.Marshal(settingmodel.FromEntity(entity))
	require.NoError(t, err)

	assert.NotContains(t, string(raw), "SUPERSECRETPLAINTEXTVALUE")
	assert.NotContains(t, string(raw), "sk-SUPERSECRET")
	assert.NotContains(t, strings.ToLower(string(raw)), `"value"`)
	assert.Contains(t, string(raw), `"configured":true`)
	assert.Contains(t, string(raw), `"masked":"sk-SUP...7890"`)
}

func TestFromEntity_SecretNotConfiguredHasNoMaskedLeak(t *testing.T) {
	entity := settingmodel.Setting{Key: string(settingmodel.KeyT8starAPIKey), Value: "", IsSecret: true}

	raw, err := json.Marshal(settingmodel.FromEntity(entity))
	require.NoError(t, err)

	assert.NotContains(t, string(raw), `"configured":true`)
}

func TestFromEntity_PlainKeyKeepsValue(t *testing.T) {
	entity := settingmodel.Setting{Key: string(settingmodel.KeyRegion), Value: "cn-beijing", IsSecret: false}

	resp := settingmodel.FromEntity(entity)

	assert.Equal(t, "cn-beijing", resp.Value)
	assert.False(t, resp.Configured)
	assert.Empty(t, resp.Masked)
}

func TestFromEntities_MixedSecretAndPlain(t *testing.T) {
	items := []settingmodel.Setting{
		{Key: string(settingmodel.KeyRegion), Value: "cn-beijing", IsSecret: false},
		{Key: string(settingmodel.KeyDashscopeAPIKey), Value: "sk-anothersecretvalue", IsSecret: true},
	}

	raw, err := json.Marshal(settingmodel.FromEntities(items))
	require.NoError(t, err)

	assert.NotContains(t, string(raw), "sk-anothersecretvalue")
	assert.Contains(t, string(raw), `"cn-beijing"`)
}

func TestKey_IsSecret(t *testing.T) {
	assert.True(t, settingmodel.KeyDashscopeAPIKey.IsSecret())
	assert.True(t, settingmodel.KeyT8starAPIKey.IsSecret())
	assert.True(t, settingmodel.KeyOSSAccessKeyID.IsSecret())
	assert.True(t, settingmodel.KeyOSSAccessKeySecret.IsSecret())
	assert.False(t, settingmodel.KeyRegion.IsSecret())
	assert.False(t, settingmodel.KeyDashscopeEndpoint.IsSecret())
	assert.False(t, settingmodel.KeyT8starEndpoint.IsSecret())
	assert.False(t, settingmodel.KeyWorkspaceID.IsSecret())
	assert.False(t, settingmodel.KeyOSSBucket.IsSecret())
	assert.False(t, settingmodel.KeyOSSRegion.IsSecret())
	assert.False(t, settingmodel.KeyOSSRoleArn.IsSecret())
}
