# 算力配额 · 用量报表 · t8star 连接测试 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** 给每个用户加算力配额（每次生成扣 1、失败退回），在历史页加用量报表（按模型/按天，可筛时间与用户），并为 t8star 补一个连接测试按钮。

**Architecture:** 配额是 `users` 表上的两列，扣减用单条带条件的 UPDATE 保证并发安全；`generation_tasks.quota_charged` 让扣减与退回幂等。报表在 SQL 里聚合，不在 Go 或浏览器里算。t8star 测试复用现有 provider，按 HTTP 状态码分类而非按是否出图分类。

**Tech Stack:** Go / Gin / wire / pgx / Postgres 17；React / Vite / antd 5 / vitest。

**设计文档：**
- `docs/superpowers/specs/2026-07-19-quota-and-stats-design.md`
- `docs/superpowers/specs/2026-07-19-t8star-connection-test-design.md`

---

## 每个任务开工前必读

- **改后端代码后必须手动重启 Go 服务。** Vite 热更新前端，Go 不会。本项目已经因此产生过一次假 bug（前端发新 setting key、旧后端不认，报「提交的内容不合法」）。
- **前端类型检查用 `npx tsc -b --force`，不是 `tsc --noEmit`。** 根 tsconfig 是 `"files": []` + project references，裸跑 tsc 什么都不检查就退出 0。
- **Postgres 是 Docker 的 `postgres-17`（17.7），:5432，密码 `123456`。** 若报 PostgreSQL 14.x，是 Homebrew 实例又起来遮蔽了 Docker——停下报 BLOCKED，别绕。
- **`/Users/chenhao/go/bin` 不在 PATH。** 用 `server/Makefile` 的 targets。
- **新错误码必须同时加进 `web/src/locales/{zh-CN,en}.json` 和 `locales.test.ts` 的覆盖列表**，否则前端测试红。
- **杀进程一律按精确 PID，绝不用 `pkill -f`。** 本项目已有先例：一个 `pkill -f vite` 杀掉了另一个项目的四个 dev server。
- 服务当前在跑：后端 :8080、前端 :5173，登录 `admin` / `admin12345`。**不要杀它们**。

---

## 文件结构

### 配额

| 文件 | 职责 |
|---|---|
| `server/migrations/000006_add_user_quota.{up,down}.sql` | users 两列 + generation_tasks 一列 |
| `server/internal/model/user/types.go` | `User` 加 `QuotaTotal *int` / `QuotaUsed int` |
| `server/internal/model/user/{request,response}.go` | 建/改用户接受额度；`UserResponse` 暴露额度 |
| `server/internal/repository/user.go` | `ConsumeQuota` / `RefundQuota` |
| `server/internal/service/quota.go` | 扣减与退回的业务封装，被两个生成 service 共用 |
| `server/internal/service/generation_image.go` | 接入扣减 + defer 退款 |
| `server/internal/service/generation_video.go` | 同上 |
| `server/internal/worker/poller.go` | 任务判失败时退款 |

### 报表

| 文件 | 职责 |
|---|---|
| `server/internal/model/stats/{request,response,types}.go` | 查询参数与三块返回结构 |
| `server/internal/repository/stats.go` | 三条聚合 SQL |
| `server/internal/service/stats.go` | 权限收敛（非管理员强制只看自己） |
| `server/internal/handler/stats.go` | `GET /api/stats` |
| `web/src/types/stats.ts` · `web/src/api/stats.ts` | 前端契约 |
| `web/src/pages/history/StatsPanel.tsx` | 报表 UI |

### t8star 测试

| 文件 | 职责 |
|---|---|
| `server/internal/service/setting.go` | `TestConnection(ctx, provider)` 分派 |
| `server/internal/service/t8star_tester.go` | t8star 探测实现 |

---

# Phase A：算力配额

## Task 1: 数据库与 UserRepository 配额方法

**Files:**
- Create: `server/migrations/000006_add_user_quota.{up,down}.sql`
- Modify: `server/internal/model/user/types.go`, `server/internal/repository/user.go`
- Test: `server/internal/repository/user_test.go`

- [ ] **Step 1: 写迁移**

`000006_add_user_quota.up.sql`：

```sql
-- quota_total 为 NULL 表示不限量。刻意不用 -1 之类的魔法值：
-- SQL 里 "quota_total IS NULL OR quota_used < quota_total" 读起来就是它的字面意思。
ALTER TABLE users
  ADD COLUMN quota_total INT,
  ADD COLUMN quota_used  INT NOT NULL DEFAULT 0;

-- quota_charged 用于防重复退款。视频异步，轮询 worker 判失败时退回；
-- worker 若重跑同一任务，没有这个标志就会退第二次，凭空送额度。
ALTER TABLE generation_tasks
  ADD COLUMN quota_charged BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE users ADD CONSTRAINT users_quota_used_nonneg CHECK (quota_used >= 0);
```

