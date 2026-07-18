package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

// TaskRepository 持久化 generation_tasks——图片生成同步落库为终态，视频
// 生成异步落库后由 Task 12 的轮询 worker 通过 ClaimPending/UpdateStatus/
// UpdateResult 推进状态机。这张表同时就是历史记录功能的数据源，不再有
// 单独的 history 表。
type TaskRepository interface {
	Create(ctx context.Context, t *generationmodel.Task) error
	// GetByIDForUser 按 id + user_id 一起过滤。找不到行——无论是 id 根本
	// 不存在，还是存在但属于别的用户——一律返回 apperr.ErrTaskNotFound。
	// 绝不能在"属于别人"的情况下改报 403：那会向调用方泄露"这个 id 确实
	// 存在"，本身就是一个存在性 oracle。
	GetByIDForUser(ctx context.Context, id, userID int64) (*generationmodel.Task, error)
	ListForUser(ctx context.Context, userID int64, offset, limit int) ([]generationmodel.Task, int64, error)
	// UpdateStatus 覆盖写 status/error_code/error_message 三列。errCode/
	// errMsg 传空字符串会把这两列清成 NULL（比如从 FAILED 重试回
	// PENDING/RUNNING 时，旧的错误信息不应该继续挂在任务上）。
	UpdateStatus(ctx context.Context, id int64, status generationmodel.Status, errCode, errMsg string) error
	// UpdateResult 写入最终结果并把任务落到终态 SUCCEEDED——它没有单独的
	// status 参数，因为"有结果可写"和"成功了"在当前调用方（Task 11/12）
	// 的用法里是同一件事；失败路径一律走 UpdateStatus(FAILED, ...)。
	UpdateResult(ctx context.Context, id int64, urls []string, usage map[string]any, note string) error
	// ClaimPending 取出 PENDING/RUNNING 的任务供轮询 worker 处理，按创建
	// 时间升序（最老的先处理）、受 limit 约束。当前只有单个 worker
	// goroutine 调用它（Task 12），因此没有用 SELECT ... FOR UPDATE SKIP
	// LOCKED 做跨进程的互斥抢占——如果以后要跑多个 worker 实例，这里需要
	// 补上真正的行锁，否则同一个任务会被两个 worker 同时轮询。
	ClaimPending(ctx context.Context, limit int) ([]generationmodel.Task, error)
	// DeleteForUser 按 id + user_id 一起过滤删除，与 GetByIDForUser 同一个
	// 归属规则：id 不存在、或存在但属于别的用户，一律返回
	// apperr.ErrTaskNotFound，绝不通过"能不能删"向调用方泄露"这个 id 是否
	// 存在"。删除不区分任务当前状态——PENDING/RUNNING 的任务也能删，见
	// service/generation_video.go DeleteTask 的doc 对这个决定的说明。
	DeleteForUser(ctx context.Context, id, userID int64) error
	// DeleteAllForUser 清空某用户名下的全部任务，返回实际删除的行数。
	// 该用户没有任何任务时返回 (0, nil)，不是错误。
	DeleteAllForUser(ctx context.Context, userID int64) (int64, error)
}

type taskRepository struct{ db DB }

func NewTaskRepository(db DB) TaskRepository { return &taskRepository{db: db} }

const taskColumns = `id, user_id, mode, model, status, upstream_task_id, prompt,
	params, input_urls, result_urls, usage, note, error_code, error_message,
	created_at, updated_at`

