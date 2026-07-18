# 地基与登录 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 OmniGen AI 建立 Go + Gin + wire + Postgres 后端骨架与 React + Vite + antd 前端骨架，交付完整的多用户认证、用户管理、登录页、主界面外壳、i18n 与深色主题。

**Architecture:** 后端严格三层单向依赖 handler → service → repository，各层对上暴露 interface 由 wire 编译期装配；所有请求体/响应体收敛在 `internal/model/` 下按业务域拆分。认证用 JWT 承载身份，但中间件每请求回查 `users` 表确认 `status='active'`，使禁用与改密立即生效。后端不做 i18n，只返回错误码，前端查表得文案。

**Tech Stack:** Go 1.25 / Gin / google-wire / pgx v5 / golang-migrate / golang-jwt v5 / bcrypt；Vite / React 18 / TypeScript / antd 5 / react-router 6 / zustand / axios / react-i18next / vitest。

**设计文档：** `docs/superpowers/specs/2026-07-18-rewrite-foundation-auth-design.md`

---

## 环境前置

已验证：Go 1.25.6、Node v24.12.0、Docker 容器 `postgres-17`（PostgreSQL 17.7）监听 `localhost:5432`，用户 `postgres`，密码 `123456`。Homebrew postgresql@14 已停止以腾出端口。

未安装、需在 Task 1 装：`golang-migrate`、`wire`。

---

## 文件结构

### 后端 `server/`

| 文件 | 职责 |
|---|---|
| `cmd/server/main.go` | 进程入口：读配置、调 wire、播种 admin、启 HTTP |
| `internal/config/config.go` | 环境变量 → `Config` 结构体，缺必填项则报错 |
| `internal/model/common/response.go` | 统一响应包装 `Response`、`OK()` |
| `internal/model/common/types.go` | 通用分页查询参数 |
| `internal/model/user/types.go` | `User` 实体、`Role`/`Status` 枚举 |
| `internal/model/user/request.go` | `CreateRequest`、`UpdateRequest`、`ResetPasswordRequest`、`ListQuery` |
| `internal/model/user/response.go` | `UserResponse`、`UserListResponse`、`FromEntity()` |
| `internal/model/auth/request.go` | `LoginRequest`、`ChangePasswordRequest` |
| `internal/model/auth/response.go` | `LoginResponse` |
| `internal/model/auth/types.go` | JWT `Claims` |
| `internal/pkg/apperr/apperr.go` | `AppError` 与全部错误码哨兵 |
| `internal/pkg/password/password.go` | bcrypt 哈希与校验 |
| `internal/pkg/jwtx/jwtx.go` | JWT 签发与解析 |
| `internal/repository/user.go` | `UserRepository` interface + pgx 实现 |
| `internal/service/auth.go` | 登录、改密、取当前用户 |
| `internal/service/user.go` | 用户 CRUD 与业务规则、播种 admin |
| `internal/handler/auth.go` | 认证相关 HTTP 入口 |
| `internal/handler/user.go` | 用户管理 HTTP 入口 |
| `internal/handler/health.go` | 存活与数据库连通 |
| `internal/middleware/error.go` | `AppError` → 统一响应 |
| `internal/middleware/auth.go` | JWT 校验 + 回查用户状态 + 角色守卫 |
| `internal/router/router.go` | 路由注册 |
| `internal/wire.go` / `wire_gen.go` | 依赖装配 |
| `migrations/*.sql` | golang-migrate 迁移 |

### 前端 `web/src/`

| 文件 | 职责 |
|---|---|
| `theme/index.ts` | antd token 配置，唯一允许写颜色的地方 |
| `i18n/index.ts` | react-i18next 初始化 |
| `locales/zh-CN.json` / `en.json` | 全量文案，含 `errors` 命名空间 |
| `types/common.ts` / `auth.ts` / `user.ts` | 与后端 model 对应的 TS 类型 |
| `api/client.ts` | 唯一 axios 实例 + 拦截器 |
| `api/auth.ts` / `api/user.ts` | 接口函数 |
| `stores/auth.ts` | zustand 认证状态 |
| `layouts/AppShell.tsx` | 窄图标栏 + 顶栏 |
| `components/ProtectedRoute.tsx` / `AdminRoute.tsx` | 路由守卫 |
| `pages/LoginPage.tsx` | 左右分屏登录页 |
| `pages/UsersPage.tsx` | 用户管理 |
| `pages/PlaceholderPage.tsx` | 生成类功能占位 |
| `App.tsx` / `main.tsx` | 路由表与根装配 |

---

# Phase A：后端

## Task 1: 项目骨架与 health 接口

**Files:**
- Create: `server/go.mod`, `server/cmd/server/main.go`, `server/internal/handler/health.go`, `server/internal/model/common/response.go`
- Test: `server/internal/handler/health_test.go`

- [ ] **Step 1: 初始化 module 与工具**

```bash
mkdir -p server/cmd/server server/internal/{config,model/{common,user,auth},pkg/{apperr,password,jwtx},repository,service,handler,middleware,router} server/migrations
cd server && go mod init github.com/chenhao/omnigen-ai/server
go get github.com/gin-gonic/gin@latest github.com/jackc/pgx/v5@latest github.com/golang-jwt/jwt/v5@latest golang.org/x/crypto@latest github.com/stretchr/testify@latest
go install github.com/google/wire/cmd/wire@latest
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

- [ ] **Step 2: 写统一响应包装**

`server/internal/model/common/response.go`：

```go
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
```

- [ ] **Step 3: 写失败的 health 测试**

`server/internal/handler/health_test.go`：

```go
package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
)

func TestHealth_ReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handler.NewHealthHandler(nil)
	r.GET("/api/health", h.Check)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, common.CodeOK, resp.Code)
}
```

- [ ] **Step 4: 运行测试确认失败**

```bash
cd server && go test ./internal/handler/ -run TestHealth -v
```
预期：编译失败，`undefined: handler.NewHealthHandler`

- [ ] **Step 5: 实现 health handler**

`server/internal/handler/health.go`：

```go
package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/model/common"
)

// Pinger 抽象数据库连通性检查，便于测试时传 nil。
type Pinger interface {
	Ping(ctx context.Context) error
}

type HealthHandler struct {
	db Pinger
}

func NewHealthHandler(db Pinger) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) Check(c *gin.Context) {
	status := gin.H{"service": "up"}
	if h.db != nil {
		if err := h.db.Ping(c.Request.Context()); err != nil {
			status["database"] = "down"
			c.JSON(http.StatusServiceUnavailable, common.Response{
				Code: "HEALTH_DB_UNREACHABLE", Data: status,
			})
			return
		}
		status["database"] = "up"
	}
	c.JSON(http.StatusOK, common.OK(status))
}
```

- [ ] **Step 6: 运行测试确认通过**

```bash
cd server && go test ./internal/handler/ -run TestHealth -v
```
预期：PASS

- [ ] **Step 7: 提交**

```bash
git add server/ && git commit -m "feat(server): Go module 骨架与 health 接口"
```

---

## Task 2: 配置加载

**Files:**
- Create: `server/internal/config/config.go`
- Test: `server/internal/config/config_test.go`

- [ ] **Step 1: 写失败的测试**

`server/internal/config/config_test.go`：

```go
package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/config"
)

func setRequired(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-value")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "admin12345")
}

func TestLoad_AppliesDefaults(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "localhost", cfg.DB.Host)
	assert.Equal(t, 5432, cfg.DB.Port)
	assert.Equal(t, "postgres", cfg.DB.User)
	assert.Equal(t, "omnigen", cfg.DB.Name)
	assert.Equal(t, 8080, cfg.HTTPPort)
	assert.Equal(t, 168*time.Hour, cfg.JWT.TTL)
	assert.Equal(t, "admin", cfg.Bootstrap.Username)
}

func TestLoad_FailsWithoutJWTSecret(t *testing.T) {
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "admin12345")
	t.Setenv("JWT_SECRET", "")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_SECRET")
}

func TestLoad_FailsWithoutBootstrapPassword(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-value")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BOOTSTRAP_ADMIN_PASSWORD")
}

func TestLoad_ReadsOverrides(t *testing.T) {
	setRequired(t)
	t.Setenv("DB_PORT", "5433")
	t.Setenv("DB_NAME", "omnigen_test")
	t.Setenv("JWT_TTL", "2h")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, 5433, cfg.DB.Port)
	assert.Equal(t, "omnigen_test", cfg.DB.Name)
	assert.Equal(t, 2*time.Hour, cfg.JWT.TTL)
}

func TestDSN(t *testing.T) {
	setRequired(t)
	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t,
		"postgres://postgres:123456@localhost:5432/omnigen?sslmode=disable",
		cfg.DB.DSN())
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd server && go test ./internal/config/ -v
```
预期：编译失败，`undefined: config.Load`

- [ ] **Step 3: 实现配置加载**

`server/internal/config/config.go`：

```go
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
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd server && go test ./internal/config/ -v
```
预期：全部 PASS

- [ ] **Step 5: 提交**

```bash
git add server/internal/config/ && git commit -m "feat(server): 环境变量配置加载，缺必填项拒绝启动"
```

---

## Task 3: 数据库与迁移

**Files:**
- Create: `server/migrations/000001_create_users.up.sql`, `server/migrations/000001_create_users.down.sql`, `server/internal/repository/db.go`, `server/Makefile`
- Test: `server/internal/repository/db_test.go`

- [ ] **Step 1: 创建两个数据库**

```bash
PGPASSWORD=123456 psql -h localhost -p 5432 -U postgres -c "CREATE DATABASE omnigen;"
PGPASSWORD=123456 psql -h localhost -p 5432 -U postgres -c "CREATE DATABASE omnigen_test;"
PGPASSWORD=123456 psql -h localhost -p 5432 -U postgres -lqt | cut -d'|' -f1 | grep omnigen
```
预期输出包含 `omnigen` 与 `omnigen_test`

- [ ] **Step 2: 写迁移 SQL**

`server/migrations/000001_create_users.up.sql`：

```sql
CREATE TABLE users (
  id            BIGSERIAL PRIMARY KEY,
  username      VARCHAR(64)  NOT NULL UNIQUE,
  password_hash VARCHAR(255) NOT NULL,
  display_name  VARCHAR(64)  NOT NULL DEFAULT '',
  role          VARCHAR(16)  NOT NULL DEFAULT 'user',
  status        VARCHAR(16)  NOT NULL DEFAULT 'active',
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  CONSTRAINT users_role_check   CHECK (role IN ('admin', 'user')),
  CONSTRAINT users_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE INDEX idx_users_status ON users (status);
```

`server/migrations/000001_create_users.down.sql`：

```sql
DROP TABLE IF EXISTS users;
```

- [ ] **Step 3: 写 Makefile**

`server/Makefile`：

```makefile
DB_URL      ?= postgres://postgres:123456@localhost:5432/omnigen?sslmode=disable
TEST_DB_URL ?= postgres://postgres:123456@localhost:5432/omnigen_test?sslmode=disable

# go install 把工具装到 GOPATH/bin，该目录通常不在 PATH 上（本机已确认不在），
# 因此一律用绝对路径调用，避免 "command not found"。
GOBIN   := $(shell go env GOPATH)/bin
MIGRATE := $(GOBIN)/migrate
WIRE    := $(GOBIN)/wire

.PHONY: migrate-up migrate-down migrate-test-up test wire run tools

tools:
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	go install github.com/google/wire/cmd/wire@latest

migrate-up:
	$(MIGRATE) -path migrations -database "$(DB_URL)" up

migrate-down:
	$(MIGRATE) -path migrations -database "$(DB_URL)" down 1

migrate-test-up:
	$(MIGRATE) -path migrations -database "$(TEST_DB_URL)" up

test: migrate-test-up
	go test ./... -v

wire:
	$(WIRE) ./internal/...

run:
	go run ./cmd/server
```

- [ ] **Step 4: 执行迁移并验证表结构**

```bash
cd server && make migrate-up && make migrate-test-up
PGPASSWORD=123456 psql -h localhost -p 5432 -U postgres -d omnigen -c "\d users"
```
预期：输出 users 表的 8 个字段与两个 CHECK 约束

- [ ] **Step 5: 写失败的连接池测试**

`server/internal/repository/db_test.go`：

```go
package repository_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/config"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// testDSN 指向独立的 omnigen_test 库，避免污染开发库。
func testDSN() string {
	if v := os.Getenv("TEST_DB_URL"); v != "" {
		return v
	}
	return "postgres://postgres:123456@localhost:5432/omnigen_test?sslmode=disable"
}

func TestNewPool_ConnectsAndPings(t *testing.T) {
	pool, err := repository.NewPool(context.Background(), testDSN())
	require.NoError(t, err)
	defer pool.Close()

	require.NoError(t, pool.Ping(context.Background()))
}

// 这条断言防止再次踩到 brew PG14 遮蔽 docker PG17 的坑：
// 连错实例时迁移会静默写进错误的库，不会报错。
func TestNewPool_TargetsPostgres17(t *testing.T) {
	pool, err := repository.NewPool(context.Background(), testDSN())
	require.NoError(t, err)
	defer pool.Close()

	var version string
	require.NoError(t, pool.QueryRow(context.Background(), "SHOW server_version").Scan(&version))
	assert.True(t, len(version) > 2 && version[:2] == "17",
		"期望连到 PostgreSQL 17（docker postgres-17），实际是 %s；"+
			"若为 14.x 说明 brew postgresql 又占用了 5432", version)
}

func TestNewPool_FailsOnBadDSN(t *testing.T) {
	_, err := repository.NewPool(context.Background(), "postgres://nobody:wrong@localhost:5432/nope?sslmode=disable")
	require.Error(t, err)
}

var _ = config.Config{} // 保持 import，后续任务会用到
```

- [ ] **Step 6: 运行测试确认失败**

```bash
cd server && go test ./internal/repository/ -v
```
预期：编译失败，`undefined: repository.NewPool`

- [ ] **Step 7: 实现连接池**

`server/internal/repository/db.go`：

```go
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool 建立连接池并立即 Ping，让配置错误在启动时暴露而不是首个请求时。
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析数据库连接串失败: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("创建连接池失败: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("数据库不可达: %w", err)
	}
	return pool, nil
}
```

- [ ] **Step 8: 运行测试确认通过**

```bash
cd server && go test ./internal/repository/ -v
```
预期：三条全部 PASS，其中 `TestNewPool_TargetsPostgres17` 证实连的是 docker 那个实例

- [ ] **Step 9: 提交**

```bash
git add server/migrations server/internal/repository server/Makefile
git commit -m "feat(server): users 表迁移与 pgx 连接池"
```

---

## Task 4: model 层类型定义

**Files:**
- Create: `server/internal/model/user/types.go`, `request.go`, `response.go`, `server/internal/model/auth/request.go`, `response.go`, `types.go`, `server/internal/model/common/types.go`
- Test: `server/internal/model/user/response_test.go`

- [ ] **Step 1: 写用户实体与枚举**

`server/internal/model/user/types.go`：

```go
// Package user 定义用户域的实体、请求体与响应体。
// 导入时统一使用 usermodel 别名，避免与 service/user、repository 包名冲突。
package user

import "time"

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// User 是数据库实体。含 PasswordHash，绝不可直接序列化返回，
// 对外一律经 UserResponse 转换。
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	DisplayName  string
	Role         Role
	Status       Status
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (u User) IsActive() bool { return u.Status == StatusActive }
func (u User) IsAdmin() bool  { return u.Role == RoleAdmin }
```

- [ ] **Step 2: 写请求体**

`server/internal/model/user/request.go`：

```go
package user

type CreateRequest struct {
	Username    string `json:"username" binding:"required,min=3,max=64,alphanum"`
	Password    string `json:"password" binding:"required,min=8,max=72"`
	DisplayName string `json:"displayName" binding:"max=64"`
	Role        Role   `json:"role" binding:"required,oneof=admin user"`
}

// UpdateRequest 全部字段为指针，用以区分「未提供」与「提供了零值」。
type UpdateRequest struct {
	DisplayName *string `json:"displayName" binding:"omitempty,max=64"`
	Role        *Role   `json:"role" binding:"omitempty,oneof=admin user"`
	Status      *Status `json:"status" binding:"omitempty,oneof=active disabled"`
}

type ResetPasswordRequest struct {
	Password string `json:"password" binding:"required,min=8,max=72"`
}

type ListQuery struct {
	Page     int `form:"page,default=1" binding:"min=1"`
	PageSize int `form:"pageSize,default=20" binding:"min=1,max=100"`
}

func (q ListQuery) Offset() int { return (q.Page - 1) * q.PageSize }
func (q ListQuery) Limit() int  { return q.PageSize }
```

- [ ] **Step 3: 写响应体**

`server/internal/model/user/response.go`：

```go
package user

import "time"

// UserResponse 是用户对外的唯一表示。刻意不含 PasswordHash。
type UserResponse struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        Role      `json:"role"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type UserListResponse struct {
	Total int64          `json:"total"`
	Items []UserResponse `json:"items"`
}

func FromEntity(u User) UserResponse {
	return UserResponse{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Role:        u.Role,
		Status:      u.Status,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}

func FromEntities(us []User) []UserResponse {
	out := make([]UserResponse, 0, len(us))
	for _, u := range us {
		out = append(out, FromEntity(u))
	}
	return out
}
```

- [ ] **Step 4: 写认证域类型**

`server/internal/model/auth/request.go`：

```go
package auth

type LoginRequest struct {
	Username string `json:"username" binding:"required,max=64"`
	Password string `json:"password" binding:"required,max=72"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword" binding:"required,max=72"`
	NewPassword string `json:"newPassword" binding:"required,min=8,max=72"`
}
```

`server/internal/model/auth/response.go`：

```go
package auth

import usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"

type LoginResponse struct {
	Token string                   `json:"token"`
	User  usermodel.UserResponse   `json:"user"`
}
```

`server/internal/model/auth/types.go`：

```go
package auth

import "github.com/golang-jwt/jwt/v5"

// Claims 是 JWT 载荷。Subject 存 userID 的十进制字符串。
// Role 冗余在 token 里只用于快速拒绝，真正的权威状态每请求回查数据库。
type Claims struct {
	jwt.RegisteredClaims
	Role string `json:"role"`
}
```

`server/internal/model/common/types.go`：

```go
package common

// Page 是分页查询的通用参数，供后续业务域复用。
type Page struct {
	Page     int `form:"page,default=1" binding:"min=1"`
	PageSize int `form:"pageSize,default=20" binding:"min=1,max=100"`
}

func (p Page) Offset() int { return (p.Page - 1) * p.PageSize }
func (p Page) Limit() int  { return p.PageSize }
```

- [ ] **Step 5: 写响应体不泄露密码哈希的测试**

`server/internal/model/user/response_test.go`：

```go
package user_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
)

