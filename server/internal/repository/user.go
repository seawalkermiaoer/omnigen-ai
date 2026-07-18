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
}

type userRepository struct{ db DB }

func NewUserRepository(db DB) UserRepository { return &userRepository{db: db} }

const userColumns = `id, username, password_hash, display_name, role, status, created_at, updated_at`

func scanUser(row pgx.Row) (*usermodel.User, error) {
	var u usermodel.User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName,
		&u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.ErrUserNotFound.Wrap(err)
		}
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("扫描用户行失败: %w", err))
	}
	return &u, nil
}

// isUniqueViolation 识别 Postgres 唯一约束冲突（SQLSTATE 23505）。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *userRepository) Create(ctx context.Context, u *usermodel.User) error {
	const q = `
		INSERT INTO users (username, password_hash, display_name, role, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`
	err := r.db.QueryRow(ctx, q, u.Username, u.PasswordHash, u.DisplayName, u.Role, u.Status).
		Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
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

func (r *userRepository) List(ctx context.Context, offset, limit int) ([]usermodel.User, int64, error) {
	var total int64
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(fmt.Errorf("统计用户数失败: %w", err))
	}

	rows, err := r.db.Query(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY id ASC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(fmt.Errorf("查询用户列表失败: %w", err))
	}
	defer rows.Close()

	items := make([]usermodel.User, 0, limit)
	for rows.Next() {
		var u usermodel.User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName,
			&u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, 0, apperr.ErrInternal.Wrap(fmt.Errorf("扫描用户列表失败: %w", err))
		}
		items = append(items, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(err)
	}
	return items, total, nil
}

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
		`UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2`, hash, id)
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

func (r *userRepository) CountActiveAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE role = 'admin' AND status = 'active'`).Scan(&n)
	if err != nil {
		return 0, apperr.ErrInternal.Wrap(fmt.Errorf("统计活跃管理员失败: %w", err))
	}
	return n, nil
}