`000006_add_user_quota.down.sql`：

```sql
ALTER TABLE users DROP CONSTRAINT users_quota_used_nonneg;
ALTER TABLE generation_tasks DROP COLUMN quota_charged;
ALTER TABLE users DROP COLUMN quota_used;
ALTER TABLE users DROP COLUMN quota_total;
```

- [ ] **Step 2: 应用到两个库**

```bash
cd server && make migrate-up && make migrate-test-up
PGPASSWORD=123456 /opt/homebrew/opt/libpq/bin/psql -h 127.0.0.1 -p 5432 -U postgres -d omnigen -c "\d users" | grep quota
```
预期：看到 `quota_total | integer` 与 `quota_used | integer | not null | 0`

- [ ] **Step 3: 扩展 User 实体**

`server/internal/model/user/types.go` 的 `User` 结构体增加：

```go
	// QuotaTotal 为 nil 表示不限量。
	QuotaTotal *int
	QuotaUsed  int
```

同步更新 `repository/user.go` 的 `userColumns` 常量与 `scanUserRow`，把两列加进去。**注意 `List` 的窗口函数扫描也走同一个 `scanUserRow`，加列后两处都会自动跟上——但要确认 `userColumns` 是唯一的列清单来源，不存在第二处手写列名。**

- [ ] **Step 4: 写失败的 repository 测试**

追加到 `server/internal/repository/user_test.go`：

```go
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
```

- [ ] **Step 5: 写并发测试**

这是本功能唯一的并发正确性要求，**必须真的并发跑，不能串行调用假装**。它不能用 `withTx`（同一事务内的并发写会互相阻塞），要直接用 `testPool`：

```go
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
```

- [ ] **Step 6: 运行测试确认失败**

```bash
cd server && go test ./internal/repository/ -run TestUserRepo_.*Quota -v -count=1
```
预期：编译失败，`repo.ConsumeQuota undefined`

- [ ] **Step 7: 实现**

在 `server/internal/repository/user.go` 的 `UserRepository` 接口加两个方法，并实现：

```go
// ConsumeQuota 扣减一次额度。
//
// 用单条带条件的 UPDATE 而不是"先查余额再写入"：后者在两个并发请求
// 都读到"还剩 1"时会双双通过检查、双双扣减，即超发。单条语句由
// Postgres 保证行级原子，不需要事务也不需要显式锁。
//
// quota_total 为 NULL 表示不限量，此时只累加 quota_used 不做上限判断。
func (r *userRepository) ConsumeQuota(ctx context.Context, id int64) error {
	const q = `
		UPDATE users SET quota_used = quota_used + 1, updated_at = now()
		WHERE id = $1 AND (quota_total IS NULL OR quota_used < quota_total)`
	tag, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("扣减额度失败: %w", err))
	}
	if tag.RowsAffected() == 0 {
		// 0 行有两种可能：用户不存在，或额度已用尽。
		// 用户不存在在本系统里不可达（调用方来自已认证的中间件），
		// 所以统一报额度耗尽。
		return apperr.ErrQuotaExceeded
	}
	return nil
}

// RefundQuota 退回一次额度。quota_used > 0 是守卫，防止退成负数。
func (r *userRepository) RefundQuota(ctx context.Context, id int64) error {
	const q = `
		UPDATE users SET quota_used = quota_used - 1, updated_at = now()
		WHERE id = $1 AND quota_used > 0`
	if _, err := r.db.Exec(ctx, q, id); err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("退回额度失败: %w", err))
	}
	// 0 行不是错误：没扣过就退（例如重复退款）应当是无操作。
	return nil
}
```

在 `server/internal/pkg/apperr/apperr.go` 增加：

```go
	ErrQuotaExceeded = New("QUOTA_EXCEEDED", http.StatusForbidden)
```

并在 `web/src/locales/zh-CN.json` / `en.json` 的 `errors` 命名空间与 `web/src/locales/locales.test.ts` 的必需列表里加上 `QUOTA_EXCEEDED`：
- zh-CN：`"算力额度已用尽，请联系管理员增加额度"`
- en：`"You have used up your quota. Contact an administrator to increase it."`

- [ ] **Step 8: 运行测试确认通过**

```bash
cd server && go test ./internal/repository/ -run TestUserRepo -v -count=1 -race
cd ../web && npx vitest run src/locales
```
预期：全部 PASS，包括并发测试

- [ ] **Step 9: 验证并发测试真的会红**

把 `ConsumeQuota` 临时改成先查后写：

