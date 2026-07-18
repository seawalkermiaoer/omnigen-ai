package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// withTx 在事务中运行用例并在结束时回滚，使用例之间互不污染。
// repository 接受 repository.DB 接口，pgx.Tx 与 pgxpool.Pool 都满足它。
func withTx(t *testing.T, fn func(ctx context.Context, tx repository.DB)) {
	t.Helper()
	ctx := context.Background()

	pool, err := repository.NewPool(ctx, testDSN())
	require.NoError(t, err)
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	fn(ctx, tx)
}
