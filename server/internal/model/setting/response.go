package setting

import "github.com/chenhao/omnigen-ai/server/internal/pkg/crypto"

// SettingResponse 是单个配置项对外的唯一表示。
//
// 对非 secret 项返回明文 Value；对 secret 项，Value 恒为空（omitempty 会让
// 它连字段名都不出现在 JSON 里），只给 Configured（是否已配置）与 Masked
// （首尾脱敏展示值，如 "sk-abc...wxyz"）——明文密钥不会经过这个类型序列化
// 出网。这是这个类型存在的唯一理由：把"这会被发给客户端"的事实做成一个
// 一眼可见的类型，而不是指望每个 handler 都记得手动过滤。
type SettingResponse struct {
	Key        string `json:"key"`
	IsSecret   bool   `json:"isSecret"`
	Value      string `json:"value,omitempty"`
	Configured bool   `json:"configured,omitempty"`
	Masked     string `json:"masked,omitempty"`
}

type SettingsResponse struct {
	Items []SettingResponse `json:"items"`
}

// FromEntity 把一条配置转换成对外响应。
//
// 调用方必须保证 s.Value 在这里已经是明文——加解密属于 service 层职责
// （Task 4），本函数只负责按 IsSecret 决定要不要把它塞进响应体：secret 项
// 只留下 Masked/Configured，明文本身绝不写进返回值的任何字段。
func FromEntity(s Setting) SettingResponse {
	if s.IsSecret {
		return SettingResponse{
			Key:        s.Key,
			IsSecret:   true,
			Configured: s.Value != "",
			Masked:     crypto.Mask(s.Value),
		}
	}
	return SettingResponse{
		Key:      s.Key,
		IsSecret: false,
		Value:    s.Value,
	}
}

func FromEntities(items []Setting) SettingsResponse {
	out := make([]SettingResponse, 0, len(items))
	for _, s := range items {
		out = append(out, FromEntity(s))
	}
	return SettingsResponse{Items: out}
}
