package user

import "time"

// UserResponse 是用户对外的唯一表示。刻意不含 PasswordHash。
//
// QuotaTotal / QuotaUsed 经由 LoginResponse 与 GET /api/auth/me 自动带给
// 前端——两者都复用这个结构体，不需要另外改。
type UserResponse struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        Role      `json:"role"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// QuotaTotal 为 nil 表示不限量。
	QuotaTotal *int `json:"quotaTotal"`
	QuotaUsed  int  `json:"quotaUsed"`
}

type UserListResponse struct {
	Total int64          `json:"total"`
	Items []UserResponse `json:"items"`
}

func FromEntity(u User) UserResponse {
	return UserResponse{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Role:        u.Role,
		Status:      u.Status,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
		QuotaTotal:  u.QuotaTotal,
		QuotaUsed:   u.QuotaUsed,
	}
}

func FromEntities(us []User) []UserResponse {
	out := make([]UserResponse, 0, len(us))
	for _, u := range us {
		out = append(out, FromEntity(u))
	}
	return out
}
