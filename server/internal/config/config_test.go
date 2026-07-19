package config_test

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/config"
)

// testJWTSecret 长度 ≥32 字节，满足 Load() 的最小长度校验。
const testJWTSecret = "test-secret-value-0123456789abcdef"

// testEncryptionKey 是合法的 app_encryption_key：32 原始字节的 base64 编码，
// 满足 Load() 的 AES-256 密钥长度校验。
const testEncryptionKey = "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=" // base64(32 bytes)

// writeConfig 把 body 写进 t.TempDir() 下的一个临时文件，返回其路径。
// body 是 YAML 文档正文，调用方按需拼装。
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// requiredOnly 是仅含三个必填项、其余字段全部留空（走默认值）的最小合法配置。
const requiredOnly = `
jwt:
  secret: "` + testJWTSecret + `"
bootstrap:
  admin_password: "admin12345"
app_encryption_key: "` + testEncryptionKey + `"
`

func TestLoad_AppliesDefaults(t *testing.T) {
	path := writeConfig(t, requiredOnly)

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, "localhost", cfg.DB.Host)
	assert.Equal(t, 5432, cfg.DB.Port)
	assert.Equal(t, "postgres", cfg.DB.User)
	assert.Equal(t, "123456", cfg.DB.Password)
	assert.Equal(t, "omnigen", cfg.DB.Name)
	assert.Equal(t, "disable", cfg.DB.SSLMode)
	assert.Equal(t, 8080, cfg.HTTPPort)
	assert.Equal(t, 168*time.Hour, cfg.JWT.TTL)
	assert.Equal(t, "admin", cfg.Bootstrap.Username)
	assert.Equal(t, testEncryptionKey, cfg.AppEncryptionKey)
}