// scanTaskRow 只做原始扫描 + JSONB 解码，不做错误翻译，与 scanUserRow 的
// 分工一致。extra 用于 ListForUser 在同一行里多扫一列 count(*) OVER()。
//
// 四个 JSONB 列先扫进 []byte（pgx 对 json/jsonb 列的标准做法：扫进
// *[]byte 拿到原始 JSON 字节，不经过 pgx 内建的 JSON 编解码），再由
// decodeTaskJSON 手动 json.Unmarshal——这样 nil-vs-empty 的归一化规则
// （见 decodeTaskJSON 注释）完全由这里的代码决定，不依赖 pgx 版本间可能
// 变化的隐式行为。
func scanTaskRow(row rowScanner, extra ...any) (*generationmodel.Task, error) {
	var t generationmodel.Task
	var paramsRaw, inputRaw, resultRaw, usageRaw []byte
	dest := append([]any{
		&t.ID, &t.UserID, &t.Mode, &t.Model, &t.Status, &t.UpstreamTaskID, &t.Prompt,
		&paramsRaw, &inputRaw, &resultRaw, &usageRaw,
		&t.Note, &t.ErrorCode, &t.ErrorMessage, &t.CreatedAt, &t.UpdatedAt,
	}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	if err := decodeTaskJSON(&t, paramsRaw, inputRaw, resultRaw, usageRaw); err != nil {
		return nil, err
	}
	return &t, nil
}

// decodeTaskJSON 把四个 JSONB 列的原始字节解到 Task 上，并裁决 nil-vs-empty：
//
//   - Params/InputURLs/ResultURLs 对应的数据库列是 NOT NULL DEFAULT '{}'/'[]'，
//     语义上"没有值"就是"空容器"，不存在第三种状态；因此这里保证解出来
//     的 Go 值永远不是 nil——即便某一行是通过本 repository 之外的路径
//     写入、JSON 字节意外是 "null"，这里也会兜底成空容器，不让 nil 泄漏
//     给调用方。调用方对 ResultURLs 做 range/len 或未来的序列化都不必再
//     判 nil。
//   - Usage 对应的数据库列允许 NULL，且被刻意保留：NULL（没有用量数据）
//     和 "{}"（用量数据是空对象，理论上不会发生但类型上合法）是两个不同
//     的事实，不应该被这里的归一化抹平成同一个值。
func decodeTaskJSON(t *generationmodel.Task, paramsRaw, inputRaw, resultRaw, usageRaw []byte) error {
	t.Params = map[string]any{}
	if len(paramsRaw) > 0 {
		if err := json.Unmarshal(paramsRaw, &t.Params); err != nil {
			return fmt.Errorf("解析 params 失败: %w", err)
		}
		if t.Params == nil {
			t.Params = map[string]any{}
		}
	}

	t.InputURLs = []string{}
	if len(inputRaw) > 0 {
		if err := json.Unmarshal(inputRaw, &t.InputURLs); err != nil {
			return fmt.Errorf("解析 input_urls 失败: %w", err)
		}
		if t.InputURLs == nil {
			t.InputURLs = []string{}
		}
	}

	t.ResultURLs = []string{}
	if len(resultRaw) > 0 {
		if err := json.Unmarshal(resultRaw, &t.ResultURLs); err != nil {
			return fmt.Errorf("解析 result_urls 失败: %w", err)
		}
		if t.ResultURLs == nil {
			t.ResultURLs = []string{}
		}
	}

	t.Usage = nil
	if len(usageRaw) > 0 {
		var usage map[string]any
		if err := json.Unmarshal(usageRaw, &usage); err != nil {
			return fmt.Errorf("解析 usage 失败: %w", err)
		}
		t.Usage = usage
	}
	return nil
}

// marshalJSONMap/marshalJSONSlice 是写入路径的镜像归一化：调用方传 nil
// map/slice 时落库成 "{}"/"[]" 而不是 SQL NULL 或 JSON 字面量 null——
// 三个目标列都是 NOT NULL，传 nil 序列化出的 "null" 字节虽然满足
// NOT NULL（它是一个非 NULL 的 JSON 标量），但会破坏"这三列永远是空容器
// 而不是 null"的不变量，读回来时又得再兜底一次。在写入路径就把这件事
// 做对，比每个读路径都防御性兜底更可靠。
func marshalJSONMap(v map[string]any) ([]byte, error) {
	if v == nil {
		v = map[string]any{}
	}
	return json.Marshal(v)
}

func marshalJSONSlice(v []string) ([]byte, error) {
	if v == nil {
		v = []string{}
	}
	return json.Marshal(v)
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *taskRepository) Create(ctx context.Context, t *generationmodel.Task) error {
	paramsRaw, err := marshalJSONMap(t.Params)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("序列化 params 失败: %w", err))
	}
	inputRaw, err := marshalJSONSlice(t.InputURLs)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("序列化 input_urls 失败: %w", err))
	}
	resultRaw, err := marshalJSONSlice(t.ResultURLs)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("序列化 result_urls 失败: %w", err))
	}
	var usageRaw []byte
	if t.Usage != nil {
		usageRaw, err = json.Marshal(t.Usage)
		if err != nil {
			return apperr.ErrInternal.Wrap(fmt.Errorf("序列化 usage 失败: %w", err))
		}
	}

	const q = `
		INSERT INTO generation_tasks
			(user_id, mode, model, status, upstream_task_id, prompt,
			 params, input_urls, result_urls, usage, note, error_code, error_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id, created_at, updated_at`
	err = r.db.QueryRow(ctx, q,
		t.UserID, t.Mode, t.Model, t.Status, t.UpstreamTaskID, t.Prompt,
		paramsRaw, inputRaw, resultRaw, usageRaw, t.Note, t.ErrorCode, t.ErrorMessage,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("创建生成任务失败: %w", err))
	}

	// 与读路径（decodeTaskJSON）保持同一个不变量：调用方拿到的 *Task，
	// 无论是刚创建的还是之后读回来的，Params/InputURLs/ResultURLs 都不是
	// nil——不能让"刚创建的这一份"和"读回来的那一份"在这一点上表现不同。
	if t.Params == nil {
		t.Params = map[string]any{}
	}
	if t.InputURLs == nil {
		t.InputURLs = []string{}
	}
	if t.ResultURLs == nil {
		t.ResultURLs = []string{}
	}
	return nil
}

