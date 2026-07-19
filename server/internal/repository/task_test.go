package repository_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

func sampleTask(userID int64) *generationmodel.Task {
	return &generationmodel.Task{
		UserID:     userID,
		Mode:       generationmodel.TaskModeImgGen,
		Model:      "qwen-image",
		Status:     generationmodel.StatusSucceeded,
		Prompt:     "a cat wearing sunglasses",
		Params:     map[string]any{"n": float64(2), "size": "1328*1328"},
		InputURLs:  []string{"https://example.com/in1.png"},
		ResultURLs: []string{"https://example.com/out1.png", "https://example.com/out2.png"},
		Usage:      map[string]any{"tokens": float64(42)},
	}
}

func TestTaskRepo_CreateThenGetByIDForUser_RoundTripsAllFields(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("task-owner-1", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewTaskRepository(tx)
		task := sampleTask(u.ID)
		require.NoError(t, repo.Create(ctx, task))
		assert.NotZero(t, task.ID, "Create 应回填自增 ID")
		assert.False(t, task.CreatedAt.IsZero())
		assert.False(t, task.UpdatedAt.IsZero())

		got, err := repo.GetByIDForUser(ctx, task.ID, u.ID)
		require.NoError(t, err)
		assert.Equal(t, task.ID, got.ID)
		assert.Equal(t, u.ID, got.UserID)
		assert.Equal(t, generationmodel.TaskModeImgGen, got.Mode)
		assert.Equal(t, "qwen-image", got.Model)
		assert.Equal(t, generationmodel.StatusSucceeded, got.Status)
		assert.Equal(t, "a cat wearing sunglasses", got.Prompt)
		assert.Equal(t, map[string]any{"n": float64(2), "size": "1328*1328"}, got.Params)
		assert.Equal(t, []string{"https://example.com/in1.png"}, got.InputURLs)
		assert.Equal(t, []string{"https://example.com/out1.png", "https://example.com/out2.png"}, got.ResultURLs)
		assert.Equal(t, map[string]any{"tokens": float64(42)}, got.Usage)
		assert.Nil(t, got.UpstreamTaskID)
		assert.Nil(t, got.Note)
		assert.Nil(t, got.ErrorCode)
		assert.Nil(t, got.ErrorMessage)
	})
}

func TestTaskRepo_Create_NullableFieldsRoundTrip(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("task-owner-2", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewTaskRepository(tx)
		upstream := "upstream-abc-123"
		note := "some model prose, minus the markdown image links"
		errCode := "UPSTREAM_TIMEOUT"
		errMsg := "boom"
		task := &generationmodel.Task{
			UserID:         u.ID,
			Mode:           generationmodel.TaskModeT2V,
			Model:          "wan2.7-t2v",
			Status:         generationmodel.StatusFailed,
			UpstreamTaskID: &upstream,
			Prompt:         "a dog running",
			Note:           &note,
			ErrorCode:      &errCode,
			ErrorMessage:   &errMsg,
		}
		require.NoError(t, repo.Create(ctx, task))

		got, err := repo.GetByIDForUser(ctx, task.ID, u.ID)
		require.NoError(t, err)
		require.NotNil(t, got.UpstreamTaskID)
		assert.Equal(t, upstream, *got.UpstreamTaskID)
		require.NotNil(t, got.Note)
		assert.Equal(t, note, *got.Note)
		require.NotNil(t, got.ErrorCode)
		assert.Equal(t, errCode, *got.ErrorCode)
		require.NotNil(t, got.ErrorMessage)
		assert.Equal(t, errMsg, *got.ErrorMessage)
	})
}

