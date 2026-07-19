// Package config 从一份 YAML 文件加载全部启动配置。
//
// 没有环境变量层、没有 .env、没有 fallback 链——config.yaml 是唯一的配置来源。
// 这是有意的简化：多层配置来源（env 覆盖 YAML 覆盖默认值之类）在小团队自
// 部署场景下只会增加"这个值到底从哪来的"排查成本，一份文件、一次校验、
// 失败就直接拒绝启动，反而更不容易在生产环境暴露一个不完整的配置。
package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type DBConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

// DSN 拼 pgx 连接串。
//
// 用 url.UserPassword 而非手写 Sprintf/QueryEscape：userinfo 部分的转义规则
// 与 query string 不同（例如空格在 query 里是 "+"，在 userinfo 里必须是
// "%20"，"+" 不会被当作空格解码）。url.QueryEscape 是给
// application/x-www-form-urlencoded 用的，用在这里会在密码含特殊字符时产生
// 静默的错误密码。交给 url.URL 生成整个 DSN，由标准库统一处理转义，不要
// "简化" 回手写拼接。
func (d DBConfig) DSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(d.User, d.Password),
		Host:     fmt.Sprintf("%s:%d", d.Host, d.Port),
		Path:     "/" + d.Name,
		RawQuery: url.Values{"sslmode": {d.SSLMode}}.Encode(),
	}
	return u.String()
}

// String 是 DBConfig 的脱敏表示，Password 永不出现在其中——供日志/错误信息
// 里意外把这个结构体喂给 %v/%+v 兜底，不依赖调用方每次都记得手动脱敏。
func (d DBConfig) String() string {
	return fmt.Sprintf("DBConfig{Host:%q Port:%d User:%q Password:%s Name:%q SSLMode:%q}",
		d.Host, d.Port, d.User, redactedPlaceholder, d.Name, d.SSLMode)
}

type JWTConfig struct {
	Secret string
	TTL    time.Duration
}

// String 是 JWTConfig 的脱敏表示，Secret 永不出现在其中。
func (j JWTConfig) String() string {
	return fmt.Sprintf("JWTConfig{Secret:%s TTL:%s}", redactedPlaceholder, j.TTL)
}

type BootstrapConfig struct {
	Username string
	Password string
}

// String 是 BootstrapConfig 的脱敏表示，Password 永不出现在其中。
func (b BootstrapConfig) String() string {
	return fmt.Sprintf("BootstrapConfig{Username:%q Password:%s}", b.Username, redactedPlaceholder)
}

type Config struct {
	DB               DBConfig
	JWT              JWTConfig
	Bootstrap        BootstrapConfig
	HTTPPort         int
	CORSOrigins      []string
	AppEncryptionKey string
}

// redactedPlaceholder 是四个密钥字段在任何日志/格式化输出里的替身。
const redactedPlaceholder = "«redacted»"

// String 是 Config 的脱敏表示：jwt.secret、app_encryption_key、
// database.password、bootstrap.admin_password 这四个密钥永不出现在其中。
// Config 一旦持有这个方法，fmt 的 %v/%+v/%s 都会走它而不是逐字段展开，这样
// 即便未来有人不小心 slog.Info("cfg", "config", cfg) 或 log.Printf("%+v", cfg)，
// 泄露的也只是这个脱敏视图。
func (c Config) String() string {
	return fmt.Sprintf(
		"Config{DB:%s JWT:%s Bootstrap:%s HTTPPort:%d CORSOrigins:%v AppEncryptionKey:%s}",
		c.DB, c.JWT, c.Bootstrap, c.HTTPPort, c.CORSOrigins, redactedPlaceholder,
	)
}

// LogValue 让 log/slog 在结构化日志里也走同一份脱敏视图——slog 对实现了
// slog.LogValuer 的类型会调用这个方法取值，而不是用反射展开全部字段。
func (c Config) LogValue() slog.Value {
	return slog.StringValue(c.String())
}

