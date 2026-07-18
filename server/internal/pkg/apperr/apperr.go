// Package apperr 定义带错误码的应用错误。
// service 层返回 *AppError，由 middleware.ErrorHandler 统一转成 HTTP 响应。
// Internal 字段只进日志，绝不出网。
package apperr

import "net/http"

type AppError struct {
	Code       string
	HTTPStatus int
	Internal   error
}

func (e *AppError) Error() string {
	if e.Internal != nil {
		return e.Code + ": " + e.Internal.Error()
	}
	return e.Code
}

func (e *AppError) Unwrap() error { return e.Internal }

// Wrap 返回携带底层原因的副本。刻意不修改接收者，
// 否则包级哨兵会被并发请求互相覆盖。
func (e *AppError) Wrap(cause error) *AppError {
	return &AppError{Code: e.Code, HTTPStatus: e.HTTPStatus, Internal: cause}
}

func New(code string, status int) *AppError {
	return &AppError{Code: code, HTTPStatus: status}
}

var (
	ErrInvalidCredentials = New("AUTH_INVALID_CREDENTIALS", http.StatusUnauthorized)
	ErrUnauthorized       = New("AUTH_UNAUTHORIZED", http.StatusUnauthorized)
	ErrUserDisabled       = New("AUTH_USER_DISABLED", http.StatusForbidden)
	ErrForbidden          = New("AUTH_FORBIDDEN", http.StatusForbidden)
	ErrWrongOldPassword   = New("AUTH_WRONG_OLD_PASSWORD", http.StatusUnprocessableEntity)

	ErrUserNotFound  = New("USER_NOT_FOUND", http.StatusNotFound)
	ErrUsernameTaken = New("USER_USERNAME_TAKEN", http.StatusConflict)
	ErrModifySelf    = New("USER_CANNOT_MODIFY_SELF", http.StatusUnprocessableEntity)
	ErrLastAdmin     = New("USER_LAST_ADMIN", http.StatusUnprocessableEntity)

	ErrValidation = New("VALIDATION_FAILED", http.StatusUnprocessableEntity)
	ErrInternal   = New("INTERNAL_ERROR", http.StatusInternalServerError)
)