// 空 result_urls（以及 input_urls / params）必须往返成空切片/空 map，
// 而不是 nil——调用方（handler/前端 JSON 消费者）对 ResultURLs 直接
// range/len 是常见操作，nil 切片虽然 range 安全，但一旦哪里写了
// `results[0]` 或把它塞进一个假设非 nil 的序列化路径就会出问题；
// 让"没有结果"和"没读到值"在类型层面无法区分也是一种隐患。
// Usage 则相反：它是可选的旁路信息，NULL 与 "{}" 是两个不同的事实
// （没有用量数据 vs 用量数据是空对象），所以允许它保持 nil。
func TestTaskRepo_Create_EmptyResultURLsRoundTripsAsEmptySliceNotNil(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("task-owner-3", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewTaskRepository(tx)
		task := &generationmodel.Task{
			UserID: u.ID,
			Mode:   generationmodel.TaskModeT2V,
			Model:  "wan2.7-t2v",
			Status: generationmodel.StatusPending,
			Prompt: "a dog running",
			// InputURLs / ResultURLs / Params 故意留空（nil）——
			// 调用方忘记初始化时是最常见的路径。
		}
		require.NoError(t, repo.Create(ctx, task))
		require.NotNil(t, task.ResultURLs, "Create 应把 nil 归一化为空切片，供调用方立即安全使用")
		assert.Empty(t, task.ResultURLs)

		got, err := repo.GetByIDForUser(ctx, task.ID, u.ID)
		require.NoError(t, err)
		require.NotNil(t, got.ResultURLs, "读回时 result_urls 不应是 nil")
		assert.Equal(t, []string{}, got.ResultURLs)
		require.NotNil(t, got.InputURLs)
		assert.Equal(t, []string{}, got.InputURLs)
		require.NotNil(t, got.Params)
		assert.Equal(t, map[string]any{}, got.Params)
		assert.Nil(t, got.Usage, "usage 未设置时应保持 nil，与「空对象」区分")
	})
}

// TestTaskRepo_Create_QuotaChargedRoundTrips 直接对着真实 Postgres 验证
// quota_charged 这一列——INSERT 的列清单/占位符位置、taskColumns/
// scanTaskRow 的扫描顺序，任何一处手误错位都只会在这里被抓到；内存版
// fakeTaskRepo 只是整体拷贝一份 struct，不会经过真实 SQL 的列序，测不出
// 这类漂移。这个项目已经在列/扫描顺序上踩过坑（见 scanUserRow 的文档
// 注释），quota_charged 值得单独锁一次。true 和 false 各测一次，防止
// "布尔列永远读出零值"这种更隐蔽的失败被误判为通过。
func TestTaskRepo_Create_QuotaChargedRoundTrips(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("task-owner-quota-charged", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewTaskRepository(tx)

		charged := &generationmodel.Task{
			UserID:       u.ID,
			Mode:         generationmodel.TaskModeImgGen,
			Model:        "qwen-image",
			Status:       generationmodel.StatusSucceeded,
			Prompt:       "charged",
			QuotaCharged: true,
		}
		require.NoError(t, repo.Create(ctx, charged))

		got, err := repo.GetByIDForUser(ctx, charged.ID, u.ID)
		require.NoError(t, err)
		assert.True(t, got.QuotaCharged, "QuotaCharged=true 必须原样往返")

		notCharged := &generationmodel.Task{
			UserID:       u.ID,
			Mode:         generationmodel.TaskModeImgGen,
			Model:        "qwen-image",
			Status:       generationmodel.StatusFailed,
			Prompt:       "refunded",
			QuotaCharged: false,
		}
		require.NoError(t, repo.Create(ctx, notCharged))

		got2, err := repo.GetByIDForUser(ctx, notCharged.ID, u.ID)
		require.NoError(t, err)
		assert.False(t, got2.QuotaCharged, "QuotaCharged=false 也必须原样往返，不能读出上一行的值")
	})
}

func TestTaskRepo_GetByIDForUser_OwnershipIsolation_ReturnsNotFound(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		owner := sampleUser("task-owner-4", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, owner))
		other := sampleUser("task-intruder-1", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, other))

		repo := repository.NewTaskRepository(tx)
		task := sampleTask(owner.ID)
		require.NoError(t, repo.Create(ctx, task))

		// 用户 B 读用户 A 的任务：必须是 NOT_FOUND，不是 FORBIDDEN——返回
		// 403 会向调用方泄露"这个 id 确实存在，只是不归你"，本身就是一个
		// 存在性 oracle；404 让"不存在"与"不是你的"在外部不可区分。
		_, err := repo.GetByIDForUser(ctx, task.ID, other.ID)
		require.Error(t, err)

		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "TASK_NOT_FOUND", appErr.Code())
		assert.True(t, errors.Is(err, apperr.ErrTaskNotFound))

		// owner 自己读得到，证明上面的失败确实是归属过滤的结果，
		// 不是任务本身创建失败。
		got, err := repo.GetByIDForUser(ctx, task.ID, owner.ID)
		require.NoError(t, err)
		assert.Equal(t, task.ID, got.ID)
	})
}