// defaultCORSOrigins 是 cors.origins 未设置时的默认放行范围。
//
// Vite 在默认端口被占用时会自动往上找，所以默认放行一段范围而不是单个端口。
// 5173 被占用后落到 5174/5175/... 是常见情况，写死单个端口会让前端
// 静默挂掉并且报出一个完全误导的通用错误。
var defaultCORSOrigins = []string{
	"http://localhost:5173", "http://localhost:5174",
	"http://localhost:5175", "http://localhost:5176", "http://localhost:5177",
}

// minJWTSecretLen 是 jwt.secret 的最小字节数。太短的密钥可被暴力破解，
// 从而伪造任意用户的 token；openssl rand -base64 32 产出 44 字符，
// 不会给按文档生成密钥的正常用法带来不便。
const minJWTSecretLen = 32

// appEncryptionKeySize 是 app_encryption_key 解码后必须满足的字节数，
// 对应 AES-256 的 32 字节密钥长度。这把密钥用来加密 app_settings 表里的
// 上游 API Key（详见 internal/pkg/crypto），长度不对会在启动时直接拒绝，
// 而不是留到第一次调用加解密时才暴露。
const appEncryptionKeySize = 32

// yamlConfig 是 config.yaml 的原始映射结构，字段形状对应文档里的分组
// （http/database/jwt/app_encryption_key/bootstrap/cors）。它只是解码的
// 中间产物——不直接对外暴露，Load 会把它转换、校验、填充默认值之后再
// 组装成 Config。
//
// Database.Password 用 *string 而不是 string：需要区分"YAML 里写了
// password: \"\""（合法的空密码，trust-auth Postgres 常见）与"完全没写
// password 这个键"（应套用默认值）。plain string 做不到这个区分——两种
// 情况解码后都是零值 ""。其余有默认值的字段（Host/User/Name/SSLMode/
// AdminUsername/端口/TTL/CORS 列表）不需要这个区分：旧的 env() 实现里，
// 显式设成空值和完全不设置这两种输入本来就被同等对待、都套用默认值，这里
// 延续同样的语义即可，不必对每个字段都上指针。
type yamlConfig struct {
	HTTP struct {
		Port int `yaml:"port"`
	} `yaml:"http"`

	Database struct {
		Host     string  `yaml:"host"`
		Port     int     `yaml:"port"`
		User     string  `yaml:"user"`
		Password *string `yaml:"password"`
		Name     string  `yaml:"name"`
		SSLMode  string  `yaml:"sslmode"`
	} `yaml:"database"`

	JWT struct {
		Secret string `yaml:"secret"`
		TTL    string `yaml:"ttl"`
	} `yaml:"jwt"`

	AppEncryptionKey string `yaml:"app_encryption_key"`

	Bootstrap struct {
		AdminUsername string `yaml:"admin_username"`
		AdminPassword string `yaml:"admin_password"`
	} `yaml:"bootstrap"`

	CORS struct {
		Origins []string `yaml:"origins"`
	} `yaml:"cors"`
}

