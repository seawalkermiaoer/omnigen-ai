package generation

// ListQuery 是 GET /api/tasks 的分页查询参数，与 model/user.ListQuery 同样的
// Page/PageSize → Offset/Limit 换算规则，保持两个域的分页语义一致。
type ListQuery struct {
	Page     int `form:"page,default=1" binding:"min=1"`
	PageSize int `form:"pageSize,default=20" binding:"min=1,max=100"`
}

func (q ListQuery) Offset() int { return (q.Page - 1) * q.PageSize }
func (q ListQuery) Limit() int  { return q.PageSize }