func (r *taskRepository) GetByIDForUser(ctx context.Context, id, userID int64) (*generationmodel.Task, error) {
	row := r.db.QueryRow(ctx, `SELECT `+taskColumns+` FROM generation_tasks WHERE id = $1 AND user_id = $2`, id, userID)
	t, err := scanTaskRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.ErrTaskNotFound.Wrap(err)
		}
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("查询生成任务失败: %w", err))
	}
	return t, nil
}

// ListForUser 用窗口函数把「总数」和「分页数据」并进同一条语句、同一个
// 快照里，与 user.go List 同样的理由；越界分页时同样退化成一次单独统计
// 而不是悄悄报 0，见下面的注释与该文件的说明。
func (r *taskRepository) ListForUser(ctx context.Context, userID int64, offset, limit int) ([]generationmodel.Task, int64, error) {
	const q = `
		SELECT ` + taskColumns + `, count(*) OVER() AS total
		FROM generation_tasks WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`
	rows, err := r.db.Query(ctx, q, userID, limit, offset)
	if err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(fmt.Errorf("查询任务列表失败: %w", err))
	}
	defer rows.Close()

	var total int64
	items := make([]generationmodel.Task, 0, limit)
	for rows.Next() {
		t, err := scanTaskRow(rows, &total)
		if err != nil {
			return nil, 0, apperr.ErrInternal.Wrap(fmt.Errorf("扫描任务列表失败: %w", err))
		}
		items = append(items, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(err)
	}

	// LIMIT/OFFSET 在窗口函数之后才生效：越过末尾的分页一行都不返回，
	// count(*) OVER() 也就没有行能携带 total 出来，需要退化成第二条统计
	// 查询——只在这一次请求里发生，结果非空时仍是单语句、单快照。
	if len(items) == 0 {
		if err := r.db.QueryRow(ctx, `SELECT count(*) FROM generation_tasks WHERE user_id = $1`, userID).Scan(&total); err != nil {
			return nil, 0, apperr.ErrInternal.Wrap(fmt.Errorf("统计任务数失败: %w", err))
		}
	}
	return items, total, nil
}

func (r *taskRepository) UpdateStatus(ctx context.Context, id int64, status generationmodel.Status, errCode, errMsg string) error {
	const q = `
		UPDATE generation_tasks
		SET status = $1, error_code = $2, error_message = $3, updated_at = now()
		WHERE id = $4`
	tag, err := r.db.Exec(ctx, q, status, nilIfEmpty(errCode), nilIfEmpty(errMsg), id)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("更新任务状态失败: %w", err))
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrTaskNotFound
	}
	return nil
}

func (r *taskRepository) UpdateResult(ctx context.Context, id int64, urls []string, usage map[string]any, note string) error {
	resultRaw, err := marshalJSONSlice(urls)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("序列化 result_urls 失败: %w", err))
	}
	var usageRaw []byte
	if usage != nil {
		usageRaw, err = json.Marshal(usage)
		if err != nil {
			return apperr.ErrInternal.Wrap(fmt.Errorf("序列化 usage 失败: %w", err))
		}
	}

	const q = `
		UPDATE generation_tasks
		SET result_urls = $1, usage = $2, note = $3, status = $4, updated_at = now()
		WHERE id = $5`
	tag, err := r.db.Exec(ctx, q, resultRaw, usageRaw, nilIfEmpty(note), generationmodel.StatusSucceeded, id)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("写入任务结果失败: %w", err))
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrTaskNotFound
	}
	return nil
}

func (r *taskRepository) DeleteForUser(ctx context.Context, id, userID int64) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM generation_tasks WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("删除生成任务失败: %w", err))
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrTaskNotFound
	}
	return nil
}

func (r *taskRepository) DeleteAllForUser(ctx context.Context, userID int64) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM generation_tasks WHERE user_id = $1`, userID)
	if err != nil {
		return 0, apperr.ErrInternal.Wrap(fmt.Errorf("清空生成任务失败: %w", err))
	}
	return tag.RowsAffected(), nil
}

func (r *taskRepository) ClaimPending(ctx context.Context, limit int) ([]generationmodel.Task, error) {
	const q = `
		SELECT ` + taskColumns + `
		FROM generation_tasks
		WHERE status IN ('PENDING', 'RUNNING')
		ORDER BY created_at ASC, id ASC
		LIMIT $1`
	rows, err := r.db.Query(ctx, q, limit)
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("查询待轮询任务失败: %w", err))
	}
	defer rows.Close()

	items := make([]generationmodel.Task, 0, limit)
	for rows.Next() {
		t, err := scanTaskRow(rows)
		if err != nil {
			return nil, apperr.ErrInternal.Wrap(fmt.Errorf("扫描待轮询任务失败: %w", err))
		}
		items = append(items, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return items, nil
}