func TestFromEntity_OmitsPasswordHash(t *testing.T) {
	entity := usermodel.User{
		ID:           7,
		Username:     "alice",
		PasswordHash: "$2a$10$SUPERSECRETHASHVALUE",
		DisplayName:  "Alice",
		Role:         usermodel.RoleAdmin,
		Status:       usermodel.StatusActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	raw, err := json.Marshal(usermodel.FromEntity(entity))
	require.NoError(t, err)

	assert.NotContains(t, string(raw), "SUPERSECRETHASH")
	assert.NotContains(t, strings.ToLower(string(raw)), "passwordhash")
	assert.Contains(t, string(raw), `"username":"alice"`)
	assert.Contains(t, string(raw), `"role":"admin"`)
}

func TestListQuery_OffsetAndLimit(t *testing.T) {
	q := usermodel.ListQuery{Page: 3, PageSize: 20}
	assert.Equal(t, 40, q.Offset())
	assert.Equal(t, 20, q.Limit())
}
```

- [ ] **Step 6: 运行测试确认通过**

```bash
cd server && go test ./internal/model/... -v
```
预期：全部 PASS

- [ ] **Step 7: 提交**

```bash
git add server/internal/model && git commit -m "feat(server): model 层按业务域定义请求体、响应体与实体"
```

---

## Task 5: 错误码与密码哈希

**Files:**
- Create: `server/internal/pkg/apperr/apperr.go`, `server/internal/pkg/password/password.go`
- Test: `server/internal/pkg/apperr/apperr_test.go`, `server/internal/pkg/password/password_test.go`

- [ ] **Step 1: 写失败的 apperr 测试**

`server/internal/pkg/apperr/apperr_test.go`：

```go
package apperr_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

func TestSentinels_HaveCodeAndStatus(t *testing.T) {
	assert.Equal(t, "AUTH_INVALID_CREDENTIALS", apperr.ErrInvalidCredentials.Code())
	assert.Equal(t, http.StatusUnauthorized, apperr.ErrInvalidCredentials.HTTPStatus())
	assert.Equal(t, "USER_LAST_ADMIN", apperr.ErrLastAdmin.Code())
	assert.Equal(t, http.StatusUnprocessableEntity, apperr.ErrLastAdmin.HTTPStatus())
}

// Wrap 必须返回副本，否则并发下会互相覆盖 Internal 字段。
func TestWrap_DoesNotMutateSentinel(t *testing.T) {
	cause := errors.New("boom")
	wrapped := apperr.ErrInternal.Wrap(cause)

	assert.Nil(t, apperr.ErrInternal.Internal, "哨兵不得被修改")
	assert.Equal(t, cause, wrapped.Internal())
	assert.Equal(t, apperr.ErrInternal.Code(), wrapped.Code())
	assert.NotSame(t, apperr.ErrInternal, wrapped)
}

func TestAs_ExtractsAppError(t *testing.T) {
	err := error(apperr.ErrUserNotFound.Wrap(errors.New("no rows")))

	var target *apperr.AppError
	require.True(t, errors.As(err, &target))
	assert.Equal(t, "USER_NOT_FOUND", target.Code())
}

func TestUnwrap_ReachesCause(t *testing.T) {
	cause := errors.New("root cause")
	err := apperr.ErrInternal.Wrap(cause)
	assert.True(t, errors.Is(err, cause))
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd server && go test ./internal/pkg/apperr/ -v
```
预期：编译失败，`undefined: apperr.AppError`

- [ ] **Step 3: 实现 apperr**

`server/internal/pkg/apperr/apperr.go`：

```go
// Package apperr 定义带错误码的应用错误。
// service 层返回 *AppError，由 middleware.ErrorHandler 统一转成 HTTP 响应。
// Internal 字段只进日志，绝不出网。
package apperr

import "net/http"

// 字段不导出：哨兵是包级共享指针，导出字段等于允许任意 goroutine
// 改写 apperr.ErrUserNotFound.Code 污染全局。errors.As 只要求指针类型，
// 不要求字段导出，所以访问器不损失任何能力。
type AppError struct {
	code       string
	httpStatus int
	internal   error
}

func (e *AppError) Code() string    { return e.code }
func (e *AppError) HTTPStatus() int { return e.httpStatus }
func (e *AppError) Internal() error { return e.internal }

func (e *AppError) Error() string {
	if e.internal != nil {
		return e.code + ": " + e.internal.Error()
	}
	return e.code
}

func (e *AppError) Unwrap() error { return e.internal }

// Is 让 errors.Is 按错误码而非指针身份匹配，
// 否则 Wrap 出来的副本匹配不上它自己的哨兵——这是个很容易踩的坑。
func (e *AppError) Is(target error) bool {
	t, ok := target.(*AppError)
	return ok && t.code == e.code
}

// Wrap 返回携带底层原因的副本。刻意不修改接收者，
// 否则包级哨兵会被并发请求互相覆盖。
func (e *AppError) Wrap(cause error) *AppError {
	return &AppError{code: e.code, httpStatus: e.httpStatus, internal: cause}
}

func New(code string, status int) *AppError {
	return &AppError{code: code, httpStatus: status}
}

var (
	ErrInvalidCredentials = New("AUTH_INVALID_CREDENTIALS", http.StatusUnauthorized)
	ErrUnauthorized       = New("AUTH_UNAUTHORIZED", http.StatusUnauthorized)
	ErrUserDisabled       = New("AUTH_USER_DISABLED", http.StatusForbidden)
	ErrForbidden          = New("AUTH_FORBIDDEN", http.StatusForbidden)
	ErrWrongOldPassword   = New("AUTH_WRONG_OLD_PASSWORD", http.StatusUnprocessableEntity)

	ErrUserNotFound    = New("USER_NOT_FOUND", http.StatusNotFound)
	ErrUsernameTaken   = New("USER_USERNAME_TAKEN", http.StatusConflict)
	ErrModifySelf      = New("USER_CANNOT_MODIFY_SELF", http.StatusUnprocessableEntity)
	ErrLastAdmin       = New("USER_LAST_ADMIN", http.StatusUnprocessableEntity)
	ErrPasswordTooLong = New("USER_PASSWORD_TOO_LONG", http.StatusUnprocessableEntity)

	ErrValidation = New("VALIDATION_FAILED", http.StatusUnprocessableEntity)
	ErrInternal   = New("INTERNAL_ERROR", http.StatusInternalServerError)
)
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd server && go test ./internal/pkg/apperr/ -v
```
预期：全部 PASS

- [ ] **Step 5: 写失败的 password 测试**

`server/internal/pkg/password/password_test.go`：

```go
package password_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
)

func TestHash_ThenVerify(t *testing.T) {
	hash, err := password.Hash("correct-horse")
	require.NoError(t, err)

	assert.NotEqual(t, "correct-horse", hash, "哈希不得等于明文")
	assert.True(t, password.Verify(hash, "correct-horse"))
	assert.False(t, password.Verify(hash, "wrong-password"))
}

// bcrypt 每次加盐，同一明文两次哈希必然不同。
func TestHash_IsSalted(t *testing.T) {
	a, err := password.Hash("same-input")
	require.NoError(t, err)
	b, err := password.Hash("same-input")
	require.NoError(t, err)

	assert.NotEqual(t, a, b)
	assert.True(t, password.Verify(a, "same-input"))
	assert.True(t, password.Verify(b, "same-input"))
}

// bcrypt 硬上限 72 字节，超长必须报错而非静默截断——
// 静默截断会让 73 字符与 80 字符的密码等价。
func TestHash_RejectsOverLongInput(t *testing.T) {
	_, err := password.Hash(string(make([]byte, 73)))
	require.Error(t, err)
}

func TestVerify_FalseOnGarbageHash(t *testing.T) {
	assert.False(t, password.Verify("not-a-bcrypt-hash", "anything"))
}
```

- [ ] **Step 6: 运行测试确认失败**

```bash
cd server && go test ./internal/pkg/password/ -v
```
预期：编译失败，`undefined: password.Hash`

- [ ] **Step 7: 实现 password**

`server/internal/pkg/password/password.go`：

```go
// Package password 封装 bcrypt 哈希与校验。
package password

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const MaxLength = 72 // bcrypt 硬上限

func Hash(plain string) (string, error) {
	if len(plain) > MaxLength {
		return "", fmt.Errorf("密码长度超过 %d 字节上限", MaxLength)
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("生成密码哈希失败: %w", err)
	}
	return string(b), nil
}

// Verify 在哈希损坏或密码不匹配时统一返回 false，不区分原因。
func Verify(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
```

- [ ] **Step 8: 运行测试确认通过**

```bash
cd server && go test ./internal/pkg/... -v
```
预期：全部 PASS

- [ ] **Step 9: 提交**

```bash
git add server/internal/pkg && git commit -m "feat(server): 错误码体系与 bcrypt 密码哈希"
```

---

## Task 6: JWT 签发与解析

**Files:**
- Create: `server/internal/pkg/jwtx/jwtx.go`
- Test: `server/internal/pkg/jwtx/jwtx_test.go`

- [ ] **Step 1: 写失败的测试**

`server/internal/pkg/jwtx/jwtx_test.go`：

```go
package jwtx_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
)

func newManager(t *testing.T, ttl time.Duration) *jwtx.Manager {
	t.Helper()
	return jwtx.NewManager("unit-test-secret", ttl)
}

func TestGenerateThenParse(t *testing.T) {
	m := newManager(t, time.Hour)

	token, err := m.Generate(42, usermodel.RoleAdmin)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := m.Parse(token)
	require.NoError(t, err)

	assert.Equal(t, "42", claims.Subject)
	assert.Equal(t, usermodel.RoleAdmin, claims.Role)

	uid, err := jwtx.UserID(claims)
	require.NoError(t, err)
	assert.Equal(t, int64(42), uid)
}

func TestParse_RejectsExpiredToken(t *testing.T) {
	m := newManager(t, -time.Minute) // 签发即过期

	token, err := m.Generate(1, usermodel.RoleUser)
	require.NoError(t, err)

	_, err = m.Parse(token)
	require.Error(t, err)
}

func TestParse_RejectsWrongSecret(t *testing.T) {
	issuer := jwtx.NewManager("secret-a", time.Hour)
	verifier := jwtx.NewManager("secret-b", time.Hour)

	token, err := issuer.Generate(1, usermodel.RoleUser)
	require.NoError(t, err)

	_, err = verifier.Parse(token)
	require.Error(t, err)
}

// 防 alg=none 降级攻击：篡改算法头的 token 必须被拒。
func TestParse_RejectsNoneAlgorithm(t *testing.T) {
	m := newManager(t, time.Hour)
	// header {"alg":"none","typ":"JWT"} + payload {"sub":"1"}，无签名
	forged := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiIxIn0."

	_, err := m.Parse(forged)
	require.Error(t, err)
}

func TestParse_RejectsGarbage(t *testing.T) {
	m := newManager(t, time.Hour)
	_, err := m.Parse("obviously-not-a-jwt")
	require.Error(t, err)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd server && go test ./internal/pkg/jwtx/ -v
```
预期：编译失败，`undefined: jwtx.NewManager`

- [ ] **Step 3: 实现 jwtx**

`server/internal/pkg/jwtx/jwtx.go`：

```go
// Package jwtx 封装 JWT 的签发与解析。
// token 只承载身份声明；用户是否仍然有效由中间件每请求回查数据库确认。
package jwtx

import (
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	authmodel "github.com/chenhao/omnigen-ai/server/internal/model/auth"
)

type Manager struct {
	secret []byte
	ttl    time.Duration
}

func NewManager(secret string, ttl time.Duration) *Manager {
	return &Manager{secret: []byte(secret), ttl: ttl}
}

func (m *Manager) Generate(userID int64, role usermodel.Role) (string, error) {
	now := time.Now()
	claims := authmodel.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
			Issuer:    "omnigen-ai",
		},
		Role: role,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("签发 token 失败: %w", err)
	}
	return signed, nil
}

// Parse 校验签名与有效期。显式限定签名算法为 HS256，
// 防止攻击者把算法头改成 none 或非对称算法绕过校验。
func (m *Manager) Parse(token string) (*authmodel.Claims, error) {
	parsed, err := jwt.ParseWithClaims(
		token,
		&authmodel.Claims{},
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("不接受的签名算法: %v", t.Header["alg"])
			}
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil {
		return nil, fmt.Errorf("token 校验失败: %w", err)
	}
	claims, ok := parsed.Claims.(*authmodel.Claims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("token 载荷非法")
	}
	return claims, nil
}

func UserID(c *authmodel.Claims) (int64, error) {
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("token 中的用户 ID 非法: %q", c.Subject)
	}
	return id, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd server && go test ./internal/pkg/jwtx/ -v
```
预期：五条全部 PASS

- [ ] **Step 5: 提交**

```bash
git add server/internal/pkg/jwtx && git commit -m "feat(server): JWT 签发与解析，限定 HS256 防算法降级"
```

---

## Task 7: 用户 repository

**Files:**
- Create: `server/internal/repository/user.go`, `server/internal/repository/testhelper_test.go`
- Test: `server/internal/repository/user_test.go`

- [ ] **Step 1: 写测试辅助（事务回滚隔离）**

`server/internal/repository/testhelper_test.go`：

```go
package repository_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// withTx 在事务中运行用例并在结束时回滚，使用例之间互不污染。
// repository 接受 repository.DB 接口，pgx.Tx 与 pgxpool.Pool 都满足它。
func withTx(t *testing.T, fn func(ctx context.Context, tx repository.DB)) {
	t.Helper()
	ctx := context.Background()

	pool, err := repository.NewPool(ctx, testDSN())
	require.NoError(t, err)
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	fn(ctx, tx)
}

var _ pgx.Tx = nil
```

- [ ] **Step 2: 写失败的 repository 测试**

`server/internal/repository/user_test.go`：

```go
package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

func sampleUser(name string, role usermodel.Role) *usermodel.User {
	return &usermodel.User{
		Username:     name,
		PasswordHash: "$2a$10$fakehashfakehashfakehashfakehashfakehashfakehashfakeha",
		DisplayName:  name,
		Role:         role,
		Status:       usermodel.StatusActive,
	}
}

func TestUserRepo_CreateThenGetByID(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)

		u := sampleUser("alice", usermodel.RoleAdmin)
		require.NoError(t, repo.Create(ctx, u))
		assert.NotZero(t, u.ID, "Create 应回填自增 ID")
		assert.False(t, u.CreatedAt.IsZero(), "Create 应回填 CreatedAt")

		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, "alice", got.Username)
		assert.Equal(t, usermodel.RoleAdmin, got.Role)
		assert.Equal(t, usermodel.StatusActive, got.Status)
	})
}

func TestUserRepo_GetByUsername(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		require.NoError(t, repo.Create(ctx, sampleUser("bob", usermodel.RoleUser)))

		got, err := repo.GetByUsername(ctx, "bob")
		require.NoError(t, err)
		assert.Equal(t, "bob", got.Username)
	})
}

func TestUserRepo_NotFoundReturnsAppError(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)

		_, err := repo.GetByID(ctx, 999999)
		require.Error(t, err)
		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "USER_NOT_FOUND", appErr.Code())

		_, err = repo.GetByUsername(ctx, "ghost")
		require.Error(t, err)
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "USER_NOT_FOUND", appErr.Code())
	})
}

func TestUserRepo_DuplicateUsernameReturnsTaken(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		require.NoError(t, repo.Create(ctx, sampleUser("carol", usermodel.RoleUser)))

		err := repo.Create(ctx, sampleUser("carol", usermodel.RoleUser))
		require.Error(t, err)
		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "USER_USERNAME_TAKEN", appErr.Code())
	})
}

func TestUserRepo_Update(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("dave", usermodel.RoleUser)
		require.NoError(t, repo.Create(ctx, u))

		u.DisplayName = "Dave 改名"
		u.Status = usermodel.StatusDisabled
		u.Role = usermodel.RoleAdmin
		require.NoError(t, repo.Update(ctx, u))

		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, "Dave 改名", got.DisplayName)
		assert.Equal(t, usermodel.StatusDisabled, got.Status)
		assert.Equal(t, usermodel.RoleAdmin, got.Role)
		assert.True(t, got.UpdatedAt.After(got.CreatedAt) || got.UpdatedAt.Equal(got.CreatedAt))
	})
}

func TestUserRepo_Delete(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("erin", usermodel.RoleUser)
		require.NoError(t, repo.Create(ctx, u))

		require.NoError(t, repo.Delete(ctx, u.ID))

		_, err := repo.GetByID(ctx, u.ID)
		require.Error(t, err)

		// 删除不存在的用户应报 USER_NOT_FOUND，而非静默成功
		err = repo.Delete(ctx, u.ID)
		var appErr *apperr.AppError
		require.True(t, errors.As(err, &appErr))
		assert.Equal(t, "USER_NOT_FOUND", appErr.Code())
	})
}

func TestUserRepo_ListPaginates(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		for _, n := range []string{"u1", "u2", "u3", "u4", "u5"} {
			require.NoError(t, repo.Create(ctx, sampleUser(n, usermodel.RoleUser)))
		}

		items, total, err := repo.List(ctx, 0, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total, "total 应为总数而非当页数量")
		assert.Len(t, items, 2)

		page3, total, err := repo.List(ctx, 4, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(5), total)
		assert.Len(t, page3, 1)
	})
}

func TestUserRepo_CountActiveAdmins(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)

		require.NoError(t, repo.Create(ctx, sampleUser("admin1", usermodel.RoleAdmin)))
		require.NoError(t, repo.Create(ctx, sampleUser("admin2", usermodel.RoleAdmin)))
		require.NoError(t, repo.Create(ctx, sampleUser("plain", usermodel.RoleUser)))

		disabled := sampleUser("admin3", usermodel.RoleAdmin)
		disabled.Status = usermodel.StatusDisabled
		require.NoError(t, repo.Create(ctx, disabled))

		n, err := repo.CountActiveAdmins(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n, "被禁用的 admin 不计入")
	})
}

func TestUserRepo_UpdatePasswordHash(t *testing.T) {
	withTx(t, func(ctx context.Context, tx repository.DB) {
		repo := repository.NewUserRepository(tx)
		u := sampleUser("frank", usermodel.RoleUser)
		require.NoError(t, repo.Create(ctx, u))

		newHash := "$2a$10$brandnewhashbrandnewhashbrandnewhashbrandnewhashbrandn"
		require.NoError(t, repo.UpdatePasswordHash(ctx, u.ID, newHash))

		got, err := repo.GetByID(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, newHash, got.PasswordHash)
	})
}
```

- [ ] **Step 3: 运行测试确认失败**

```bash
cd server && go test ./internal/repository/ -run TestUserRepo -v
```
预期：编译失败，`undefined: repository.NewUserRepository`

- [ ] **Step 4: 实现 repository**

`server/internal/repository/user.go`：

```go
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

// DB 抽象 pgxpool.Pool 与 pgx.Tx 的公共部分，
// 使测试可以把整个用例跑在一个会回滚的事务里。
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type UserRepository interface {
	Create(ctx context.Context, u *usermodel.User) error
	GetByID(ctx context.Context, id int64) (*usermodel.User, error)
	GetByUsername(ctx context.Context, username string) (*usermodel.User, error)
	List(ctx context.Context, offset, limit int) ([]usermodel.User, int64, error)
	Update(ctx context.Context, u *usermodel.User) error
	UpdatePasswordHash(ctx context.Context, id int64, hash string) error
	Delete(ctx context.Context, id int64) error
	CountActiveAdmins(ctx context.Context) (int64, error)
}

type userRepository struct{ db DB }

func NewUserRepository(db DB) UserRepository { return &userRepository{db: db} }

const userColumns = `id, username, password_hash, display_name, role, status, created_at, updated_at`

func scanUser(row pgx.Row) (*usermodel.User, error) {
	var u usermodel.User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName,
		&u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.ErrUserNotFound.Wrap(err)
		}
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("扫描用户行失败: %w", err))
	}
	return &u, nil
}

// isUniqueViolation 识别 Postgres 唯一约束冲突（SQLSTATE 23505）。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *userRepository) Create(ctx context.Context, u *usermodel.User) error {
	const q = `
		INSERT INTO users (username, password_hash, display_name, role, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`
	err := r.db.QueryRow(ctx, q, u.Username, u.PasswordHash, u.DisplayName, u.Role, u.Status).
		Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return apperr.ErrUsernameTaken.Wrap(err)
		}
		return apperr.ErrInternal.Wrap(fmt.Errorf("创建用户失败: %w", err))
	}
	return nil
}

func (r *userRepository) GetByID(ctx context.Context, id int64) (*usermodel.User, error) {
	return scanUser(r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

func (r *userRepository) GetByUsername(ctx context.Context, username string) (*usermodel.User, error) {
	return scanUser(r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE username = $1`, username))
}

func (r *userRepository) List(ctx context.Context, offset, limit int) ([]usermodel.User, int64, error) {
	var total int64
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(fmt.Errorf("统计用户数失败: %w", err))
	}

	rows, err := r.db.Query(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY id ASC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(fmt.Errorf("查询用户列表失败: %w", err))
	}
	defer rows.Close()

	items := make([]usermodel.User, 0, limit)
	for rows.Next() {
		var u usermodel.User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName,
			&u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, 0, apperr.ErrInternal.Wrap(fmt.Errorf("扫描用户列表失败: %w", err))
		}
		items = append(items, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(err)
	}
	return items, total, nil
}

func (r *userRepository) Update(ctx context.Context, u *usermodel.User) error {
	const q = `
		UPDATE users
		SET display_name = $1, role = $2, status = $3, updated_at = now()
		WHERE id = $4
		RETURNING updated_at`
	err := r.db.QueryRow(ctx, q, u.DisplayName, u.Role, u.Status, u.ID).Scan(&u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.ErrUserNotFound.Wrap(err)
		}
		return apperr.ErrInternal.Wrap(fmt.Errorf("更新用户失败: %w", err))
	}
	return nil
}

func (r *userRepository) UpdatePasswordHash(ctx context.Context, id int64, hash string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2`, hash, id)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("更新密码失败: %w", err))
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrUserNotFound
	}
	return nil
}

