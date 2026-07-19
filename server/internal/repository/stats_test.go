package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	statsmodel "github.com/chenhao/omnigen-ai/server/internal/model/stats"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// setCreatedAt overrides the created_at Create() just stamped with now() —
// repository.TaskRepository.Create has no created_at parameter (it's always
// "now" at insertion time), so the only way to plant a row at a specific
// instant for boundary testing is a direct UPDATE on the raw column.
func setCreatedAt(t *testing.T, ctx context.Context, tx repository.DB, taskID int64, ts time.Time) {
	t.Helper()
	_, err := tx.Exec(ctx, `UPDATE generation_tasks SET created_at = $1 WHERE id = $2`, ts, taskID)
	require.NoError(t, err)
}

func sumByModelCalls(items []statsmodel.ByModel) int64 {
	var n int64
	for _, m := range items {
		n += m.Calls
	}
	return n
}

func sumByDayCalls(items []statsmodel.ByDay) int64 {
	var n int64
	for _, d := range items {
		n += d.Calls
	}
	return n
}

// 造 5 条已知任务（4 成功 1 失败，横跨 3 个 model/mode 组合），断言总览、
// 按模型、按天三者互相对得上，也都跟造出来的 N 对得上。全部任务在同一个
// withTx 事务内创建，created_at 被 now() 冻结成同一时刻，因此按天只会有
// 一个桶——这仍然足以验证"三块自洽"，时间边界的语义由另一个用例单独测。
func TestStatsRepo_ThreeBlocksAreSelfConsistent(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("stats-consistent", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		taskRepo := repository.NewTaskRepository(tx)

		mk := func(model string, mode generationmodel.TaskMode, status generationmodel.Status, usage map[string]any) {
			task := sampleTask(u.ID)
			task.Model = model
			task.Mode = mode
			task.Status = status
			task.Usage = usage
			require.NoError(t, taskRepo.Create(ctx, task))
		}

		mk("qwen-image", generationmodel.TaskModeImgGen, generationmodel.StatusSucceeded, map[string]any{"total_tokens": float64(100)})
		mk("qwen-image", generationmodel.TaskModeImgGen, generationmodel.StatusFailed, nil)
		mk("wan-t2v", generationmodel.TaskModeT2V, generationmodel.StatusSucceeded, map[string]any{"output_video_duration": float64(5)})
		mk("wan-t2v", generationmodel.TaskModeT2V, generationmodel.StatusSucceeded, map[string]any{"output_video_duration": float64(10)})
		mk("glm-image", generationmodel.TaskModeImgEdit, generationmodel.StatusSucceeded, map[string]any{"total_tokens": float64(0)})

		statsRepo := repository.NewStatsRepository(tx)
		report, err := statsRepo.GetReport(ctx, statsmodel.Query{UserID: &u.ID})
		require.NoError(t, err)

		const n = 5
		assert.Equal(t, int64(n), report.Overview.TotalCalls)
		assert.Equal(t, int64(4), report.Overview.SucceededCalls)
		assert.Equal(t, int64(1), report.Overview.FailedCalls)
		assert.Equal(t, int64(100), report.Overview.TotalTokens, "100（task1）+ 0（task5，key 存在但值为 0）")
		assert.True(t, report.Overview.TokensAvailable, "至少一个 usage 带 total_tokens 键")
		assert.Equal(t, int64(15), report.Overview.VideoSeconds)

		assert.Equal(t, int64(n), sumByModelCalls(report.ByModel), "按模型求和应等于总调用数")
		assert.Equal(t, int64(n), sumByDayCalls(report.ByDay), "按天求和应等于总调用数")

		var modelTokens, modelSucceeded, modelFailed, modelVideoSeconds int64
		for _, m := range report.ByModel {
			modelTokens += m.Tokens
			modelSucceeded += m.Succeeded
			modelFailed += m.Failed
			modelVideoSeconds += m.VideoSeconds
		}
		assert.Equal(t, report.Overview.TotalTokens, modelTokens, "按模型的 token 求和应等于总览")
		assert.Equal(t, report.Overview.SucceededCalls, modelSucceeded)
		assert.Equal(t, report.Overview.FailedCalls, modelFailed)
		assert.Equal(t, report.Overview.VideoSeconds, modelVideoSeconds)

		require.Len(t, report.ByModel, 3, "三个不同的 (model, mode) 组合")
		assert.Equal(t, "qwen-image", report.ByModel[0].Model, "按 calls 降序，qwen-image 与 wan-t2v 都是 2 次，用 model 名再排序打平")

		var daySucceeded, dayFailed int64
		for _, d := range report.ByDay {
			daySucceeded += d.Succeeded
			dayFailed += d.Failed
		}
		assert.Equal(t, report.Overview.SucceededCalls, daySucceeded)
		assert.Equal(t, report.Overview.FailedCalls, dayFailed)
	})
}

