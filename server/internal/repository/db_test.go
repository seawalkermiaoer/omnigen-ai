package repository_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// testDSN 指向独立的 omnigen_test 库，避免污染开发库。
func testDSN() string {
	if v := os.Getenv("TEST_DB_URL"); v != "" {
		return v
	}
	return "postgres://postgres:123456@localhost:5432/omnigen_test?sslmode=disable"
}

func TestNewPool_ConnectsAndPings(t *testing.T) {
	pool, err := repository.NewPool(context.Background(), testDSN())
	require.NoError(t, err)
	defer pool.Close()

	require.NoError(t, pool.Ping(context.Background()))
}

// 这条断言防止再次踩到 brew PG14 遮蔽 docker PG17 的坑：
// 连错实例时迁移会静默写进错误的库，不会报错。
func TestNewPool_TargetsPostgres17(t *testing.T) {
	pool, err := repository.NewPool(context.Background(), testDSN())
	require.NoError(t, err)
	defer pool.Close()

	var version string
	require.NoError(t, pool.QueryRow(context.Background(), "SHOW server_version").Scan(&version))
	assert.True(t, len(version) > 2 && version[:2] == "17",
		"期望连到 PostgreSQL 17（docker postgres-17），实际是 %s；"+
			"若为 14.x 说明 brew postgresql 又占用了 5432", version)
}

func TestNewPool_FailsOnBadDSN(t *testing.T) {
	_, err := repository.NewPool(context.Background(), "postgres://nobody:wrong@localhost:5432/nope?sslmode=disable")
	require.Error(t, err)
}
