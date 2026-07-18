package common

// Page 是分页查询的通用参数，供后续业务域复用。
type Page struct {
	Page     int `form:"page,default=1" binding:"min=1"`
	PageSize int `form:"pageSize,default=20" binding:"min=1,max=100"`
}

func (p Page) Offset() int { return (p.Page - 1) * p.PageSize }
func (p Page) Limit() int  { return p.PageSize }