func (r *userRepository) Delete(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return apperr.ErrInternal.Wrap(fmt.Errorf("删除用户失败: %w", err))
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrUserNotFound
	}
	return nil
}

func (r *userRepository) CountActiveAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE role = 'admin' AND status = 'active'`).Scan(&n)
	if err != nil {
		return 0, apperr.ErrInternal.Wrap(fmt.Errorf("统计活跃管理员失败: %w", err))
	}
	return n, nil
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
cd server && make migrate-test-up && go test ./internal/repository/ -v
```
预期：全部 PASS

- [ ] **Step 6: 提交**

```bash
git add server/internal/repository && git commit -m "feat(server): 用户 repository，唯一冲突与未找到映射为错误码"
```

---

## Task 8: 认证 service

**Files:**
- Create: `server/internal/service/auth.go`, `server/internal/service/mock_test.go`
- Test: `server/internal/service/auth_test.go`

- [ ] **Step 1: 写假 repository**

`server/internal/service/mock_test.go`：

```go
package service_test

import (
	"context"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

// fakeUserRepo 是内存版 UserRepository，让 service 测试无需启动数据库。
type fakeUserRepo struct {
	users  map[int64]*usermodel.User
	nextID int64
}

func newFakeRepo() *fakeUserRepo {
	return &fakeUserRepo{users: map[int64]*usermodel.User{}, nextID: 1}
}

func (f *fakeUserRepo) add(u *usermodel.User) *usermodel.User {
	u.ID = f.nextID
	f.nextID++
	f.users[u.ID] = u
	return u
}

func (f *fakeUserRepo) Create(_ context.Context, u *usermodel.User) error {
	for _, existing := range f.users {
		if existing.Username == u.Username {
			return apperr.ErrUsernameTaken
		}
	}
	f.add(u)
	return nil
}

func (f *fakeUserRepo) GetByID(_ context.Context, id int64) (*usermodel.User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, apperr.ErrUserNotFound
	}
	clone := *u
	return &clone, nil
}

func (f *fakeUserRepo) GetByUsername(_ context.Context, username string) (*usermodel.User, error) {
	for _, u := range f.users {
		if u.Username == username {
			clone := *u
			return &clone, nil
		}
	}
	return nil, apperr.ErrUserNotFound
}

func (f *fakeUserRepo) List(_ context.Context, offset, limit int) ([]usermodel.User, int64, error) {
	all := make([]usermodel.User, 0, len(f.users))
	for id := int64(1); id < f.nextID; id++ {
		if u, ok := f.users[id]; ok {
			all = append(all, *u)
		}
	}
	total := int64(len(all))
	if offset >= len(all) {
		return []usermodel.User{}, total, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], total, nil
}

func (f *fakeUserRepo) Update(_ context.Context, u *usermodel.User) error {
	if _, ok := f.users[u.ID]; !ok {
		return apperr.ErrUserNotFound
	}
	clone := *u
	f.users[u.ID] = &clone
	return nil
}

func (f *fakeUserRepo) UpdatePasswordHash(_ context.Context, id int64, hash string) error {
	u, ok := f.users[id]
	if !ok {
		return apperr.ErrUserNotFound
	}
	u.PasswordHash = hash
	return nil
}

func (f *fakeUserRepo) Delete(_ context.Context, id int64) error {
	if _, ok := f.users[id]; !ok {
		return apperr.ErrUserNotFound
	}
	delete(f.users, id)
	return nil
}

func (f *fakeUserRepo) CountActiveAdmins(_ context.Context) (int64, error) {
	var n int64
	for _, u := range f.users {
		if u.Role == usermodel.RoleAdmin && u.Status == usermodel.StatusActive {
			n++
		}
	}
	return n, nil
}
```

- [ ] **Step 2: 写失败的认证 service 测试**

`server/internal/service/auth_test.go`：

```go
package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmodel "github.com/chenhao/omnigen-ai/server/internal/model/auth"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

func newAuthService(t *testing.T) (*service.AuthService, *fakeUserRepo) {
	t.Helper()
	repo := newFakeRepo()
	return service.NewAuthService(repo, jwtx.NewManager("test-secret", time.Hour)), repo
}

func seedUser(t *testing.T, repo *fakeUserRepo, name, plain string, role usermodel.Role, status usermodel.Status) *usermodel.User {
	t.Helper()
	hash, err := password.Hash(plain)
	require.NoError(t, err)
	return repo.add(&usermodel.User{
		Username: name, PasswordHash: hash, DisplayName: name,
		Role: role, Status: status,
	})
}

func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr), "期望 *apperr.AppError，实际 %T", err)
	assert.Equal(t, code, appErr.Code())
}

func TestLogin_Succeeds(t *testing.T) {
	svc, repo := newAuthService(t)
	seedUser(t, repo, "alice", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	resp, err := svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "alice", Password: "password123",
	})
	require.NoError(t, err)

	assert.NotEmpty(t, resp.Token)
	assert.Equal(t, "alice", resp.User.Username)
	assert.Equal(t, usermodel.RoleAdmin, resp.User.Role)
}

func TestLogin_WrongPassword(t *testing.T) {
	svc, repo := newAuthService(t)
	seedUser(t, repo, "alice", "password123", usermodel.RoleUser, usermodel.StatusActive)

	_, err := svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "alice", Password: "wrong",
	})
	assertCode(t, err, "AUTH_INVALID_CREDENTIALS")
}

// 用户不存在与密码错误必须返回同一个错误码，
// 否则接口就成了用户名枚举器。
func TestLogin_UnknownUserSameCodeAsWrongPassword(t *testing.T) {
	svc, _ := newAuthService(t)

	_, err := svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "nobody", Password: "whatever",
	})
	assertCode(t, err, "AUTH_INVALID_CREDENTIALS")
}

func TestLogin_DisabledUserRejected(t *testing.T) {
	svc, repo := newAuthService(t)
	seedUser(t, repo, "banned", "password123", usermodel.RoleUser, usermodel.StatusDisabled)

	_, err := svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "banned", Password: "password123",
	})
	assertCode(t, err, "AUTH_USER_DISABLED")
}

func TestChangePassword_Succeeds(t *testing.T) {
	svc, repo := newAuthService(t)
	u := seedUser(t, repo, "alice", "oldpassword", usermodel.RoleUser, usermodel.StatusActive)

	require.NoError(t, svc.ChangePassword(context.Background(), u.ID, authmodel.ChangePasswordRequest{
		OldPassword: "oldpassword", NewPassword: "newpassword1",
	}))

	_, err := svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "alice", Password: "newpassword1",
	})
	require.NoError(t, err, "新密码应可登录")

	_, err = svc.Login(context.Background(), authmodel.LoginRequest{
		Username: "alice", Password: "oldpassword",
	})
	assertCode(t, err, "AUTH_INVALID_CREDENTIALS")
}

func TestChangePassword_WrongOldPassword(t *testing.T) {
	svc, repo := newAuthService(t)
	u := seedUser(t, repo, "alice", "oldpassword", usermodel.RoleUser, usermodel.StatusActive)

	err := svc.ChangePassword(context.Background(), u.ID, authmodel.ChangePasswordRequest{
		OldPassword: "not-the-old-one", NewPassword: "newpassword1",
	})
	assertCode(t, err, "AUTH_WRONG_OLD_PASSWORD")
}

func TestGetCurrentUser(t *testing.T) {
	svc, repo := newAuthService(t)
	u := seedUser(t, repo, "alice", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	got, err := svc.GetCurrentUser(context.Background(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "alice", got.Username)
	assert.Equal(t, usermodel.RoleAdmin, got.Role)
}

func TestGetCurrentUser_NotFound(t *testing.T) {
	svc, _ := newAuthService(t)
	_, err := svc.GetCurrentUser(context.Background(), 12345)
	assertCode(t, err, "USER_NOT_FOUND")
}
```

- [ ] **Step 3: 运行测试确认失败**

```bash
cd server && go test ./internal/service/ -v
```
预期：编译失败，`undefined: service.NewAuthService`

- [ ] **Step 4: 实现认证 service**

`server/internal/service/auth.go`：

```go
package service

import (
	"context"
	"errors"

	authmodel "github.com/chenhao/omnigen-ai/server/internal/model/auth"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// mapHashError 把 password.Hash 的失败翻成对客户端有意义的错误码。
// bcrypt 的 72 字节上限按字节算，而 gin binding 的 max=72 按 rune 算，
// 24 个以上中文字符会通过前端与绑定校验却在这里失败——
// 那是用户输入问题，必须是 422 而不是 500。
func mapHashError(err error) *apperr.AppError {
	if errors.Is(err, password.ErrTooLong) {
		return apperr.ErrPasswordTooLong.Wrap(err)
	}
	return apperr.ErrInternal.Wrap(err)
}

type AuthService struct {
	users repository.UserRepository
	jwt   *jwtx.Manager
}

func NewAuthService(users repository.UserRepository, jwt *jwtx.Manager) *AuthService {
	return &AuthService{users: users, jwt: jwt}
}

// Login 校验凭据并签发 token。
// 用户不存在与密码错误返回同一错误码，避免接口沦为用户名枚举器。
func (s *AuthService) Login(ctx context.Context, req authmodel.LoginRequest) (*authmodel.LoginResponse, error) {
	u, err := s.users.GetByUsername(ctx, req.Username)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(err, &appErr) && appErr.Code() == apperr.ErrUserNotFound.Code() {
			return nil, apperr.ErrInvalidCredentials
		}
		return nil, err
	}

	if !password.Verify(u.PasswordHash, req.Password) {
		return nil, apperr.ErrInvalidCredentials
	}
	if !u.IsActive() {
		return nil, apperr.ErrUserDisabled
	}

	token, err := s.jwt.Generate(u.ID, u.Role)
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return &authmodel.LoginResponse{Token: token, User: usermodel.FromEntity(*u)}, nil
}

func (s *AuthService) ChangePassword(ctx context.Context, userID int64, req authmodel.ChangePasswordRequest) error {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if !password.Verify(u.PasswordHash, req.OldPassword) {
		return apperr.ErrWrongOldPassword
	}
	hash, err := password.Hash(req.NewPassword)
	if err != nil {
		return mapHashError(err)
	}
	return s.users.UpdatePasswordHash(ctx, userID, hash)
}

func (s *AuthService) GetCurrentUser(ctx context.Context, userID int64) (*usermodel.UserResponse, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	resp := usermodel.FromEntity(*u)
	return &resp, nil
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
cd server && go test ./internal/service/ -v
```
预期：全部 PASS

- [ ] **Step 6: 提交**

```bash
git add server/internal/service && git commit -m "feat(server): 认证 service，登录失败不区分用户名与密码错误"
```

---

## Task 9: 用户管理 service 与业务规则

**Files:**
- Create: `server/internal/service/user.go`
- Test: `server/internal/service/user_test.go`

- [ ] **Step 1: 写失败的测试**

`server/internal/service/user_test.go`：

```go
package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

func newUserService(t *testing.T) (*service.UserService, *fakeUserRepo) {
	t.Helper()
	repo := newFakeRepo()
	return service.NewUserService(repo), repo
}

func ptr[T any](v T) *T { return &v }

func TestUserService_Create(t *testing.T) {
	svc, _ := newUserService(t)

	got, err := svc.Create(context.Background(), usermodel.CreateRequest{
		Username: "newbie", Password: "password123",
		DisplayName: "新人", Role: usermodel.RoleUser,
	})
	require.NoError(t, err)

	assert.Equal(t, "newbie", got.Username)
	assert.Equal(t, usermodel.RoleUser, got.Role)
	assert.Equal(t, usermodel.StatusActive, got.Status, "新建用户默认 active")
}

func TestUserService_Create_DuplicateUsername(t *testing.T) {
	svc, repo := newUserService(t)
	seedUser(t, repo, "taken", "password123", usermodel.RoleUser, usermodel.StatusActive)

	_, err := svc.Create(context.Background(), usermodel.CreateRequest{
		Username: "taken", Password: "password123", Role: usermodel.RoleUser,
	})
	assertCode(t, err, "USER_USERNAME_TAKEN")
}

func TestUserService_Create_HashesPassword(t *testing.T) {
	svc, repo := newUserService(t)

	_, err := svc.Create(context.Background(), usermodel.CreateRequest{
		Username: "hashme", Password: "password123", Role: usermodel.RoleUser,
	})
	require.NoError(t, err)

	stored, err := repo.GetByUsername(context.Background(), "hashme")
	require.NoError(t, err)
	assert.NotEqual(t, "password123", stored.PasswordHash)
	assert.True(t, password.Verify(stored.PasswordHash, "password123"))
}

func TestUserService_List(t *testing.T) {
	svc, repo := newUserService(t)
	for _, n := range []string{"a", "b", "c"} {
		seedUser(t, repo, n, "password123", usermodel.RoleUser, usermodel.StatusActive)
	}

	got, err := svc.List(context.Background(), usermodel.ListQuery{Page: 1, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, int64(3), got.Total)
	assert.Len(t, got.Items, 2)
}

func TestUserService_Update(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	other := seedUser(t, repo, "victim", "password123", usermodel.RoleUser, usermodel.StatusActive)
	seedUser(t, repo, "admin2", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	got, err := svc.Update(context.Background(), admin.ID, other.ID, usermodel.UpdateRequest{
		DisplayName: ptr("改过的名字"),
		Status:      ptr(usermodel.StatusDisabled),
	})
	require.NoError(t, err)
	assert.Equal(t, "改过的名字", got.DisplayName)
	assert.Equal(t, usermodel.StatusDisabled, got.Status)
}

func TestUserService_Update_CannotModifySelf(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	seedUser(t, repo, "admin2", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	_, err := svc.Update(context.Background(), admin.ID, admin.ID, usermodel.UpdateRequest{
		Status: ptr(usermodel.StatusDisabled),
	})
	assertCode(t, err, "USER_CANNOT_MODIFY_SELF")

	_, err = svc.Update(context.Background(), admin.ID, admin.ID, usermodel.UpdateRequest{
		Role: ptr(usermodel.RoleUser),
	})
	assertCode(t, err, "USER_CANNOT_MODIFY_SELF")
}

// 只改自己的显示名是允许的，不属于危险自我操作。
func TestUserService_Update_SelfDisplayNameAllowed(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	got, err := svc.Update(context.Background(), admin.ID, admin.ID, usermodel.UpdateRequest{
		DisplayName: ptr("我的新昵称"),
	})
	require.NoError(t, err)
	assert.Equal(t, "我的新昵称", got.DisplayName)
}

func TestUserService_Update_CannotDemoteLastAdmin(t *testing.T) {
	svc, repo := newUserService(t)
	actor := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	// 手动制造「另一个 admin 是唯一活跃 admin」的场景：把 actor 设为禁用之外的场景不现实，
	// 因此直接用两个 admin 中删掉一个的方式验证。
	lone := seedUser(t, repo, "lonely-admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	require.NoError(t, repo.Delete(context.Background(), actor.ID))

	_, err := svc.Update(context.Background(), 9999, lone.ID, usermodel.UpdateRequest{
		Role: ptr(usermodel.RoleUser),
	})
	assertCode(t, err, "USER_LAST_ADMIN")

	_, err = svc.Update(context.Background(), 9999, lone.ID, usermodel.UpdateRequest{
		Status: ptr(usermodel.StatusDisabled),
	})
	assertCode(t, err, "USER_LAST_ADMIN")
}

func TestUserService_Delete(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	target := seedUser(t, repo, "target", "password123", usermodel.RoleUser, usermodel.StatusActive)

	require.NoError(t, svc.Delete(context.Background(), admin.ID, target.ID))

	_, err := repo.GetByID(context.Background(), target.ID)
	assertCode(t, err, "USER_NOT_FOUND")
}

func TestUserService_Delete_CannotDeleteSelf(t *testing.T) {
	svc, repo := newUserService(t)
	admin := seedUser(t, repo, "admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)
	seedUser(t, repo, "admin2", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	err := svc.Delete(context.Background(), admin.ID, admin.ID)
	assertCode(t, err, "USER_CANNOT_MODIFY_SELF")
}

func TestUserService_Delete_CannotDeleteLastAdmin(t *testing.T) {
	svc, repo := newUserService(t)
	lone := seedUser(t, repo, "lonely-admin", "password123", usermodel.RoleAdmin, usermodel.StatusActive)

	err := svc.Delete(context.Background(), 9999, lone.ID)
	assertCode(t, err, "USER_LAST_ADMIN")
}

func TestUserService_ResetPassword(t *testing.T) {
	svc, repo := newUserService(t)
	target := seedUser(t, repo, "target", "oldpassword", usermodel.RoleUser, usermodel.StatusActive)

	require.NoError(t, svc.ResetPassword(context.Background(), target.ID,
		usermodel.ResetPasswordRequest{Password: "resetpassword"}))

	stored, err := repo.GetByID(context.Background(), target.ID)
	require.NoError(t, err)
	assert.True(t, password.Verify(stored.PasswordHash, "resetpassword"))
	assert.False(t, password.Verify(stored.PasswordHash, "oldpassword"))
}

func TestUserService_EnsureBootstrapAdmin_CreatesWhenNone(t *testing.T) {
	svc, repo := newUserService(t)

	require.NoError(t, svc.EnsureBootstrapAdmin(context.Background(), "root", "rootpassword"))

	created, err := repo.GetByUsername(context.Background(), "root")
	require.NoError(t, err)
	assert.Equal(t, usermodel.RoleAdmin, created.Role)
	assert.True(t, password.Verify(created.PasswordHash, "rootpassword"))
}

// 已存在 admin 时不得覆盖——否则每次重启都会重置密码。
func TestUserService_EnsureBootstrapAdmin_SkipsWhenAdminExists(t *testing.T) {
	svc, repo := newUserService(t)
	existing := seedUser(t, repo, "admin", "originalpass", usermodel.RoleAdmin, usermodel.StatusActive)

	require.NoError(t, svc.EnsureBootstrapAdmin(context.Background(), "root", "rootpassword"))

	_, err := repo.GetByUsername(context.Background(), "root")
	assertCode(t, err, "USER_NOT_FOUND")

	unchanged, err := repo.GetByID(context.Background(), existing.ID)
	require.NoError(t, err)
	assert.True(t, password.Verify(unchanged.PasswordHash, "originalpass"))
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd server && go test ./internal/service/ -run TestUserService -v
```
预期：编译失败，`undefined: service.NewUserService`

- [ ] **Step 3: 实现用户 service**

`server/internal/service/user.go`：

