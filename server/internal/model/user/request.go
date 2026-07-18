package user

type CreateRequest struct {
	Username    string `json:"username" binding:"required,min=3,max=64,alphanum"`
	Password    string `json:"password" binding:"required,min=8,max=72"`
	DisplayName string `json:"displayName" binding:"max=64"`
	Role        Role   `json:"role" binding:"required,oneof=admin user"`
}

// UpdateRequest 全部字段为指针，用以区分「未提供」与「提供了零值」。
type UpdateRequest struct {
	DisplayName *string `json:"displayName" binding:"omitempty,max=64"`
	Role        *Role   `json:"role" binding:"omitempty,oneof=admin user"`
	Status      *Status `json:"status" binding:"omitempty,oneof=active disabled"`
}

type ResetPasswordRequest struct {
	Password string `json:"password" binding:"required,min=8,max=72"`
}

type ListQuery struct {
	Page     int `form:"page,default=1" binding:"min=1"`
	PageSize int `form:"pageSize,default=20" binding:"min=1,max=100"`
}

func (q ListQuery) Offset() int { return (q.Page - 1) * q.PageSize }
func (q ListQuery) Limit() int  { return q.PageSize }
