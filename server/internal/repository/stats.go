package repository

import (
	"context"
	"fmt"

	statsmodel "github.com/chenhao/omnigen-ai/server/internal/model/stats"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

// StatsRepository 在 SQL 里聚合 generation_tasks，产出用量报表的三块数据。
// 聚合刻意不在 Go 里做、更不在浏览器里做：浏览器只拿得到当前分页的几条
// 记录，据此算出的"总统计"是错的；任务量增长后把全表拉进内存也不合理。
//
// 报表与配额（UserRepository.ConsumeQuota/RefundQuota）读写的是同一张
// generation_tasks 表，但两者刻意互不依赖：配额是 users 表上的当前状态，
// 报表是对历史行的聚合查询，删除历史任务会改变聚合数字，但绝不能因此
// 改变用户余额。这里的任何方法都不得被用来反推或校正配额。
type StatsRepository interface {
	// GetReport 用同一个 Query 过滤条件跑三条独立的聚合查询（总览/按模型/
	// 按天），返回结构完整的零值而非 nil——空结果集时 Overview 全 0，
	// ByModel/ByDay 是长度为 0 的切片，前端可以直接渲染不必判空。
	GetReport(ctx context.Context, q statsmodel.Query) (*statsmodel.Report, error)
}

type statsRepository struct{ db DB }

func NewStatsRepository(db DB) StatsRepository { return &statsRepository{db: db} }

// statsWhere 是三条聚合查询共享的过滤子句。时间区间是左闭右开
// [from, to)——这是唯一不会在跨天记录上重复计入或漏计的选择：
// created_at 恰好等于 to 的一行应该落进下一段而不是这一段，恰好等于
// from 的一行属于这一段。三个参数皆可为 NULL 表示不筛选。
const statsWhere = `
	WHERE ($1::bigint IS NULL OR user_id = $1)
	  AND ($2::timestamptz IS NULL OR created_at >= $2)
	  AND ($3::timestamptz IS NULL OR created_at <  $3)`

func (r *statsRepository) GetReport(ctx context.Context, q statsmodel.Query) (*statsmodel.Report, error) {
	overview, err := r.overview(ctx, q)
	if err != nil {
		return nil, err
	}
	byModel, err := r.byModel(ctx, q)
	if err != nil {
		return nil, err
	}
	byDay, err := r.byDay(ctx, q)
	if err != nil {
		return nil, err
	}
	return &statsmodel.Report{Overview: *overview, ByModel: byModel, ByDay: byDay}, nil
}

// overview 聚合出总调用数/成功/失败/Token 合计/视频总时长这一行。
//
// usage ? 'total_tokens' 是 JSONB 的键存在判断——这就是区分"没有 token
// 数据"（视频三种模式）与"token 就是 0"（某些图片模型只给 image_count）
// 的机制，不能用 SUM/COALESCE 的值本身来判断，那会把两种情况都读成 0。
//
// bool_or 在没有任何行匹配 WHERE 时返回 SQL NULL（不是 false）——这里
// 因此扫进 *bool 而不是 bool，再在扫描之后把 nil 显式归一化成 false：
// 空结果集的 TokensAvailable 应该是"没有数据"意义上的 false，而不是让
// pgx 在 bool 目标上遇到 NULL 直接报扫描错误。
func (r *statsRepository) overview(ctx context.Context, q statsmodel.Query) (*statsmodel.Overview, error) {
	const query = `
		SELECT
		  count(*)                                                            AS total_calls,
		  count(*) FILTER (WHERE status = 'SUCCEEDED')                        AS succeeded,
		  count(*) FILTER (WHERE status = 'FAILED')                           AS failed,
		  COALESCE(SUM((usage->>'total_tokens')::numeric), 0)::bigint         AS total_tokens,
		  bool_or(usage ? 'total_tokens')                                     AS tokens_available,
		  COALESCE(SUM((usage->>'output_video_duration')::numeric), 0)::bigint AS video_seconds
		FROM generation_tasks` + statsWhere

	var o statsmodel.Overview
	var tokensAvailable *bool
	err := r.db.QueryRow(ctx, query, q.UserID, q.From, q.To).Scan(
		&o.TotalCalls, &o.SucceededCalls, &o.FailedCalls,
		&o.TotalTokens, &tokensAvailable, &o.VideoSeconds,
	)
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("聚合用量总览失败: %w", err))
	}
	o.TokensAvailable = tokensAvailable != nil && *tokensAvailable
	return &o, nil
}

// byModel 按 (model, mode) 分组，按调用数降序——GROUP BY 产出的每一组
// 天然至少有一行，bool_or 在组内不会是 NULL，因此这里不需要像 overview
// 那样过 *bool 中转。
func (r *statsRepository) byModel(ctx context.Context, q statsmodel.Query) ([]statsmodel.ByModel, error) {
	const query = `
		SELECT
		  model, mode,
		  count(*)                                                            AS calls,
		  count(*) FILTER (WHERE status = 'SUCCEEDED')                        AS succeeded,
		  count(*) FILTER (WHERE status = 'FAILED')                           AS failed,
		  COALESCE(SUM((usage->>'total_tokens')::numeric), 0)::bigint         AS tokens,
		  bool_or(usage ? 'total_tokens')                                     AS tokens_available,
		  COALESCE(SUM((usage->>'output_video_duration')::numeric), 0)::bigint AS video_seconds
		FROM generation_tasks` + statsWhere + `
		GROUP BY model, mode
		ORDER BY calls DESC, model ASC, mode ASC`

	rows, err := r.db.Query(ctx, query, q.UserID, q.From, q.To)
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("聚合按模型用量失败: %w", err))
	}
	defer rows.Close()

	items := make([]statsmodel.ByModel, 0)
	for rows.Next() {
		var m statsmodel.ByModel
		if err := rows.Scan(&m.Model, &m.Mode, &m.Calls, &m.Succeeded, &m.Failed,
			&m.Tokens, &m.TokensAvailable, &m.VideoSeconds); err != nil {
			return nil, apperr.ErrInternal.Wrap(fmt.Errorf("扫描按模型用量失败: %w", err))
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return items, nil
}

// byDay 按天分组，按天升序——用于前端画时间序列图。
func (r *statsRepository) byDay(ctx context.Context, q statsmodel.Query) ([]statsmodel.ByDay, error) {
	const query = `
		SELECT
		  date_trunc('day', created_at)                                AS day,
		  count(*)                                                     AS calls,
		  count(*) FILTER (WHERE status = 'SUCCEEDED')                 AS succeeded,
		  count(*) FILTER (WHERE status = 'FAILED')                    AS failed
		FROM generation_tasks` + statsWhere + `
		GROUP BY date_trunc('day', created_at)
		ORDER BY day ASC`

	rows, err := r.db.Query(ctx, query, q.UserID, q.From, q.To)
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("聚合按天用量失败: %w", err))
	}
	defer rows.Close()

	items := make([]statsmodel.ByDay, 0)
	for rows.Next() {
		var d statsmodel.ByDay
		if err := rows.Scan(&d.Day, &d.Calls, &d.Succeeded, &d.Failed); err != nil {
			return nil, apperr.ErrInternal.Wrap(fmt.Errorf("扫描按天用量失败: %w", err))
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return items, nil
}
