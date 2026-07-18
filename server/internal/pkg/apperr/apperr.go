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
	// ErrSettingIncomplete: TestConnection 之类需要先读到某个凭证才能继续的
	// 操作，在该凭证还没配置时使用——是校验失败（422），不是找不到资源。
	ErrSettingIncomplete = New("SETTING_INCOMPLETE", http.StatusUnprocessableEntity)
	// ErrUpstreamTestFailed 表示凭证已配置但上游拒绝了这次探测请求
	// （网络失败、非 2xx 响应等），502 反映"我们能连，但下游有问题"。
	ErrUpstreamTestFailed = New("SETTING_TEST_FAILED", http.StatusBadGateway)

	// ErrTaskNotFound 同时覆盖"任务真的不存在"和"任务属于别人"两种情况——
	// GetByIDForUser 归属过滤后两者在数据库层面是同一个结果（零行），
	// repository 也必须只返回这一个错误码，不能替调用方猜出 403，
	// 否则 404/403 的区别本身就成了一个存在性 oracle。
	ErrTaskNotFound = New("TASK_NOT_FOUND", http.StatusNotFound)

	// ErrUploadFileMissing: 请求里没有找到名为 "file" 的表单字段
	// （server.js:266 `if (!req.file)`）。
	ErrUploadFileMissing = New("UPLOAD_FILE_MISSING", http.StatusBadRequest)
	// ErrUploadUnsupportedType: MIME 不在白名单
	// （image/jpeg、image/png、image/webp、image/bmp）内，与
	// ErrUploadFileMissing 分开——旧系统把这两种情况混成同一个模糊提示
	// （multer fileFilter 静默丢弃文件，此后 req.file 为空，走的是上面那条
	// "缺文件"分支），新版给出可区分的错误码，前端能分别提示。
	ErrUploadUnsupportedType = New("UPLOAD_UNSUPPORTED_TYPE", http.StatusBadRequest)
	// ErrUploadTooLarge: 原始文件超过 50MB 硬上限
	// （server.js:76 multer `limits.fileSize`）。
	ErrUploadTooLarge = New("UPLOAD_TOO_LARGE", http.StatusRequestEntityTooLarge)
	// ErrUploadOSSNotConfigured: 文件 > 12MB 阈值必须走 OSS，但 OSS 未配置
	// （缺 access key / secret）。旧系统对这种情况也返回 413
	// （server.js:287），不是 500——请求本身没错，是服务端配置不完整；
	// 与 ErrUploadTooLarge 用同一个 HTTP 状态码但错误码不同，
	// 前端可以分别提示"文件太大"和"服务未配置大文件上传"。
	ErrUploadOSSNotConfigured = New("UPLOAD_OSS_NOT_CONFIGURED", http.StatusRequestEntityTooLarge)

	// ErrTaskPollFailed: 视频轮询 worker 连续 5 次收到 UNKNOWN 状态或网络失败
	// 后判任务失败（Task 12，修的是旧系统 task.js:135 把单次 UNKNOWN 当终态的
	// bug）。502 反映"我们能连上游，但反复拿不到一个可信的状态"。
	ErrTaskPollFailed = New("TASK_POLL_FAILED", http.StatusBadGateway)
	// ErrTaskTimeout: 视频任务从创建起超过 30 分钟仍未到终态，worker 主动判超时
	// 失败（Task 12 新增的上限，旧系统的轮询循环没有这个上限）。504 反映
	// "不是上游明确拒绝，是我们等够了"。
	ErrTaskTimeout = New("TASK_TIMEOUT", http.StatusGatewayTimeout)

	// ErrDownloadFailed 覆盖 /api/download/:taskId/:index 转发失败的全部原因：
	// 结果 URL 的主机不在白名单内（无论是任务记录里的原始 URL 还是转发途中
	// 某一跳重定向落到的主机）、重定向跳数超过上限、上游网络错误、上游返回
	// 非 2xx。这些都是"我们代为转发这次请求，但没能拿到结果"，统一用 502，
	// 不单独区分——细分成因只在 internal 里带着供日志排查，绝不需要前端按
	// 错误码分别处理。
	ErrDownloadFailed = New("DOWNLOAD_FAILED", http.StatusBadGateway)
)
