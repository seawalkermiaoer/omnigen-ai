package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/ossx"
)

// White-box tests for settingsOSSResolver, the production OSSResolver —
// they live in package service (not service_test) because settingsOSSResolver
// and its newSTSClient injection point are unexported, mirroring
// internal/provider/dashscope's endpoint_test.go/json_test.go convention
// for testing unexported helpers.

// mapSettingReader is a trivial in-memory SettingReader: no crypto, no
// repository, just whatever the test put in the map. Missing keys resolve
// to "", matching SettingService.GetDecrypted's "never configured == empty
// string" contract.
type mapSettingReader map[settingmodel.Key]string

func (m mapSettingReader) GetDecrypted(_ context.Context, key settingmodel.Key) (string, error) {
	return m[key], nil
}

// noopAssumeRoleClient satisfies ossx.AssumeRoleClient without ever being
// called in these tests — they only assert on *how many times* the STS
// client constructor ran, not on AssumeRole behavior (that's
// credentials_test.go's job, in package ossx).
type noopAssumeRoleClient struct{}

func (noopAssumeRoleClient) AssumeRole(context.Context, string, string, int64) (ossx.Credentials, error) {
	return ossx.Credentials{}, errors.New("unexpected: AssumeRole should not be called by these tests")
}

func TestSettingsOSSResolver_MissingCredentials_ReturnsClearNotConfiguredError(t *testing.T) {
	r := newSettingsOSSResolver(mapSettingReader{})

	_, err := r.Resolve(context.Background())
	require.Error(t, err)

	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.ErrUploadOSSNotConfigured.Code(), appErr.Code())
}

func TestSettingsOSSResolver_DirectMode_NoRoleArn_BuildsStoreWithoutSTS(t *testing.T) {
	settings := mapSettingReader{
		settingmodel.KeyOSSAccessKeyID:     "ak",
		settingmodel.KeyOSSAccessKeySecret: "sk",
		settingmodel.KeyOSSBucket:          "my-bucket",
		settingmodel.KeyOSSRegion:          "oss-cn-hangzhou",
	}
	stsCalls := 0
	r := newSettingsOSSResolver(settings)
	r.newSTSClient = func(string, string) (ossx.AssumeRoleClient, error) {
		stsCalls++
		return noopAssumeRoleClient{}, nil
	}

	store, err := r.Resolve(context.Background())
	require.NoError(t, err)
	require.NotNil(t, store)
	assert.Zero(t, stsCalls, "没配置 roleArn 时不应该构造 STS 客户端")

	// SignedURL 是纯本地签名计算，不发网络请求（见
	// internal/pkg/ossx/client_test.go 的说明），可以放心直接调用来验证
	// endpoint 确实来自配置的 region。
	url, err := store.SignedURL(context.Background(), "k.jpg")
	require.NoError(t, err)
	assert.Contains(t, url, "oss-cn-hangzhou.aliyuncs.com")
}

func TestSettingsOSSResolver_DefaultsBucketAndRegion_WhenUnset(t *testing.T) {
	settings := mapSettingReader{
		settingmodel.KeyOSSAccessKeyID:     "ak",
		settingmodel.KeyOSSAccessKeySecret: "sk",
		// bucket/region 留空，应落回 server.js 同款默认值。
	}
	r := newSettingsOSSResolver(settings)

	store, err := r.Resolve(context.Background())
	require.NoError(t, err)

	url, err := store.SignedURL(context.Background(), "k.jpg")
	require.NoError(t, err)
	assert.Contains(t, url, defaultOSSRegion+".aliyuncs.com")
}

func TestSettingsOSSResolver_STSMode_CachesCredentialCacheAcrossCalls(t *testing.T) {
	settings := mapSettingReader{
		settingmodel.KeyOSSAccessKeyID:     "ak",
		settingmodel.KeyOSSAccessKeySecret: "sk",
		settingmodel.KeyOSSBucket:          "bucket",
		settingmodel.KeyOSSRoleArn:         "acs:ram::123:role/upload",
	}
	stsCalls := 0
	r := newSettingsOSSResolver(settings)
	r.newSTSClient = func(string, string) (ossx.AssumeRoleClient, error) {
		stsCalls++
		return noopAssumeRoleClient{}, nil
	}

	_, err := r.Resolve(context.Background())
	require.NoError(t, err)
	_, err = r.Resolve(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, stsCalls,
		"roleArn/accessKeyID 不变时，第二次 Resolve 必须复用同一个 CredentialCache，"+
			"否则 STS 客户端会在每次大文件上传时重新构造，等于绕开了缓存")
}

func TestSettingsOSSResolver_STSMode_RebuildsCacheWhenRoleArnChanges(t *testing.T) {
	settings := mapSettingReader{
		settingmodel.KeyOSSAccessKeyID:     "ak",
		settingmodel.KeyOSSAccessKeySecret: "sk",
		settingmodel.KeyOSSBucket:          "bucket",
		settingmodel.KeyOSSRoleArn:         "acs:ram::123:role/upload-a",
	}
	stsCalls := 0
	r := newSettingsOSSResolver(settings)
	r.newSTSClient = func(string, string) (ossx.AssumeRoleClient, error) {
		stsCalls++
		return noopAssumeRoleClient{}, nil
	}

	_, err := r.Resolve(context.Background())
	require.NoError(t, err)

	settings[settingmodel.KeyOSSRoleArn] = "acs:ram::123:role/upload-b"
	_, err = r.Resolve(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, stsCalls, "admin 换了 RAM 角色后，下一次上传应该重新构造 STS 客户端而不是沿用旧缓存")
}