```go
u, _ := r.GetByID(ctx, id)
if u.QuotaTotal != nil && u.QuotaUsed >= *u.QuotaTotal { return apperr.ErrQuotaExceeded }
_, err := r.db.Exec(ctx, `UPDATE users SET quota_used = quota_used + 1 WHERE id = $1`, id)
```

跑 `TestUserRepo_ConsumeQuota_ConcurrentNeverOverdraws`，**确认它失败**（成功数 > 5）。然后恢复。报告观察到的数字——如果它没失败，说明测试没有真正并发，必须查清楚再往下走。

- [ ] **Step 10: 提交**

```bash
git add server/migrations/000006_add_user_quota.up.sql server/migrations/000006_add_user_quota.down.sql \
        server/internal/model/user/types.go server/internal/repository/user.go server/internal/repository/user_test.go \
        server/internal/pkg/apperr/apperr.go web/src/locales/
git commit -m "feat(server): 用户算力配额的存储与原子扣减"
```

---

## Task 2: QuotaService 与图片生成接入

**Files:**
- Create: `server/internal/service/quota.go`
- Modify: `server/internal/service/generation_image.go`
- Test: `server/internal/service/quota_test.go`, `generation_image_test.go`

**这个任务有一个必须先理解的顺序问题。** `generation_image.go` 当前的流程是：校验 → 取凭证 → **调 provider（150-151 行）** → **再创建 task 行（176 或 192 行）**。而扣费必须在调 provider 之前，此时 task 行还不存在，`quota_charged` 无处可写。

解决办法是用 `defer` + 命名返回值：扣费后注册一个延迟退款。注意：**命名返回值 + defer 并不覆盖 panic**——panic 展开时 retErr 仍是 nil，必须在 defer 里 recover 再 re-panic，否则用户会被扣费却拿到一个 500。task 行创建时按当时的状态写 `quota_charged`。

- [ ] **Step 1: 写 QuotaService 与它的假 repository 测试**

`server/internal/service/quota_test.go`：

```go
package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/service"
)

func TestQuotaService_Consume_ThenRefund(t *testing.T) {
	repo := newFakeRepo()
	u := seedUser(t, repo, "q", "password123", usermodel.RoleUser, usermodel.StatusActive)
	total := 2
	u.QuotaTotal = &total

	q := service.NewQuotaService(repo)

	require.NoError(t, q.Consume(context.Background(), u.ID))
	assert.Equal(t, 1, repo.users[u.ID].QuotaUsed)

	require.NoError(t, q.Refund(context.Background(), u.ID))
	assert.Equal(t, 0, repo.users[u.ID].QuotaUsed)
}

func TestQuotaService_Consume_ExhaustedSurfacesQuotaExceeded(t *testing.T) {
	repo := newFakeRepo()
	u := seedUser(t, repo, "q2", "password123", usermodel.RoleUser, usermodel.StatusActive)
	total := 1
	u.QuotaTotal = &total

	q := service.NewQuotaService(repo)
	require.NoError(t, q.Consume(context.Background(), u.ID))

	err := q.Consume(context.Background(), u.ID)
	assertCode(t, err, "QUOTA_EXCEEDED")
}
```

`fakeUserRepo` 需要实现 `ConsumeQuota` / `RefundQuota`，语义要**忠实于真实 repository**（不限量不拦但仍计数；退回不低于 0）。测试替身与真实实现语义不一致，是本项目已经踩过的坑（`fakeUserRepo.Update` 曾整体覆盖结构体，与真实 SQL 只更新三列不符）。

- [ ] **Step 2: 实现 QuotaService**

`server/internal/service/quota.go`：

```go
package service

import (
	"context"

	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// QuotaService 封装算力额度的扣减与退回。
//
// 它刻意只有两个动作、不持有任何状态：并发安全完全由 repository 的
// 单条带条件 UPDATE 保证，service 层不做"先查再判断"的逻辑，否则
// 就把好不容易避开的竞态又加回来了。
type QuotaService struct {
	users repository.UserRepository
}

func NewQuotaService(users repository.UserRepository) *QuotaService {
	return &QuotaService{users: users}
}

// Consume 扣减一次。额度耗尽返回 apperr.ErrQuotaExceeded。
func (s *QuotaService) Consume(ctx context.Context, userID int64) error {
	return s.users.ConsumeQuota(ctx, userID)
}

// Refund 退回一次。这是尽力而为的补偿动作——退款失败不应该把
// 一个"生成已失败"的结果再变成另一个错误盖住原因，调用方只记日志。
func (s *QuotaService) Refund(ctx context.Context, userID int64) error {
	return s.users.RefundQuota(ctx, userID)
}
```

- [ ] **Step 3: 写图片生成的配额测试**

追加到 `server/internal/service/generation_image_test.go`：

