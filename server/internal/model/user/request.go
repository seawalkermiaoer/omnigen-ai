package user

type CreateRequest struct {
	Username    string `json:"username" binding:"required,min=3,max=64,alphanum"`
	Password    string `json:"password" binding:"required,min=8,max=72"`
	DisplayName string `json:"displayName" binding:"max=64"`
	Role        Role   `json:"role" binding:"required,oneof=admin user"`
	// QuotaTotal 为 nil 时，UserService.Create 会默认填 100，而不是留空表示
	// 不限量——见该方法注释。管理员创建时可以显式传一个不同的值。
	QuotaTotal *int `json:"quotaTotal" binding:"omitempty,min=0"`
}

// UpdateRequest 全部字段为指针，用以区分「未提供」与「提供了零值」。
//
// QuotaTotal 单独一个 *int 无法表达"把额度改成不限量"：nil 已经被占用来
// 表示"这次请求不改这个字段"，而"不限量"在 SQL 层面是 quota_total = NULL，
// *int 没有第三种状态可用来表达"显式设为 NULL"。所以额外加一个
// QuotaUnlimited *bool：非 nil 且为 true 时，把 quota_total 置为 NULL，
// 并忽略同一次请求里可能一并传来的 QuotaTotal。不要把这两个字段合并回一
// 个——那样就再分不清"不改"和"改成不限量"了。
type UpdateRequest struct {
	DisplayName    *string `json:"displayName" binding:"omitempty,max=64"`
	Role           *Role   `json:"role" binding:"omitempty,oneof=admin user"`
	Status         *Status `json:"status" binding:"omitempty,oneof=active disabled"`
	QuotaTotal     *int    `json:"quotaTotal" binding:"omitempty,min=0"`
	QuotaUnlimited *bool   `json:"quotaUnlimited"`
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
