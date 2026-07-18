// Package apperr 定义带错误码的应用错误。
// service 层返回 *AppError，由 middleware.ErrorHandler 统一转成 HTTP 响应。
// internal 字段只进日志，绝不出网。
package apperr

import "net/http"

// AppError 的字段刻意不导出：包级哨兵（ErrUserNotFound 等）是共享指针，
// 导出字段会让任何调用方一行代码就改写全局单例，波及所有 goroutine。
// 只能通过构造函数 / Wrap 生成新值，通过访问器只读。
type AppError struct {
	code       string
	httpStatus int
	internal   error
}

func (e *AppError) Code() string    { return e.code }
func (e *AppError) HTTPStatus() int { return e.httpStatus }
func (e *AppError) Internal() error { return e.internal }

func (e *AppError) Error() string {
	if e.internal != nil {
		return e.code + ": " + e.internal.Error()
	}
	return e.code
}

func (e *AppError) Unwrap() error { return e.internal }

// Is 让 errors.Is 按错误码而非指针身份匹配，
// 这样 Wrap 出来的副本仍能匹配它的哨兵。
func (e *AppError) Is(target error) bool {
	t, ok := target.(*AppError)
	return ok && t.code == e.code
}

// Wrap 返回携带底层原因的副本。刻意不修改接收者，
// 否则包级哨兵会被并发请求互相覆盖。
func (e *AppError) Wrap(cause error) *AppError {
	return &AppError{code: e.code, httpStatus: e.httpStatus, internal: cause}
}

func New(code string, status int) *AppError {
	return &AppError{code: code, httpStatus: status}
}

var (
	ErrInvalidCredentials = New("AUTH_INVALID_CREDENTIALS", http.StatusUnauthorized)
	ErrUnauthorized       = New("AUTH_UNAUTHORIZED", http.StatusUnauthorized)
	ErrUserDisabled       = New("AUTH_USER_DISABLED", http.StatusForbidden)
	ErrForbidden          = New("AUTH_FORBIDDEN", http.StatusForbidden)
	ErrWrongOldPassword   = New("AUTH_WRONG_OLD_PASSWORD", http.StatusUnprocessableEntity)

	ErrUserNotFound    = New("USER_NOT_FOUND", http.StatusNotFound)
	ErrUsernameTaken   = New("USER_USERNAME_TAKEN", http.StatusConflict)
	ErrModifySelf      = New("USER_CANNOT_MODIFY_SELF", http.StatusUnprocessableEntity)
	ErrLastAdmin       = New("USER_LAST_ADMIN", http.StatusUnprocessableEntity)
	ErrPasswordTooLong = New("USER_PASSWORD_TOO_LONG", http.StatusUnprocessableEntity)

	ErrValidation = New("VALIDATION_FAILED", http.StatusUnprocessableEntity)
	ErrInternal   = New("INTERNAL_ERROR", http.StatusInternalServerError)

	ErrSettingNotFound = New("SETTING_NOT_FOUND", http.StatusNotFound)
)