```go
// 额度耗尽时必须在调用上游之前就拦住——不能先花钱再报错。
func TestGenerateImage_QuotaExhausted_NeverCallsUpstream(t *testing.T) {
	// 构造一个额度为 0 的用户与一个会记录调用次数的假 provider
	// 断言：返回 QUOTA_EXCEEDED，且 provider 调用次数为 0，且没有创建 task 行
}

func TestGenerateImage_Success_ConsumesOneAndMarksCharged(t *testing.T) {
	// 断言：quota_used 从 0 变 1，创建的 task 行 QuotaCharged == true
}

func TestGenerateImage_UpstreamFailure_RefundsAndMarksNotCharged(t *testing.T) {
	// 断言：quota_used 回到 0，创建的 FAILED task 行 QuotaCharged == false
}

// 校验阶段失败（模型不存在 / 地域不允许 / 凭证未配置）不该动额度。
func TestGenerateImage_ValidationFailure_DoesNotTouchQuota(t *testing.T) {
	// 表驱动覆盖三种校验失败，断言 quota_used 始终为 0
}
```

把上面四个用例写成完整代码，沿用该文件既有的 fake 构造方式（`newFakeRepo`、假 provider factory、假 settings）。

- [ ] **Step 4: 运行测试确认失败**

```bash
cd server && go test ./internal/service/ -run TestGenerateImage_Quota -v -count=1
```

- [ ] **Step 5: 接入图片生成**

`generation_image.go` 的 `GenerateImage` 改为命名返回值，并在所有校验通过之后、构造 provider 之前插入扣费：

```go
func (s *ImageGenerationService) GenerateImage(
	ctx context.Context, userID int64, req GenerateImageRequest,
) (task *generationmodel.Task, retErr error) {

	// ...（既有的目录校验、参数校验、地域校验、凭证读取全部保持不变）...

	// 扣费点：所有校验之后、真正调上游之前。
	// 校验失败不该动额度；而一旦要发请求就必须先扣，否则并发下会超发。
	if err := s.quota.Consume(ctx, userID); err != nil {
		return nil, err
	}
	charged := true
	defer func() {
		// panic 不会设置命名返回值——retErr 在这里仍是 nil。只判断 retErr
		// 会漏掉 panic 路径：middleware.Recovery 把 panic 转成干净的 500，
		// 用户被扣了费、拿到一个 500、没有任何补偿，外部还看不出哪里坏了。
		r := recover()
		if charged && (retErr != nil || r != nil) {
			if refundErr := s.quota.Refund(ctx, userID); refundErr != nil {
				slog.Error("退回额度失败", "userID", userID, "error", refundErr)
			}
			charged = false
		}
		if r != nil {
			panic(r) // 交还给 middleware.Recovery
		}
	}()

	p := s.factory(model.Protocol, apiKey, region, workspaceID, endpoint)
	result, callErr := p.GenerateImage(ctx, provider.ImageRequest{ /* 不变 */ })

	// 失败分支：先退款（由上面的 defer 在 return 时统一处理），
	// task 行按当时是否仍持有扣费写 QuotaCharged。
	// 成功分支：QuotaCharged 为 true。
	// 具体写法见下方说明。
}
```

**关键细节**：失败分支当前会 `s.tasks.Create(ctx, task)` 写一条 FAILED 行然后 `return nil, callErr`。由于 defer 在 `return` 之后才执行，创建行时 `charged` 仍是 true——但这条行的语义应该是"没有计费"。做法是在失败分支里**显式先退款并把 `charged` 置 false**，再以 `QuotaCharged: false` 创建行；成功分支以 `QuotaCharged: true` 创建行。defer 保留作为兜底，只有在既没显式处理、又发生错误时才触发（例如 `s.tasks.Create` 自身失败）。

`ImageGenerationService` 结构体增加 `quota *QuotaService` 字段，`NewImageGenerationService` 增加对应参数，wire 里补上。

- [ ] **Step 6: 运行测试确认通过**

```bash
cd server && go build ./... && go test ./internal/service/ -v -count=1 -race
```

- [ ] **Step 7: 验证 defer 兜底真的起作用**

把失败分支里的显式退款临时删掉，只留 defer。跑 `TestGenerateImage_UpstreamFailure_RefundsAndMarksNotCharged`——额度应仍然被退回（因为 defer 兜住了），但 `QuotaCharged` 会是 true 而非 false，测试应因后者失败。报告观察到的现象，确认两条路径各自的职责清晰。恢复代码。

- [ ] **Step 8: 提交**

```bash
git add server/internal/service/quota.go server/internal/service/quota_test.go \
        server/internal/service/generation_image.go server/internal/service/generation_image_test.go \
        server/internal/service/generation_image_mock_test.go server/internal/wire.go server/internal/wire_gen.go
git commit -m "feat(server): 图片生成接入配额扣减与失败退回"
```

