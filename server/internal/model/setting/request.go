package setting

// UpdateItem 是 PUT /api/settings 请求体里的单个配置项。
//
// Value 留空表示"不修改"而非"清空"——旧系统的真实陷阱是前端表单把没有
// 手动重新输入的密钥字段整段清空后一并提交，导致原本配置好的密钥被空值
// 覆盖。service 层（Task 4）据此跳过空值项，绝不能把它当成"用户要清空这
// 一项"来处理；确需清空必须走单独的、明确的操作——即下面的 Clear 字段。
//
// Clear 为 true 时无条件清空该项（忽略 Value，即便它非空）；这是"用户确实
// 想清掉这个值"的唯一显式入口。Clear 为 false（默认，零值）时才适用上面
// "空值=不修改"的规则。
type UpdateItem struct {
	Key   Key    `json:"key" binding:"required"`
	Value string `json:"value"`
	Clear bool   `json:"clear"`
}

type UpdateRequest struct {
	Items []UpdateItem `json:"items" binding:"required,dive"`
}

// TestConnectionRequest 是 POST /api/settings/test 的请求体。
//
// Provider 留空等价于 "dashscope"——旧前端从没发过这个字段，缺省值必须
// 与它历来的唯一行为一致，否则老前端打新后端会莫名其妙地报错。非空但不
// 认识的值（既不是 "dashscope" 也不是 "t8star"）由 service 层校验并返回
// VALIDATION_FAILED；这里不用 binding:"oneof=..." 是因为空字符串也要合法
// 通过（表示"用默认"），而 oneof 校验空值同样会失败。
type TestConnectionRequest struct {
	Provider string `json:"provider"`
}
