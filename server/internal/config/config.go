package config

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
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

type JWTConfig struct {
	Secret string
	TTL    time.Duration
}

type BootstrapConfig struct {
	Username string
	Password string
}

type Config struct {
	DB               DBConfig
	JWT              JWTConfig
	Bootstrap        BootstrapConfig
	HTTPPort         int
	CORSOrigins      []string
	AppEncryptionKey string
}

// defaultCORSOrigins 是 CORS_ORIGINS 未设置时的默认放行范围。
//
// Vite 在默认端口被占用时会自动往上找，所以默认放行一段范围而不是单个端口。
// 5173 被占用后落到 5174/5175/... 是常见情况，写死单个端口会让前端
// 静默挂掉并且报出一个完全误导的通用错误。
var defaultCORSOrigins = []string{
	"http://localhost:5173", "http://localhost:5174",
	"http://localhost:5175", "http://localhost:5176", "http://localhost:5177",
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envPresent 区分「未设置」与「显式设为空」。
// 只用于 DB_PASSWORD：空密码是合法配置（trust 认证），
// 不能像其他变量那样把空值当作「未设置」而套用默认值。
func envPresent(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s 不是合法整数: %q", key, raw)
	}
	return v, nil
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s 不是合法时长: %q", key, raw)
	}
	return v, nil
}

// envCORSOrigins 解析逗号分隔的 CORS_ORIGINS。未设置时返回 def；
// 逐项 trim 空白并跳过空项，避免 "a, ,b" 这类输入产生空字符串 origin
// （空字符串在 AllowOrigins 里语义不明确，容易被误当成通配）。
func envCORSOrigins(key string, def []string) []string {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def
	}
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		origins = append(origins, p)
	}
	if len(origins) == 0 {
		return def
	}
	return origins
}

// minJWTSecretLen 是 JWT_SECRET 的最小字节数。太短的密钥可被暴力破解，
// 从而伪造任意用户的 token；openssl rand -base64 32 产出 44 字符，
// 不会给按文档生成密钥的正常用法带来不便。
const minJWTSecretLen = 32

// appEncryptionKeySize 是 APP_ENCRYPTION_KEY 解码后必须满足的字节数，
// 对应 AES-256 的 32 字节密钥长度。这把密钥用来加密 app_settings 表里的
// 上游 API Key（详见 internal/pkg/crypto），长度不对会在启动时直接拒绝，
// 而不是留到第一次调用加解密时才暴露。
const appEncryptionKeySize = 32

// Load 从环境变量构建配置。
// JWT_SECRET 与 BOOTSTRAP_ADMIN_PASSWORD 无默认值：缺失时直接失败。
// 前者若自动生成随机值，每次重启都会使全部 token 失效且极难排查。
func Load() (*Config, error) {
	// 必填项先检查：缺失是无条件致命的，比任何可选项的格式错误都更根本，
	// 必须最先暴露给运维，不能被后面的解析错误挡住。
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("JWT_SECRET 必须设置，且不得为空")
	}
	if len(secret) < minJWTSecretLen {
		return nil, fmt.Errorf("JWT_SECRET 至少需要 %d 字节，当前 %d 字节；生成方式：openssl rand -base64 32",
			minJWTSecretLen, len(secret))
	}
	bootstrapPwd := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if bootstrapPwd == "" {
		return nil, fmt.Errorf("BOOTSTRAP_ADMIN_PASSWORD 必须设置，且不得为空")
	}
	encryptionKey := os.Getenv("APP_ENCRYPTION_KEY")
	if encryptionKey == "" {
		return nil, fmt.Errorf("APP_ENCRYPTION_KEY 必须设置，且不得为空；生成方式：openssl rand -base64 32")
	}
	decodedKey, err := base64.StdEncoding.DecodeString(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("APP_ENCRYPTION_KEY 不是合法的 base64；生成方式：openssl rand -base64 32")
	}
	if len(decodedKey) != appEncryptionKeySize {
		return nil, fmt.Errorf("APP_ENCRYPTION_KEY 解码后必须是 %d 字节，实际 %d 字节；生成方式：openssl rand -base64 32",
			appEncryptionKeySize, len(decodedKey))
	}

	dbPort, err := envInt("DB_PORT", 5432)
	if err != nil {
		return nil, err
	}
	httpPort, err := envInt("HTTP_PORT", 8080)
	if err != nil {
		return nil, err
	}
	ttl, err := envDuration("JWT_TTL", 168*time.Hour)
	if err != nil {
		return nil, err
	}

	return &Config{
		DB: DBConfig{
			Host:     env("DB_HOST", "localhost"),
			Port:     dbPort,
			User:     env("DB_USER", "postgres"),
			Password: envPresent("DB_PASSWORD", "123456"),
			Name:     env("DB_NAME", "omnigen"),
			SSLMode:  env("DB_SSLMODE", "disable"),
		},
		JWT:              JWTConfig{Secret: secret, TTL: ttl},
		Bootstrap:        BootstrapConfig{Username: env("BOOTSTRAP_ADMIN_USERNAME", "admin"), Password: bootstrapPwd},
		HTTPPort:         httpPort,
		CORSOrigins:      envCORSOrigins("CORS_ORIGINS", defaultCORSOrigins),
		AppEncryptionKey: encryptionKey,
	}, nil
}