---

## Task 3: 视频生成与轮询 worker 接入配额

**Files:**
- Modify: `server/internal/service/generation_video.go`, `server/internal/worker/poller.go`
- Test: `server/internal/service/generation_video_test.go`, `server/internal/worker/poller_test.go`

视频是异步的：提交时扣费，任务落库时 `quota_charged = true`；worker 在判定 FAILED 或超时时退款并把 `quota_charged` 置 false。

- [ ] **Step 1: 写测试**

服务层：与 Task 2 同构的四个用例（额度耗尽不调上游、成功扣 1 且标记 charged、CreateVideoTask 失败退回、校验失败不动额度）。

worker 层：

```go
// 任务判定失败时退回额度，并把 quota_charged 置 false。
func TestPoller_TaskFailed_RefundsQuota(t *testing.T)

// 已退过的任务（quota_charged=false）再次被处理不会重复退款。
// 这是 quota_charged 存在的唯一理由，必须直接测。
func TestPoller_AlreadyRefunded_DoesNotRefundTwice(t *testing.T)

// 超时终止也要退款。
func TestPoller_Timeout_RefundsQuota(t *testing.T)

// 成功的任务不退款。
func TestPoller_Succeeded_DoesNotRefund(t *testing.T)
```

- [ ] **Step 2: 运行确认失败**

```bash
cd server && go test ./internal/service/ ./internal/worker/ -run Quota -v -count=1
```

- [ ] **Step 3: 实现**

`TaskRepository` 增加：

```go
// RefundQuotaForTask 在同一事务里退回用户额度并把任务标记为未计费。
// 以 quota_charged = true 为条件，保证重复调用只生效一次——
// worker 可能因重试或重启重复处理同一个任务。
RefundQuotaForTask(ctx context.Context, taskID, userID int64) error
```

实现用一条 CTE 保证原子与幂等：

```sql
WITH flipped AS (
    UPDATE generation_tasks SET quota_charged = false, updated_at = now()
    WHERE id = $1 AND quota_charged = true
    RETURNING user_id
)
UPDATE users SET quota_used = quota_used - 1, updated_at = now()
WHERE id = (SELECT user_id FROM flipped) AND quota_used > 0
```

若 `flipped` 为空（已退过），第二条 UPDATE 的 WHERE 匹配不到行，什么也不做——天然幂等。

worker 在判定失败/超时的分支里调用它。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd server && go build ./... && go vet ./... && gofmt -l . && go test ./... -count=1 -race
```

- [ ] **Step 5: 验证幂等真的生效**

把 CTE 里的 `AND quota_charged = true` 临时删掉，跑 `TestPoller_AlreadyRefunded_DoesNotRefundTwice`，**确认它失败**（额度被退了两次）。恢复。报告观察到的数字。

- [ ] **Step 6: 提交**

```bash
git add server/internal/service/generation_video.go server/internal/service/generation_video_test.go \
        server/internal/worker/ server/internal/repository/task.go server/internal/repository/task_test.go
git commit -m "feat(server): 视频生成与轮询 worker 接入配额，退款幂等"
```

---

## Task 4: 配额的接口与管理界面

**Files:**
- Modify: `server/internal/model/user/{request,response}.go`, `server/internal/service/user.go`, `server/internal/handler/user.go`, `server/internal/model/auth/response.go`
- Modify: `web/src/types/user.ts`, `web/src/pages/UsersPage.tsx`, `web/src/pages/UserFormModal.tsx`, `web/src/layouts/AppShell.tsx`, `web/src/stores/auth.ts`
- Test: 对应的 `_test.go` 与 `.test.tsx`

- [ ] **Step 1: 后端**

- `usermodel.CreateRequest` / `UpdateRequest` 增加 `QuotaTotal *int`（`binding:"omitempty,min=0"`）。**注意 `UpdateRequest` 已全是指针以区分"未提供"与"提供了零值"，`QuotaTotal` 是指针的指针语义问题**：用 `*int` 无法表达"把额度改成不限量（NULL）"。方案是额外加一个 `QuotaUnlimited *bool`，为 true 时置 NULL。把这个取舍写成注释。
- `UserResponse` 增加 `QuotaTotal *int` / `QuotaUsed int`
- `authmodel.LoginResponse` 与 `GET /api/auth/me` 的响应已经复用 `UserResponse`，自动带上，无需另改
- `UserService.Create` 默认 `QuotaTotal` 为 nil（不限量）还是某个数？**默认给 nil 会让新用户无限用，与本功能意图相反。默认给 100，并在表单里预填。** 管理员创建时可改。

测试：建用户带额度、改额度、改成不限量、`/auth/me` 返回额度、非管理员改不了别人的额度。

- [ ] **Step 2: 前端**

- 用户管理列表加「额度」列：`{quotaUsed}/{quotaTotal}`，不限量显示「不限」
- 建/改表单加额度输入 + 「不限量」开关
- 顶栏用户菜单旁显示剩余次数（`quotaTotal - quotaUsed`），不限量则不显示；剩余 ≤ 10 时用警告色
- `auth store` 的 `user` 已经是 `UserResponse` 形状，加字段即可

测试：列表渲染额度、不限量显示「不限」、表单提交带上额度、顶栏在不限量时不显示数字。

- [ ] **Step 3: 验证与提交**

```bash
cd server && go build ./... && go vet ./... && gofmt -l . && go test ./... -count=1
cd ../web && npx tsc -b --force && npx vitest run && npm run build
```

```bash
git add server/internal/model/ server/internal/service/user.go server/internal/handler/user.go \
        web/src/types/user.ts web/src/pages/UsersPage.tsx web/src/pages/UserFormModal.tsx \
        web/src/layouts/AppShell.tsx web/src/stores/auth.ts web/src/locales/