func TestTaskRepo_GetByIDForUser_NonExistentID_ReturnsNotFound(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("task-owner-5", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewTaskRepository(tx)
		_, err := repo.GetByIDForUser(ctx, 999999, u.ID)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperr.ErrTaskNotFound))
	})
}

func TestTaskRepo_ListForUser_PaginatesOrdersAndIsolates(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		a := sampleUser("list-user-a", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, a))
		b := sampleUser("list-user-b", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, b))

		repo := repository.NewTaskRepository(tx)

		for i := 0; i < 5; i++ {
			task := sampleTask(a.ID)
			task.Prompt = fmt.Sprintf("a-task-%d", i)
			require.NoError(t, repo.Create(ctx, task))
		}
		// user B 的任务不应出现在 A 的列表里。
		require.NoError(t, repo.Create(ctx, sampleTask(b.ID)))

		// 同一个 withTx 事务内 now() 是冻结的（Postgres 事务开始时刻的快照），
		// 因此这 5 条 A 的任务 created_at 全部相同——"newest first" 只能靠
		// id 做二级排序体现，仓库实现里 ORDER BY created_at DESC, id DESC
		// 正是为此设计；这里断言的顺序同时验证了这一点。
		page1, total, err := repo.ListForUser(ctx, a.ID, 0, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total, "total 应为该用户任务总数而非当页数量")
		require.Len(t, page1, 2)
		assert.Equal(t, "a-task-4", page1[0].Prompt, "最新创建的应排在最前")
		assert.Equal(t, "a-task-3", page1[1].Prompt)

		page2, total, err := repo.ListForUser(ctx, a.ID, 2, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total)
		require.Len(t, page2, 2)
		assert.Equal(t, "a-task-2", page2[0].Prompt)
		assert.Equal(t, "a-task-1", page2[1].Prompt)

		page3, total, err := repo.ListForUser(ctx, a.ID, 4, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total)
		require.Len(t, page3, 1)
		assert.Equal(t, "a-task-0", page3[0].Prompt)

		all := append(append(page1, page2...), page3...)
		for _, task := range all {
			assert.Equal(t, a.ID, task.UserID, "只应返回该用户自己的任务")
		}
	})
}

func TestTaskRepo_ListForUser_PastEndPage_StillReportsTotal(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("list-user-pastend", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewTaskRepository(tx)
		for i := 0; i < 3; i++ {
			require.NoError(t, repo.Create(ctx, sampleTask(u.ID)))
		}

		// LIMIT/OFFSET 应用在 count(*) OVER() 之后，越过末尾时一行都取不到，
		// 窗口函数没有行能携带 total——repository 需要退化成一次单独统计，
		// 而不是悄悄报 0（与 user.go List 的越界分页处理同一个理由）。
		items, total, err := repo.ListForUser(ctx, u.ID, 9999, 10)
		require.NoError(t, err)
		assert.Equal(t, int64(3), total, "越过末尾的分页也应报告正确的 total，而非 0")
		assert.Empty(t, items)
	})
}

func TestTaskRepo_ClaimPending_OnlyInFlightOldestFirstRespectsLimit(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("claim-user", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewTaskRepository(tx)

		mk := func(status generationmodel.Status, prompt string) *generationmodel.Task {
			task := sampleTask(u.ID)
			task.Status = status
			task.Prompt = prompt
			require.NoError(t, repo.Create(ctx, task))
			return task
		}

		pending1 := mk(generationmodel.StatusPending, "pending-1")
		running1 := mk(generationmodel.StatusRunning, "running-1")
		mk(generationmodel.StatusSucceeded, "succeeded-1")
		mk(generationmodel.StatusFailed, "failed-1")
		mk(generationmodel.StatusCanceled, "canceled-1")
		pending2 := mk(generationmodel.StatusPending, "pending-2")

		claimed, err := repo.ClaimPending(ctx, 10)
		require.NoError(t, err)
		require.Len(t, claimed, 3, "只应取 PENDING/RUNNING")
		gotIDs := []int64{claimed[0].ID, claimed[1].ID, claimed[2].ID}
		assert.Equal(t, []int64{pending1.ID, running1.ID, pending2.ID}, gotIDs, "应按创建顺序（oldest first）返回")
		for _, task := range claimed {
			assert.True(t, task.IsInFlight())
		}

		limited, err := repo.ClaimPending(ctx, 2)
		require.NoError(t, err)
		require.Len(t, limited, 2, "应受 limit 约束")
		assert.Equal(t, pending1.ID, limited[0].ID)
		assert.Equal(t, running1.ID, limited[1].ID)
	})
}

