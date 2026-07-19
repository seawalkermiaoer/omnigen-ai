// Package stats 定义用量报表域的查询参数与响应体。
// 导入时统一使用 statsmodel 别名，避免与 service/stats、repository 包名冲突。
//
// 与 user/setting/generation 三个域不同，这个包没有自己的数据库实体——
// 报表全部聚合自 generation_tasks（见 model/generation.Task），repository
// 层直接在 SQL 里做 count/sum/group by，Go 侧只承载查询参数（request.go）
// 与聚合结果（response.go），因此本文件只留下包级说明，不定义任何类型。
package stats