git commit -m "feat: 配额的管理界面与剩余次数展示"
```

---

# Phase B：用量报表

## Task 5: 聚合 SQL 与 stats repository

**Files:**
- Create: `server/internal/model/stats/{types,request,response}.go`, `server/internal/repository/stats.go`
- Test: `server/internal/repository/stats_test.go`

- [ ] **Step 1: 定义返回结构**

`server/internal/model/stats/response.go`：

```go
package stats

import "time"

// Overview 是总览。TokensAvailable 区分「上游没给 token 数据」与「token 为 0」——
// 视频三种模式的上游根本不返回 token，界面必须显示「—」而不是 0，
// 否则会让人误以为视频免费。
type Overview struct {
	TotalCalls      int64 `json:"totalCalls"`
	SucceededCalls  int64 `json:"succeededCalls"`
	FailedCalls     int64 `json:"failedCalls"`
	TotalTokens     int64 `json:"totalTokens"`
	TokensAvailable bool  `json:"tokensAvailable"`
	VideoSeconds    int64 `json:"videoSeconds"`
}

type ByModel struct {
	Model           string `json:"model"`
	Mode            string `json:"mode"`
	Calls           int64  `json:"calls"`
	Succeeded       int64  `json:"succeeded"`
	Failed          int64  `json:"failed"`
	Tokens          int64  `json:"tokens"`
	TokensAvailable bool   `json:"tokensAvailable"`
	VideoSeconds    int64  `json:"videoSeconds"`
}

type ByDay struct {
	Day       time.Time `json:"day"`
	Calls     int64     `json:"calls"`
	Succeeded int64     `json:"succeeded"`
	Failed    int64     `json:"failed"`
}

type Report struct {
	Overview Overview  `json:"overview"`
	ByModel  []ByModel `json:"byModel"`
	ByDay    []ByDay   `json:"byDay"`
}
```

`request.go` 定义 `Query{ From, To *time.Time; UserID *int64 }`。

- [ ] **Step 2: 写测试**

`server/internal/repository/stats_test.go`，用 `withTx` 造已知数据后断言三块自洽：

```go
// 造 N 条已知任务，断言总数、按模型、按天三者互相对得上。
func TestStatsRepo_ThreeBlocksAreSelfConsistent(t *testing.T)

// 归属隔离：用户 A 的报表里不含用户 B 的任务。
func TestStatsRepo_UserScoped(t *testing.T)

// 时间边界：from/to 的闭开区间语义必须明确并被测到，
// 否则跨天记录会被重复计入或漏计。
func TestStatsRepo_TimeBoundaryIsHalfOpen(t *testing.T)

// 视频任务的 tokens_available 为 false。
func TestStatsRepo_VideoHasNoTokenData(t *testing.T)

// 空数据集返回结构完整的零值，不是 nil slice。
func TestStatsRepo_EmptyReturnsZeroValuesNotNil(t *testing.T)
```

把每个用例写成完整代码。

- [ ] **Step 3: 运行确认失败**

- [ ] **Step 4: 实现聚合 SQL**

时间区间统一用**左闭右开** `[from, to)`，并在代码注释里写明——半开区间是唯一不会在跨天时重复计入的选择。

总览：

```sql
SELECT
  count(*)                                                   AS total_calls,
  count(*) FILTER (WHERE status = 'SUCCEEDED')               AS succeeded,
  count(*) FILTER (WHERE status = 'FAILED')                  AS failed,
  COALESCE(SUM((usage->>'total_tokens')::numeric), 0)::bigint AS total_tokens,
  bool_or(usage ? 'total_tokens')                            AS tokens_available,
  COALESCE(SUM((usage->>'output_video_duration')::numeric), 0)::bigint AS video_seconds
