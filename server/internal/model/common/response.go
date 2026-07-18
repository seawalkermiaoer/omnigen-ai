package common

// Response 是所有 HTTP 接口的统一响应体。
// Code 为 "OK" 表示成功，否则是错误码，供前端查 i18n 表得到文案。
// Message 仅供日志与调试，不直接展示给用户。
type Response struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

const CodeOK = "OK"

func OK(data any) Response {
	return Response{Code: CodeOK, Data: data}
}
