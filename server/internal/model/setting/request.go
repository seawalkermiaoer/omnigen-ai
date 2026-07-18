package setting

// UpdateItem 是 PUT /api/settings 请求体里的单个配置项。
//
// Value 留空表示"不修改"而非"清空"——旧系统的真实陷阱是前端表单把没有
// 手动重新输入的密钥字段整段清空后一并提交，导致原本配置好的密钥被空值
// 覆盖。service 层（Task 4）据此跳过空值项，绝不能把它当成"用户要清空这
// 一项"来处理；确需清空必须走单独的、明确的操作。
type UpdateItem struct {
	Key   Key    `json:"key" binding:"required"`
	Value string `json:"value"`
}

type UpdateRequest struct {
	Items []UpdateItem `json:"items" binding:"required,dive"`
}
