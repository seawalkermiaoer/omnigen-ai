package dashscope

import "testing"

// TestIsAccessDeniedResponse 照抄 server.js:245-249 的不对称判定：
// code 字段找无空格的 "AccessDenied"（大小写不敏感），message 字段找带
// 空格的 "Access denied"（大小写不敏感），两者不得统一。
func TestIsAccessDeniedResponse(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		want bool
	}{
		{
			name: "code contains AccessDenied exact case",
			data: map[string]any{"code": "AccessDenied.Unauthorized"},
			want: true,
		},
		{
			name: "code contains accessdenied lowercase",
			data: map[string]any{"code": "some.accessdenied.error"},
			want: true,
		},
		{
			name: "message contains Access denied with space",
			data: map[string]any{"message": "Access denied for this resource"},
			want: true,
		},
		{
			name: "message contains access denied lowercase with space",
			data: map[string]any{"message": "sorry, access denied here"},
			want: true,
		},
		{
			name: "nested error.code AccessDenied",
			data: map[string]any{"error": map[string]any{"code": "AccessDenied"}},
			want: true,
		},
		{
			name: "nested error.message Access denied",
			data: map[string]any{"error": map[string]any{"message": "Access denied"}},
			want: true,
		},
		{
			name: "code with underscore does not match",
			data: map[string]any{"code": "access_denied"},
			want: false,
		},
		{
			name: "message without space does not match",
			data: map[string]any{"message": "AccessDenied"},
			want: false,
		},
		{
			name: "empty data does not match",
			data: map[string]any{},
			want: false,
		},
		{
			name: "nil data does not match",
			data: nil,
			want: false,
		},
		{
			name: "unrelated error does not match",
			data: map[string]any{"code": "InvalidParameter", "message": "bad request"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isAccessDeniedResponse(tc.data)
			if got != tc.want {
				t.Errorf("isAccessDeniedResponse(%+v) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

func TestExtractHTTPError_PrefersNestedErrorMessage(t *testing.T) {
	data := map[string]any{
		"message": "top-level message",
		"code":    "TOP_CODE",
		"error":   map[string]any{"message": "nested message"},
	}
	if got := extractHTTPError(data); got != "nested message" {
		t.Errorf("extractHTTPError = %q, want %q", got, "nested message")
	}
}

func TestExtractHTTPError_FallsBackToTopLevelMessageThenCode(t *testing.T) {
	if got := extractHTTPError(map[string]any{"message": "m", "code": "c"}); got != "m" {
		t.Errorf("extractHTTPError = %q, want %q", got, "m")
	}
	if got := extractHTTPError(map[string]any{"code": "c"}); got != "c" {
		t.Errorf("extractHTTPError = %q, want %q", got, "c")
	}
	if got := extractHTTPError(map[string]any{}); got != "" {
		t.Errorf("extractHTTPError = %q, want empty", got)
	}
}

func TestExtractNativeError_PrefersTopLevelMessage(t *testing.T) {
	data := map[string]any{
		"message": "top-level message",
		"code":    "TOP_CODE",
		"error":   map[string]any{"message": "nested message"},
	}
	if got := extractNativeError(data); got != "top-level message" {
		t.Errorf("extractNativeError = %q, want %q", got, "top-level message")
	}
}

func TestExtractNativeError_FallsBackToNestedThenCode(t *testing.T) {
	data := map[string]any{"error": map[string]any{"message": "nested"}, "code": "c"}
	if got := extractNativeError(data); got != "nested" {
		t.Errorf("extractNativeError = %q, want %q", got, "nested")
	}
	if got := extractNativeError(map[string]any{"code": "c"}); got != "c" {
		t.Errorf("extractNativeError = %q, want %q", got, "c")
	}
}

func TestHasNativeError(t *testing.T) {
	if hasNativeError(nil) {
		t.Error("nil data should not have native error")
	}
	if hasNativeError(map[string]any{}) {
		t.Error("empty data should not have native error")
	}
	if !hasNativeError(map[string]any{"code": "SOME_CODE"}) {
		t.Error("non-empty code should be a native error")
	}
	if hasNativeError(map[string]any{"code": ""}) {
		t.Error("empty string code should not be a native error")
	}
	if !hasNativeError(map[string]any{"error": map[string]any{"message": "x"}}) {
		t.Error("non-nil error object should be a native error")
	}
	if hasNativeError(map[string]any{"error": nil}) {
		t.Error("nil error field should not be a native error")
	}
}