// 归属隔离：用户 A 的报表里不含用户 B 的任务——三块（总览/按模型/按天）
// 都要测到，不能只测总览就假设按模型/按天也对。
func TestStatsRepo_UserScoped(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		a := sampleUser("stats-user-a", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, a))
		b := sampleUser("stats-user-b", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, b))

		taskRepo := repository.NewTaskRepository(tx)

		for i := 0; i < 2; i++ {
			task := sampleTask(a.ID)
			task.Model = "a-only-model"
			require.NoError(t, taskRepo.Create(ctx, task))
		}
		task := sampleTask(b.ID)
		task.Model = "b-only-model"
		require.NoError(t, taskRepo.Create(ctx, task))

		statsRepo := repository.NewStatsRepository(tx)
		report, err := statsRepo.GetReport(ctx, statsmodel.Query{UserID: &a.ID})
		require.NoError(t, err)

		assert.Equal(t, int64(2), report.Overview.TotalCalls, "只应看到 A 自己的 2 条")
		require.Len(t, report.ByModel, 1)
		assert.Equal(t, "a-only-model", report.ByModel[0].Model, "不应出现 B 的 model")
		assert.Equal(t, int64(2), sumByDayCalls(report.ByDay))
	})
}

// 时间边界必须是左闭右开 [from, to)：恰好落在 to 上的一条记录被排除，
// 恰好落在 from 上的一条记录被包含——这是唯一不会在跨天记录上重复计入
// 或漏计的选择。
func TestStatsRepo_TimeBoundaryIsHalfOpen(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("stats-boundary", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		taskRepo := repository.NewTaskRepository(tx)

		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
		middle := from.Add(12 * time.Hour)
		before := from.Add(-1 * time.Second)

		mk := func(ts time.Time) int64 {
			task := sampleTask(u.ID)
			require.NoError(t, taskRepo.Create(ctx, task))
			setCreatedAt(t, ctx, tx, task.ID, ts)
			return task.ID
		}

		mk(before) // 早于 from，必须被排除
		mk(from)   // 恰好等于 from，必须被包含
		mk(middle) // 严格落在区间内，必须被包含
		mk(to)     // 恰好等于 to，必须被排除

		statsRepo := repository.NewStatsRepository(tx)
		report, err := statsRepo.GetReport(ctx, statsmodel.Query{UserID: &u.ID, From: &from, To: &to})
		require.NoError(t, err)

		assert.Equal(t, int64(2), report.Overview.TotalCalls,
			"应只包含 from 与 middle 两条，before 早于区间、to 恰好等于右端点都应被排除")
	})
}

// 视频任务没有 token 数据：usage 只有 output_video_duration，没有
// total_tokens 键，TokensAvailable 必须是 false。同一个用例里再放一个
// total_tokens 为 0（但键存在）的图片任务，证明 false 是因为"键不存在"
// 而不是因为"聚合到的值恰好是 0"——如果实现误用了值判断，这个反例会
// 让两者的 TokensAvailable 都读成同一个结果，测试才抓得到。
func TestStatsRepo_VideoHasNoTokenData(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("stats-video-tokens", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		taskRepo := repository.NewTaskRepository(tx)

		videoTask := sampleTask(u.ID)
		videoTask.Model = "wan2.7-t2v"
		videoTask.Mode = generationmodel.TaskModeT2V
		videoTask.Usage = map[string]any{"output_video_duration": float64(8), "ratio": "16:9"}
		require.NoError(t, taskRepo.Create(ctx, videoTask))

		imageZeroTokenTask := sampleTask(u.ID)
		imageZeroTokenTask.Model = "qwen-image-zero"
		imageZeroTokenTask.Mode = generationmodel.TaskModeImgGen
		imageZeroTokenTask.Usage = map[string]any{"total_tokens": float64(0), "image_count": float64(1)}
		require.NoError(t, taskRepo.Create(ctx, imageZeroTokenTask))

		statsRepo := repository.NewStatsRepository(tx)
		report, err := statsRepo.GetReport(ctx, statsmodel.Query{UserID: &u.ID})
		require.NoError(t, err)

		// 总览混合了两条：至少一条带 total_tokens 键，所以总览层面是 true——
		// 真正的断言在按模型这一层，两个模型必须分道扬镳。
		assert.True(t, report.Overview.TokensAvailable)

		byModel := map[string]statsmodel.ByModel{}
		for _, m := range report.ByModel {
			byModel[m.Model] = m
		}
		require.Contains(t, byModel, "wan2.7-t2v")
		require.Contains(t, byModel, "qwen-image-zero")

		videoRow := byModel["wan2.7-t2v"]
		assert.False(t, videoRow.TokensAvailable, "视频没有 token 数据，必须是 false 而不是 0")
		assert.Equal(t, int64(0), videoRow.Tokens)
		assert.Equal(t, int64(8), videoRow.VideoSeconds)

		imageRow := byModel["qwen-image-zero"]
		assert.True(t, imageRow.TokensAvailable, "total_tokens 键存在（即便值是 0）就必须是 true")
		assert.Equal(t, int64(0), imageRow.Tokens)
	})
}

