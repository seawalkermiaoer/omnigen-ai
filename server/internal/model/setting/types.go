// Package setting 定义应用配置项（app_settings）的实体、请求体与响应体。
// 导入时统一使用 settingmodel 别名，避免与 service/setting、repository 包名冲突。
package setting

import "time"

// Key 是合法的配置项键名，收敛成有限的类型化常量集合，避免调用方到处手写
// 裸字符串——裸字符串既容易拼错，也没有地方能顺带把"是否 secret"钉在一起。
type Key string

const (
	KeyDashscopeAPIKey Key = "dashscope_api_key"
	KeyT8starAPIKey    Key = "t8star_api_key"
	KeyRegion          Key = "region"
	// KeyDashscopeEndpoint and KeyT8starEndpoint used to be a single shared
	// KeyEndpoint ("endpoint") key. That was fine as long as the old system
	// only ever had one upstream provider active at a time (a single global
	// switch), but the new per-model protocol selection (generation_image.go
	// picks t8star for openai-protocol models, dashscope for everything
	// else) makes the two providers independent — a t8star relay address
	// must never leak into a DashScope call and vice versa. See migration
	// 000005_split_endpoint_setting for how an existing "endpoint" row is
	// carried forward (into dashscope_endpoint, the old default meaning).
	KeyDashscopeEndpoint  Key = "dashscope_endpoint"
	KeyT8starEndpoint     Key = "t8star_endpoint"
	KeyWorkspaceID        Key = "workspace_id"
	KeyOSSBucket          Key = "oss_bucket"
	KeyOSSRegion          Key = "oss_region"
	KeyOSSRoleArn         Key = "oss_role_arn"
	KeyOSSAccessKeyID     Key = "oss_access_key_id"
	KeyOSSAccessKeySecret Key = "oss_access_key_secret"
)

// AllKeys 是全部合法配置项，顺序固定——service 层用它做批量初始化/校验时，
// 不依赖 map 遍历那种每次运行都可能不同的顺序。
var AllKeys = []Key{
	KeyDashscopeAPIKey,
	KeyT8starAPIKey,
	KeyRegion,
	KeyDashscopeEndpoint,
	KeyT8starEndpoint,
	KeyWorkspaceID,
	KeyOSSBucket,
	KeyOSSRegion,
	KeyOSSRoleArn,
	KeyOSSAccessKeyID,
	KeyOSSAccessKeySecret,
}

// secretKeys 标记哪些配置项是敏感信息：值须加密落库（service 层，Task 4），
// 响应体只能给 {configured, masked}，绝不给明文（model 层，本文件 + response.go）。
var secretKeys = map[Key]bool{
	KeyDashscopeAPIKey:    true,
	KeyT8starAPIKey:       true,
	KeyOSSAccessKeyID:     true,
	KeyOSSAccessKeySecret: true,
}

// IsSecret 报告某个 key 是否属于敏感配置项。
func (k Key) IsSecret() bool { return secretKeys[k] }

// Setting 是数据库实体。Value 究竟是明文还是密文取决于调用方：repository
// 层原样存取（Task 2 职责，不关心内容），加解密由 service 层负责（Task 4
// 职责）。FromEntity（见 response.go）假定传入的 Value 已经是明文。
type Setting struct {
	Key       string
	Value     string
	IsSecret  bool
	UpdatedBy *int64
	UpdatedAt time.Time
}
