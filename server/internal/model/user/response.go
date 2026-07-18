package user

import "time"

// UserResponse 是用户对外的唯一表示。刻意不含 PasswordHash。
type UserResponse struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        Role      `json:"role"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
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
	}
}

func FromEntities(us []User) []UserResponse {
	out := make([]UserResponse, 0, len(us))
	for _, u := range us {
		out = append(out, FromEntity(u))
	}
	return out
}