// byModel 分组内可能整组的 usage 都是 NULL——一个从未到达上游就失败的
// 任务（例如参数校验/配额检查在提交给上游之前就失败）不会有 usage 数据。
// bool_or(usage ? 'total_tokens') 在这种整组全 NULL 的分组上返回 SQL
// NULL，而不是 false；如果 byModel 把它直接扫进非指针 bool，pgx 会在扫描
// 阶段报错，整个 /api/stats 请求 500。这个用例只造一个「全组 usage 皆
// NULL」的 (model, mode)，断言 byModel 能正常返回且 TokensAvailable 是
// false，而不是让请求出错。
func TestStatsRepo_ByModelAllNullUsageDoesNotError(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("stats-all-null-usage", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		taskRepo := repository.NewTaskRepository(tx)

		// 两条同一 (model, mode) 分组的任务，都是提交前就失败、从未拿到
		// 上游响应，usage 全 NULL——整组没有任何一行 usage 非 NULL。
		for i := 0; i < 2; i++ {
			task := sampleTask(u.ID)
			task.Model = "never-reached-upstream"
			task.Mode = generationmodel.TaskModeImgGen
			task.Status = generationmodel.StatusFailed
			task.Usage = nil
			require.NoError(t, taskRepo.Create(ctx, task))
		}

		statsRepo := repository.NewStatsRepository(tx)
		report, err := statsRepo.GetReport(ctx, statsmodel.Query{UserID: &u.ID})
		require.NoError(t, err, "整组 usage 皆 NULL 时 byModel 不应报错（bool_or 返回 NULL 时不能扫进非指针 bool）")

		require.Len(t, report.ByModel, 1)
		row := report.ByModel[0]
		assert.Equal(t, "never-reached-upstream", row.Model)
		assert.Equal(t, int64(2), row.Calls)
		assert.Equal(t, int64(2), row.Failed)
		assert.False(t, row.TokensAvailable, "整组 usage 皆 NULL，必须归一化成 false 而不是报错或读成 true")
		assert.Equal(t, int64(0), row.Tokens)
	})
}

// 空数据集返回结构完整的零值，不是 nil slice——前端要能直接渲染
// report.byModel.map(...) / report.byDay.map(...) 而不必先判空。
func TestStatsRepo_EmptyReturnsZeroValuesNotNil(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		userRepo := repository.NewUserRepository(tx)
		u := sampleUser("stats-empty", usermodel.RoleUser)
		require.NoError(t, userRepo.Create(ctx, u))

		statsRepo := repository.NewStatsRepository(tx)
		report, err := statsRepo.GetReport(ctx, statsmodel.Query{UserID: &u.ID})
		require.NoError(t, err)

		assert.Equal(t, int64(0), report.Overview.TotalCalls)
		assert.Equal(t, int64(0), report.Overview.SucceededCalls)
		assert.Equal(t, int64(0), report.Overview.FailedCalls)
		assert.Equal(t, int64(0), report.Overview.TotalTokens)
		assert.False(t, report.Overview.TokensAvailable, "空结果集上 bool_or 是 NULL，必须被归一化成 false")
		assert.Equal(t, int64(0), report.Overview.VideoSeconds)

		require.NotNil(t, report.ByModel, "空切片而非 nil，前端不必判空")
		assert.Empty(t, report.ByModel)
		require.NotNil(t, report.ByDay, "空切片而非 nil，前端不必判空")
		assert.Empty(t, report.ByDay)
	})
}
