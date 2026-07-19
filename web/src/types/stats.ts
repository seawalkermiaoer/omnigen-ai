/**
 * 用量报表（GET /api/stats）的类型定义。
 *
 * 字段名与 server/internal/model/stats/response.go 的 Overview / ByModel /
 * ByDay / Report 逐字段对应（Go 结构体的 json tag 已经是 camelCase）。
 *
 * `tokensAvailable` 是本契约里最重要的一个字段：它区分「上游没有返回
 * token 数据」（视频三种模式）与「token 就是 0」，判断依据是 JSONB 里
 * `total_tokens` 这个键是否存在，不是值是否为 0。前端必须原样保留这个
 * 区分——`tokensAvailable === false` 时只允许渲染「—」，绝不能因为
 * `tokens` 恰好也是 0 就把两者当同一件事处理，否则会让人误以为视频生成
 * 不消耗任何算力。
 */

import type { TaskMode } from './generation'

export interface StatsOverview {
  totalCalls: number
  succeededCalls: number
  failedCalls: number
  totalTokens: number
  tokensAvailable: boolean
  videoSeconds: number
}

/** 按 (model, mode) 分组的一行，后端已按 calls 降序排列。 */
export interface StatsByModel {
  model: string
  mode: TaskMode
  calls: number
  succeeded: number
  failed: number
  tokens: number
  tokensAvailable: boolean
  videoSeconds: number
}

/** 按天分组的一行，后端已按天升序排列。day 是 RFC3339 时间戳字符串。 */
export interface StatsByDay {
  day: string
  calls: number
  succeeded: number
  failed: number
}

export interface StatsReport {
  overview: StatsOverview
  byModel: StatsByModel[]
  byDay: StatsByDay[]
}

/**
 * GET /api/stats 的查询参数。from/to 是 RFC3339 字符串（与
 * server/internal/handler/stats.go 的 statsQueryBody 对应，服务端用
 * time.Parse(time.RFC3339, …) 解析）；留空等价于该端不设边界。
 *
 * userId 由管理员的用户选择器提供；非管理员即使传了也会被服务端忽略、
 * 强制收敛为调用者自己，所以非管理员场景下前端从不设置这个字段。
 */
export interface StatsQuery {
  from?: string
  to?: string
  userId?: number
}