func TestLoad_FullYAMLProducesExpectedConfig(t *testing.T) {
	path := writeConfig(t, `
http:
  port: 9090

database:
  host: db.internal
  port: 5555
  user: appuser
  password: "s3cret"
  name: omnigen_prod
  sslmode: require

jwt:
  secret: "`+testJWTSecret+`"
  ttl: 2h

app_encryption_key: "`+testEncryptionKey+`"

bootstrap:
  admin_username: root
  admin_password: "admin12345"

cors:
  origins:
    - https://app.example.com
    - https://admin.example.com
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, 9090, cfg.HTTPPort)
	assert.Equal(t, "db.internal", cfg.DB.Host)
	assert.Equal(t, 5555, cfg.DB.Port)
	assert.Equal(t, "appuser", cfg.DB.User)
	assert.Equal(t, "s3cret", cfg.DB.Password)
	assert.Equal(t, "omnigen_prod", cfg.DB.Name)
	assert.Equal(t, "require", cfg.DB.SSLMode)
	assert.Equal(t, testJWTSecret, cfg.JWT.Secret)
	assert.Equal(t, 2*time.Hour, cfg.JWT.TTL)
	assert.Equal(t, testEncryptionKey, cfg.AppEncryptionKey)
	assert.Equal(t, "root", cfg.Bootstrap.Username)
	assert.Equal(t, "admin12345", cfg.Bootstrap.Password)
	assert.Equal(t, []string{"https://app.example.com", "https://admin.example.com"}, cfg.CORSOrigins)
}

func TestLoad_MissingFileFailsWithActionableMessageNotPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	require.NotPanics(t, func() {
		_, err := config.Load(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), path)
		assert.Contains(t, err.Error(), "config.yaml.example")
	})
}

func TestLoad_MalformedYAMLFailsCleanlyNotPanic(t *testing.T) {
	path := writeConfig(t, "not: [valid: yaml: at: all")

	require.NotPanics(t, func() {
		_, err := config.Load(path)
		require.Error(t, err)
	})
}

func TestLoad_UnknownKeyIsRejected(t *testing.T) {
	path := writeConfig(t, requiredOnly+"\njwt_typo_field: oops\n")

	_, err := config.Load(path)
	require.Error(t, err)
}

func TestLoad_UnknownNestedKeyIsRejected(t *testing.T) {
	path := writeConfig(t, `
jwt:
  scret: "`+testJWTSecret+`"
bootstrap:
  admin_password: "admin12345"
app_encryption_key: "`+testEncryptionKey+`"
`)

	_, err := config.Load(path)
	require.Error(t, err)
}

func TestLoad_FailsWithoutJWTSecret(t *testing.T) {
	path := writeConfig(t, `
bootstrap:
  admin_password: "admin12345"
app_encryption_key: "`+testEncryptionKey+`"
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jwt.secret")
}

func TestLoad_FailsWithoutBootstrapPassword(t *testing.T) {
	path := writeConfig(t, `
jwt:
  secret: "`+testJWTSecret+`"
app_encryption_key: "`+testEncryptionKey+`"
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bootstrap.admin_password")
}

func TestLoad_FailsOnShortJWTSecret(t *testing.T) {
	path := writeConfig(t, `
jwt:
  secret: "too-short"
bootstrap:
  admin_password: "admin12345"
app_encryption_key: "`+testEncryptionKey+`"
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jwt.secret")
}

func TestLoad_FailsWithoutEncryptionKey(t *testing.T) {
	path := writeConfig(t, `
jwt:
  secret: "`+testJWTSecret+`"
bootstrap:
  admin_password: "admin12345"
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app_encryption_key")
}

func TestLoad_FailsOnInvalidBase64EncryptionKey(t *testing.T) {
	path := writeConfig(t, `
jwt:
  secret: "`+testJWTSecret+`"
bootstrap:
  admin_password: "admin12345"
app_encryption_key: "not-valid-base64!!!"
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app_encryption_key")
}

func TestLoad_FailsOnWrongLengthEncryptionKey(t *testing.T) {
	// 合法 base64，但解码后只有 16 字节而非 AES-256 要求的 32 字节。
	path := writeConfig(t, `
jwt:
  secret: "`+testJWTSecret+`"
bootstrap:
  admin_password: "admin12345"
app_encryption_key: "MDEyMzQ1Njc4OTAxMjM0NQ=="
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app_encryption_key")
}

func TestLoad_ReadsOverrides(t *testing.T) {
	path := writeConfig(t, `
database:
  port: 5433
  name: omnigen_test

jwt:
  secret: "`+testJWTSecret+`"
  ttl: 2h

bootstrap:
  admin_password: "admin12345"

app_encryption_key: "`+testEncryptionKey+`"
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, 5433, cfg.DB.Port)
	assert.Equal(t, "omnigen_test", cfg.DB.Name)
	assert.Equal(t, 2*time.Hour, cfg.JWT.TTL)
}

func TestDSN(t *testing.T) {
	path := writeConfig(t, requiredOnly)
	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t,
		"postgres://postgres:123456@localhost:5432/omnigen?sslmode=disable",
		cfg.DB.DSN())
}

func TestDSN_EscapesSpecialCharacters(t *testing.T) {
	const rawPassword = "p@ss w:rd/x"
	path := writeConfig(t, `
database:
  password: "`+rawPassword+`"

jwt:
  secret: "`+testJWTSecret+`"

bootstrap:
  admin_password: "admin12345"

app_encryption_key: "`+testEncryptionKey+`"
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)

	dsn := cfg.DB.DSN()
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)

	got, ok := parsed.User.Password()
	require.True(t, ok, "DSN must carry a password component")
	assert.Equal(t, rawPassword, got)
}

func TestLoad_FailsOnUnparseableDuration(t *testing.T) {
	path := writeConfig(t, `
jwt:
  secret: "`+testJWTSecret+`"
  ttl: notaduration

bootstrap:
  admin_password: "admin12345"

app_encryption_key: "`+testEncryptionKey+`"
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jwt.ttl")
}

// TestLoad_RequiredSecretErrorTakesPrecedence 验证必填项校验先于可选项解析
// 校验运行：jwt.secret 缺失 且 jwt.ttl 是非法时长同时出现时，报出的必须是
// jwt.secret 缺失，而不是 ttl 解析失败——前者更根本，不能被后者挡住。
func TestLoad_RequiredSecretErrorTakesPrecedence(t *testing.T) {
	path := writeConfig(t, `
jwt:
  ttl: notaduration

bootstrap:
  admin_password: "admin12345"

app_encryption_key: "`+testEncryptionKey+`"
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jwt.secret")
}

func TestLoad_EmptyDBPasswordIsRespected(t *testing.T) {
	path := writeConfig(t, `
database:
  password: ""

jwt:
  secret: "`+testJWTSecret+`"

bootstrap:
  admin_password: "admin12345"

app_encryption_key: "`+testEncryptionKey+`"
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "", cfg.DB.Password)
}

func TestLoad_DBPasswordAbsentUsesDefault(t *testing.T) {
	path := writeConfig(t, requiredOnly)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "123456", cfg.DB.Password)
}

func TestLoad_CORSOriginsDefaultsToViteRange(t *testing.T) {
	path := writeConfig(t, requiredOnly)

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"http://localhost:5173", "http://localhost:5174",
		"http://localhost:5175", "http://localhost:5176", "http://localhost:5177",
	}, cfg.CORSOrigins)
}