```go
package service

import (
	"context"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

type UserService struct {
	users repository.UserRepository
}

func NewUserService(users repository.UserRepository) *UserService {
	return &UserService{users: users}
}

func (s *UserService) Create(ctx context.Context, req usermodel.CreateRequest) (*usermodel.UserResponse, error) {
	hash, err := password.Hash(req.Password)
	if err != nil {
		return nil, mapHashError(err)
	}
	u := &usermodel.User{
		Username:     req.Username,
		PasswordHash: hash,
		DisplayName:  req.DisplayName,
		Role:         req.Role,
		Status:       usermodel.StatusActive,
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, err
	}
	resp := usermodel.FromEntity(*u)
	return &resp, nil
}

func (s *UserService) List(ctx context.Context, q usermodel.ListQuery) (*usermodel.UserListResponse, error) {
	items, total, err := s.users.List(ctx, q.Offset(), q.Limit())
	if err != nil {
		return nil, err
	}
	return &usermodel.UserListResponse{Total: total, Items: usermodel.FromEntities(items)}, nil
}

// Update 修改目标用户。两条护栏：
//   - 不允许对自己做降级或禁用（改自己的显示名可以）
//   - 不允许把系统里最后一个活跃 admin 降级或禁用
func (s *UserService) Update(ctx context.Context, actorID, targetID int64, req usermodel.UpdateRequest) (*usermodel.UserResponse, error) {
	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return nil, err
	}

	demoting := req.Role != nil && *req.Role != usermodel.RoleAdmin && target.IsAdmin()
	disabling := req.Status != nil && *req.Status == usermodel.StatusDisabled && target.IsActive()

	if actorID == targetID && (demoting || disabling) {
		return nil, apperr.ErrModifySelf
	}
	if demoting || disabling {
		if err := s.ensureNotLastAdmin(ctx, *target); err != nil {
			return nil, err
		}
	}

	if req.DisplayName != nil {
		target.DisplayName = *req.DisplayName
	}
	if req.Role != nil {
		target.Role = *req.Role
	}
	if req.Status != nil {
		target.Status = *req.Status
	}

	if err := s.users.Update(ctx, target); err != nil {
		return nil, err
	}
	resp := usermodel.FromEntity(*target)
	return &resp, nil
}

func (s *UserService) ResetPassword(ctx context.Context, targetID int64, req usermodel.ResetPasswordRequest) error {
	hash, err := password.Hash(req.Password)
	if err != nil {
		return mapHashError(err)
	}
	return s.users.UpdatePasswordHash(ctx, targetID, hash)
}

func (s *UserService) Delete(ctx context.Context, actorID, targetID int64) error {
	if actorID == targetID {
		return apperr.ErrModifySelf
	}
	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return err
	}
	if err := s.ensureNotLastAdmin(ctx, *target); err != nil {
		return err
	}
	return s.users.Delete(ctx, targetID)
}

// ensureNotLastAdmin 在目标是活跃 admin 且系统中仅剩这一个时拒绝操作，
// 避免把自己彻底锁在系统外面。
func (s *UserService) ensureNotLastAdmin(ctx context.Context, target usermodel.User) error {
	if !target.IsAdmin() || !target.IsActive() {
		return nil
	}
	n, err := s.users.CountActiveAdmins(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return apperr.ErrLastAdmin
	}
	return nil
}

// EnsureBootstrapAdmin 在系统中没有任何活跃 admin 时创建首个管理员。
// 已存在则跳过，绝不覆盖——否则每次重启都会重置密码。
func (s *UserService) EnsureBootstrapAdmin(ctx context.Context, username, plainPassword string) error {
	n, err := s.users.CountActiveAdmins(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = s.Create(ctx, usermodel.CreateRequest{
		Username:    username,
		Password:    plainPassword,
		DisplayName: username,
		Role:        usermodel.RoleAdmin,
	})
	return err
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd server && go test ./internal/service/ -v
```
预期：全部 PASS

- [ ] **Step 5: 提交**

```bash
git add server/internal/service && git commit -m "feat(server): 用户管理 service 与最后一个 admin 保护规则"
```

---

## Task 10: 中间件

**Files:**
- Create: `server/internal/middleware/error.go`, `server/internal/middleware/auth.go`, `server/internal/middleware/context.go`
- Test: `server/internal/middleware/auth_test.go`

- [ ] **Step 1: 写失败的测试**

`server/internal/middleware/auth_test.go`：

```go
package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
)

// stubLoader 替代 repository，仅用于中间件测试。
type stubLoader struct{ users map[int64]*usermodel.User }

func (s stubLoader) GetByID(_ context.Context, id int64) (*usermodel.User, error) {
	u, ok := s.users[id]
	if !ok {
		return nil, apperr.ErrUserNotFound
	}
	return u, nil
}

func setup(t *testing.T, users map[int64]*usermodel.User) (*gin.Engine, *jwtx.Manager) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	jwtMgr := jwtx.NewManager("mw-test-secret", time.Hour)

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	auth := r.Group("/", middleware.Auth(jwtMgr, stubLoader{users: users}))
	auth.GET("/whoami", func(c *gin.Context) {
		id, _ := middleware.UserIDFrom(c)
		c.JSON(http.StatusOK, common.OK(gin.H{"id": id}))
	})
	auth.GET("/admin-only", middleware.RequireAdmin(), func(c *gin.Context) {
		c.JSON(http.StatusOK, common.OK(nil))
	})
	return r, jwtMgr
}

func do(r *gin.Engine, path, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w
}

func codeOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Code
}

func activeUser(id int64, role usermodel.Role) *usermodel.User {
	return &usermodel.User{ID: id, Username: "u", Role: role, Status: usermodel.StatusActive}
}

func TestAuth_NoTokenReturns401(t *testing.T) {
	r, _ := setup(t, map[int64]*usermodel.User{})
	w := do(r, "/whoami", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "AUTH_UNAUTHORIZED", codeOf(t, w))
}

func TestAuth_GarbageTokenReturns401(t *testing.T) {
	r, _ := setup(t, map[int64]*usermodel.User{})
	w := do(r, "/whoami", "not-a-real-token")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuth_ValidTokenPasses(t *testing.T) {
	users := map[int64]*usermodel.User{7: activeUser(7, usermodel.RoleUser)}
	r, jwtMgr := setup(t, users)

	token, err := jwtMgr.Generate(7, usermodel.RoleUser)
	require.NoError(t, err)

	w := do(r, "/whoami", token)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"id":7`)
}

// 这是「乙方案」的核心断言：token 仍然有效，但用户已被禁用，
// 必须立即拒绝，而不是等 token 过期。
func TestAuth_DisabledUserRejectedImmediately(t *testing.T) {
	u := activeUser(7, usermodel.RoleUser)
	users := map[int64]*usermodel.User{7: u}
	r, jwtMgr := setup(t, users)

	token, err := jwtMgr.Generate(7, usermodel.RoleUser)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, do(r, "/whoami", token).Code)

	u.Status = usermodel.StatusDisabled // admin 在别处禁用了该用户

	w := do(r, "/whoami", token)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "AUTH_USER_DISABLED", codeOf(t, w))
}

// 用户被删除后，其 token 也必须立即失效。
func TestAuth_DeletedUserRejected(t *testing.T) {
	users := map[int64]*usermodel.User{7: activeUser(7, usermodel.RoleUser)}
	r, jwtMgr := setup(t, users)

	token, err := jwtMgr.Generate(7, usermodel.RoleUser)
	require.NoError(t, err)
	delete(users, 7)

	w := do(r, "/whoami", token)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireAdmin_RejectsPlainUser(t *testing.T) {
	users := map[int64]*usermodel.User{7: activeUser(7, usermodel.RoleUser)}
	r, jwtMgr := setup(t, users)

	token, err := jwtMgr.Generate(7, usermodel.RoleUser)
	require.NoError(t, err)

	w := do(r, "/admin-only", token)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "AUTH_FORBIDDEN", codeOf(t, w))
}

// 角色以数据库为准，不信 token 里的 role 声明：
// token 说自己是 admin，但库里是 user，必须拒绝。
func TestRequireAdmin_TrustsDatabaseNotToken(t *testing.T) {
	users := map[int64]*usermodel.User{7: activeUser(7, usermodel.RoleUser)}
	r, jwtMgr := setup(t, users)

	forged, err := jwtMgr.Generate(7, usermodel.RoleAdmin)
	require.NoError(t, err)

	w := do(r, "/admin-only", forged)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireAdmin_AllowsAdmin(t *testing.T) {
	users := map[int64]*usermodel.User{9: activeUser(9, usermodel.RoleAdmin)}
	r, jwtMgr := setup(t, users)

	token, err := jwtMgr.Generate(9, usermodel.RoleAdmin)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, do(r, "/admin-only", token).Code)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd server && go test ./internal/middleware/ -v
```
预期：编译失败，`undefined: middleware.ErrorHandler`

- [ ] **Step 3: 实现上下文键**

`server/internal/middleware/context.go`：

```go
package middleware

import (
	"github.com/gin-gonic/gin"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
)

const (
	ctxUserID   = "omnigen.userID"
	ctxUserRole = "omnigen.userRole"
)

func UserIDFrom(c *gin.Context) (int64, bool) {
	v, ok := c.Get(ctxUserID)
	if !ok {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}

func RoleFrom(c *gin.Context) (usermodel.Role, bool) {
	v, ok := c.Get(ctxUserRole)
	if !ok {
		return "", false
	}
	role, ok := v.(usermodel.Role)
	return role, ok
}
```

- [ ] **Step 4: 实现错误处理中间件**

`server/internal/middleware/error.go`：

```go
package middleware

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

// Fail 让 handler 以统一方式上报错误，由 ErrorHandler 收口渲染。
func Fail(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()
}

// ErrorHandler 把 handler 上报的错误转成统一响应。
// AppError.Internal 只写日志，绝不进响应体。
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}
		err := c.Errors.Last().Err

		var appErr *apperr.AppError
		if !errors.As(err, &appErr) {
			appErr = apperr.ErrInternal.Wrap(err)
		}

		if appErr.HTTPStatus() >= http.StatusInternalServerError {
			slog.Error("请求处理失败",
				"code", appErr.Code(), "path", c.Request.URL.Path,
				"method", c.Request.Method, "internal", appErr.Internal())
		} else {
			slog.Info("请求被拒绝",
				"code", appErr.Code(), "path", c.Request.URL.Path, "method", c.Request.Method)
		}

		c.JSON(appErr.HTTPStatus(), common.Response{Code: appErr.Code()})
	}
}

// Recovery 兜住 panic，避免进程崩溃并保持响应格式统一。
func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		slog.Error("panic 恢复", "path", c.Request.URL.Path, "recovered", recovered)
		c.AbortWithStatusJSON(http.StatusInternalServerError,
			common.Response{Code: apperr.ErrInternal.Code()})
	})
}
```

- [ ] **Step 5: 实现认证中间件**

`server/internal/middleware/auth.go`：

```go
package middleware

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
)

// UserLoader 是中间件对 repository 的最小依赖面。
type UserLoader interface {
	GetByID(ctx context.Context, id int64) (*usermodel.User, error)
}

// Auth 校验 JWT 签名，随后按 ID 回查数据库确认用户仍存在且处于 active。
// 这次查询是刻意为之：它让禁用、删除、改密立即生效，
// 代价是每请求一次主键查询——在本系统的量级下可忽略。
func Auth(jwtMgr *jwtx.Manager, users UserLoader) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		if raw == "" || !strings.HasPrefix(raw, "Bearer ") {
			Fail(c, apperr.ErrUnauthorized)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
		if token == "" {
			Fail(c, apperr.ErrUnauthorized)
			return
		}

		claims, err := jwtMgr.Parse(token)
		if err != nil {
			Fail(c, apperr.ErrUnauthorized.Wrap(err))
			return
		}
		userID, err := jwtx.UserID(claims)
		if err != nil {
			Fail(c, apperr.ErrUnauthorized.Wrap(err))
			return
		}

		u, err := users.GetByID(c.Request.Context(), userID)
		if err != nil {
			// 用户已被删除：token 仍有签名有效性，但身份已不存在。
			Fail(c, apperr.ErrUnauthorized.Wrap(err))
			return
		}
		if !u.IsActive() {
			Fail(c, apperr.ErrUserDisabled)
			return
		}

		c.Set(ctxUserID, u.ID)
		c.Set(ctxUserRole, u.Role) // 以数据库为准，不采信 token 里的 role
		c.Next()
	}
}

// RequireAdmin 必须挂在 Auth 之后。
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := RoleFrom(c)
		if !ok || role != usermodel.RoleAdmin {
			Fail(c, apperr.ErrForbidden)
			return
		}
		c.Next()
	}
}
```

- [ ] **Step 6: 运行测试确认通过**

```bash
cd server && go test ./internal/middleware/ -v
```
预期：八条全部 PASS，其中 `TestAuth_DisabledUserRejectedImmediately` 与 `TestRequireAdmin_TrustsDatabaseNotToken` 是关键断言

- [ ] **Step 7: 提交**

```bash
git add server/internal/middleware && git commit -m "feat(server): 认证与角色中间件，每请求回查用户状态使禁用立即生效"
```

---

## Task 11: HTTP handler

**Files:**
- Create: `server/internal/handler/auth.go`, `server/internal/handler/user.go`
- Test: `server/internal/handler/auth_test.go`

- [ ] **Step 1: 写失败的测试**

`server/internal/handler/auth_test.go`：

```go
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// memRepo 是 handler 测试用的内存 repository。
type memRepo struct {
	users  map[int64]*usermodel.User
	nextID int64
}

func newMemRepo() *memRepo { return &memRepo{users: map[int64]*usermodel.User{}, nextID: 1} }

func (m *memRepo) Create(_ context.Context, u *usermodel.User) error {
	for _, e := range m.users {
		if e.Username == u.Username {
			return apperr.ErrUsernameTaken
		}
	}
	u.ID = m.nextID
	m.nextID++
	u.CreatedAt = time.Now()
	u.UpdatedAt = u.CreatedAt
	m.users[u.ID] = u
	return nil
}
func (m *memRepo) GetByID(_ context.Context, id int64) (*usermodel.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, apperr.ErrUserNotFound
	}
	c := *u
	return &c, nil
}
func (m *memRepo) GetByUsername(_ context.Context, name string) (*usermodel.User, error) {
	for _, u := range m.users {
		if u.Username == name {
			c := *u
			return &c, nil
		}
	}
	return nil, apperr.ErrUserNotFound
}
func (m *memRepo) List(_ context.Context, offset, limit int) ([]usermodel.User, int64, error) {
	all := []usermodel.User{}
	for id := int64(1); id < m.nextID; id++ {
		if u, ok := m.users[id]; ok {
			all = append(all, *u)
		}
	}
	total := int64(len(all))
	if offset >= len(all) {
		return []usermodel.User{}, total, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], total, nil
}
func (m *memRepo) Update(_ context.Context, u *usermodel.User) error {
	if _, ok := m.users[u.ID]; !ok {
		return apperr.ErrUserNotFound
	}
	c := *u
	m.users[u.ID] = &c
	return nil
}
func (m *memRepo) UpdatePasswordHash(_ context.Context, id int64, hash string) error {
	u, ok := m.users[id]
	if !ok {
		return apperr.ErrUserNotFound
	}
	u.PasswordHash = hash
	return nil
}
func (m *memRepo) Delete(_ context.Context, id int64) error {
	if _, ok := m.users[id]; !ok {
		return apperr.ErrUserNotFound
	}
	delete(m.users, id)
	return nil
}
func (m *memRepo) CountActiveAdmins(_ context.Context) (int64, error) {
	var n int64
	for _, u := range m.users {
		if u.Role == usermodel.RoleAdmin && u.Status == usermodel.StatusActive {
			n++
		}
	}
	return n, nil
}

var _ repository.UserRepository = (*memRepo)(nil)

type testEnv struct {
	r      *gin.Engine
	repo   *memRepo
	jwtMgr *jwtx.Manager
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := newMemRepo()
	jwtMgr := jwtx.NewManager("handler-test-secret", time.Hour)
	authSvc := service.NewAuthService(repo, jwtMgr)
	userSvc := service.NewUserService(repo)

	authH := handler.NewAuthHandler(authSvc)
	userH := handler.NewUserHandler(userSvc)

	r := gin.New()
	r.Use(middleware.Recovery(), middleware.ErrorHandler())

	api := r.Group("/api")
	api.POST("/auth/login", authH.Login)

	authed := api.Group("", middleware.Auth(jwtMgr, repo))
	authed.GET("/auth/me", authH.Me)
	authed.POST("/auth/logout", authH.Logout)
	authed.PUT("/auth/password", authH.ChangePassword)

	admin := authed.Group("", middleware.RequireAdmin())
	admin.GET("/users", userH.List)
	admin.POST("/users", userH.Create)
	admin.PUT("/users/:id", userH.Update)
	admin.PUT("/users/:id/password", userH.ResetPassword)
	admin.DELETE("/users/:id", userH.Delete)

	return &testEnv{r: r, repo: repo, jwtMgr: jwtMgr}
}

func (e *testEnv) seed(t *testing.T, name, plain string, role usermodel.Role) *usermodel.User {
	t.Helper()
	hash, err := password.Hash(plain)
	require.NoError(t, err)
	u := &usermodel.User{Username: name, PasswordHash: hash, DisplayName: name,
		Role: role, Status: usermodel.StatusActive}
	require.NoError(t, e.repo.Create(context.Background(), u))
	return u
}

func (e *testEnv) request(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func (e *testEnv) login(t *testing.T, name, plain string) string {
	t.Helper()
	w := e.request(t, http.MethodPost, "/api/auth/login", "",
		gin.H{"username": name, "password": plain})
	require.Equal(t, http.StatusOK, w.Code, "登录应成功，响应=%s", w.Body.String())

	var resp struct {
		Code string `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data.Token)
	return resp.Data.Token
}

func respCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var r common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &r))
	return r.Code
}

func TestLogin_Success(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "alice", "password123", usermodel.RoleAdmin)

	w := e.request(t, http.MethodPost, "/api/auth/login", "",
		gin.H{"username": "alice", "password": "password123"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, common.CodeOK, respCode(t, w))
	assert.Contains(t, w.Body.String(), `"username":"alice"`)
}

// 响应体绝不能带出密码哈希。
func TestLogin_ResponseHasNoPasswordHash(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "alice", "password123", usermodel.RoleAdmin)

	w := e.request(t, http.MethodPost, "/api/auth/login", "",
		gin.H{"username": "alice", "password": "password123"})

	assert.NotContains(t, w.Body.String(), "$2a$")
	assert.NotContains(t, w.Body.String(), "passwordHash")
	assert.NotContains(t, w.Body.String(), "password_hash")
}

func TestLogin_MissingFieldsReturns422(t *testing.T) {
	e := newTestEnv(t)
	w := e.request(t, http.MethodPost, "/api/auth/login", "", gin.H{"username": "alice"})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Equal(t, "VALIDATION_FAILED", respCode(t, w))
}

func TestLogin_BadCredentialsReturns401(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "alice", "password123", usermodel.RoleUser)

	w := e.request(t, http.MethodPost, "/api/auth/login", "",
		gin.H{"username": "alice", "password": "nope"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "AUTH_INVALID_CREDENTIALS", respCode(t, w))
}

func TestMe_RequiresToken(t *testing.T) {
	e := newTestEnv(t)
	w := e.request(t, http.MethodGet, "/api/auth/me", "", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMe_ReturnsCurrentUser(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "alice", "password123", usermodel.RoleAdmin)
	token := e.login(t, "alice", "password123")

	w := e.request(t, http.MethodGet, "/api/auth/me", token, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"username":"alice"`)
}

func TestChangePassword_Flow(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "alice", "password123", usermodel.RoleUser)
	token := e.login(t, "alice", "password123")

	w := e.request(t, http.MethodPut, "/api/auth/password", token,
		gin.H{"oldPassword": "password123", "newPassword": "newpassword1"})
	assert.Equal(t, http.StatusOK, w.Code)

	_ = e.login(t, "alice", "newpassword1")

	bad := e.request(t, http.MethodPost, "/api/auth/login", "",
		gin.H{"username": "alice", "password": "password123"})
	assert.Equal(t, http.StatusUnauthorized, bad.Code)
}

func TestUsers_PlainUserForbidden(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	e.seed(t, "plain", "password123", usermodel.RoleUser)
	token := e.login(t, "plain", "password123")

	w := e.request(t, http.MethodGet, "/api/users", token, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "AUTH_FORBIDDEN", respCode(t, w))
}

func TestUsers_AdminCanCreateAndList(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	w := e.request(t, http.MethodPost, "/api/users", token, gin.H{
		"username": "newbie", "password": "password123",
		"displayName": "新人", "role": "user",
	})
	require.Equal(t, http.StatusCreated, w.Code, "响应=%s", w.Body.String())
	assert.NotContains(t, w.Body.String(), "$2a$")

	list := e.request(t, http.MethodGet, "/api/users?page=1&pageSize=10", token, nil)
	assert.Equal(t, http.StatusOK, list.Code)
	assert.Contains(t, list.Body.String(), `"total":2`)
}