FROM generation_tasks
WHERE ($1::bigint IS NULL OR user_id = $1)
  AND ($2::timestamptz IS NULL OR created_at >= $2)
  AND ($3::timestamptz IS NULL OR created_at <  $3)
```

`usage ? 'total_tokens'` 是 JSONB 的键存在判断——这就是区分「没有 token 数据」与「token 是 0」的机制。`bool_or` 在没有任何行时返回 NULL，扫描时要处理成 false。

按模型：同样的 WHERE，`GROUP BY model, mode`，按 calls 降序。
按天：`GROUP BY date_trunc('day', created_at)`，按天升序。

- [ ] **Step 5: 运行测试确认通过**

```bash
cd server && make migrate-test-up && go test ./internal/repository/ -run TestStatsRepo -v -count=1
```

- [ ] **Step 6: 提交**

---

## Task 6: stats service、handler 与路由

**Files:**
- Create: `server/internal/service/stats.go`, `server/internal/handler/stats.go`
- Modify: `server/internal/router/router.go`, `server/internal/wire.go`
- Test: 对应 `_test.go`

- [ ] **Step 1: 写测试**

```go
// 非管理员传 userId 被忽略，只拿到自己的数据——这是权限收敛的核心断言。
func TestStatsService_NonAdmin_UserIDParamIgnored(t *testing.T)

// 管理员不传 userId 得到全部用户合计。
func TestStatsService_Admin_NoUserIDReturnsAll(t *testing.T)

// 管理员传 userId 筛选到指定用户。
func TestStatsService_Admin_UserIDFilters(t *testing.T)

// handler：未登录 401；非法时间格式 422。
```

- [ ] **Step 2: 实现**

service 的 `GetReport(ctx, actorID int64, actorRole usermodel.Role, q stats.Query)`：非管理员时**无条件把 `q.UserID` 覆写为 `&actorID`**，不是"如果传了就报错"——忽略比报错更符合"这个参数对你不存在"的语义，也避免泄露"存在这个参数"。

handler 注册 `GET /api/stats`，走登录中间件。`wire` 补上。

- [ ] **Step 3: 验证并提交**

```bash
cd server && make wire && go build ./... && go vet ./... && gofmt -l . && go test ./... -count=1
```

**改完后端记得重启 :8080 上的服务**，否则前端会打到旧二进制上。

---

## Task 7: 报表前端

**Files:**
- Create: `web/src/types/stats.ts`, `web/src/api/stats.ts`, `web/src/pages/history/StatsPanel.tsx`
- Modify: `web/src/pages/HistoryPage.tsx`
- Test: `web/src/pages/history/StatsPanel.test.tsx`

**先调用 dataviz skill**，再写图表代码。

- [ ] **Step 1: 历史页改为两个 Tab**

`HistoryPage.tsx` 用 antd `Tabs`：「记录」渲染现有表格，「报表」渲染 `StatsPanel`。Tab 状态不需要持久化。

- [ ] **Step 2: StatsPanel**

- 顶部筛选：时间预设（近 7 天 / 近 30 天 / 全部）+ 自定义区间；管理员多一个用户选择器（默认「全部用户」）
- 总览：数字卡片
- 按天：时间序列图（dataviz）
- 按模型：表格。**Token 列在 `tokensAvailable` 为 false 时显示「—」，不是 0。**表头注明视频不返回 token。

- [ ] **Step 3: 测试**

```
- 渲染总览/按模型/按天三块
- 视频行的 Token 列显示「—」而非 0（这是本功能最容易做错的地方，必须测）
- 非管理员看不到用户选择器
- 时间预设切换会重新请求
- 空数据集渲染空状态而不崩
```

- [ ] **Step 4: 验证并提交**

```bash
cd web && npx tsc -b --force && npx vitest run && npm run build
```

---

# Phase C：t8star 连接测试

## Task 8: 后端按 provider 分派

**Files:**
- Create: `server/internal/service/t8star_tester.go`
- Modify: `server/internal/service/setting.go`, `server/internal/handler/setting.go`
- Test: `server/internal/service/setting_test.go`, `server/internal/handler/setting_test.go`

- [ ] **Step 1: 写测试**

```go
// provider=t8star 走 t8star tester，provider=dashscope 走 DashScope tester，互不串。
// 这是 endpoint 拆分那个 bug 的同类风险，必须直接测。
func TestTestConnection_RoutesToCorrectProvider(t *testing.T)

// 缺省不传 provider 等价于 dashscope（保持与现有前端兼容）。
func TestTestConnection_DefaultsToDashscope(t *testing.T)

// 非法 provider 值返回 VALIDATION_FAILED。
func TestTestConnection_InvalidProviderRejected(t *testing.T)

