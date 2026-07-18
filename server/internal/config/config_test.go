package config_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/config"
)

func setRequired(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-value")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "admin12345")
}

func TestLoad_AppliesDefaults(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "localhost", cfg.DB.Host)
	assert.Equal(t, 5432, cfg.DB.Port)
	assert.Equal(t, "postgres", cfg.DB.User)
	assert.Equal(t, "omnigen", cfg.DB.Name)
	assert.Equal(t, 8080, cfg.HTTPPort)
	assert.Equal(t, 168*time.Hour, cfg.JWT.TTL)
	assert.Equal(t, "admin", cfg.Bootstrap.Username)
}

func TestLoad_FailsWithoutJWTSecret(t *testing.T) {
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "admin12345")
	t.Setenv("JWT_SECRET", "")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_SECRET")
}

func TestLoad_FailsWithoutBootstrapPassword(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-value")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BOOTSTRAP_ADMIN_PASSWORD")
}

func TestLoad_ReadsOverrides(t *testing.T) {
	setRequired(t)
	t.Setenv("DB_PORT", "5433")
	t.Setenv("DB_NAME", "omnigen_test")
	t.Setenv("JWT_TTL", "2h")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, 5433, cfg.DB.Port)
	assert.Equal(t, "omnigen_test", cfg.DB.Name)
	assert.Equal(t, 2*time.Hour, cfg.JWT.TTL)
}

func TestDSN(t *testing.T) {
	setRequired(t)
	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t,
		"postgres://postgres:123456@localhost:5432/omnigen?sslmode=disable",
		cfg.DB.DSN())
}

func TestDSN_EscapesSpecialCharacters(t *testing.T) {
	setRequired(t)
	const rawPassword = "p@ss w:rd/x"
	t.Setenv("DB_PASSWORD", rawPassword)

	cfg, err := config.Load()
	require.NoError(t, err)

	dsn := cfg.DB.DSN()
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)

	got, ok := parsed.User.Password()
	require.True(t, ok, "DSN must carry a password component")
	assert.Equal(t, rawPassword, got)
}

func TestLoad_FailsOnUnparseableInt(t *testing.T) {
	setRequired(t)
	t.Setenv("DB_PORT", "abc")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_PORT")
}

func TestLoad_FailsOnUnparseableDuration(t *testing.T) {
	setRequired(t)
	t.Setenv("JWT_TTL", "notaduration")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_TTL")
}

func TestLoad_RequiredSecretErrorTakesPrecedence(t *testing.T) {
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "admin12345")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("DB_PORT", "abc")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_SECRET")
}

func TestLoad_EmptyDBPasswordIsRespected(t *testing.T) {
	setRequired(t)
	t.Setenv("DB_PASSWORD", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "", cfg.DB.Password)
}