// now() 在同一个 Postgres 事务内是冻结的（事务开始时刻的快照），要观察
// updated_at 真的前进，不能像其它用例一样跑在 withTx 里——否则两次更新
// 的 updated_at 会完全相同。这里比照 setting_test.go 里
// TestSettingRepo_UpsertOnExistingKey_UpdatesNotDuplicates 的做法：直接用
// 包级共享的 testPool，让每次写入落在各自真正提交的事务里，用例自己在
// defer 里做清理（不依赖 withTx 的回滚兜底）。
func TestTaskRepo_UpdateStatus_AdvancesUpdatedAt(t *testing.T) {
	ctx := context.Background()
	userRepo := repository.NewUserRepository(testPool)
	u := sampleUser("update-status-user", usermodel.RoleUser)
	require.NoError(t, userRepo.Create(ctx, u))
	defer func() { _, _ = testPool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID) }()

	repo := repository.NewTaskRepository(testPool)
	task := sampleTask(u.ID)
	task.Status = generationmodel.StatusPending
	require.NoError(t, repo.Create(ctx, task))
	defer func() { _, _ = testPool.Exec(ctx, `DELETE FROM generation_tasks WHERE id = $1`, task.ID) }()

	firstUpdatedAt := task.UpdatedAt
	time.Sleep(10 * time.Millisecond) // 确保下一次写入的 now() 严格晚于上一次

	require.NoError(t, repo.UpdateStatus(ctx, task.ID, generationmodel.StatusRunning, "", ""))

	got, err := repo.GetByIDForUser(ctx, task.ID, u.ID)
	require.NoError(t, err)
	assert.Equal(t, generationmodel.StatusRunning, got.Status)
	assert.True(t, got.UpdatedAt.After(firstUpdatedAt), "updated_at 应前进")
	assert.Nil(t, got.ErrorCode)
	assert.Nil(t, got.ErrorMessage)

	require.NoError(t, repo.UpdateStatus(ctx, task.ID, generationmodel.StatusFailed, "UPSTREAM_TIMEOUT", "上游超时"))
	got2, err := repo.GetByIDForUser(ctx, task.ID, u.ID)
	require.NoError(t, err)
	assert.Equal(t, generationmodel.StatusFailed, got2.Status)
	require.NotNil(t, got2.ErrorCode)
	assert.Equal(t, "UPSTREAM_TIMEOUT", *got2.ErrorCode)
	require.NotNil(t, got2.ErrorMessage)
	assert.Equal(t, "上游超时", *got2.ErrorMessage)
}

func TestTaskRepo_UpdateResult_AdvancesUpdatedAtAndSetsSucceeded(t *testing.T) {
	ctx := context.Background()
	userRepo := repository.NewUserRepository(testPool)
	u := sampleUser("update-result-user", usermodel.RoleUser)
	require.NoError(t, userRepo.Create(ctx, u))
	defer func() { _, _ = testPool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID) }()

	repo := repository.NewTaskRepository(testPool)
	task := sampleTask(u.ID)
	task.Status = generationmodel.StatusRunning
	task.ResultURLs = nil
	require.NoError(t, repo.Create(ctx, task))
	defer func() { _, _ = testPool.Exec(ctx, `DELETE FROM generation_tasks WHERE id = $1`, task.ID) }()

	firstUpdatedAt := task.UpdatedAt
	time.Sleep(10 * time.Millisecond)

	urls := []string{"https://example.com/video1.mp4"}
	usage := map[string]any{"output_video_duration": float64(5), "ratio": "16:9"}
	require.NoError(t, repo.UpdateResult(ctx, task.ID, urls, usage, "some note"))

	got, err := repo.GetByIDForUser(ctx, task.ID, u.ID)
	require.NoError(t, err)
	assert.Equal(t, generationmodel.StatusSucceeded, got.Status, "UpdateResult 应把任务落到终态 SUCCEEDED")
	assert.Equal(t, urls, got.ResultURLs)
	assert.Equal(t, usage, got.Usage)
	require.NotNil(t, got.Note)
	assert.Equal(t, "some note", *got.Note)
	assert.True(t, got.UpdatedAt.After(firstUpdatedAt), "updated_at 应前进")
}