func TestLoad_CORSOriginsSingleCustomValue(t *testing.T) {
	path := writeConfig(t, requiredOnly+`
cors:
  origins:
    - https://app.example.com
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://app.example.com"}, cfg.CORSOrigins)
}

func TestLoad_CORSOriginsListValue(t *testing.T) {
	path := writeConfig(t, requiredOnly+`
cors:
  origins:
    - https://app.example.com
    - https://admin.example.com
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://app.example.com", "https://admin.example.com"}, cfg.CORSOrigins)
}

func TestLoad_CORSOriginsTrimsWhitespaceAndSkipsBlank(t *testing.T) {
	path := writeConfig(t, requiredOnly+`
cors:
  origins:
    - " https://app.example.com "
    - ""
    - "https://admin.example.com"
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://app.example.com", "https://admin.example.com"}, cfg.CORSOrigins)
}

// ── 密钥脱敏 ─────────────────────────────────────────────────────────

func TestConfig_StringRedactsAllFourSecrets(t *testing.T) {
	path := writeConfig(t, `
database:
  password: "db-secret-marker"

jwt:
  secret: "`+testJWTSecret+`"

bootstrap:
  admin_password: "bootstrap-secret-marker"

app_encryption_key: "`+testEncryptionKey+`"
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)

	for _, formatted := range []string{
		cfg.String(),
		toStringViaFmtV(cfg),
		toStringViaFmtPlusV(cfg),
	} {
		assert.NotContains(t, formatted, testJWTSecret)
		assert.NotContains(t, formatted, testEncryptionKey)
		assert.NotContains(t, formatted, "db-secret-marker")
		assert.NotContains(t, formatted, "bootstrap-secret-marker")
	}
}

func TestConfig_LogValueRedactsAllFourSecrets(t *testing.T) {
	path := writeConfig(t, `
database:
  password: "db-secret-marker"

jwt:
  secret: "`+testJWTSecret+`"

bootstrap:
  admin_password: "bootstrap-secret-marker"

app_encryption_key: "`+testEncryptionKey+`"
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)

	logged := cfg.LogValue().String()
	assert.NotContains(t, logged, testJWTSecret)
	assert.NotContains(t, logged, testEncryptionKey)
	assert.NotContains(t, logged, "db-secret-marker")
	assert.NotContains(t, logged, "bootstrap-secret-marker")
}

func toStringViaFmtV(cfg *config.Config) string     { return fmt.Sprintf("%v", *cfg) }
func toStringViaFmtPlusV(cfg *config.Config) string { return fmt.Sprintf("%+v", *cfg) }
