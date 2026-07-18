package dashscope

import "strings"

// 这个文件里的辅助函数都在做同一件事：在一个已经解码成
// map[string]any（即 JSON 反序列化后的通用形状）的上游响应体上，模拟 JS
// 那种"随手取一个可能不存在的嵌套字段，取不到就当空"的访问方式
// （`data?.error?.message || data?.message || ''` 这种链式写法）。
// Go 没有可选链，这些函数就是显式写出来的等价物。

// stringField 取 m[key]，只有它真的是字符串时才返回，否则返回空串——
// 对应 JS 里 `m?.[key]` 后面接的隐式 falsy 处理。
func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// nestedMap 取 m[key] 并断言为 map[string]any，取不到或类型不对时返回 nil。
func nestedMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[key]; ok {
		if mm, ok := v.(map[string]any); ok {
			return mm
		}
	}
	return nil
}

// isJSONTruthy 复刻 JS 的真值判断规则，用于 `data.code || data.error` 这类
// "只要非假值就算存在"的检查——data.error 在实践中永远是个 object（永远
// truthy）或干脆不存在，但完整实现真值规则比只判断 nil 更贴近原意，也不会
// 在遇到 error:false / error:"" 这种边界输入时表现出与 JS 不同的行为。
func isJSONTruthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	default:
		// map、slice 等复合类型在 JS 里永远是 truthy 的对象/数组。
		return true
	}
}

// hasNativeError 对应 server.js 里反复出现的 `r.data && (r.data.code ||
// r.data.error)`——DashScope 原生协议用这两个字段中的任一个标记错误响应，
// 不依赖 HTTP 状态码（很多错误响应本身是 200）。
func hasNativeError(data map[string]any) bool {
	if data == nil {
		return false
	}
	return isJSONTruthy(data["code"]) || isJSONTruthy(data["error"])
}

// extractHTTPError 对应 server.js `/api/generate-image` 里 HTTP 状态 >= 400
// 时的错误文案链（server.js:408）：
//
//	data?.error?.message || data?.message || data?.code
//
// 注意取值顺序：先看嵌套 error.message，再看顶层 message，最后退到顶层
// code。这与 extractNativeError 的顺序不同，两者都要照原样保留，不能合并成
// 一个通用函数——旧代码就是这样写的，谁先谁后决定了同一个响应体在两种场景
// 下可能报出不同的错误文案。
func extractHTTPError(data map[string]any) string {
	if errObj := nestedMap(data, "error"); errObj != nil {
		if m := stringField(errObj, "message"); m != "" {
			return m
		}
	}
	if m := stringField(data, "message"); m != "" {
		return m
	}
	return stringField(data, "code")
}

// extractNativeError 对应 server.js 里出现两次、顺序相同的错误文案链
// （generate-image:414 与 optimize-prompt:629）：
//
//	data.message || data.error?.message || data.code
//
// 与 extractHTTPError 相比，message 和 error.message 的优先级是反过来的。
func extractNativeError(data map[string]any) string {
	if m := stringField(data, "message"); m != "" {
		return m
	}
	if errObj := nestedMap(data, "error"); errObj != nil {
		if m := stringField(errObj, "message"); m != "" {
			return m
		}
	}
	return stringField(data, "code")
}

// isAccessDeniedResponse 逐字照抄 server.js:245-249：
//
//	function isAccessDeniedResponse(data) {
//	  const code = data?.code || data?.error?.code || '';
//	  const message = data?.message || data?.error?.message || '';
//	  return /AccessDenied/i.test(code) || /Access denied/i.test(message);
//	}
//
// 两个正则不对称，且是有意为之，不得"统一"：
//   - code 字段找的是无空格的连续子串 "AccessDenied"（大小写不敏感）；
//   - message 字段找的是带一个空格的连续子串 "Access denied"（大小写不敏感）；
//
// 因此 code="access_denied"（下划线）不命中，message="AccessDenied"（无空格）
// 也不命中——这两种输入都必须保持"不触发降级重试"的旧行为。
func isAccessDeniedResponse(data map[string]any) bool {
	code := stringField(data, "code")
	if code == "" {
		if errObj := nestedMap(data, "error"); errObj != nil {
			code = stringField(errObj, "code")
		}
	}
	message := stringField(data, "message")
	if message == "" {
		if errObj := nestedMap(data, "error"); errObj != nil {
			message = stringField(errObj, "message")
		}
	}
	return strings.Contains(strings.ToLower(code), "accessdenied") ||
		strings.Contains(strings.ToLower(message), "access denied")
}
