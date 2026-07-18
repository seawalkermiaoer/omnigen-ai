// Package user 定义用户域的实体、请求体与响应体。
// 导入时统一使用 usermodel 别名，避免与 service/user、repository 包名冲突。
package user

import "time"

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// User 是数据库实体。含 PasswordHash，绝不可直接序列化返回，
// 对外一律经 UserResponse 转换。
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	DisplayName  string
	Role         Role
	Status       Status
	CreatedAt    time.Time
	UpdatedAt    time.Time
	// PasswordChangedAt 记录密码最近一次变更的时间。
	// Task 10 中间件用它与 token 的签发时间比较，让改密前签发的 token 立即失效，
	// 不必等到 TTL 到期。纯内部字段，绝不出现在 UserResponse 里。
	PasswordChangedAt time.Time
}

func (u User) IsActive() bool { return u.Status == StatusActive }
func (u User) IsAdmin() bool  { return u.Role == RoleAdmin }
