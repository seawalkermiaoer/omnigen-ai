package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

func TestSettingRepo_UpsertThenGet(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("setting-owner-1", usermodel.RoleAdmin)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewSettingRepository(tx)
		s := &settingmodel.Setting{
			Key:       string(settingmodel.KeyDashscopeAPIKey),
			Value:     "sk-plaintext-value",
			IsSecret:  true,
			UpdatedBy: &u.ID,
		}
		require.NoError(t, repo.Upsert(ctx, s))
		assert.False(t, s.UpdatedAt.IsZero(), "Upsert 应回填 UpdatedAt")

		got, err := repo.Get(ctx, string(settingmodel.KeyDashscopeAPIKey))
		require.NoError(t, err)
		assert.Equal(t, string(settingmodel.KeyDashscopeAPIKey), got.Key)
		assert.Equal(t, "sk-plaintext-value", got.Value)
		assert.True(t, got.IsSecret)
		require.NotNil(t, got.UpdatedBy)
		assert.Equal(t, u.ID, *got.UpdatedBy)
	})
}

func TestSettingRepo_UpsertWithoutUpdatedBy(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewSettingRepository(tx)
		s := &settingmodel.Setting{
			Key:      string(settingmodel.KeyRegion),
			Value:    "cn-beijing",
			IsSecret: false,
		}
		require.NoError(t, repo.Upsert(ctx, s))

		got, err := repo.Get(ctx, string(settingmodel.KeyRegion))
		require.NoError(t, err)
		assert.Nil(t, got.UpdatedBy)
		assert.Equal(t, "cn-beijing", got.Value)
		assert.False(t, got.IsSecret)
	})
}

// UpsertOnExistingKey_UpdatesNotDuplicates 断言 Upsert 在已存在的 key 上是
// 更新而非插入第二行，并且 updated_at 真的前进。Postgres 的 now() 是事务
// 开始时刻的快照，同一个事务内多次调用不会前进——这里必须像
// TestUserRepo_Update 一样跳出 withTx，直接用包级共享的 testPool 让两次
// Upsert 落在两个真正各自提交的事务里，用例自己在 defer 里清理，不依赖
// 回滚兜底隔离。
func TestSettingRepo_UpsertOnExistingKey_UpdatesNotDuplicates(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewSettingRepository(testPool)

	key := "setting-repo-update-advance-test"
	s := &settingmodel.Setting{Key: key, Value: "v1", IsSecret: false}
	require.NoError(t, repo.Upsert(ctx, s))
	defer func() { _, _ = testPool.Exec(ctx, `DELETE FROM app_settings WHERE key = $1`, key) }()

	firstUpdatedAt := s.UpdatedAt

	time.Sleep(10 * time.Millisecond) // 确保下一个事务的 now() 严格晚于上一个

	s2 := &settingmodel.Setting{Key: key, Value: "v2", IsSecret: false}
	require.NoError(t, repo.Upsert(ctx, s2))

	got, err := repo.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "v2", got.Value, "第二次 Upsert 应更新已存在的行")
	assert.True(t, got.UpdatedAt.After(firstUpdatedAt), "updated_at 应前进")

	all, err := repo.GetAll(ctx)
	require.NoError(t, err)
	count := 0
	for _, item := range all {
		if item.Key == key {
			count++
		}
	}
	assert.Equal(t, 1, count, "Upsert 在已存在的 key 上不应产生第二行")
}

func TestSettingRepo_GetAll_ReturnsEverythingInStableOrder(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewSettingRepository(tx)
		require.NoError(t, repo.Upsert(ctx, &settingmodel.Setting{Key: string(settingmodel.KeyRegion), Value: "cn-beijing"}))
		require.NoError(t, repo.Upsert(ctx, &settingmodel.Setting{Key: string(settingmodel.KeyEndpoint), Value: "https://example.com"}))
		require.NoError(t, repo.Upsert(ctx, &settingmodel.Setting{Key: string(settingmodel.KeyWorkspaceID), Value: "ws-1"}))

		all, err := repo.GetAll(ctx)
		require.NoError(t, err)
		require.Len(t, all, 3)

		// 稳定顺序：按 key 字母序（repository 实现里 ORDER BY key ASC）。
		assert.Equal(t, string(settingmodel.KeyEndpoint), all[0].Key)
		assert.Equal(t, string(settingmodel.KeyRegion), all[1].Key)
		assert.Equal(t, string(settingmodel.KeyWorkspaceID), all[2].Key)

		// 多跑一次确认顺序不是巧合。
		all2, err := repo.GetAll(ctx)
		require.NoError(t, err)
		require.Len(t, all2, 3)
		assert.Equal(t, all, all2)
	})
}

