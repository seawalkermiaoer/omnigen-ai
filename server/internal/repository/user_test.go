package repository_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

func sampleUser(name string, role usermodel.Role) *usermodel.User {
	return &usermodel.User{
		Username:     name,
		PasswordHash: "$2a$10$fakehashfakehashfakehashfakehashfakehashfakehashfakeha",
		DisplayName:  name,
		Role:         role,
		Status:       usermodel.StatusActive,
	}
}

func TestUserRepo_CreateThenGetByID(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)

		u := sampleUser("alice", usermodel.RoleAdmin)
		require.NoError(t, repo.Create(ctx, u))
		assert.NotZero(t, u.ID, "Create 应回填自增 ID")
		assert.False(t, u.CreatedAt.IsZero(), "Create 应回填 CreatedAt")

		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, "alice", got.Username)
		assert.Equal(t, usermodel.RoleAdmin, got.Role)
		assert.Equal(t, usermodel.StatusActive, got.Status)
	})
}

func TestUserRepo_GetByUsername(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		require.NoError(t, repo.Create(ctx, sampleUser("bob", usermodel.RoleUser)))

		got, err := repo.GetByUsername(ctx, "bob")
		require.NoError(t, err)
		assert.Equal(t, "bob", got.Username)
	})
}

func TestUserRepo_NotFoundReturnsAppError(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)

		_, err := repo.GetByID(ctx, 999999)
		require.Error(t, err)
		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "USER_NOT_FOUND", appErr.Code())

		_, err = repo.GetByUsername(ctx, "ghost")
		require.Error(t, err)
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "USER_NOT_FOUND", appErr.Code())
	})
}

func TestUserRepo_DuplicateUsernameReturnsTaken(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		require.NoError(t, repo.Create(ctx, sampleUser("carol", usermodel.RoleUser)))

		err := repo.Create(ctx, sampleUser("carol", usermodel.RoleUser))
		require.Error(t, err)
		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "USER_USERNAME_TAKEN", appErr.Code())
	})
}

// updated_at 完全靠应用层 SQL 里显式的 `now()`，没有数据库触发器兜底——
// 这是唯一守着「它确实会前进」这条不变式的测试。但 Postgres 的 now()
// 是当前事务开始时刻的快照，同一个事务内多次调用不会前进
// （用 psql 验证过：BEGIN 后隔 1 秒两次 SELECT now() 结果完全相同，
// clock_timestamp() 才会走）。withTx 把整条用例包在一个事务里，
// 如果 Create 和 Update 共享它，updated_at 就永远等于 created_at，
// 断言表面成立实则测不出任何东西。这里改为直接用包级共享的 testPool
// （不再包一层事务），让 Create 和 Update 落在两个真正各自提交的事务里，
// 从而能真实观察到 updated_at 前进；用例自己在 defer 里清理创建的行，
// 不依赖回滚兜底隔离。
func TestUserRepo_Update(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewUserRepository(testPool)

	u := sampleUser("dave", usermodel.RoleUser)
	require.NoError(t, repo.Create(ctx, u))
	defer func() { _ = repo.Delete(ctx, u.ID) }()

	originalUsername := u.Username
	originalHash := u.PasswordHash
	createdAt := u.CreatedAt
	updatedAtBeforeUpdate := u.UpdatedAt

	time.Sleep(10 * time.Millisecond) // 确保下一个事务的 now() 严格晚于上一个

	u.DisplayName = "Dave 改名"
	u.Status = usermodel.StatusDisabled
	u.Role = usermodel.RoleAdmin
	require.NoError(t, repo.Update(ctx, u))

	got, err := repo.GetByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, "Dave 改名", got.DisplayName)
	assert.Equal(t, usermodel.StatusDisabled, got.Status)
	assert.Equal(t, usermodel.RoleAdmin, got.Role)
	assert.True(t, got.UpdatedAt.After(updatedAtBeforeUpdate),
		"updated_at 应真正前进；如果这条断言失败，先检查是不是又被套进了共享事务")
	assert.Equal(t, createdAt, got.CreatedAt, "Update 不应改动 created_at")
	assert.Equal(t, originalUsername, got.Username, "Update 不应改动 username")
	assert.Equal(t, originalHash, got.PasswordHash, "Update 不应改动 password_hash")
}

// Update 现在也写 quota_total（Task 4）：既要能把额度改成一个具体值，
// 也要能把它改成 NULL（不限量）——这条 SQL 之前只覆盖 display_name/role/status，
// 漏了 quota_total 会让「改成不限量」在数据库里悄悄失效。
func TestUserRepo_Update_QuotaTotal(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("quota-update", usermodel.RoleUser)
		total := 10
		u.QuotaTotal = &total
		require.NoError(t, repo.Create(ctx, u))

		newTotal := 250
		u.QuotaTotal = &newTotal
		require.NoError(t, repo.Update(ctx, u))

		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		require.NotNil(t, got.QuotaTotal)
		assert.Equal(t, 250, *got.QuotaTotal)

		u.QuotaTotal = nil
		require.NoError(t, repo.Update(ctx, u))

		got, err = repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Nil(t, got.QuotaTotal, "quota_total 应能被 Update 改回 NULL（不限量）")
	})
}

func TestUserRepo_Delete(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("erin", usermodel.RoleUser)
		require.NoError(t, repo.Create(ctx, u))

		require.NoError(t, repo.Delete(ctx, u.ID))

		_, err := repo.GetByID(ctx, u.ID)
		require.Error(t, err)

		// 删除不存在的用户应报 USER_NOT_FOUND，而非静默成功
		err = repo.Delete(ctx, u.ID)
		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "USER_NOT_FOUND", appErr.Code())
	})
}