func TestTaskRepo_UpdateStatus_NonExistentID_ReturnsNotFound(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewTaskRepository(tx)
		err := repo.UpdateStatus(ctx, 999999, generationmodel.StatusRunning, "", "")
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperr.ErrTaskNotFound), "更新不存在的 id 应报 NOT_FOUND 而非静默成功")
	})
}

func TestTaskRepo_UpdateResult_NonExistentID_ReturnsNotFound(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewTaskRepository(tx)
		err := repo.UpdateResult(ctx, 999999, []string{"x"}, nil, "")
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperr.ErrTaskNotFound), "更新不存在的 id 应报 NOT_FOUND 而非静默成功")
	})
}

func TestTaskRepo_DeleteForUser_RemovesRow_SecondDeleteNotFound(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("delete-owner-1", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewTaskRepository(tx)
		task := sampleTask(u.ID)
		require.NoError(t, repo.Create(ctx, task))

		require.NoError(t, repo.DeleteForUser(ctx, task.ID, u.ID))

		_, err := repo.GetByIDForUser(ctx, task.ID, u.ID)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperr.ErrTaskNotFound), "删除后应查不到这一行")

		// 对同一个已经删掉的 id 再删一次，必须是 NOT_FOUND，而不是静默成功
		// （RowsAffected 为 0 时不能假装删除生效了）。
		err = repo.DeleteForUser(ctx, task.ID, u.ID)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperr.ErrTaskNotFound))
	})
}

func TestTaskRepo_DeleteForUser_OwnershipIsolation_ReturnsNotFound_LeavesRowIntact(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		owner := sampleUser("delete-owner-2", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, owner))
		intruder := sampleUser("delete-intruder-1", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, intruder))

		repo := repository.NewTaskRepository(tx)
		task := sampleTask(owner.ID)
		require.NoError(t, repo.Create(ctx, task))

		// 用户 B 删用户 A 的任务：必须是 NOT_FOUND，不是 FORBIDDEN——同
		// GetByIDForUser 的理由，403 会泄露"这个 id 确实存在"。
		err := repo.DeleteForUser(ctx, task.ID, intruder.ID)
		require.Error(t, err)
		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "TASK_NOT_FOUND", appErr.Code())
		assert.True(t, errors.Is(err, apperr.ErrTaskNotFound))

		// A 的任务必须原样还在——上面那次失败的删除不能有任何副作用。
		got, err := repo.GetByIDForUser(ctx, task.ID, owner.ID)
		require.NoError(t, err)
		assert.Equal(t, task.ID, got.ID)
	})
}

func TestTaskRepo_DeleteForUser_NonExistentID_ReturnsNotFound(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("delete-owner-3", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewTaskRepository(tx)
		err := repo.DeleteForUser(ctx, 999999, u.ID)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperr.ErrTaskNotFound))
	})
}

// 在飞状态（PENDING/RUNNING）的任务同样可以删除——见
// service.VideoGenerationService.DeleteTask 的 doc：这是刻意的决定，
// 不是遗漏的状态检查。
func TestTaskRepo_DeleteForUser_InFlightTask_StillDeletable(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("delete-owner-inflight", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewTaskRepository(tx)
		task := sampleTask(u.ID)
		task.Status = generationmodel.StatusPending
		require.NoError(t, repo.Create(ctx, task))

		require.NoError(t, repo.DeleteForUser(ctx, task.ID, u.ID), "PENDING 任务也应允许删除")

		_, err := repo.GetByIDForUser(ctx, task.ID, u.ID)
		assert.True(t, errors.Is(err, apperr.ErrTaskNotFound))
	})
}

