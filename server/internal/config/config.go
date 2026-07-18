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

// DSN 拼 pgx 连接串。密码经 URL 编码，避免特殊字符破坏连接串。
func (d DBConfig) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		url.QueryEscape(d.User), url.QueryEscape(d.Password),
		d.Host, d.Port, d.Name, d.SSLMode)
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

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("JWT_SECRET 必须设置，且不得为空")
	}
	bootstrapPwd := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if bootstrapPwd == "" {
		return nil, fmt.Errorf("BOOTSTRAP_ADMIN_PASSWORD 必须设置，且不得为空")
	}

	return &Config{
		DB: DBConfig{
			Host:     env("DB_HOST", "localhost"),
			Port:     dbPort,
			User:     env("DB_USER", "postgres"),
			Password: env("DB_PASSWORD", "123456"),
			Name:     env("DB_NAME", "omnigen"),
			SSLMode:  env("DB_SSLMODE", "disable"),
		},
		JWT:       JWTConfig{Secret: secret, TTL: ttl},
		Bootstrap: BootstrapConfig{Username: env("BOOTSTRAP_ADMIN_USERNAME", "admin"), Password: bootstrapPwd},
		HTTPPort:  httpPort,
	}, nil
}