func TestUsers_CreateDuplicateReturns409(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	body := gin.H{"username": "dup", "password": "password123", "role": "user"}
	require.Equal(t, http.StatusCreated, e.request(t, http.MethodPost, "/api/users", token, body).Code)

	w := e.request(t, http.MethodPost, "/api/users", token, body)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "USER_USERNAME_TAKEN", respCode(t, w))
}

func TestUsers_CreateShortPasswordReturns422(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	w := e.request(t, http.MethodPost, "/api/users", token,
		gin.H{"username": "shorty", "password": "123", "role": "user"})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Equal(t, "VALIDATION_FAILED", respCode(t, w))
}

func TestUsers_BadIDReturns422(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	token := e.login(t, "admin", "password123")

	w := e.request(t, http.MethodDelete, "/api/users/not-a-number", token, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

// 端到端验证「乙方案」：禁用后旧 token 立刻失效。
func TestDisableUser_RevokesTokenImmediately(t *testing.T) {
	e := newTestEnv(t)
	e.seed(t, "admin", "password123", usermodel.RoleAdmin)
	victim := e.seed(t, "victim", "password123", usermodel.RoleUser)

	adminToken := e.login(t, "admin", "password123")
	victimToken := e.login(t, "victim", "password123")

	require.Equal(t, http.StatusOK, e.request(t, http.MethodGet, "/api/auth/me", victimToken, nil).Code)

	w := e.request(t, http.MethodPut, "/api/users/"+strconv.FormatInt(victim.ID, 10), adminToken,
		gin.H{"status": "disabled"})
	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())

	after := e.request(t, http.MethodGet, "/api/auth/me", victimToken, nil)
	assert.Equal(t, http.StatusForbidden, after.Code, "禁用后旧 token 必须立即失效")
	assert.Equal(t, "AUTH_USER_DISABLED", respCode(t, after))
}
```

该文件的 import 块需包含 `"strconv"`。

- [ ] **Step 2: 运行测试确认失败**

```bash
cd server && go test ./internal/handler/ -v
```
预期：编译失败，`undefined: handler.NewAuthHandler`

- [ ] **Step 3: 实现认证 handler**

`server/internal/handler/auth.go`：

```go
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	authmodel "github.com/chenhao/omnigen-ai/server/internal/model/auth"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

type AuthHandler struct {
	auth *service.AuthService
}

func NewAuthHandler(auth *service.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req authmodel.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	resp, err := h.auth.Login(c.Request.Context(), req)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(resp))
}

// Logout 是无状态的：token 由前端丢弃即可。
// 保留该接口是为了给前端一个统一的登出动作，并留下审计日志。
func (h *AuthHandler) Logout(c *gin.Context) {
	c.JSON(http.StatusOK, common.OK(nil))
}

func (h *AuthHandler) Me(c *gin.Context) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}
	resp, err := h.auth.GetCurrentUser(c.Request.Context(), userID)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(resp))
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}
	var req authmodel.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	if err := h.auth.ChangePassword(c.Request.Context(), userID, req); err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(nil))
}
```

- [ ] **Step 4: 实现用户 handler**

`server/internal/handler/user.go`：

```go
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

type UserHandler struct {
	users *service.UserService
}

func NewUserHandler(users *service.UserService) *UserHandler {
	return &UserHandler{users: users}
}

func pathID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.ErrValidation.Wrap(err)
	}
	return id, nil
}

func (h *UserHandler) List(c *gin.Context) {
	var q usermodel.ListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	resp, err := h.users.List(c.Request.Context(), q)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(resp))
}

func (h *UserHandler) Create(c *gin.Context) {
	var req usermodel.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	resp, err := h.users.Create(c.Request.Context(), req)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, common.OK(resp))
}

func (h *UserHandler) Update(c *gin.Context) {
	targetID, err := pathID(c)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	actorID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}
	var req usermodel.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	resp, err := h.users.Update(c.Request.Context(), actorID, targetID, req)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(resp))
}

func (h *UserHandler) ResetPassword(c *gin.Context) {
	targetID, err := pathID(c)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	var req usermodel.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}
	if err := h.users.ResetPassword(c.Request.Context(), targetID, req); err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(nil))
}

func (h *UserHandler) Delete(c *gin.Context) {
	targetID, err := pathID(c)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	actorID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}
	if err := h.users.Delete(c.Request.Context(), actorID, targetID); err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(nil))
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
cd server && go test ./internal/handler/ -v
```
预期：全部 PASS

- [ ] **Step 6: 提交**

```bash
git add server/internal/handler && git commit -m "feat(server): 认证与用户管理 HTTP handler"
```

---

## Task 12: 路由、wire 装配与入口

**Files:**
- Create: `server/internal/router/router.go`, `server/internal/wire.go`, `server/cmd/server/main.go`, `server/.env.example`
- Generate: `server/internal/wire_gen.go`

- [ ] **Step 1: 写路由注册**

`server/internal/router/router.go`：

```go
package router

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

type Handlers struct {
	Auth   *handler.AuthHandler
	User   *handler.UserHandler
	Health *handler.HealthHandler
}

func New(h Handlers, jwtMgr *jwtx.Manager, users repository.UserRepository) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), middleware.Recovery(), middleware.ErrorHandler())

	// 开发期允许 Vite dev server 跨域；生产由同源部署或反向代理承担。
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	api := r.Group("/api")
	api.GET("/health", h.Health.Check)
	api.POST("/auth/login", h.Auth.Login)

	authed := api.Group("", middleware.Auth(jwtMgr, users))
	authed.GET("/auth/me", h.Auth.Me)
	authed.POST("/auth/logout", h.Auth.Logout)
	authed.PUT("/auth/password", h.Auth.ChangePassword)

	admin := authed.Group("", middleware.RequireAdmin())
	admin.GET("/users", h.User.List)
	admin.POST("/users", h.User.Create)
	admin.PUT("/users/:id", h.User.Update)
	admin.PUT("/users/:id/password", h.User.ResetPassword)
	admin.DELETE("/users/:id", h.User.Delete)

	return r
}
```

- [ ] **Step 2: 写 wire 声明**

```bash
cd server && go get github.com/gin-contrib/cors@latest github.com/google/wire@latest
```

`server/internal/wire.go`：

```go
//go:build wireinject

package internal

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chenhao/omnigen-ai/server/internal/config"
	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/router"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// App 是装配完成的应用，暴露给 main 使用。
type App struct {
	Engine *gin.Engine
	Pool   *pgxpool.Pool
	Users  *service.UserService
	Config *config.Config
}

func provideJWT(cfg *config.Config) *jwtx.Manager {
	return jwtx.NewManager(cfg.JWT.Secret, cfg.JWT.TTL)
}

func providePool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	return repository.NewPool(ctx, cfg.DB.DSN())
}

// provideDB 把连接池收窄成 repository.DB 接口，
// 使 repository 的实现对「池」还是「事务」无感。
func provideDB(pool *pgxpool.Pool) repository.DB { return pool }

func providePinger(pool *pgxpool.Pool) handler.Pinger { return pool }

func provideHandlers(a *handler.AuthHandler, u *handler.UserHandler, h *handler.HealthHandler) router.Handlers {
	return router.Handlers{Auth: a, User: u, Health: h}
}

func provideApp(e *gin.Engine, p *pgxpool.Pool, u *service.UserService, cfg *config.Config) *App {
	return &App{Engine: e, Pool: p, Users: u, Config: cfg}
}

var providerSet = wire.NewSet(
	providePool,
	provideDB,
	providePinger,
	provideJWT,
	repository.NewUserRepository,
	service.NewAuthService,
	service.NewUserService,
	handler.NewAuthHandler,
	handler.NewUserHandler,
	handler.NewHealthHandler,
	provideHandlers,
	router.New,
	provideApp,
)

func InitApp(ctx context.Context, cfg *config.Config) (*App, error) {
	wire.Build(providerSet)
	return nil, nil
}
```

- [ ] **Step 3: 生成 wire 代码**

```bash
cd server && make wire && ls -la internal/wire_gen.go
```
预期：生成 `internal/wire_gen.go`，无报错

- [ ] **Step 4: 写入口**

`server/cmd/server/main.go`：

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chenhao/omnigen-ai/server/internal"
	"github.com/chenhao/omnigen-ai/server/internal/config"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if err := run(); err != nil {
		slog.Error("服务启动失败", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	app, err := internal.InitApp(ctx, cfg)
	if err != nil {
		return fmt.Errorf("装配应用失败: %w", err)
	}
	defer app.Pool.Close()

	// 首启播种：无任何活跃 admin 时才创建，已存在则跳过、不覆盖。
	if err := app.Users.EnsureBootstrapAdmin(ctx, cfg.Bootstrap.Username, cfg.Bootstrap.Password); err != nil {
		return fmt.Errorf("播种管理员失败: %w", err)
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           app.Engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("HTTP 服务已启动", "port", cfg.HTTPPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP 服务异常退出", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("正在关闭服务…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
```

- [ ] **Step 5: 写环境变量样例**

`server/.env.example`：

```
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=123456
DB_NAME=omnigen
DB_SSLMODE=disable

# 必填，缺失则拒绝启动。生成方式：openssl rand -base64 32
JWT_SECRET=
JWT_TTL=168h

BOOTSTRAP_ADMIN_USERNAME=admin
# 必填，仅在系统中不存在任何活跃 admin 时使用
BOOTSTRAP_ADMIN_PASSWORD=

HTTP_PORT=8080
```

- [ ] **Step 6: 启动并手工验证**

```bash
cd server
export JWT_SECRET=$(openssl rand -base64 32)
export BOOTSTRAP_ADMIN_PASSWORD=admin12345
make migrate-up
go run ./cmd/server &
sleep 3

curl -s localhost:8080/api/health
echo
TOKEN=$(curl -s -X POST localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin12345"}' | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
echo "token 前缀: ${TOKEN:0:20}"
curl -s localhost:8080/api/auth/me -H "Authorization: Bearer $TOKEN"
echo
curl -s 'localhost:8080/api/users?page=1&pageSize=10' -H "Authorization: Bearer $TOKEN"
```
预期：health 返回 `"database":"up"`；登录拿到 token；`/auth/me` 返回 admin 用户；用户列表 `"total":1`

- [ ] **Step 7: 停止服务并提交**

```bash
kill %1
git add server/ && git commit -m "feat(server): wire 装配、路由注册与服务入口"
```

---

## Task 13: 后端全量回归

**Files:**
- Modify: 无（仅验证）

- [ ] **Step 1: 跑全部后端测试**

```bash
cd server && make migrate-test-up && go test ./... -v 2>&1 | tail -40
```
预期：所有包 ok，无 FAIL

- [ ] **Step 2: 静态检查**

```bash
cd server && go vet ./... && gofmt -l . | tee /tmp/unformatted.txt && test ! -s /tmp/unformatted.txt && echo "格式检查通过"
```
预期：`go vet` 无输出，`gofmt -l` 无文件列出

- [ ] **Step 3: 提交**

```bash
git add -A server/ && git commit -m "chore(server): 后端全量测试与静态检查通过" --allow-empty
```

---

# Phase B：前端

## Task 14: Vite 脚手架与主题

**Files:**
- Create: `web/` 全套脚手架、`web/src/theme/index.ts`、`web/vite.config.ts`、`web/vitest.config.ts`
- Test: `web/src/theme/theme.test.ts`

- [ ] **Step 1: 创建工程**

```bash
cd /Users/chenhao/codes/myself/omnigen-ai
npm create vite@latest web -- --template react-ts
cd web
npm install
npm install antd @ant-design/icons react-router-dom zustand axios i18next react-i18next
npm install -D vitest @testing-library/react @testing-library/user-event @testing-library/jest-dom jsdom
```

- [ ] **Step 2: 配置 Vite 与 vitest**

`web/vite.config.ts`：

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
```

`web/vitest.config.ts`：

```ts
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import path from 'node:path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
  },
})
```

`web/src/test/setup.ts`：

```ts
import '@testing-library/jest-dom/vitest'

// antd 的响应式组件依赖 matchMedia，jsdom 未实现，需打桩。
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
})
```

在 `web/package.json` 的 `scripts` 中加入：

```json
"test": "vitest run",
"test:watch": "vitest"
```

- [ ] **Step 3: 写失败的主题测试**

`web/src/theme/theme.test.ts`：

```ts
import { describe, it, expect } from 'vitest'
import { theme as antdTheme } from 'antd'
import { omnigenTheme, colors } from './index'

