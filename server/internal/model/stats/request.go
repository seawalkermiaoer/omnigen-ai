package stats

import "time"

// Query 是三条聚合 SQL 共用的过滤条件。
//
// From/To 是左闭右开区间 [From, To)——这是唯一不会在跨天记录上重复计入
// 或漏计的选择：一条 created_at 恰好等于 To 的记录应该算进"下一段"而不是
// 这一段，恰好等于 From 的记录属于这一段。三者皆为 nil 表示不筛选。
//
// UserID 为 nil 表示不按用户筛选（全部用户合计）；非 nil 表示只看该用户。
// 非管理员调用方的权限收敛（把 UserID 强制覆写为调用者自己）是
// service 层职责，Query 本身不关心是谁在问。
type Query struct {
	From   *time.Time
	To     *time.Time
	UserID *int64
}