func usernamesOf(items []usermodel.User) []string {
	out := make([]string, len(items))
	for i, u := range items {
		out[i] = u.Username
	}
	return out
}

func TestUserRepo_ListPaginates(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		for _, n := range []string{"u1", "u2", "u3", "u4", "u5"} {
			require.NoError(t, repo.Create(ctx, sampleUser(n, usermodel.RoleUser)))
		}

		page1, total, err := repo.List(ctx, 0, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total, "total 应为总数而非当页数量")
		require.Len(t, page1, 2)
		assert.Equal(t, []string{"u1", "u2"}, usernamesOf(page1), "第一页应按 id 升序返回前两条")
		assert.True(t, page1[0].ID < page1[1].ID, "页内应按 id 升序排列")

		page2, total, err := repo.List(ctx, 2, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total)
		require.Len(t, page2, 2)
		assert.Equal(t, []string{"u3", "u4"}, usernamesOf(page2), "第二页应紧接第一页之后")
		assert.True(t, page2[0].ID > page1[1].ID, "第二页与第一页不应重叠")

		page3, total, err := repo.List(ctx, 4, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total)
		require.Len(t, page3, 1)
		assert.Equal(t, []string{"u5"}, usernamesOf(page3))

		// 越过末尾的分页：LIMIT/OFFSET 应用在 count(*) OVER() 之后，
		// 一行都取不到时窗口函数本身没有行能携带 total——
		// repository 需要在这种情况下退化成一次单独统计，而不是悄悄报 0。
		pastEnd, total, err := repo.List(ctx, 9999, 10)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total, "越过末尾的分页也应报告正确的 total，而非 0")
		assert.Empty(t, pastEnd)
	})
}

func TestUserRepo_CountActiveAdmins(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)

		require.NoError(t, repo.Create(ctx, sampleUser("admin1", usermodel.RoleAdmin)))
		require.NoError(t, repo.Create(ctx, sampleUser("admin2", usermodel.RoleAdmin)))
		require.NoError(t, repo.Create(ctx, sampleUser("plain", usermodel.RoleUser)))

		disabled := sampleUser("admin3", usermodel.RoleAdmin)
		disabled.Status = usermodel.StatusDisabled
		require.NoError(t, repo.Create(ctx, disabled))

		n, err := repo.CountActiveAdmins(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n, "被禁用的 admin 不计入")
	})
}

func TestUserRepo_UpdatePasswordHash(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("frank", usermodel.RoleUser)
		require.NoError(t, repo.Create(ctx, u))

		newHash := "$2a$10$brandnewhashbrandnewhashbrandnewhashbrandnewhashbrandn"
		require.NoError(t, repo.UpdatePasswordHash(ctx, u.ID, newHash))

		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, newHash, got.PasswordHash)
	})
}

func TestUserRepo_ConsumeQuota_DeductsOne(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("quota1", usermodel.RoleUser)
		total := 3
		u.QuotaTotal = &total
		require.NoError(t, repo.Create(ctx, u))

		require.NoError(t, repo.ConsumeQuota(ctx, u.ID))

		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, got.QuotaUsed)
	})
}

func TestUserRepo_ConsumeQuota_ExhaustedReturnsError(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("quota2", usermodel.RoleUser)
		total := 1
		u.QuotaTotal = &total
		require.NoError(t, repo.Create(ctx, u))

		require.NoError(t, repo.ConsumeQuota(ctx, u.ID))

		err := repo.ConsumeQuota(ctx, u.ID)
		require.Error(t, err)
		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "QUOTA_EXCEEDED", appErr.Code())
	})
}

// quota_total 为 NULL 的用户永远不该被拦。
func TestUserRepo_ConsumeQuota_UnlimitedNeverBlocked(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("unlimited", usermodel.RoleAdmin) // QuotaTotal 留 nil
		require.NoError(t, repo.Create(ctx, u))

		for i := 0; i < 50; i++ {
			require.NoError(t, repo.ConsumeQuota(ctx, u.ID))
		}
		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, 50, got.QuotaUsed, "不限量用户仍然计数，只是不拦")
	})
}

func TestUserRepo_RefundQuota_RestoresOne(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("refund1", usermodel.RoleUser)
		total := 5
		u.QuotaTotal = &total
		require.NoError(t, repo.Create(ctx, u))

		require.NoError(t, repo.ConsumeQuota(ctx, u.ID))
		require.NoError(t, repo.RefundQuota(ctx, u.ID))

		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, 0, got.QuotaUsed)
	})
}

// quota_used 不能被退成负数。
func TestUserRepo_RefundQuota_NeverGoesNegative(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("refund2", usermodel.RoleUser)
		require.NoError(t, repo.Create(ctx, u))

		require.NoError(t, repo.RefundQuota(ctx, u.ID)) // 没扣过就退
		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, 0, got.QuotaUsed)
	})
}

// 20 个 goroutine 抢 5 个额度，必须恰好 5 个成功。
// 先查后写的实现会在这里超发——这正是要防的。
func TestUserRepo_ConsumeQuota_ConcurrentNeverOverdraws(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewUserRepository(testPool)

	u := sampleUser(fmt.Sprintf("race-%d", time.Now().UnixNano()), usermodel.RoleUser)
	total := 5
	u.QuotaTotal = &total
	require.NoError(t, repo.Create(ctx, u))
	defer func() { _ = repo.Delete(ctx, u.ID) }()

	const goroutines = 20
	var wg sync.WaitGroup
	var okCount int64
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if err := repo.ConsumeQuota(ctx, u.ID); err == nil {
				atomic.AddInt64(&okCount, 1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(5), atomic.LoadInt64(&okCount), "恰好 5 次成功，多一次就是超发")

	got, err := repo.GetByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, 5, got.QuotaUsed)
}