describe('主题配置', () => {
  it('固定使用暗色算法', () => {
    expect(omnigenTheme.algorithm).toBe(antdTheme.darkAlgorithm)
  })

  it('暴露品牌主色与背景色 token', () => {
    expect(omnigenTheme.token?.colorPrimary).toBe(colors.primary)
    expect(omnigenTheme.token?.colorBgBase).toBe(colors.bgBase)
  })

  it('所有色值为合法十六进制，避免拼写错误静默生效', () => {
    Object.entries(colors).forEach(([name, value]) => {
      expect(value, `${name} 不是合法色值`).toMatch(/^#[0-9a-fA-F]{6}$/)
    })
  })
})
```

- [ ] **Step 4: 运行测试确认失败**

```bash
cd web && npx vitest run src/theme
```
预期：FAIL，无法解析 `./index`

- [ ] **Step 5: 实现主题**

`web/src/theme/index.ts`：

```ts
import { theme, type ThemeConfig } from 'antd'

/**
 * 全系统唯一允许定义颜色的地方。
 * 组件中不得出现硬编码色值——子项目 2-4 会引入大量新页面，
 * 这是唯一能守住视觉一致性的手段。
 */
export const colors = {
  primary: '#6366f1',
  primaryHover: '#818cf8',
  bgBase: '#0d0d0f',
  bgContainer: '#131317',
  bgElevated: '#17171b',
  border: '#26262c',
  borderStrong: '#2c2c33',
  textBase: '#f2f2f4',
  textMuted: '#6b6b76',
  success: '#059669',
  warning: '#f59e0b',
  error: '#f43f5e',
} as const

export const omnigenTheme: ThemeConfig = {
  algorithm: theme.darkAlgorithm,
  token: {
    colorPrimary: colors.primary,
    colorBgBase: colors.bgBase,
    colorBgContainer: colors.bgContainer,
    colorBgElevated: colors.bgElevated,
    colorBorder: colors.border,
    colorText: colors.textBase,
    colorTextSecondary: colors.textMuted,
    colorSuccess: colors.success,
    colorWarning: colors.warning,
    colorError: colors.error,
    borderRadius: 8,
    fontFamily:
      "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif",
  },
  components: {
    Layout: {
      siderBg: colors.bgContainer,
      headerBg: colors.bgContainer,
      bodyBg: colors.bgBase,
    },
    Menu: {
      darkItemBg: colors.bgContainer,
      darkItemSelectedBg: colors.primary,
    },
  },
}
```

- [ ] **Step 6: 运行测试确认通过**

```bash
cd web && npx vitest run src/theme
```
预期：三条 PASS

- [ ] **Step 7: 提交**

```bash
git add web/ && git commit -m "feat(web): Vite + React + antd 脚手架与深色主题 token"
```

---

## Task 15: i18n 框架与文案

**Files:**
- Create: `web/src/i18n/index.ts`, `web/src/locales/zh-CN.json`, `web/src/locales/en.json`
- Test: `web/src/locales/locales.test.ts`

- [ ] **Step 1: 写中文文案**

`web/src/locales/zh-CN.json`：

```json
{
  "app": {
    "title": "OmniGen AI",
    "subtitle": "图片生成 · 视频生成"
  },
  "nav": {
    "imggen": "图片生成",
    "imgedit": "图片编辑",
    "t2v": "文生视频",
    "i2v": "图生视频",
    "r2v": "参考生视频",
    "history": "历史记录",
    "users": "用户管理",
    "settings": "设置"
  },
  "login": {
    "title": "登录",
    "subtitle": "请使用管理员分配的账号",
    "username": "用户名",
    "usernamePlaceholder": "请输入用户名",
    "password": "密码",
    "passwordPlaceholder": "请输入密码",
    "submit": "登录",
    "submitting": "登录中…",
    "usernameRequired": "请输入用户名",
    "passwordRequired": "请输入密码",
    "brandTagline": "一个控制台，接管图片与视频生成",
    "featureImage": "图片生成与编辑",
    "featureVideo": "文生 · 图生 · 参考生视频",
    "featureHistory": "全流程历史留痕"
  },
  "common": {
    "confirm": "确定",
    "cancel": "取消",
    "save": "保存",
    "edit": "编辑",
    "delete": "删除",
    "create": "新建",
    "search": "搜索",
    "loading": "加载中…",
    "success": "操作成功",
    "empty": "暂无数据",
    "actions": "操作",
    "language": "语言",
    "logout": "退出登录",
    "changePassword": "修改密码",
    "expand": "展开侧边栏",
    "collapse": "收起侧边栏"
  },
  "placeholder": {
    "title": "功能开发中",
    "description": "「{{name}}」将在后续阶段接入，当前版本仅提供地基与登录能力。"
  },
  "users": {
    "title": "用户管理",
    "username": "用户名",
    "displayName": "显示名",
    "role": "角色",
    "status": "状态",
    "createdAt": "创建时间",
    "roleAdmin": "管理员",
    "roleUser": "普通用户",
    "statusActive": "正常",
    "statusDisabled": "已禁用",
    "create": "新建用户",
    "createTitle": "新建用户",
    "editTitle": "编辑用户",
    "resetPassword": "重置密码",
    "resetPasswordTitle": "重置密码",
    "newPassword": "新密码",
    "passwordRule": "密码长度需 8-72 个字符",
    "usernameRule": "用户名需 3-64 位字母或数字",
    "deleteConfirm": "确定删除用户「{{name}}」吗？此操作不可撤销。",
    "disable": "禁用",
    "enable": "启用",
    "createSuccess": "用户创建成功",
    "updateSuccess": "用户已更新",
    "deleteSuccess": "用户已删除",
    "resetSuccess": "密码已重置"
  },
  "password": {
    "title": "修改密码",
    "oldPassword": "当前密码",
    "newPassword": "新密码",
    "confirmPassword": "确认新密码",
    "mismatch": "两次输入的新密码不一致",
    "success": "密码修改成功，请重新登录"
  },
  "errors": {
    "AUTH_INVALID_CREDENTIALS": "用户名或密码错误",
    "AUTH_UNAUTHORIZED": "登录已失效，请重新登录",
    "AUTH_USER_DISABLED": "账号已被禁用，请联系管理员",
    "AUTH_FORBIDDEN": "没有权限执行该操作",
    "AUTH_WRONG_OLD_PASSWORD": "当前密码不正确",
    "USER_NOT_FOUND": "用户不存在",
    "USER_USERNAME_TAKEN": "该用户名已被占用",
    "USER_CANNOT_MODIFY_SELF": "不能对自己执行该操作",
    "USER_LAST_ADMIN": "系统必须保留至少一个可用的管理员",
    "USER_PASSWORD_TOO_LONG": "密码过长（上限 72 字节，一个中文字符占 3 字节）",
    "VALIDATION_FAILED": "提交的内容不合法，请检查后重试",
    "HEALTH_DB_UNREACHABLE": "数据库连接异常",
    "INTERNAL_ERROR": "服务器出错了，请稍后重试",
    "NETWORK_ERROR": "网络连接失败，请检查后端服务是否已启动",
    "UNKNOWN": "操作失败，请稍后重试"
  }
}
```

- [ ] **Step 2: 写英文文案**

`web/src/locales/en.json`：

```json
{
  "app": {
    "title": "OmniGen AI",
    "subtitle": "Image & Video Generation"
  },
  "nav": {
    "imggen": "Image Generation",
    "imgedit": "Image Editing",
    "t2v": "Text to Video",
    "i2v": "Image to Video",
    "r2v": "Reference to Video",
    "history": "History",
    "users": "Users",
    "settings": "Settings"
  },
  "login": {
    "title": "Sign in",
    "subtitle": "Use the account assigned by your administrator",
    "username": "Username",
    "usernamePlaceholder": "Enter your username",
    "password": "Password",
    "passwordPlaceholder": "Enter your password",
    "submit": "Sign in",
    "submitting": "Signing in…",
    "usernameRequired": "Username is required",
    "passwordRequired": "Password is required",
    "brandTagline": "One console for image and video generation",
    "featureImage": "Image generation and editing",
    "featureVideo": "Text, image and reference to video",
    "featureHistory": "Full history of every run"
  },
  "common": {
    "confirm": "Confirm",
    "cancel": "Cancel",
    "save": "Save",
    "edit": "Edit",
    "delete": "Delete",
    "create": "Create",
    "search": "Search",
    "loading": "Loading…",
    "success": "Done",
    "empty": "No data",
    "actions": "Actions",
    "language": "Language",
    "logout": "Sign out",
    "changePassword": "Change password",
    "expand": "Expand sidebar",
    "collapse": "Collapse sidebar"
  },
  "placeholder": {
    "title": "Coming soon",
    "description": "\"{{name}}\" arrives in a later stage. This build ships the foundation and authentication only."
  },
  "users": {
    "title": "Users",
    "username": "Username",
    "displayName": "Display name",
    "role": "Role",
    "status": "Status",
    "createdAt": "Created",
    "roleAdmin": "Admin",
    "roleUser": "User",
    "statusActive": "Active",
    "statusDisabled": "Disabled",
    "create": "New user",
    "createTitle": "Create user",
    "editTitle": "Edit user",
    "resetPassword": "Reset password",
    "resetPasswordTitle": "Reset password",
    "newPassword": "New password",
    "passwordRule": "Password must be 8-72 characters",
    "usernameRule": "Username must be 3-64 alphanumeric characters",
    "deleteConfirm": "Delete user \"{{name}}\"? This cannot be undone.",
    "disable": "Disable",
    "enable": "Enable",
    "createSuccess": "User created",
    "updateSuccess": "User updated",
    "deleteSuccess": "User deleted",
    "resetSuccess": "Password reset"
  },
  "password": {
    "title": "Change password",
    "oldPassword": "Current password",
    "newPassword": "New password",
    "confirmPassword": "Confirm new password",
    "mismatch": "The two passwords do not match",
    "success": "Password changed, please sign in again"
  },
  "errors": {
    "AUTH_INVALID_CREDENTIALS": "Incorrect username or password",
    "AUTH_UNAUTHORIZED": "Your session expired, please sign in again",
    "AUTH_USER_DISABLED": "This account is disabled, contact your administrator",
    "AUTH_FORBIDDEN": "You do not have permission to do that",
    "AUTH_WRONG_OLD_PASSWORD": "Current password is incorrect",
    "USER_NOT_FOUND": "User not found",
    "USER_USERNAME_TAKEN": "That username is already taken",
    "USER_CANNOT_MODIFY_SELF": "You cannot perform this action on yourself",
    "USER_LAST_ADMIN": "The system must keep at least one usable administrator",
    "USER_PASSWORD_TOO_LONG": "Password is too long (max 72 bytes; each Chinese character uses 3)",
    "VALIDATION_FAILED": "The submitted data is invalid, please check and retry",
    "HEALTH_DB_UNREACHABLE": "Database is unreachable",
    "INTERNAL_ERROR": "Something went wrong, please try again later",
    "NETWORK_ERROR": "Network error, check whether the backend is running",
    "UNKNOWN": "Action failed, please try again later"
  }
}
```

- [ ] **Step 3: 写失败的 key 对齐测试**

`web/src/locales/locales.test.ts`：

```ts
import { describe, it, expect } from 'vitest'
import zhCN from './zh-CN.json'
import en from './en.json'

/** 把嵌套对象展平成 'a.b.c' 形式的 key 列表。 */
function flatten(obj: Record<string, unknown>, prefix = ''): string[] {
  return Object.entries(obj).flatMap(([k, v]) => {
    const key = prefix ? `${prefix}.${k}` : k
    return v !== null && typeof v === 'object'
      ? flatten(v as Record<string, unknown>, key)
      : [key]
  })
}

describe('语言文件', () => {
  const zhKeys = flatten(zhCN).sort()
  const enKeys = flatten(en).sort()

  // 旧系统就是靠人工同步两份翻译，漏译到线上才发现。这条测试是闸门。
  it('中英 key 完全一致', () => {
    expect(enKeys.filter((k) => !zhKeys.includes(k))).toEqual([])
    expect(zhKeys.filter((k) => !enKeys.includes(k))).toEqual([])
  })

  it('没有空文案', () => {
    const findEmpty = (obj: Record<string, unknown>, prefix = ''): string[] =>
      Object.entries(obj).flatMap(([k, v]) => {
        const key = prefix ? `${prefix}.${k}` : k
        if (v !== null && typeof v === 'object') return findEmpty(v as Record<string, unknown>, key)
        return typeof v === 'string' && v.trim() === '' ? [key] : []
      })

    expect(findEmpty(zhCN)).toEqual([])
    expect(findEmpty(en)).toEqual([])
  })

  // 后端每个错误码都必须有对应文案，否则用户会看到裸露的 code。
  it('errors 命名空间覆盖后端全部错误码', () => {
    const required = [
      'AUTH_INVALID_CREDENTIALS', 'AUTH_UNAUTHORIZED', 'AUTH_USER_DISABLED',
      'AUTH_FORBIDDEN', 'AUTH_WRONG_OLD_PASSWORD', 'USER_NOT_FOUND',
      'USER_USERNAME_TAKEN', 'USER_CANNOT_MODIFY_SELF', 'USER_LAST_ADMIN',
      'USER_PASSWORD_TOO_LONG',
      'VALIDATION_FAILED', 'INTERNAL_ERROR', 'UNKNOWN',
    ]
    required.forEach((code) => {
      expect(zhCN.errors, `zh-CN 缺少 ${code}`).toHaveProperty(code)
      expect(en.errors, `en 缺少 ${code}`).toHaveProperty(code)
    })
  })
})
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd web && npx vitest run src/locales
```
预期：三条 PASS（文案已在前两步写好，此测试用于持续守卫）

- [ ] **Step 5: 实现 i18n 初始化**

`web/src/i18n/index.ts`：

```ts
import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import zhCN from '@/locales/zh-CN.json'
import en from '@/locales/en.json'

export const SUPPORTED_LOCALES = ['zh-CN', 'en'] as const
export type Locale = (typeof SUPPORTED_LOCALES)[number]

const STORAGE_KEY = 'omnigen_locale'

export function getStoredLocale(): Locale {
  const stored = localStorage.getItem(STORAGE_KEY)
  return SUPPORTED_LOCALES.includes(stored as Locale) ? (stored as Locale) : 'zh-CN'
}

export function setLocale(locale: Locale): void {
  localStorage.setItem(STORAGE_KEY, locale)
  void i18n.changeLanguage(locale)
}

void i18n.use(initReactI18next).init({
  resources: {
    'zh-CN': { translation: zhCN },
    en: { translation: en },
  },
  lng: getStoredLocale(),
  fallbackLng: 'zh-CN',
  interpolation: { escapeValue: false },
})

export default i18n
```

- [ ] **Step 6: 提交**

```bash
git add web/src/i18n web/src/locales && git commit -m "feat(web): i18n 框架与中英全量文案，含错误码命名空间"
```

---

## Task 16: 类型、API 客户端与认证 store

**Files:**
- Create: `web/src/types/{common,auth,user}.ts`, `web/src/api/{client,auth,user}.ts`, `web/src/stores/auth.ts`
- Test: `web/src/stores/auth.test.ts`

- [ ] **Step 1: 写类型**

`web/src/types/common.ts`：

```ts
export interface ApiResponse<T = unknown> {
  code: string
  message?: string
  data?: T
}

export interface PageQuery {
  page: number
  pageSize: number
}
```

`web/src/types/user.ts`：

```ts
export type Role = 'admin' | 'user'
export type UserStatus = 'active' | 'disabled'

/** 对应后端 usermodel.UserResponse。刻意不含 passwordHash。 */
export interface User {
  id: number
  username: string
  displayName: string
  role: Role
  status: UserStatus
  createdAt: string
  updatedAt: string
}

export interface UserListResponse {
  total: number
  items: User[]
}

export interface CreateUserRequest {
  username: string
  password: string
  displayName?: string
  role: Role
}

export interface UpdateUserRequest {
  displayName?: string
  role?: Role
  status?: UserStatus
}

export interface ResetPasswordRequest {
  password: string
}
```

`web/src/types/auth.ts`：

```ts
import type { User } from './user'

export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  token: string
  user: User
}

export interface ChangePasswordRequest {
  oldPassword: string
  newPassword: string
}
```

- [ ] **Step 2: 实现 API 客户端**

`web/src/api/client.ts`：

```ts
import axios, { AxiosError } from 'axios'
import type { ApiResponse } from '@/types/common'

export const TOKEN_STORAGE_KEY = 'omnigen_token'

/** 携带后端错误码的异常，供 UI 查 i18n 表得到文案。 */
export class ApiError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
  ) {
    super(code)
    this.name = 'ApiError'
  }
}

export const client = axios.create({
  baseURL: '/api',
  timeout: 15000,
  headers: { 'Content-Type': 'application/json' },
})

client.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_STORAGE_KEY)
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

/** 401 时清空本地凭据并跳登录页。由 stores/auth 注册具体动作，避免循环依赖。 */
let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn
}

client.interceptors.response.use(
  (response) => response,
  (error: AxiosError<ApiResponse>) => {
    if (!error.response) {
      return Promise.reject(new ApiError('NETWORK_ERROR', 0))
    }
    const { status, data } = error.response
    const code = data?.code ?? 'UNKNOWN'

    if (status === 401) {
      onUnauthorized?.()
    }
    return Promise.reject(new ApiError(code, status))
  },
)

/** 拆掉统一响应外壳，直接返回 data。 */
export async function unwrap<T>(promise: Promise<{ data: ApiResponse<T> }>): Promise<T> {
  const res = await promise
  return res.data.data as T
}
```

`web/src/api/auth.ts`：

```ts
import { client, unwrap } from './client'
import type { ApiResponse } from '@/types/common'
import type { ChangePasswordRequest, LoginRequest, LoginResponse } from '@/types/auth'
import type { User } from '@/types/user'

export const authApi = {
  login: (req: LoginRequest) =>
    unwrap<LoginResponse>(client.post<ApiResponse<LoginResponse>>('/auth/login', req)),

  me: () => unwrap<User>(client.get<ApiResponse<User>>('/auth/me')),

  logout: () => client.post('/auth/logout'),

  changePassword: (req: ChangePasswordRequest) => client.put('/auth/password', req),
}
```

`web/src/api/user.ts`：

```ts
import { client, unwrap } from './client'
import type { ApiResponse, PageQuery } from '@/types/common'
import type {
  CreateUserRequest,
  ResetPasswordRequest,
  UpdateUserRequest,
  User,
  UserListResponse,
} from '@/types/user'

export const userApi = {
  list: (query: PageQuery) =>
    unwrap<UserListResponse>(
      client.get<ApiResponse<UserListResponse>>('/users', { params: query }),
    ),

  create: (req: CreateUserRequest) =>
    unwrap<User>(client.post<ApiResponse<User>>('/users', req)),

  update: (id: number, req: UpdateUserRequest) =>
    unwrap<User>(client.put<ApiResponse<User>>(`/users/${id}`, req)),

  resetPassword: (id: number, req: ResetPasswordRequest) =>
    client.put(`/users/${id}/password`, req),

  remove: (id: number) => client.delete(`/users/${id}`),
}
```

- [ ] **Step 3: 写失败的 auth store 测试**

`web/src/stores/auth.test.ts`：

```ts
import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { useAuthStore } from './auth'
import { authApi } from '@/api/auth'
import { TOKEN_STORAGE_KEY, ApiError } from '@/api/client'
import type { User } from '@/types/user'

vi.mock('@/api/auth', () => ({
  authApi: { login: vi.fn(), me: vi.fn(), logout: vi.fn(), changePassword: vi.fn() },
}))

const fakeUser: User = {
  id: 1, username: 'alice', displayName: 'Alice', role: 'admin',
  status: 'active', createdAt: '2026-07-18T00:00:00Z', updatedAt: '2026-07-18T00:00:00Z',
}

describe('auth store', () => {
  beforeEach(() => {
    localStorage.clear()
    useAuthStore.setState({ token: null, user: null, initializing: true })
    vi.clearAllMocks()
  })
  afterEach(() => vi.resetAllMocks())

  it('登录成功后写入 token 与用户，并持久化 token', async () => {
    vi.mocked(authApi.login).mockResolvedValue({ token: 'tok-123', user: fakeUser })

    await useAuthStore.getState().login({ username: 'alice', password: 'password123' })

    const state = useAuthStore.getState()
    expect(state.token).toBe('tok-123')
    expect(state.user).toEqual(fakeUser)
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBe('tok-123')
  })

  it('登录失败时不写入任何凭据', async () => {
    vi.mocked(authApi.login).mockRejectedValue(new ApiError('AUTH_INVALID_CREDENTIALS', 401))

    await expect(
      useAuthStore.getState().login({ username: 'alice', password: 'wrong' }),
    ).rejects.toThrow()

    expect(useAuthStore.getState().token).toBeNull()
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
  })

  it('登出清空状态与 localStorage', async () => {
    localStorage.setItem(TOKEN_STORAGE_KEY, 'tok-123')
    useAuthStore.setState({ token: 'tok-123', user: fakeUser, initializing: false })
    vi.mocked(authApi.logout).mockResolvedValue({} as never)

    await useAuthStore.getState().logout()

    expect(useAuthStore.getState().token).toBeNull()
    expect(useAuthStore.getState().user).toBeNull()
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
  })

  // 刷新页面时若 token 已过期，必须先验活再渲染，
  // 否则会拿着废 token 闪一下主界面再跳走。
  it('initialize 在 token 有效时恢复用户', async () => {
    localStorage.setItem(TOKEN_STORAGE_KEY, 'tok-123')
    vi.mocked(authApi.me).mockResolvedValue(fakeUser)

    await useAuthStore.getState().initialize()

    const state = useAuthStore.getState()
    expect(state.user).toEqual(fakeUser)
    expect(state.initializing).toBe(false)
  })

  it('initialize 在 token 失效时清空凭据', async () => {
    localStorage.setItem(TOKEN_STORAGE_KEY, 'stale-token')
    vi.mocked(authApi.me).mockRejectedValue(new ApiError('AUTH_UNAUTHORIZED', 401))

    await useAuthStore.getState().initialize()

    const state = useAuthStore.getState()
    expect(state.user).toBeNull()
    expect(state.token).toBeNull()
    expect(state.initializing).toBe(false)
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
  })

  it('initialize 在无 token 时直接结束', async () => {
    await useAuthStore.getState().initialize()

    expect(useAuthStore.getState().initializing).toBe(false)
    expect(authApi.me).not.toHaveBeenCalled()
  })

  it('isAdmin 反映当前用户角色', () => {
    useAuthStore.setState({ user: fakeUser })
    expect(useAuthStore.getState().isAdmin()).toBe(true)

    useAuthStore.setState({ user: { ...fakeUser, role: 'user' } })
    expect(useAuthStore.getState().isAdmin()).toBe(false)

    useAuthStore.setState({ user: null })
    expect(useAuthStore.getState().isAdmin()).toBe(false)
  })
})
```

- [ ] **Step 4: 运行测试确认失败**

```bash
cd web && npx vitest run src/stores
```
预期：FAIL，无法解析 `./auth`

- [ ] **Step 5: 实现 auth store**

`web/src/stores/auth.ts`：

```ts
import { create } from 'zustand'
import { authApi } from '@/api/auth'
import { TOKEN_STORAGE_KEY, setUnauthorizedHandler } from '@/api/client'
import type { LoginRequest } from '@/types/auth'
import type { User } from '@/types/user'

interface AuthState {
  token: string | null
  user: User | null
  /** 应用启动时的验活阶段。为 true 时不应渲染任何受保护路由。 */
  initializing: boolean
  login: (req: LoginRequest) => Promise<void>
  logout: () => Promise<void>
  initialize: () => Promise<void>
  clear: () => void
  isAdmin: () => boolean
}

export const useAuthStore = create<AuthState>((set, get) => ({
  token: localStorage.getItem(TOKEN_STORAGE_KEY),
  user: null,
  initializing: true,

  login: async (req) => {
    const resp = await authApi.login(req)
    localStorage.setItem(TOKEN_STORAGE_KEY, resp.token)
    set({ token: resp.token, user: resp.user, initializing: false })
  },

  logout: async () => {
    try {
      await authApi.logout()
    } catch {
      // 登出接口失败不应阻止本地清理——用户的意图是离开。
    }
    get().clear()
  },

  clear: () => {
    localStorage.removeItem(TOKEN_STORAGE_KEY)
    set({ token: null, user: null, initializing: false })
  },

  // 刷新页面后先验活再渲染，避免持过期 token 闪现主界面。
  initialize: async () => {
    const token = localStorage.getItem(TOKEN_STORAGE_KEY)
    if (!token) {
      set({ token: null, user: null, initializing: false })
      return
    }
    try {
      const user = await authApi.me()
      set({ token, user, initializing: false })
    } catch {
      get().clear()
    }
  },

  isAdmin: () => get().user?.role === 'admin',
}))