func TestSettingRepo_Get_MissingKeyReturnsNotFoundAppError(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewSettingRepository(tx)

		_, err := repo.Get(ctx, "does_not_exist")
		require.Error(t, err)

		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "SETTING_NOT_FOUND", appErr.Code())
		assert.True(t, errors.Is(err, apperr.ErrSettingNotFound))
	})
}

// UpsertMany 内部是一条多行 INSERT 语句，其中一行违反约束会让 Postgres
// 把整个外层事务标记为 aborted——在 aborted 状态下事务里任何后续语句都会
// 被拒绝（"current transaction is aborted"），并不是"干净地什么都没发生"。
// 用 SAVEPOINT 包住这次调用，失败后 ROLLBACK TO SAVEPOINT 把事务从
// aborted 状态里恢复出来，才能在同一个 withTx 事务里继续用 Get 断言
// "确实一行都没写入"。
func TestSettingRepo_UpsertMany_IsAtomic(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewSettingRepository(tx)

		_, err := tx.Exec(ctx, `SAVEPOINT before_upsert_many`)
		require.NoError(t, err)

		badUserID := int64(999999999) // 不存在的用户，触发 FK 违反
		items := []settingmodel.Setting{
			{Key: string(settingmodel.KeyRegion), Value: "cn-beijing"},
			{Key: string(settingmodel.KeyEndpoint), Value: "https://example.com", UpdatedBy: &badUserID},
		}

		upsertErr := repo.UpsertMany(ctx, items)
		require.Error(t, upsertErr)

		var appErr *apperr.AppError
		require.True(t, errors.As(upsertErr, &appErr))
		assert.Equal(t, "VALIDATION_FAILED", appErr.Code())

		_, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT before_upsert_many`)
		require.NoError(t, err)

		// 断言"一个都没写入"——不仅是失败项，连本该成功的那一项也不能落地。
		_, getErr := repo.Get(ctx, string(settingmodel.KeyRegion))
		require.Error(t, getErr)
		assert.True(t, errors.Is(getErr, apperr.ErrSettingNotFound))

		_, getErr2 := repo.Get(ctx, string(settingmodel.KeyEndpoint))
		require.Error(t, getErr2)
		assert.True(t, errors.Is(getErr2, apperr.ErrSettingNotFound))
	})
}

func TestSettingRepo_UpsertMany_AllSucceed(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("setting-owner-2", usermodel.RoleAdmin)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewSettingRepository(tx)
		items := []settingmodel.Setting{
			{Key: string(settingmodel.KeyRegion), Value: "cn-beijing", UpdatedBy: &u.ID},
			{Key: string(settingmodel.KeyEndpoint), Value: "https://example.com", UpdatedBy: &u.ID},
			{Key: string(settingmodel.KeyWorkspaceID), Value: "ws-1", UpdatedBy: &u.ID},
		}
		require.NoError(t, repo.UpsertMany(ctx, items))

		all, err := repo.GetAll(ctx)
		require.NoError(t, err)
		assert.Len(t, all, 3)
	})
}

func TestSettingRepo_UpsertMany_UpdatesExistingKeys(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewSettingRepository(tx)
		require.NoError(t, repo.Upsert(ctx, &settingmodel.Setting{Key: string(settingmodel.KeyRegion), Value: "old-value"}))

		require.NoError(t, repo.UpsertMany(ctx, []settingmodel.Setting{
			{Key: string(settingmodel.KeyRegion), Value: "new-value"},
		}))

		got, err := repo.Get(ctx, string(settingmodel.KeyRegion))
		require.NoError(t, err)
		assert.Equal(t, "new-value", got.Value)

		all, err := repo.GetAll(ctx)
		require.NoError(t, err)
		assert.Len(t, all, 1, "UpsertMany 在已存在的 key 上不应产生重复行")
	})
}

func TestSettingRepo_UpsertMany_EmptyIsNoop(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewSettingRepository(tx)
		require.NoError(t, repo.UpsertMany(ctx, nil))

		all, err := repo.GetAll(ctx)
		require.NoError(t, err)
		assert.Empty(t, all)
	})
}

func TestSettingRepo_UpdatedByFK_RejectsNonExistentUser(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewSettingRepository(tx)

		badUserID := int64(999999999)
		s := &settingmodel.Setting{
			Key:       string(settingmodel.KeyRegion),
			Value:     "cn-beijing",
			UpdatedBy: &badUserID,
		}
		err := repo.Upsert(ctx, s)
		require.Error(t, err)

		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "VALIDATION_FAILED", appErr.Code())
	})
}