// t8star Key 未配置 → SETTING_INCOMPLETE，且上游零调用。
func TestTestConnection_T8starKeyMissing_NoUpstreamCall(t *testing.T)

// 上游 401/403 → UPSTREAM_AUTH_FAILED。
func TestT8starTester_AuthFailure(t *testing.T)

// 上游 4xx 业务错误 → 判定为成功（凭证有效）。
// 这条极容易写反，必须显式测。
func TestT8starTester_BusinessErrorMeansCredentialValid(t *testing.T)

// 连接失败 → UPSTREAM_FAILED。
func TestT8starTester_NetworkFailure(t *testing.T)
```

- [ ] **Step 2: 实现**

`TestConnection(ctx, provider string)`：

| provider | API Key | Endpoint | 其他 |
|---|---|---|---|
| `dashscope`（缺省） | `dashscope_api_key` | `dashscope_endpoint` | `region`、`workspace_id` |
| `t8star` | `t8star_api_key` | `t8star_endpoint` | 无 |

t8star tester 复用 `provider/t8star` 的 `Client`（协议解析已有真实报文夹具覆盖），发一个空 prompt 请求，**按 HTTP 状态码分类而不是按是否拿到图片分类**：401/403 → `UPSTREAM_AUTH_FAILED`；其余任何响应（含 4xx 业务错误、含成功出图）→ 凭证有效；网络层失败 → `UPSTREAM_FAILED`。

handler 的请求体加 `{provider}`，缺省 `dashscope`。

- [ ] **Step 3: 验证并提交**

---

## Task 9: t8star 测试按钮

**Files:**
- Modify: `web/src/pages/SettingsPage.tsx`, `web/src/api/setting.ts`, `web/src/locales/*`
- Test: `web/src/pages/SettingsPage.test.tsx`

- [ ] **Step 1: 实现**

t8star 卡片加「测试连接」按钮与结果区，复用百炼卡片已有的展示组件。`settingApi.test(provider)` 带上 provider 参数。

**按钮下方必须注明**：「t8star 没有独立的鉴权探测接口，本次测试会向上游发起一次真实请求，可能产生少量费用」。百炼卡片不加这句——它确实是免费的，这个不对称是真实的，藏起来才是误导。

- [ ] **Step 2: 测试**

```
- 两个按钮各自触发正确的 provider，结果互不覆盖
- t8star 按钮带费用说明，百炼按钮不带
- 成功与失败都渲染翻译后的文案，不出现原始错误码
```

- [ ] **Step 3: 验证并提交**

---

# Phase D：回归与联调

## Task 10: 全栈回归

- [ ] **Step 1: 两侧全量**

```bash
cd server && go build ./... && go vet ./... && gofmt -l . && go test ./... -count=1 -race
cd ../web && npx tsc -b --force && npx vitest run && npm run build
```

- [ ] **Step 2: 重启后端并浏览器走查**

**先重启 :8080 上的 Go 服务**（按精确 PID 杀，绝不用 `pkill -f`），否则走查的是旧二进制。

用浏览器自动化（`agent-browser` 或 `browse`）逐条确认：

1. 管理员给测试用户设 2 点额度
2. 该用户生成两次（可以用假 Key，失败即可）——**失败会退款，所以额度应该没变**
3. 把额度改成 1，直接改数据库把 `quota_used` 设为 1，再生成一次 → 前端显示「算力额度已用尽」的翻译文案，不是原始错误码
4. 顶栏剩余次数正确显示；管理员（不限量）不显示数字
5. 历史页两个 Tab 可切换；报表三块都渲染
6. 视频任务行的 Token 列显示「—」而不是 0
7. 非管理员看不到用户选择器；管理员默认看到全部
8. t8star 卡片的测试按钮存在且带费用说明

**没有真实 API Key，所以上游相关的项一律以「失败得干净」为通过标准。任何无法验证的项明确标注 UNVERIFIED，不要写成通过。**

- [ ] **Step 3: 清理与提交**

```bash
lsof -iTCP:3000 -iTCP:5173 -iTCP:8080 -sTCP:LISTEN
```
报告还有什么在监听、是不是自己起的。

---

## 完成标准

- [ ] 后端 `go test ./... -race` 全绿
- [ ] 前端 `npm test` / `npx tsc -b --force` / `npm run build` 全绿
- [ ] **并发扣减测试在改成"先查后写"时确实会红**（Task 1 Step 9 已验证）
- [ ] **重复退款测试在去掉 `quota_charged` 守卫时确实会红**（Task 3 Step 5 已验证）
- [ ] 视频行 Token 显示「—」而非 0
- [ ] 校验失败不扣额度、prompt 优化不扣额度
- [ ] 旧系统 `npm start`（:3000）仍可独立运行