// 任何请求收到 401 都直接清空本地凭据，路由守卫随之跳回登录页。
setUnauthorizedHandler(() => useAuthStore.getState().clear())
```

- [ ] **Step 6: 运行测试确认通过**

```bash
cd web && npx vitest run src/stores
```
预期：七条全部 PASS

- [ ] **Step 7: 提交**

```bash
git add web/src/types web/src/api web/src/stores
git commit -m "feat(web): API 客户端、类型定义与认证 store"
```

---

## Task 17: 登录页

**Files:**
- Create: `web/src/pages/LoginPage.tsx`, `web/src/pages/LoginPage.css`, `web/src/hooks/useApiError.ts`
- Test: `web/src/pages/LoginPage.test.tsx`

**视觉方向已定：** 左右分屏。左半屏为品牌区与能力展示（渐变色块网格，后续可替换为系统真实生成的作品缩略图），右半屏为登录表单。窄屏（< 768px）时左半屏隐藏。实现前先用 **ui-ux-pro-max** skill 产出具体视觉细节，色值一律取自 `@/theme` 的 `colors`，不得硬编码。

- [ ] **Step 1: 实现错误码转文案的 hook**

`web/src/hooks/useApiError.ts`：

```ts
import { useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { App } from 'antd'
import { ApiError } from '@/api/client'

/**
 * 把后端错误码翻成用户可读文案。
 * 未知 code 兜底为通用文案，绝不把 code 裸露给用户。
 */
export function useApiError() {
  const { t } = useTranslation()
  const { message } = App.useApp()

  const toMessage = useCallback(
    (error: unknown): string => {
      const code = error instanceof ApiError ? error.code : 'UNKNOWN'
      const key = `errors.${code}`
      const text = t(key)
      return text === key ? t('errors.UNKNOWN') : text
    },
    [t],
  )

  const notify = useCallback(
    (error: unknown) => {
      void message.error(toMessage(error))
    },
    [message, toMessage],
  )

  return { toMessage, notify }
}
```

- [ ] **Step 2: 写失败的登录页测试**

`web/src/pages/LoginPage.test.tsx`：

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import LoginPage from './LoginPage'
import { useAuthStore } from '@/stores/auth'
import { ApiError } from '@/api/client'

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

function renderLogin() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <MemoryRouter>
            <LoginPage />
          </MemoryRouter>
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('LoginPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useAuthStore.setState({ token: null, user: null, initializing: false })
  })

  it('渲染用户名、密码与提交按钮', () => {
    renderLogin()
    expect(screen.getByLabelText(i18n.t('login.username'))).toBeInTheDocument()
    expect(screen.getByLabelText(i18n.t('login.password'))).toBeInTheDocument()
    expect(screen.getByRole('button', { name: i18n.t('login.submit') })).toBeInTheDocument()
  })

  it('空表单提交时展示校验错误且不调用登录', async () => {
    const login = vi.fn()
    useAuthStore.setState({ login } as never)
    const user = userEvent.setup()
    renderLogin()

    await user.click(screen.getByRole('button', { name: i18n.t('login.submit') }))

    expect(await screen.findByText(i18n.t('login.usernameRequired'))).toBeInTheDocument()
    expect(login).not.toHaveBeenCalled()
  })

  it('填写完整时调用登录并跳转首页', async () => {
    const login = vi.fn().mockResolvedValue(undefined)
    useAuthStore.setState({ login } as never)
    const user = userEvent.setup()
    renderLogin()

    await user.type(screen.getByLabelText(i18n.t('login.username')), 'alice')
    await user.type(screen.getByLabelText(i18n.t('login.password')), 'password123')
    await user.click(screen.getByRole('button', { name: i18n.t('login.submit') }))

    await waitFor(() => {
      expect(login).toHaveBeenCalledWith({ username: 'alice', password: 'password123' })
    })
    await waitFor(() => expect(mockNavigate).toHaveBeenCalled())
  })

  // 用户必须看到「用户名或密码错误」，而不是 AUTH_INVALID_CREDENTIALS。
  it('登录失败时展示翻译后的错误文案', async () => {
    const login = vi.fn().mockRejectedValue(new ApiError('AUTH_INVALID_CREDENTIALS', 401))
    useAuthStore.setState({ login } as never)
    const user = userEvent.setup()
    renderLogin()

    await user.type(screen.getByLabelText(i18n.t('login.username')), 'alice')
    await user.type(screen.getByLabelText(i18n.t('login.password')), 'wrongpass')
    await user.click(screen.getByRole('button', { name: i18n.t('login.submit') }))

    expect(await screen.findByText(i18n.t('errors.AUTH_INVALID_CREDENTIALS'))).toBeInTheDocument()
    expect(mockNavigate).not.toHaveBeenCalled()
  })

  it('提供语言切换', async () => {
    renderLogin()
    expect(screen.getByTestId('locale-switch')).toBeInTheDocument()
  })
})
```

- [ ] **Step 3: 运行测试确认失败**

```bash
cd web && npx vitest run src/pages/LoginPage
```
预期：FAIL，无法解析 `./LoginPage`

- [ ] **Step 4: 实现登录页**

`web/src/pages/LoginPage.css`：

```css
.login-shell {
  display: flex;
  min-height: 100vh;
}

.login-brand {
  flex: 1.15;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 48px 44px;
  background: var(--omnigen-bg-base);
  border-right: 1px solid var(--omnigen-border);
}

.login-brand__logo {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 18px;
  font-weight: 600;
}

.login-brand__mark {
  width: 32px;
  height: 32px;
  border-radius: 9px;
  background: linear-gradient(135deg, #6366f1, #a855f7);
}

.login-brand__tagline {
  font-size: 26px;
  line-height: 1.45;
  font-weight: 600;
  max-width: 12em;
  margin: 40px 0 28px;
}

.login-brand__grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
  max-width: 420px;
}

.login-brand__tile {
  aspect-ratio: 1;
  border-radius: 10px;
}

.login-brand__features {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 36px;
  font-size: 13px;
}

.login-form-pane {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 32px;
  background: var(--omnigen-bg-container);
  position: relative;
}

.login-form {
  width: 100%;
  max-width: 340px;
}

.login-form__title {
  font-size: 24px;
  font-weight: 600;
  margin-bottom: 6px;
}

.login-form__subtitle {
  font-size: 13px;
  margin-bottom: 28px;
}

.login-locale {
  position: absolute;
  top: 24px;
  right: 24px;
}

@media (max-width: 768px) {
  .login-brand {
    display: none;
  }
}
```

`web/src/pages/LoginPage.tsx`：

```tsx
import { useState } from 'react'
import { Alert, Button, Form, Input, Segmented, Typography } from 'antd'
import { LockOutlined, UserOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/hooks/useApiError'
import { colors } from '@/theme'
import { setLocale, getStoredLocale, type Locale } from '@/i18n'
import type { LoginRequest } from '@/types/auth'
import './LoginPage.css'

const { Text } = Typography

// 左半屏的能力展示。子项目 3 完成后可替换为系统真实生成的作品缩略图。
const TILE_GRADIENTS = [
  'linear-gradient(135deg, #7c3aed, #2563eb)',
  'linear-gradient(135deg, #db2777, #f59e0b)',
  'linear-gradient(135deg, #059669, #14b8a6)',
  'linear-gradient(135deg, #f43f5e, #7c3aed)',
  'linear-gradient(135deg, #0ea5e9, #6366f1)',
  'linear-gradient(135deg, #f59e0b, #db2777)',
]

export default function LoginPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const login = useAuthStore((s) => s.login)
  const { toMessage } = useApiError()

  const [submitting, setSubmitting] = useState(false)
  const [errorText, setErrorText] = useState<string | null>(null)
  const [locale, setLocaleState] = useState<Locale>(getStoredLocale())

  const handleLocaleChange = (value: string | number) => {
    const next = value as Locale
    setLocale(next)
    setLocaleState(next)
  }

  const handleSubmit = async (values: LoginRequest) => {
    setSubmitting(true)
    setErrorText(null)
    try {
      await login(values)
      navigate('/', { replace: true })
    } catch (err) {
      setErrorText(toMessage(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div
      className="login-shell"
      style={
        {
          '--omnigen-bg-base': colors.bgBase,
          '--omnigen-bg-container': colors.bgContainer,
          '--omnigen-border': colors.border,
        } as React.CSSProperties
      }
    >
      <aside className="login-brand">
        <div className="login-brand__logo">
          <span className="login-brand__mark" />
          <span>{t('app.title')}</span>
        </div>

        <div>
          <div className="login-brand__tagline">{t('login.brandTagline')}</div>
          <div className="login-brand__grid">
            {TILE_GRADIENTS.map((bg) => (
              <div key={bg} className="login-brand__tile" style={{ background: bg }} />
            ))}
          </div>
        </div>

        <div className="login-brand__features" style={{ color: colors.textMuted }}>
          <span>{t('login.featureImage')}</span>
          <span>{t('login.featureVideo')}</span>
          <span>{t('login.featureHistory')}</span>
        </div>
      </aside>

      <main className="login-form-pane">
        <div className="login-locale" data-testid="locale-switch">
          <Segmented
            size="small"
            value={locale}
            onChange={handleLocaleChange}
            options={[
              { label: '中文', value: 'zh-CN' },
              { label: 'EN', value: 'en' },
            ]}
          />
        </div>

        <div className="login-form">
          <div className="login-form__title">{t('login.title')}</div>
          <Text className="login-form__subtitle" type="secondary">
            {t('login.subtitle')}
          </Text>

          {errorText && (
            <Alert
              type="error"
              showIcon
              message={errorText}
              style={{ marginBottom: 16 }}
            />
          )}

          <Form layout="vertical" onFinish={handleSubmit} requiredMark={false} size="large">
            <Form.Item
              name="username"
              label={t('login.username')}
              rules={[{ required: true, message: t('login.usernameRequired') }]}
            >
              <Input prefix={<UserOutlined />} placeholder={t('login.usernamePlaceholder')} autoComplete="username" />
            </Form.Item>

            <Form.Item
              name="password"
              label={t('login.password')}
              rules={[{ required: true, message: t('login.passwordRequired') }]}
            >
              <Input.Password
                prefix={<LockOutlined />}
                placeholder={t('login.passwordPlaceholder')}
                autoComplete="current-password"
              />
            </Form.Item>

            <Button type="primary" htmlType="submit" block loading={submitting}>
              {submitting ? t('login.submitting') : t('login.submit')}
            </Button>
          </Form>
        </div>
      </main>
    </div>
  )
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
cd web && npx vitest run src/pages/LoginPage
```
预期：五条全部 PASS

- [ ] **Step 6: 提交**

```bash
git add web/src/pages web/src/hooks && git commit -m "feat(web): 左右分屏登录页与错误码文案映射"
```

---

## Task 18: 主界面外壳与路由守卫

**Files:**
- Create: `web/src/layouts/AppShell.tsx`, `web/src/layouts/AppShell.css`, `web/src/components/{ProtectedRoute,AdminRoute}.tsx`, `web/src/pages/PlaceholderPage.tsx`, `web/src/components/ChangePasswordModal.tsx`
- Test: `web/src/components/ProtectedRoute.test.tsx`

- [ ] **Step 1: 写失败的守卫测试**

`web/src/components/ProtectedRoute.test.tsx`：

```tsx
import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

import ProtectedRoute from './ProtectedRoute'
import AdminRoute from './AdminRoute'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types/user'

const adminUser: User = {
  id: 1, username: 'admin', displayName: 'Admin', role: 'admin',
  status: 'active', createdAt: '', updatedAt: '',
}
const plainUser: User = { ...adminUser, id: 2, username: 'bob', role: 'user' }

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/login" element={<div>登录页</div>} />
        <Route element={<ProtectedRoute />}>
          <Route path="/home" element={<div>受保护内容</div>} />
          <Route element={<AdminRoute />}>
            <Route path="/users" element={<div>管理员内容</div>} />
          </Route>
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

describe('路由守卫', () => {
  beforeEach(() => {
    useAuthStore.setState({ token: null, user: null, initializing: false })
  })

  it('未登录访问受保护路由时跳转登录页', () => {
    renderAt('/home')
    expect(screen.getByText('登录页')).toBeInTheDocument()
    expect(screen.queryByText('受保护内容')).not.toBeInTheDocument()
  })

  it('已登录可访问受保护路由', () => {
    useAuthStore.setState({ token: 'tok', user: plainUser, initializing: false })
    renderAt('/home')
    expect(screen.getByText('受保护内容')).toBeInTheDocument()
  })

  // 验活未完成时不能渲染内容，也不能急着跳登录页——
  // 否则刷新页面会闪一下登录页再跳回来。
  it('验活进行中时既不渲染内容也不跳转', () => {
    useAuthStore.setState({ token: 'tok', user: null, initializing: true })
    renderAt('/home')
    expect(screen.queryByText('受保护内容')).not.toBeInTheDocument()
    expect(screen.queryByText('登录页')).not.toBeInTheDocument()
  })

  it('普通用户访问管理员路由被挡下', () => {
    useAuthStore.setState({ token: 'tok', user: plainUser, initializing: false })
    renderAt('/users')
    expect(screen.queryByText('管理员内容')).not.toBeInTheDocument()
  })

  it('管理员可访问管理员路由', () => {
    useAuthStore.setState({ token: 'tok', user: adminUser, initializing: false })
    renderAt('/users')
    expect(screen.getByText('管理员内容')).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd web && npx vitest run src/components/ProtectedRoute
```
预期：FAIL，无法解析 `./ProtectedRoute`

- [ ] **Step 3: 实现路由守卫**

`web/src/components/ProtectedRoute.tsx`：

```tsx
import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { Spin } from 'antd'
import { useAuthStore } from '@/stores/auth'

export default function ProtectedRoute() {
  const { user, initializing } = useAuthStore()
  const location = useLocation()

  // 启动验活期间既不渲染内容也不跳转，避免刷新时闪现登录页。
  if (initializing) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
        <Spin size="large" />
      </div>
    )
  }

  if (!user) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }

  return <Outlet />
}
```

`web/src/components/AdminRoute.tsx`：

```tsx
import { Navigate, Outlet } from 'react-router-dom'
import { Result } from 'antd'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth'

export default function AdminRoute() {
  const { t } = useTranslation()
  const user = useAuthStore((s) => s.user)

  if (!user) {
    return <Navigate to="/login" replace />
  }
  if (user.role !== 'admin') {
    return <Result status="403" title="403" subTitle={t('errors.AUTH_FORBIDDEN')} />
  }
  return <Outlet />
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd web && npx vitest run src/components/ProtectedRoute
```
预期：五条全部 PASS

- [ ] **Step 5: 实现占位页**

`web/src/pages/PlaceholderPage.tsx`：

```tsx
import { Empty, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

const { Title, Paragraph } = Typography

/** 生成类功能的占位页。子项目 3 会逐个替换掉。 */
export default function PlaceholderPage({ nameKey }: { nameKey: string }) {
  const { t } = useTranslation()
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', paddingTop: 80 }}>
      <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={false} />
      <Title level={4} style={{ marginTop: 24 }}>
        {t('placeholder.title')}
      </Title>
      <Paragraph type="secondary" style={{ maxWidth: 420, textAlign: 'center' }}>
        {t('placeholder.description', { name: t(nameKey) })}
      </Paragraph>
    </div>
  )
}
```

- [ ] **Step 6: 实现改密弹窗**

`web/src/components/ChangePasswordModal.tsx`：

```tsx
import { useState } from 'react'
import { App, Form, Input, Modal } from 'antd'
import { useTranslation } from 'react-i18next'

import { authApi } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/hooks/useApiError'

interface Values {
  oldPassword: string
  newPassword: string
  confirmPassword: string
}

export default function ChangePasswordModal({
  open,
  onClose,
}: {
  open: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [form] = Form.useForm<Values>()
  const [submitting, setSubmitting] = useState(false)
  const { notify } = useApiError()
  const { message } = App.useApp()
  const clear = useAuthStore((s) => s.clear)

  const handleOk = async () => {
    const values = await form.validateFields()
    setSubmitting(true)
    try {
      await authApi.changePassword({
        oldPassword: values.oldPassword,
        newPassword: values.newPassword,
      })
      void message.success(t('password.success'))
      form.resetFields()
      onClose()
      // 改密后旧 token 对应的凭据已不可信，强制重新登录。
      clear()
    } catch (err) {
      notify(err)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={open}
      title={t('password.title')}
      onCancel={onClose}
      onOk={handleOk}
      confirmLoading={submitting}
      okText={t('common.confirm')}
      cancelText={t('common.cancel')}
      destroyOnClose
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item
          name="oldPassword"
          label={t('password.oldPassword')}
          rules={[{ required: true, message: t('login.passwordRequired') }]}
        >
          <Input.Password autoComplete="current-password" />
        </Form.Item>

        <Form.Item
          name="newPassword"
          label={t('password.newPassword')}
          rules={[{ required: true, min: 8, max: 72, message: t('users.passwordRule') }]}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>

        <Form.Item
          name="confirmPassword"
          label={t('password.confirmPassword')}
          dependencies={['newPassword']}
          rules={[
            { required: true, message: t('users.passwordRule') },
            ({ getFieldValue }) => ({
              validator: (_, value) =>
                !value || getFieldValue('newPassword') === value
                  ? Promise.resolve()
                  : Promise.reject(new Error(t('password.mismatch'))),
            }),
          ]}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>
      </Form>
    </Modal>
  )
}
```

- [ ] **Step 7: 实现主界面外壳**

`web/src/layouts/AppShell.css`：

```css
.shell-logo {
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  border-bottom: 1px solid var(--omnigen-border);
  overflow: hidden;
  white-space: nowrap;
}

.shell-logo__mark {
  width: 26px;
  height: 26px;
  border-radius: 7px;
  background: linear-gradient(135deg, #6366f1, #a855f7);
  flex-shrink: 0;
}

.shell-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  border-bottom: 1px solid var(--omnigen-border);
}

.shell-header__right {
  display: flex;
  align-items: center;
  gap: 14px;
}

.shell-content {
  padding: 20px;
  overflow: auto;
}
```

`web/src/layouts/AppShell.tsx`：

```tsx
import { useState } from 'react'
import { Avatar, Dropdown, Layout, Menu, Segmented, Tooltip, Typography } from 'antd'
import {
  EditOutlined, HistoryOutlined, LogoutOutlined, MenuFoldOutlined,
  MenuUnfoldOutlined, PictureOutlined, PlayCircleOutlined, TeamOutlined,
  UserOutlined, VideoCameraOutlined,
} from '@ant-design/icons'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

import { useAuthStore } from '@/stores/auth'
import { colors } from '@/theme'
import { getStoredLocale, setLocale, type Locale } from '@/i18n'
import ChangePasswordModal from '@/components/ChangePasswordModal'
import './AppShell.css'

const { Header, Sider, Content } = Layout
const { Text } = Typography

const COLLAPSE_KEY = 'omnigen_sider_collapsed'

interface NavItem {
  key: string
  path: string
  icon: React.ReactNode
  labelKey: string
  adminOnly?: boolean
}

const NAV_ITEMS: NavItem[] = [
  { key: 'imggen', path: '/imggen', icon: <PictureOutlined />, labelKey: 'nav.imggen' },
  { key: 'imgedit', path: '/imgedit', icon: <EditOutlined />, labelKey: 'nav.imgedit' },
  { key: 't2v', path: '/t2v', icon: <PlayCircleOutlined />, labelKey: 'nav.t2v' },
  { key: 'i2v', path: '/i2v', icon: <VideoCameraOutlined />, labelKey: 'nav.i2v' },
  { key: 'r2v', path: '/r2v', icon: <VideoCameraOutlined />, labelKey: 'nav.r2v' },
  { key: 'history', path: '/history', icon: <HistoryOutlined />, labelKey: 'nav.history' },
  { key: 'users', path: '/users', icon: <TeamOutlined />, labelKey: 'nav.users', adminOnly: true },
]

export default function AppShell() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const { user, logout, isAdmin } = useAuthStore()

  // 折叠状态持久化：这是用户的长期偏好，不该每次刷新都重置。
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem(COLLAPSE_KEY) !== 'false',
  )
  const [locale, setLocaleState] = useState<Locale>(getStoredLocale())
  const [pwdOpen, setPwdOpen] = useState(false)

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      localStorage.setItem(COLLAPSE_KEY, String(!prev))
      return !prev
    })
  }

  const visibleItems = NAV_ITEMS.filter((item) => !item.adminOnly || isAdmin())
  const activeKey =
    visibleItems.find((item) => location.pathname.startsWith(item.path))?.key ?? 'imggen'
  const activeLabel = visibleItems.find((item) => item.key === activeKey)?.labelKey

  const handleLocaleChange = (value: string | number) => {
    const next = value as Locale
    setLocale(next)
    setLocaleState(next)
  }

  return (
    <Layout style={{ minHeight: '100vh', ['--omnigen-border' as string]: colors.border }}>
      <Sider
        collapsible
        collapsed={collapsed}
        trigger={null}
        collapsedWidth={64}
        width={220}
        theme="dark"
      >
        <div className="shell-logo">
          <span className="shell-logo__mark" />
          {!collapsed && <Text strong>{t('app.title')}</Text>}
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[activeKey]}
          onClick={({ key }) => {
            const target = visibleItems.find((i) => i.key === key)
            if (target) navigate(target.path)
          }}
          items={visibleItems.map((item) => ({
            key: item.key,
            icon: item.icon,
            label: t(item.labelKey),
          }))}
        />
      </Sider>

      <Layout>
        <Header className="shell-header" style={{ background: colors.bgContainer, padding: '0 20px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <Tooltip title={collapsed ? t('common.expand') : t('common.collapse')}>
              <span
                role="button"
                aria-label={collapsed ? t('common.expand') : t('common.collapse')}
                onClick={toggleCollapsed}
                style={{ cursor: 'pointer', fontSize: 16 }}
              >
                {collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
              </span>
            </Tooltip>
            <Text strong>{activeLabel ? t(activeLabel) : t('app.title')}</Text>
          </div>

          <div className="shell-header__right">
            <Segmented
              size="small"
              value={locale}
              onChange={handleLocaleChange}
              options={[
                { label: '中文', value: 'zh-CN' },
                { label: 'EN', value: 'en' },
              ]}
            />
            <Dropdown
              menu={{
                items: [
                  { key: 'password', icon: <UserOutlined />, label: t('common.changePassword') },
                  { type: 'divider' },
                  { key: 'logout', icon: <LogoutOutlined />, label: t('common.logout'), danger: true },
                ],
                onClick: ({ key }) => {
                  if (key === 'logout') void logout()
                  if (key === 'password') setPwdOpen(true)
                },
              }}
            >
              <span style={{ cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 8 }}>
                <Avatar size="small" style={{ background: colors.primary }}>
                  {(user?.displayName || user?.username || '?').charAt(0).toUpperCase()}
                </Avatar>
                <Text>{user?.displayName || user?.username}</Text>
              </span>
            </Dropdown>
          </div>
        </Header>

        <Content className="shell-content">
          <Outlet />
        </Content>
      </Layout>

      <ChangePasswordModal open={pwdOpen} onClose={() => setPwdOpen(false)} />
    </Layout>
  )
}
```

- [ ] **Step 8: 运行前端全部测试**

```bash
cd web && npm test
```
预期：全部 PASS

- [ ] **Step 9: 提交**

```bash
git add web/src && git commit -m "feat(web): 窄图标栏外壳、路由守卫、占位页与改密弹窗"
```

---

## Task 19: 用户管理页

**Files:**
- Create: `web/src/pages/UsersPage.tsx`, `web/src/pages/UserFormModal.tsx`
- Test: `web/src/pages/UsersPage.test.tsx`

- [ ] **Step 1: 写失败的测试**

`web/src/pages/UsersPage.test.tsx`：

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import UsersPage from './UsersPage'
import { userApi } from '@/api/user'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types/user'

vi.mock('@/api/user', () => ({
  userApi: {
    list: vi.fn(), create: vi.fn(), update: vi.fn(),
    resetPassword: vi.fn(), remove: vi.fn(),
  },
}))

const admin: User = {
  id: 1, username: 'admin', displayName: '管理员', role: 'admin',
  status: 'active', createdAt: '2026-07-18T00:00:00Z', updatedAt: '2026-07-18T00:00:00Z',
}
const bob: User = { ...admin, id: 2, username: 'bob', displayName: 'Bob', role: 'user' }

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <UsersPage />
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('UsersPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    vi.mocked(userApi.list).mockResolvedValue({ total: 2, items: [admin, bob] })
  })

  it('加载并展示用户列表', async () => {
    renderPage()
    expect(await screen.findByText('admin')).toBeInTheDocument()
    expect(screen.getByText('bob')).toBeInTheDocument()
  })

  it('展示角色与状态的翻译文案而非原始枚举值', async () => {
    renderPage()
    await screen.findByText('admin')
    expect(screen.getAllByText(i18n.t('users.roleAdmin')).length).toBeGreaterThan(0)
    expect(screen.getAllByText(i18n.t('users.statusActive')).length).toBeGreaterThan(0)
  })

  it('创建用户后刷新列表', async () => {
    vi.mocked(userApi.create).mockResolvedValue({ ...bob, id: 3, username: 'carol' })
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('admin')

    await user.click(screen.getByRole('button', { name: i18n.t('users.create') }))
    await user.type(await screen.findByLabelText(i18n.t('users.username')), 'carol')
    await user.type(screen.getByLabelText(i18n.t('login.password')), 'password123')
    await user.click(screen.getByRole('button', { name: i18n.t('common.confirm') }))

    await waitFor(() => expect(userApi.create).toHaveBeenCalled())
    await waitFor(() => expect(userApi.list).toHaveBeenCalledTimes(2))
  })

  // 后端已有护栏，前端也要藏起来，避免用户点了才被拒绝。
  it('不为当前登录用户自己渲染删除按钮', async () => {
    renderPage()
    await screen.findByText('admin')

    const rows = screen.getAllByRole('row')
    const adminRow = rows.find((r) => r.textContent?.includes('admin'))
    expect(adminRow?.querySelector('[data-testid="delete-user-1"]')).toBeNull()

    const bobRow = rows.find((r) => r.textContent?.includes('bob'))
    expect(bobRow?.querySelector('[data-testid="delete-user-2"]')).not.toBeNull()
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd web && npx vitest run src/pages/UsersPage
```
预期：FAIL，无法解析 `./UsersPage`

- [ ] **Step 3: 实现用户表单弹窗**

`web/src/pages/UserFormModal.tsx`：

```tsx
import { useEffect, useState } from 'react'
import { Form, Input, Modal, Select } from 'antd'
import { useTranslation } from 'react-i18next'

import type { CreateUserRequest, Role, UpdateUserRequest, User, UserStatus } from '@/types/user'

interface Props {
  open: boolean
  /** 传 null 表示新建，传用户表示编辑。 */
  editing: User | null
  submitting: boolean
  onCancel: () => void
  onCreate: (req: CreateUserRequest) => Promise<void>
  onUpdate: (id: number, req: UpdateUserRequest) => Promise<void>
}

interface FormValues {
  username: string
  password: string
  displayName: string
  role: Role
  status: UserStatus
}

export default function UserFormModal({
  open, editing, submitting, onCancel, onCreate, onUpdate,
}: Props) {
  const { t } = useTranslation()
  const [form] = Form.useForm<FormValues>()
  const [internalSubmitting, setInternalSubmitting] = useState(false)

  useEffect(() => {
    if (!open) return
    if (editing) {
      form.setFieldsValue({
        username: editing.username,
        displayName: editing.displayName,
        role: editing.role,
        status: editing.status,
      })
    } else {
      form.resetFields()
      form.setFieldsValue({ role: 'user', status: 'active' })
    }
  }, [open, editing, form])

  const handleOk = async () => {
    const values = await form.validateFields()
    setInternalSubmitting(true)
    try {
      if (editing) {
        await onUpdate(editing.id, {
          displayName: values.displayName,
          role: values.role,
          status: values.status,
        })
      } else {
        await onCreate({
          username: values.username,
          password: values.password,
          displayName: values.displayName,
          role: values.role,
        })
      }
    } finally {
      setInternalSubmitting(false)
    }
  }

  return (
    <Modal
      open={open}
      title={editing ? t('users.editTitle') : t('users.createTitle')}
      onCancel={onCancel}
      onOk={handleOk}
      confirmLoading={submitting || internalSubmitting}
      okText={t('common.confirm')}
      cancelText={t('common.cancel')}
      destroyOnClose
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item
          name="username"
          label={t('users.username')}
          rules={[
            { required: true, message: t('users.usernameRule') },
            { min: 3, max: 64, pattern: /^[a-zA-Z0-9]+$/, message: t('users.usernameRule') },
          ]}
        >
          {/* 用户名是账号标识，创建后不允许修改 */}
          <Input disabled={!!editing} autoComplete="off" />
        </Form.Item>

        {!editing && (
          <Form.Item
            name="password"
            label={t('login.password')}
            rules={[{ required: true, min: 8, max: 72, message: t('users.passwordRule') }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        )}

        <Form.Item name="displayName" label={t('users.displayName')} rules={[{ max: 64 }]}>
          <Input autoComplete="off" />
        </Form.Item>

        <Form.Item name="role" label={t('users.role')} rules={[{ required: true }]}>
          <Select
            options={[
              { value: 'admin', label: t('users.roleAdmin') },
              { value: 'user', label: t('users.roleUser') },
            ]}
          />
        </Form.Item>

        {editing && (
          <Form.Item name="status" label={t('users.status')} rules={[{ required: true }]}>
            <Select
              options={[
                { value: 'active', label: t('users.statusActive') },
                { value: 'disabled', label: t('users.statusDisabled') },
              ]}
            />
          </Form.Item>
        )}
      </Form>
    </Modal>
  )
}
```

- [ ] **Step 4: 实现用户管理页**

`web/src/pages/UsersPage.tsx`：

```tsx
import { useCallback, useEffect, useState } from 'react'
import { App, Button, Input, Modal, Space, Table, Tag } from 'antd'
import { DeleteOutlined, EditOutlined, KeyOutlined, PlusOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import type { ColumnsType } from 'antd/es/table'

import { userApi } from '@/api/user'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/hooks/useApiError'
import type { CreateUserRequest, UpdateUserRequest, User } from '@/types/user'
import UserFormModal from './UserFormModal'

export default function UsersPage() {
  const { t } = useTranslation()
  const { message, modal } = App.useApp()
  const { notify } = useApiError()
  const currentUser = useAuthStore((s) => s.user)

  const [items, setItems] = useState<User[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<User | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const resp = await userApi.list({ page, pageSize })
      setItems(resp.items)
      setTotal(resp.total)
    } catch (err) {
      notify(err)
    } finally {
      setLoading(false)
    }
  }, [page, pageSize, notify])

  useEffect(() => {
    void load()
  }, [load])

  const handleCreate = async (req: CreateUserRequest) => {
    setSubmitting(true)
    try {
      await userApi.create(req)
      void message.success(t('users.createSuccess'))
      setFormOpen(false)
      await load()
    } catch (err) {
      notify(err)
    } finally {
      setSubmitting(false)
    }
  }

  const handleUpdate = async (id: number, req: UpdateUserRequest) => {
    setSubmitting(true)
    try {
      await userApi.update(id, req)
      void message.success(t('users.updateSuccess'))
      setFormOpen(false)
      await load()
    } catch (err) {
      notify(err)
    } finally {
      setSubmitting(false)
    }
  }

  const handleDelete = (target: User) => {
    modal.confirm({
      title: t('common.delete'),
      content: t('users.deleteConfirm', { name: target.username }),
      okText: t('common.confirm'),
      cancelText: t('common.cancel'),
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await userApi.remove(target.id)
          void message.success(t('users.deleteSuccess'))
          await load()
        } catch (err) {
          notify(err)
        }
      },
    })
  }

  const handleResetPassword = (target: User) => {
    let password = ''
    modal.confirm({
      title: t('users.resetPasswordTitle'),
      content: (
        <Input.Password
          placeholder={t('users.passwordRule')}
          onChange={(e) => {
            password = e.target.value
          }}
        />
      ),
      okText: t('common.confirm'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        if (password.length < 8 || password.length > 72) {
          void message.error(t('users.passwordRule'))
          return Promise.reject(new Error('invalid'))
        }
        try {
          await userApi.resetPassword(target.id, { password })
          void message.success(t('users.resetSuccess'))
        } catch (err) {
          notify(err)
        }
      },
    })
  }

  const columns: ColumnsType<User> = [
    { title: t('users.username'), dataIndex: 'username', key: 'username' },
    { title: t('users.displayName'), dataIndex: 'displayName', key: 'displayName' },
    {
      title: t('users.role'),
      dataIndex: 'role',
      key: 'role',
      render: (role: User['role']) => (
        <Tag color={role === 'admin' ? 'purple' : 'default'}>
          {role === 'admin' ? t('users.roleAdmin') : t('users.roleUser')}
        </Tag>
      ),
    },
    {
      title: t('users.status'),
      dataIndex: 'status',
      key: 'status',
      render: (status: User['status']) => (
        <Tag color={status === 'active' ? 'success' : 'error'}>
          {status === 'active' ? t('users.statusActive') : t('users.statusDisabled')}
        </Tag>
      ),
    },
    {
      title: t('users.createdAt'),
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (v: string) => new Date(v).toLocaleString(),
    },
    {
      title: t('common.actions'),
      key: 'actions',
      render: (_, record) => {
        // 后端对自我删除已有护栏；前端一并隐藏，避免用户点了才被拒。
        const isSelf = record.id === currentUser?.id
        return (
          <Space>
            <Button
              size="small"
              icon={<EditOutlined />}
              data-testid={`edit-user-${record.id}`}
              onClick={() => {
                setEditing(record)
                setFormOpen(true)
              }}
            >
              {t('common.edit')}
            </Button>
            <Button
              size="small"
              icon={<KeyOutlined />}
              data-testid={`reset-user-${record.id}`}
              onClick={() => handleResetPassword(record)}
            >
              {t('users.resetPassword')}
            </Button>
            {!isSelf && (
              <Button
                size="small"
                danger
                icon={<DeleteOutlined />}
                data-testid={`delete-user-${record.id}`}
                onClick={() => handleDelete(record)}
              >
                {t('common.delete')}
              </Button>
            )}
          </Space>
        )
      },
    },
  ]

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 16 }}>
        <h2 style={{ margin: 0 }}>{t('users.title')}</h2>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => {
            setEditing(null)
            setFormOpen(true)
          }}
        >
          {t('users.create')}
        </Button>
      </div>

      <Table<User>
        rowKey="id"
        loading={loading}
        columns={columns}
        dataSource={items}
        pagination={{
          current: page,
          pageSize,
          total,
          showSizeChanger: true,
          onChange: (p, ps) => {
            setPage(p)
            setPageSize(ps)
          },
        }}
      />

      <UserFormModal
        open={formOpen}
        editing={editing}
        submitting={submitting}
        onCancel={() => setFormOpen(false)}
        onCreate={handleCreate}
        onUpdate={handleUpdate}
      />
    </div>
  )
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
cd web && npx vitest run src/pages/UsersPage
```
预期：四条全部 PASS

- [ ] **Step 6: 提交**

```bash
git add web/src/pages && git commit -m "feat(web): 用户管理页与用户表单弹窗"
```

---

## Task 20: 根装配与路由表

**Files:**
- Create/Modify: `web/src/App.tsx`, `web/src/main.tsx`, `web/index.html`

- [ ] **Step 1: 写路由表**

`web/src/App.tsx`：

```tsx
import { useEffect } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'

