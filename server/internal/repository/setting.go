package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

// SettingRepository 只做存取，不做加解密——密文还是明文完全由调用方
// （service 层，Task 4）决定，这里原样存取给定的字节。
type SettingRepository interface {
	Get(ctx context.Context, key string) (*settingmodel.Setting, error)
	GetAll(ctx context.Context) ([]settingmodel.Setting, error)
	Upsert(ctx context.Context, s *settingmodel.Setting) error
	// UpsertMany 必须是原子的：全部成功或全部不生效。
	UpsertMany(ctx context.Context, items []settingmodel.Setting) error
}

type settingRepository struct{ db DB }

func NewSettingRepository(db DB) SettingRepository { return &settingRepository{db: db} }

const settingColumns = `key, value, is_secret, updated_by, updated_at`

// scanSettingRow 只做原始扫描，不做错误翻译，与 scanUserRow 的分工一致。
func scanSettingRow(row rowScanner) (*settingmodel.Setting, error) {
	var s settingmodel.Setting
	if err := row.Scan(&s.Key, &s.Value, &s.IsSecret, &s.UpdatedBy, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

// isForeignKeyViolation 判断是否违反了指定名字的外键约束，与
// isUniqueViolation（user.go）同样的理由：不能只看 SQLSTATE 23503 就断定
// 是哪个外键，表以后可能有第二个外键。
func isForeignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == constraint
}

const settingUpdatedByFKConstraint = "app_settings_updated_by_fkey"

func (r *settingRepository) Get(ctx context.Context, key string) (*settingmodel.Setting, error) {
	row := r.db.QueryRow(ctx, `SELECT `+settingColumns+` FROM app_settings WHERE key = $1`, key)
	s, err := scanSettingRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.ErrSettingNotFound.Wrap(err)
		}
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("查询配置项失败: %w", err))
	}
	return s, nil
}

func (r *settingRepository) GetAll(ctx context.Context) ([]settingmodel.Setting, error) {
	rows, err := r.db.Query(ctx, `SELECT `+settingColumns+` FROM app_settings ORDER BY key ASC`)
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("查询配置列表失败: %w", err))
	}
	defer rows.Close()

	items := make([]settingmodel.Setting, 0)
	for rows.Next() {
		s, err := scanSettingRow(rows)
		if err != nil {
			return nil, apperr.ErrInternal.Wrap(fmt.Errorf("扫描配置行失败: %w", err))
		}
		items = append(items, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return items, nil
}

func (r *settingRepository) Upsert(ctx context.Context, s *settingmodel.Setting) error {
	const q = `
		INSERT INTO app_settings (key, value, is_secret, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value, is_secret = EXCLUDED.is_secret,
		    updated_by = EXCLUDED.updated_by, updated_at = now()
		RETURNING updated_at`
	err := r.db.QueryRow(ctx, q, s.Key, s.Value, s.IsSecret, s.UpdatedBy).Scan(&s.UpdatedAt)
	if err != nil {
		if isForeignKeyViolation(err, settingUpdatedByFKConstraint) {
			return apperr.ErrValidation.Wrap(err)
		}
		return apperr.ErrInternal.Wrap(fmt.Errorf("写入配置项失败: %w", err))
	}
	return nil
}

// UpsertMany 用一条多行 INSERT ... ON CONFLICT 语句而非"开一个事务、循环
// Upsert"来实现原子性。原因是 DB 接口（db.go）只暴露 Exec/Query/QueryRow，
// 不暴露 Begin——r.db 在仓库测试里本身就是外层事务（withTx），要求它再开一
// 层子事务超出了这个抽象的职责范围。单条语句在 Postgres 里天然原子：任何
// 一行违反约束，整条语句（包括它此前"看似已生效"的其它行）一起回滚，不会
// 出现部分写入。
func (r *settingRepository) UpsertMany(ctx context.Context, items []settingmodel.Setting) error {
	if len(items) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString(`INSERT INTO app_settings (key, value, is_secret, updated_by, updated_at) VALUES `)
	args := make([]any, 0, len(items)*4)
	for i, s := range items {
		if i > 0 {
			sb.WriteString(", ")
		}
		base := i * 4
		fmt.Fprintf(&sb, "($%d, $%d, $%d, $%d, now())", base+1, base+2, base+3, base+4)
		args = append(args, s.Key, s.Value, s.IsSecret, s.UpdatedBy)
	}
	sb.WriteString(` ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value, is_secret = EXCLUDED.is_secret,
		    updated_by = EXCLUDED.updated_by, updated_at = now()`)

	_, err := r.db.Exec(ctx, sb.String(), args...)
	if err != nil {
		if isForeignKeyViolation(err, settingUpdatedByFKConstraint) {
			return apperr.ErrValidation.Wrap(err)
		}
		return apperr.ErrInternal.Wrap(fmt.Errorf("批量写入配置项失败: %w", err))
	}
	return nil
}