func TestTaskRepo_DeleteAllForUser_OnlyRemovesCallersOwnRows_ReturnsCount(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		a := sampleUser("deleteall-user-a", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, a))
		b := sampleUser("deleteall-user-b", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, b))

		repo := repository.NewTaskRepository(tx)
		for i := 0; i < 3; i++ {
			require.NoError(t, repo.Create(ctx, sampleTask(a.ID)))
		}
		bTask := sampleTask(b.ID)
		require.NoError(t, repo.Create(ctx, bTask))

		deleted, err := repo.DeleteAllForUser(ctx, a.ID)
		require.NoError(t, err)
		assert.Equal(t, int64(3), deleted, "应只报告并删除 A 自己的行数")

		_, total, err := repo.ListForUser(ctx, a.ID, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, int64(0), total, "A 的任务应全部清空")

		// B 的任务必须原样保留，不受影响。
		got, err := repo.GetByIDForUser(ctx, bTask.ID, b.ID)
		require.NoError(t, err)
		assert.Equal(t, bTask.ID, got.ID)
	})
}

func TestTaskRepo_DeleteAllForUser_NoTasks_ReturnsZeroNoError(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("deleteall-user-empty", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewTaskRepository(tx)
		deleted, err := repo.DeleteAllForUser(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, int64(0), deleted)
	})
}

// ── Delete 时的额度退回 ─────────────────────────────────────────────
//
// 删除一个仍 quota_charged=true 的在飞任务（DeleteTask 明确允许删除
// PENDING/RUNNING 任务），若只是单纯 DELETE 而不退款，那份额度会随着行
// 一起消失——RefundQuotaForTask 之后再也没有那一行可以 WHERE id = $1 了。
// 这些测试和 RefundQuotaForTask 一样跑在真实 Postgres 上：退款与删除
// 绑在同一条语句里这件事本身就是一个 SQL 原子性属性。

func TestTaskRepo_DeleteForUser_ChargedInFlightTask_RefundsExactlyOne(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("delete-refund-charged", usermodel.RoleUser)
		total := 5
		u.QuotaTotal = &total
		require.NoError(t, userRepo.Create(ctx, u))
		require.NoError(t, userRepo.ConsumeQuota(ctx, u.ID))

		repo := repository.NewTaskRepository(tx)
		task := sampleTask(u.ID)
		task.Status = generationmodel.StatusPending
		task.QuotaCharged = true
		require.NoError(t, repo.Create(ctx, task))

		require.NoError(t, repo.DeleteForUser(ctx, task.ID, u.ID))

		gotUser, err := userRepo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, 0, gotUser.QuotaUsed, "删除一个已计费的在飞任务必须退回它那一份额度")
	})
}

func TestTaskRepo_DeleteForUser_UnchargedTask_RefundsZero(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("delete-refund-uncharged", usermodel.RoleUser)
		total := 5
		u.QuotaTotal = &total
		require.NoError(t, userRepo.Create(ctx, u))
		require.NoError(t, userRepo.ConsumeQuota(ctx, u.ID)) // 由另一个任务扣的，与本任务无关

		repo := repository.NewTaskRepository(tx)
		task := sampleTask(u.ID) // sampleTask 默认 QuotaCharged=false（同步成功的图片任务）
		require.NoError(t, repo.Create(ctx, task))

		require.NoError(t, repo.DeleteForUser(ctx, task.ID, u.ID))

		gotUser, err := userRepo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, gotUser.QuotaUsed, "删除一个从未计费的任务不应该退掉别的任务的额度")
	})
}

// 用两份独立计费而不是一份——单份计费时"退一份"和"错误地退两份"在
// quota_used 上都可能落在同一个数字上（quota_used > 0 的门槛会掩盖多退的
// 那一次），必须让另一个任务的欠款仍然挂着，才能真正证明这次删除只退了
// 自己那一份。同 worker 包 TestPoller_AlreadyRefunded_DoesNotRefundTwice
// 与 TestTaskRepo_RefundQuotaForTask_AlreadyRefunded_DoesNotRefundTwice 的
// 同一处教训。
func TestTaskRepo_DeleteForUser_OneOfTwoChargedTasks_OnlyRefundsItsOwnShare(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("delete-refund-two-charges", usermodel.RoleUser)
		total := 5
		u.QuotaTotal = &total
		require.NoError(t, userRepo.Create(ctx, u))
		require.NoError(t, userRepo.ConsumeQuota(ctx, u.ID))
		require.NoError(t, userRepo.ConsumeQuota(ctx, u.ID))

		repo := repository.NewTaskRepository(tx)
		taskA := sampleTask(u.ID)
		taskA.Status = generationmodel.StatusPending
		taskA.QuotaCharged = true
		require.NoError(t, repo.Create(ctx, taskA))

		taskB := sampleTask(u.ID)
		taskB.Status = generationmodel.StatusPending
		taskB.QuotaCharged = true
		require.NoError(t, repo.Create(ctx, taskB))

		require.NoError(t, repo.DeleteForUser(ctx, taskA.ID, u.ID))

		gotUser, err := userRepo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, gotUser.QuotaUsed, "只删了 A，B 仍然欠着的那一份额度必须保留")
	})
}

