package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
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
	DB        DBConfig
	JWT       JWTConfig
	Bootstrap BootstrapConfig
	HTTPPort  int
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
	bootstrapPwd := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if bootstrapPwd == "" {
		return nil, fmt.Errorf("BOOTSTRAP_ADMIN_PASSWORD 必须设置，且不得为空")
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
		JWT:       JWTConfig{Secret: secret, TTL: ttl},
		Bootstrap: BootstrapConfig{Username: env("BOOTSTRAP_ADMIN_USERNAME", "admin"), Password: bootstrapPwd},
		HTTPPort:  httpPort,
	}, nil
}