// Load 从 path 指向的 YAML 文件构建配置。
//
// 文件缺失/不可读、YAML 语法错误、出现未声明的键（typo 或者已废弃的旧
// 键名）都是致命错误——没有环境变量兜底，这份文件就是唯一的配置来源，
// 悄悄忽略一个打错字的键（例如把 jwt.secret 写成 jwt.scret）会让服务用一个
// 空密钥启动而没有任何提示，比直接拒绝启动危险得多，所以用 KnownFields(true)
// 把它变成一个显式报错。
//
// jwt.secret、app_encryption_key、bootstrap.admin_password 这三项没有默认
// 值，缺失时直接失败，并且这个检查发生在任何"可选项解析"（如 jwt.ttl 的
// 时长解析）之前——必填项缺失是无条件致命的，比任何可选项的格式错误都更
// 根本，必须最先暴露给运维，不能被后面的解析错误挡住。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			abs = path
		}
		return nil, fmt.Errorf(
			"读取配置文件失败: %w\n查找路径: %s\n请复制 config.yaml.example 为 config.yaml 并填入必填项（jwt.secret / app_encryption_key / bootstrap.admin_password）",
			err, abs,
		)
	}

	var doc yamlConfig
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("配置文件 %s 解析失败（YAML 语法错误或包含未知字段）: %w", path, err)
	}

	// 必填项先检查：缺失是无条件致命的，比任何可选项的格式错误都更根本，
	// 必须最先暴露给运维，不能被后面的解析错误挡住。
	secret := doc.JWT.Secret
	if secret == "" {
		return nil, fmt.Errorf("jwt.secret 必须设置，且不得为空")
	}
	if len(secret) < minJWTSecretLen {
		return nil, fmt.Errorf("jwt.secret 至少需要 %d 字节，当前 %d 字节；生成方式：openssl rand -base64 32",
			minJWTSecretLen, len(secret))
	}
	bootstrapPwd := doc.Bootstrap.AdminPassword
	if bootstrapPwd == "" {
		return nil, fmt.Errorf("bootstrap.admin_password 必须设置，且不得为空")
	}
	encryptionKey := doc.AppEncryptionKey
	if encryptionKey == "" {
		return nil, fmt.Errorf("app_encryption_key 必须设置，且不得为空；生成方式：openssl rand -base64 32")
	}
	decodedKey, err := base64.StdEncoding.DecodeString(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("app_encryption_key 不是合法的 base64；生成方式：openssl rand -base64 32")
	}
	if len(decodedKey) != appEncryptionKeySize {
		return nil, fmt.Errorf("app_encryption_key 解码后必须是 %d 字节，实际 %d 字节；生成方式：openssl rand -base64 32",
			appEncryptionKeySize, len(decodedKey))
	}

	ttl := 168 * time.Hour
	if doc.JWT.TTL != "" {
		ttl, err = time.ParseDuration(doc.JWT.TTL)
		if err != nil {
			return nil, fmt.Errorf("jwt.ttl 不是合法时长: %q", doc.JWT.TTL)
		}
	}

	dbPort := doc.Database.Port
	if dbPort == 0 {
		dbPort = 5432
	}
	httpPort := doc.HTTP.Port
	if httpPort == 0 {
		httpPort = 8080
	}

	dbPassword := "123456"
	if doc.Database.Password != nil {
		dbPassword = *doc.Database.Password
	}

	return &Config{
		DB: DBConfig{
			Host:     stringOrDefault(doc.Database.Host, "localhost"),
			Port:     dbPort,
			User:     stringOrDefault(doc.Database.User, "postgres"),
			Password: dbPassword,
			Name:     stringOrDefault(doc.Database.Name, "omnigen"),
			SSLMode:  stringOrDefault(doc.Database.SSLMode, "disable"),
		},
		JWT:              JWTConfig{Secret: secret, TTL: ttl},
		Bootstrap:        BootstrapConfig{Username: stringOrDefault(doc.Bootstrap.AdminUsername, "admin"), Password: bootstrapPwd},
		HTTPPort:         httpPort,
		CORSOrigins:      corsOriginsOrDefault(doc.CORS.Origins),
		AppEncryptionKey: encryptionKey,
	}, nil
}

func stringOrDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// corsOriginsOrDefault 逐项 trim 空白并跳过空项，避免 YAML 里手误留下的空
// 列表项产生空字符串 origin（空字符串在 AllowOrigins 里语义不明确，容易被
// 误当成通配）；结果为空则套用默认放行范围。
func corsOriginsOrDefault(raw []string) []string {
	origins := make([]string, 0, len(raw))
	for _, o := range raw {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		origins = append(origins, o)
	}
	if len(origins) == 0 {
		return defaultCORSOrigins
	}
	return origins
}
