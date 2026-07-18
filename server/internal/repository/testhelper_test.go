package repository_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// testPool 是整个测试包共享的连接池。这个文件是后续 app_settings/history
// 仓库测试套件的模板，逐个用例各开一个池（MaxConns=10）在用例少时无害，
// 但会很快把连接数堆起来；改成包级共享池，用例仍靠事务回滚互相隔离。
var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	pool, err := repository.NewPool(ctx, testDSN())
	if err != nil {
		panic(err)
	}
	testPool = pool

	code := m.Run()

	testPool.Close()
	os.Exit(code)
}

// withTx 在事务中运行用例并在结束时回滚，使用例之间互不污染。
// repository 接受 repository.DB 接口，pgx.Tx 与 pgxpool.Pool 都满足它。
func withTx(t *testing.T, fn func(ctx context.Context, tx repository.DB)) {
	t.Helper()
	ctx := context.Background()

	tx, err := testPool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	fn(ctx, tx)
}