// 删别人的任务必须是 NOT_FOUND，且不能碰任何一方的额度——即便是"顺手"退
// 了误删失败的那次退款也不行，ownership 校验必须先于退款生效。
func TestTaskRepo_DeleteForUser_OtherUsersTask_NotFound_TouchesNeitherQuota(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		owner := sampleUser("delete-refund-owner", usermodel.RoleUser)
		total := 5
		owner.QuotaTotal = &total
		require.NoError(t, userRepo.Create(ctx, owner))
		require.NoError(t, userRepo.ConsumeQuota(ctx, owner.ID))

		intruder := sampleUser("delete-refund-intruder", usermodel.RoleUser)
		intruder.QuotaTotal = &total
		require.NoError(t, userRepo.Create(ctx, intruder))
		require.NoError(t, userRepo.ConsumeQuota(ctx, intruder.ID))

		repo := repository.NewTaskRepository(tx)
		task := sampleTask(owner.ID)
		task.Status = generationmodel.StatusPending
		task.QuotaCharged = true
		require.NoError(t, repo.Create(ctx, task))

		err := repo.DeleteForUser(ctx, task.ID, intruder.ID)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperr.ErrTaskNotFound))

		gotOwner, err := userRepo.GetByID(ctx, owner.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, gotOwner.QuotaUsed, "删除失败，属主的额度不应该被退")

		gotIntruder, err := userRepo.GetByID(ctx, intruder.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, gotIntruder.QuotaUsed, "入侵者自己的额度也不该被这次失败的删除动到")
	})
}

func TestTaskRepo_DeleteAllForUser_MixOfChargedAndUncharged_RefundsExactlyChargedCount(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("deleteall-refund-mix", usermodel.RoleUser)
		total := 10
		u.QuotaTotal = &total
		require.NoError(t, userRepo.Create(ctx, u))

		repo := repository.NewTaskRepository(tx)
		// 3 个已计费的在飞任务 + 2 个未计费的（已同步成功的图片任务）。
		for i := 0; i < 3; i++ {
			require.NoError(t, userRepo.ConsumeQuota(ctx, u.ID))
			task := sampleTask(u.ID)
			task.Status = generationmodel.StatusPending
			task.QuotaCharged = true
			require.NoError(t, repo.Create(ctx, task))
		}
		for i := 0; i < 2; i++ {
			task := sampleTask(u.ID)
			task.QuotaCharged = false
			require.NoError(t, repo.Create(ctx, task))
		}

		gotBefore, err := userRepo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		require.Equal(t, 3, gotBefore.QuotaUsed)

		deleted, err := repo.DeleteAllForUser(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, int64(5), deleted, "应该报告全部 5 行被删除，不只是计费的那 3 行")

		gotAfter, err := userRepo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, 0, gotAfter.QuotaUsed, "应该只退回 3 个计费任务对应的额度")
	})
}

// ── RefundQuotaForTask ──────────────────────────────────────────────
//
// Idempotency here is a SQL property (the CTE's WHERE quota_charged = true
// gate), not something a Go-level in-memory double can prove — these run
// against real Postgres, unlike the worker package's fake-repo-backed poller
// tests that exercise the same contract at the Go level.