import AppShell from '@/layouts/AppShell'
import ProtectedRoute from '@/components/ProtectedRoute'
import AdminRoute from '@/components/AdminRoute'
import LoginPage from '@/pages/LoginPage'
import UsersPage from '@/pages/UsersPage'
import PlaceholderPage from '@/pages/PlaceholderPage'
import { useAuthStore } from '@/stores/auth'

export default function App() {
  const initialize = useAuthStore((s) => s.initialize)

  // 启动时用 /auth/me 验活，避免持过期 token 闪现主界面。
  useEffect(() => {
    void initialize()
  }, [initialize])

  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />

      <Route element={<ProtectedRoute />}>
        <Route element={<AppShell />}>
          <Route index element={<Navigate to="/imggen" replace />} />
          <Route path="/imggen" element={<PlaceholderPage nameKey="nav.imggen" />} />
          <Route path="/imgedit" element={<PlaceholderPage nameKey="nav.imgedit" />} />
          <Route path="/t2v" element={<PlaceholderPage nameKey="nav.t2v" />} />
          <Route path="/i2v" element={<PlaceholderPage nameKey="nav.i2v" />} />
          <Route path="/r2v" element={<PlaceholderPage nameKey="nav.r2v" />} />
          <Route path="/history" element={<PlaceholderPage nameKey="nav.history" />} />

          <Route element={<AdminRoute />}>
            <Route path="/users" element={<UsersPage />} />
          </Route>
        </Route>
      </Route>

      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
```

- [ ] **Step 2: 写根入口**

`web/src/main.tsx`：

```tsx
import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import enUS from 'antd/locale/en_US'
import { useTranslation } from 'react-i18next'

import App from './App'
import { omnigenTheme } from '@/theme'
import '@/i18n'
import './index.css'

/** antd 自带组件文案跟随应用语言切换。 */
function Root() {
  const { i18n } = useTranslation()
  return (
    <ConfigProvider theme={omnigenTheme} locale={i18n.language === 'en' ? enUS : zhCN}>
      <AntdApp>
        <BrowserRouter>
          <App />
        </BrowserRouter>
      </AntdApp>
    </ConfigProvider>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <Root />
  </React.StrictMode>,
)
```

`web/src/index.css`：

```css
* {
  box-sizing: border-box;
}

html,
body,
#root {
  margin: 0;
  padding: 0;
  height: 100%;
}

body {
  background: #0d0d0f;
}
```

`web/index.html` 的 `<title>` 改为：

```html
<title>OmniGen AI</title>
```

- [ ] **Step 3: 类型检查与构建**

```bash
cd web && npx tsc --noEmit && npm run build
```
预期：无类型错误，构建成功

- [ ] **Step 4: 提交**

```bash
git add web/ && git commit -m "feat(web): 路由表与根装配，antd 文案跟随语言切换"
```

---

## Task 21: 前后端联调验证

**Files:** 无（仅验证）

- [ ] **Step 1: 启动后端**

```bash
cd server
export JWT_SECRET=$(openssl rand -base64 32)
export BOOTSTRAP_ADMIN_PASSWORD=admin12345
make migrate-up
go run ./cmd/server &
sleep 3
curl -s localhost:8080/api/health
```
预期：`{"code":"OK","data":{"database":"up","service":"up"}}`

- [ ] **Step 2: 启动前端**

```bash
cd web && npm run dev &
sleep 5
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:5173
```
预期：`200`

- [ ] **Step 3: 用浏览器逐条走查**

打开 http://localhost:5173，逐项确认：

1. 未登录访问 `/imggen` → 自动跳转 `/login`
2. 登录页呈左右分屏，窄屏（拖窄窗口至 < 768px）时左半屏隐藏
3. 用错误密码登录 → 页面显示「用户名或密码错误」，**不是** `AUTH_INVALID_CREDENTIALS`
4. 用 `admin` / `admin12345` 登录成功 → 进入主界面，默认落在图片生成占位页
5. 侧边栏默认为 64px 窄图标栏，hover 出 tooltip；点顶栏折叠按钮可展开为 220px
6. 刷新页面 → 侧边栏保持展开/折叠状态，且不闪现登录页
7. 切换语言为 EN → 导航、按钮、日期选择器等 antd 组件文案一并变英文
8. 进入用户管理 → 建一个 `tester` / `password123` / 普通用户
9. 用户列表中自己那一行没有删除按钮
10. 退出登录 → 用 `tester` 登录 → 侧边栏**没有**「用户管理」入口；手动访问 `/users` 显示 403

- [ ] **Step 4: 验证禁用立即生效（核心断言）**

保持 `tester` 在浏览器中登录状态，用另一个终端：

```bash
ADMIN_TOKEN=$(curl -s -X POST localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin12345"}' | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')

TESTER_ID=$(curl -s 'localhost:8080/api/users?page=1&pageSize=50' \
  -H "Authorization: Bearer $ADMIN_TOKEN" | sed -n 's/.*"id":\([0-9]*\),"username":"tester".*/\1/p')

curl -s -X PUT "localhost:8080/api/users/$TESTER_ID" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"status":"disabled"}'
```

回到浏览器中 `tester` 的标签页，点击任意导航。

预期：立即被登出并跳回登录页；再次尝试登录时提示「账号已被禁用，请联系管理员」。**这验证了设计中选定的方案——禁用不必等 token 过期。**

- [ ] **Step 5: 关闭服务并跑全量测试**

```bash
kill %1 2>/dev/null; pkill -f "go run ./cmd/server" 2>/dev/null; pkill -f vite 2>/dev/null
cd server && go test ./... 2>&1 | tail -20
cd ../web && npm test 2>&1 | tail -20
```
预期：两侧全部 PASS

- [ ] **Step 6: 更新 README 并提交**

在 `README.md` 顶部加一节：

```markdown
## 新版（改造中）

新版前后端位于 `server/`（Go + Gin + wire + Postgres）与 `web/`（React + Vite + antd）。
旧版 `server.js` + `public/` 仍可独立运行，改造全部完成后移除。

### 启动新版

```bash
# 数据库：docker 容器 postgres-17，端口 5432
# 注意：若本机 brew postgresql 在运行会遮蔽该端口，需先 brew services stop postgresql@14

cd server
cp .env.example .env   # 填入 JWT_SECRET 与 BOOTSTRAP_ADMIN_PASSWORD
make migrate-up
make run               # :8080

cd ../web
npm install
npm run dev            # :5173
```

改造分四个阶段推进，当前已完成阶段 1「地基与登录」：
详见 `docs/superpowers/specs/2026-07-18-rewrite-foundation-auth-design.md`。
```

```bash
git add README.md && git commit -m "docs: README 补充新版启动方式与改造阶段说明"
```

---

## 完成标准

- [ ] `cd server && go test ./...` 全绿
- [ ] `cd server && go vet ./...` 无输出，`gofmt -l .` 无文件
- [ ] `cd web && npm test` 全绿
- [ ] `cd web && npx tsc --noEmit && npm run build` 通过
- [ ] Task 21 的十条浏览器走查逐项确认
- [ ] Task 21 Step 4 的禁用立即生效验证通过
- [ ] 旧版 `npm start`（:3000）仍可独立运行，未受影响

