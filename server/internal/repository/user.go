package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

// DB 抽象 pgxpool.Pool 与 pgx.Tx 的公共部分，
// 使测试可以把整个用例跑在一个会回滚的事务里。
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type UserRepository interface {
	Create(ctx context.Context, u *usermodel.User) error
	GetByID(ctx context.Context, id int64) (*usermodel.User, error)
	GetByUsername(ctx context.Context, username string) (*usermodel.User, error)
	List(ctx context.Context, offset, limit int) ([]usermodel.User, int64, error)
	Update(ctx context.Context, u *usermodel.User) error
	UpdatePasswordHash(ctx context.Context, id int64, hash string) error
	Delete(ctx context.Context, id int64) error
	CountActiveAdmins(ctx context.Context) (int64, error)

	// ConsumeQuota 原子扣减一次算力额度，用单条带条件的 UPDATE 而非
	// "先查余额再写入"（并发下会超发，见方法内注释）。额度耗尽返回
	// apperr.ErrQuotaExceeded。
	ConsumeQuota(ctx context.Context, id int64) error
	// RefundQuota 退回一次算力额度。0 行受影响不是错误——
	// 退一个没扣过的用户应当是无操作，因为退款调用会被重试。
	RefundQuota(ctx context.Context, id int64) error
}

type userRepository struct{ db DB }

func NewUserRepository(db DB) UserRepository { return &userRepository{db: db} }

const userColumns = `id, username, password_hash, display_name, role, status, created_at, updated_at, password_changed_at, quota_total, quota_used`

// rowScanner 同时被 pgx.Row 与 pgx.Rows 满足，
// 让单行查询和列表循环共用同一份扫描逻辑——
// 否则新增字段时漏改一处会静默读错列。
type rowScanner interface {
	Scan(dest ...any) error
}

// scanUserRow 只做原始扫描，不做错误翻译。extra 用于 List 等需要在同一行里
// 多扫一列（如窗口函数算出的 total）的场景；GetByID/GetByUsername 不传。
func scanUserRow(row rowScanner, extra ...any) (*usermodel.User, error) {
	var u usermodel.User
	dest := append([]any{&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName,
		&u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt, &u.PasswordChangedAt,
		&u.QuotaTotal, &u.QuotaUsed}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	return &u, nil
}

// scanUser 在 scanUserRow 之上把 pgx.ErrNoRows 翻译成 ErrUserNotFound。
// 仅用于单行查询（GetByID/GetByUsername）——List 走 rows.Next() 循环，
// 不会遇到 ErrNoRows，若也套用这层翻译反而会掩盖真实的扫描错误。
func scanUser(row rowScanner) (*usermodel.User, error) {
	u, err := scanUserRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.ErrUserNotFound.Wrap(err)
		}
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("扫描用户行失败: %w", err))
	}
	return u, nil
}

// isUniqueViolation 判断是否违反了指定名字的唯一约束。
// 不能只看 SQLSTATE 23505 就断定是用户名冲突——
// 表以后会有第二个唯一约束，那样会报错到完全无关的字段上。
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func (r *userRepository) Create(ctx context.Context, u *usermodel.User) error {
	const q = `
		INSERT INTO users (username, password_hash, display_name, role, status, quota_total)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at, password_changed_at, quota_used`
	err := r.db.QueryRow(ctx, q, u.Username, u.PasswordHash, u.DisplayName, u.Role, u.Status, u.QuotaTotal).
		Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.PasswordChangedAt, &u.QuotaUsed)
	if err != nil {
		if isUniqueViolation(err, "users_username_key") {
			return apperr.ErrUsernameTaken.Wrap(err)
		}
		return apperr.ErrInternal.Wrap(fmt.Errorf("创建用户失败: %w", err))
	}
	return nil
}

func (r *userRepository) GetByID(ctx context.Context, id int64) (*usermodel.User, error) {
	return scanUser(r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

func (r *userRepository) GetByUsername(ctx context.Context, username string) (*usermodel.User, error) {
	return scanUser(r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE username = $1`, username))
}

// List 用窗口函数把「总数」和「分页数据」并进同一条语句、同一个快照里，
// 避免统计与分页各查一次时在并发写入下互相不一致。
//
// 有一种情况这条单语句查询顾不到：LIMIT/OFFSET 在窗口函数之后才生效，
// 如果请求的页刚好落在数据范围之外（比如 offset 远大于总行数），
// 结果集会一行都不返回，count(*) OVER() 也就没有行能携带 total 出来。
// 这种「越界分页」只在那一次请求里退化成第二条统计查询；
// 只要结果非空，就仍然是单语句、单快照。
func (r *userRepository) List(ctx context.Context, offset, limit int) ([]usermodel.User, int64, error) {
	const q = `
		SELECT ` + userColumns + `, count(*) OVER() AS total
		FROM users ORDER BY id ASC LIMIT $1 OFFSET $2`
	rows, err := r.db.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(fmt.Errorf("查询用户列表失败: %w", err))
	}
	defer rows.Close()

	var total int64
	items := make([]usermodel.User, 0, limit)
	for rows.Next() {
		u, err := scanUserRow(rows, &total)
		if err != nil {
			return nil, 0, apperr.ErrInternal.Wrap(fmt.Errorf("扫描用户列表失败: %w", err))
		}
		items = append(items, *u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(err)
	}

	if len(items) == 0 {
		if err := r.db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&total); err != nil {
			return nil, 0, apperr.ErrInternal.Wrap(fmt.Errorf("统计用户数失败: %w", err))
		}
	}
	return items, total, nil
}

// Update 接收整个实体而 UpdatePasswordHash 只接收 id+hash，两者形状不对称是刻意的：
// Update 的调用方（service 层）套用部分更新语义，需要整条实体；密码重置永远只碰一列，
// 没有理由为它多传一个用不上的整条 User。
func (r *userRepository) Update(ctx context.Context, u *usermodel.User) error {
	const q = `
		UPDATE users
		SET display_name = $1, role = $2, status = $3, updated_at = now()
		WHERE id = $4
		RETURNING updated_at`
	err := r.db.QueryRow(ctx, q, u.DisplayName, u.Role, u.Status, u.ID).Scan(&u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.ErrUserNotFound.Wrap(err)
		}
		return apperr.ErrInternal.Wrap(fmt.Errorf("更新用户失败: %w", err))
	}
	return nil
}

func (r *userRepository) UpdatePasswordHash(ctx context.Context, id int64, hash string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE users SET password_hash = $1, updated_at = now(), password_changed_at = now() WHERE id = $2`,
		hash, id)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("更新密码失败: %w", err))
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrUserNotFound
	}
	return nil
}

func (r *userRepository) Delete(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("删除用户失败: %w", err))
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrUserNotFound
	}
	return nil
}

// CountActiveAdmins 供 service 层的「最后一个管理员受保护」规则使用。
// 注意这里存在 TOCTOU：service 先查数量再改数据，两个管理员同时互删
// 理论上可能把活跃管理员清零。未加事务/咨询锁是权衡后的选择——
// 该窗口极窄，且真发生时可自愈：EnsureBootstrapAdmin 在每次启动时
// 发现零管理员就会重新播种。
func (r *userRepository) CountActiveAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE role = 'admin' AND status = 'active'`).Scan(&n)
	if err != nil {
		return 0, apperr.ErrInternal.Wrap(fmt.Errorf("统计活跃管理员失败: %w", err))
	}
	return n, nil
}

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
