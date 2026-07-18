package common

// Response 是所有 HTTP 接口的统一响应体。
// Code 为 "OK" 表示成功，否则是错误码，供前端查 i18n 表得到文案。
// Message 仅供日志与调试，不直接展示给用户。
type Response struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
	// Data 序列化时依赖 omitempty，只在接口本身为 nil 时才会省略字段；
	// 传入类型非 nil 但值为 nil 的指针（如 (*UserResponse)(nil)）会被
	// 序列化为 "data":null。调用 OK 前务必确认不会传入这种"类型化 nil"。
	Data any `json:"data,omitempty"`
}

const CodeOK = "OK"

func OK(data any) Response {
	return Response{Code: CodeOK, Data: data}
}

// Err 构造错误响应。Code 为业务错误码，前端据此查 i18n 表得到文案。
func Err(code string) Response {
	return Response{Code: code}
}