func TestTaskRepo_RefundQuotaForTask_RefundsAndFlipsCharged(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("refund-task-owner", usermodel.RoleUser)
		total := 5
		u.QuotaTotal = &total
		require.NoError(t, userRepo.Create(ctx, u))
		require.NoError(t, userRepo.ConsumeQuota(ctx, u.ID))

		repo := repository.NewTaskRepository(tx)
		task := sampleTask(u.ID)
		task.Status = generationmodel.StatusPending
		task.QuotaCharged = true
		require.NoError(t, repo.Create(ctx, task))

		require.NoError(t, repo.RefundQuotaForTask(ctx, task.ID))

		gotUser, err := userRepo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, 0, gotUser.QuotaUsed, "退款应该让 quota_used 回到 0")

		gotTask, err := repo.GetByIDForUser(ctx, task.ID, u.ID)
		require.NoError(t, err)
		assert.False(t, gotTask.QuotaCharged, "退款应该把 quota_charged 翻转成 false")
	})
}

// 重复调用（worker 因重试/重启重复处理同一个任务）必须只生效一次——这是
// quota_charged 存在的唯一理由，且这条不变量由 CTE 的
// WHERE quota_charged = true 保证，只能在真实 Postgres 上证明。
func TestTaskRepo_RefundQuotaForTask_AlreadyRefunded_DoesNotRefundTwice(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("refund-task-twice", usermodel.RoleUser)
		total := 5
		u.QuotaTotal = &total
		require.NoError(t, userRepo.Create(ctx, u))
		// 两份独立的计费——不是一份：如果这个用户全程只欠 1 份额度，
		// "退两次"和"退一次"在 quota_used 上都会落在 0（第二次会被
		// users.quota_used > 0 那道独立守卫挡住），quota_charged 那道
		// 守卫是否真的生效就无法单从 quota_used 的数值上分辨出来。见
		// worker 包 TestPoller_AlreadyRefunded_DoesNotRefundTwice 的
		// 同一处注释——这个坑在验证 Step 5 时被踩到过一次。
		require.NoError(t, userRepo.ConsumeQuota(ctx, u.ID))
		require.NoError(t, userRepo.ConsumeQuota(ctx, u.ID))

		repo := repository.NewTaskRepository(tx)
		taskA := sampleTask(u.ID)
		taskA.Status = generationmodel.StatusPending
		taskA.QuotaCharged = true
		require.NoError(t, repo.Create(ctx, taskA))

		taskB := sampleTask(u.ID)
		taskB.Status = generationmodel.StatusPending
		taskB.QuotaCharged = true
		require.NoError(t, repo.Create(ctx, taskB))

		require.NoError(t, repo.RefundQuotaForTask(ctx, taskA.ID))
		gotUser, err := userRepo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, gotUser.QuotaUsed, "第一次退款只应该退掉任务 A 自己的那一份，任务 B 仍然欠着")

		require.NoError(t, repo.RefundQuotaForTask(ctx, taskA.ID))
		gotUser, err = userRepo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, gotUser.QuotaUsed, "第二次对同一个任务的退款必须是无操作——不能连带把任务 B 尚未退的额度也退掉")
	})
}

// 从未计费的任务（quota_charged 从建库起就是 false，比如同步成功的图片
// 任务）不应该被退款影响到——WHERE quota_charged = true 从一开始就不匹配。
func TestTaskRepo_RefundQuotaForTask_NeverCharged_NoOp(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("refund-task-nevercharged", usermodel.RoleUser)
		total := 5
		u.QuotaTotal = &total
		require.NoError(t, userRepo.Create(ctx, u))
		require.NoError(t, userRepo.ConsumeQuota(ctx, u.ID)) // 由某个别的任务扣的，与本任务无关

		repo := repository.NewTaskRepository(tx)
		task := sampleTask(u.ID)
		task.QuotaCharged = false
		require.NoError(t, repo.Create(ctx, task))

		require.NoError(t, repo.RefundQuotaForTask(ctx, task.ID))

		gotUser, err := userRepo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, gotUser.QuotaUsed, "任务从未计费，不应该退掉别的额度")
	})
}

// 不存在的任务 id 是无操作，不是错误——同 RefundQuota 对不存在用户 id 的
// 处理方式，worker 侧调用可能因竞态看到一个已经被删除的任务行。
func TestTaskRepo_RefundQuotaForTask_UnknownTaskID_NoOpNoError(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewTaskRepository(tx)
		assert.NoError(t, repo.RefundQuotaForTask(ctx, 9_999_999))
	})
}
