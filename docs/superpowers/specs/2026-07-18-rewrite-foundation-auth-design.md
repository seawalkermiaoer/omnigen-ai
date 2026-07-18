# OmniGen AI 改造 · 子项目 1：地基与登录

- 日期：2026-07-18
- 分支：`rewrite-react-go`
- 状态：设计已确认，待写实现计划

## 背景

现有 OmniGen AI 是一个自用的 AIGC 控制台：`server.js`（708 行 Express 单文件）做上游代理，`public/` 下是无构建步骤的原生 HTML/CSS/JS，全部前端脚本共享同一个全局作用域、靠 `index.html` 里的 `<script>` 顺序隐式依赖。七个视图（参考生视频、图生视频、文生视频、图片生成、图片编辑、历史记录、设置）覆盖 DashScope 原生协议与 t8star 的 OpenAI 兼容协议两套上游。

**所有状态都在浏览器 localStorage**：API Key、区域、endpoint、语言偏好、以及最多 100 条含 base64 缩略图的历史记录。没有数据库、没有用户、没有会话、没有鉴权。

本次改造把它重写为 React + Vite + antd 前端、Go + Gin + wire 后端、Postgres 持久化，并引入登录。

## 范围拆分

改造整体拆为四个子项目，各自走一遍 spec → plan → 实现，依赖顺序为 1 → 2 → 3 → 4：

| # | 子项目 | 内容 |
|---|---|---|
| 1 | **地基 + 登录** | Go/wire/Postgres 骨架、users 表、认证与用户管理、React+antd 外壳、登录页、i18n 框架、深色主题 |
| 2 | 配置与密钥 | 应用配置落库（API Key 加密存储）、region/endpoint、模型目录接口、设置页 |
| 3 | 生成核心 | 图片生成/编辑、R2V/I2V/T2V、上传与 OSS、prompt 优化、任务轮询 |
| 4 | 历史记录 | 历史从 localStorage 迁至 Postgres，列表/详情/复用/导出 |

**本文档只覆盖子项目 1。**

拆分理由：这次改造不只是换技术栈，数据归属模型整个变了——从"浏览器单机应用"变成"有服务端状态的多层系统"。一次性写成单个 spec 会产生大量未定项和相互矛盾的假设。

## 目标

- 建立可运行、可测试的前后端骨架，后续三个子项目在其上增量开发
- 交付完整的多用户认证与用户管理能力
- 交付登录页与主界面外壳，以及贯穿全系统的 i18n 与主题基础设施

## 非目标（明确不做）

- 任何生成功能、上传、prompt 优化、任务轮询、历史记录
- 设置页中的 API Key / endpoint / region 配置项（属子项目 2）
- 用户注册、找回密码、邮箱验证、第三方登录
- 浏览器端到端测试（Playwright，留待子项目 3）
- 浅色主题
- 删除旧代码

导航中的生成类入口在本阶段渲染为占位页，使整个骨架能够立即端到端跑通并验证。

## 与旧系统共存

`server.js` 与 `public/` 原地保留、不做修改。新代码进入两个新建子目录，新旧两套可并行运行（旧系统 :3000，新后端 :8080，新前端 :5173）。四个子项目全部完成后再移除旧实现。

## 关键决策

| 决策 | 结论 | 理由 |
|---|---|---|
| 用户模型 | 多用户 + 共享一套配置 + 角色（admin / user） | 多人使用，但 API Key 由 admin 统一配置，普通用户只能用不能改 |
| 开户方式 | 仅 admin 后台建号，不开放注册 | 共享 API Key 意味着任何注册者都在消耗额度 |
| 会话机制 | JWT（形式）+ 每请求校验用户状态（见下） | 见「JWT 与禁用生效」 |
| 登录页视觉 | 左右分屏：左品牌/作品墙，右表单 | 一眼可辨是 AIGC 工具；窄屏时左半屏折叠隐藏 |
| 主界面骨架 | 64px 窄图标栏（可展开并记忆状态）+ 顶栏 | 参数表单与结果预览都吃横向空间；顶部导航方案在 i18n 英文长标签下会撑破顶栏 |
| 主题 | 仅深色 | 与现有系统一致；深色背景不干扰图片与视频预览 |
| 后端框架 | Gin | Go Web 生态最厚 |
| 依赖注入 | google/wire | 编译期生成，依赖未接上则编译失败，而非运行时才炸 |
| 数据访问 | pgx/v5 + 手写 SQL，不用 ORM | 查询简单；子项目 4 的历史筛选手写 SQL 更可控 |
| 后端 i18n | 不做，只返回错误码 | 旧系统客户端与服务端各写一份 i18n，两份翻译需同步 |

### JWT 与禁用生效

纯无状态 JWT 下，admin 禁用某用户后，其手中 token 在过期前仍然有效——而「admin 建号并可禁用」正是选定的用户模型，禁用不生效会使该功能形同虚设。

**采用方案**：token 格式仍为 JWT（自包含、无需 sessions 表），中间件校验签名通过后，按 `sub` 查一次 `users` 表确认 `status = 'active'`。禁用与改密立即生效，代价是每请求增加一次主键查询——本地 Postgres 亚毫秒级，在本系统请求量下无感知影响。

放弃的替代方案：纯无状态（禁用延迟最长等于 TTL）；`token_version` 自增比对（同样要查库，更复杂且无额外收益）。

## 目录结构

```
omnigen-ai/
├── server/                     # Go 后端（新建）
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── model/              # 统一模型层，按业务域拆分
│   │   │   ├── common/
│   │   │   │   ├── response.go
│   │   │   │   └── types.go
│   │   │   ├── auth/
│   │   │   │   ├── request.go
│   │   │   │   ├── response.go
│   │   │   │   └── types.go
│   │   │   └── user/
│   │   │       ├── request.go
│   │   │       ├── response.go
│   │   │       └── types.go
│   │   ├── config/
│   │   ├── repository/
│   │   ├── service/
│   │   ├── handler/
│   │   ├── middleware/
│   │   └── pkg/                # jwt、password、apperr
│   ├── migrations/
│   ├── wire.go
│   ├── wire_gen.go
│   └── go.mod
├── web/                        # React 前端（新建）
│   ├── src/
│   │   ├── api/
│   │   ├── types/
│   │   ├── pages/
│   │   ├── layouts/
│   │   ├── components/
│   │   ├── stores/
│   │   ├── locales/
│   │   └── theme/
│   ├── vite.config.ts
│   └── package.json
├── server.js                   # 旧系统，不动
└── public/                     # 旧系统，不动
```

### model 层约定

`internal/model/` 按业务域一级拆分，每个域固定三个文件：`request.go`（请求体）、`response.go`（响应体）、`types.go`（领域实体与枚举）。子项目 2–4 依次追加 `setting/`、`generation/`、`history/`、`upload/`，结构不变。

两条强制纪律：

1. **`types.go` 中的实体不得直接作为响应返回。** `user.User` 含 `PasswordHash` 字段，对外必须经 `response.go` 中的 `UserResponse` 转换。域内分文件的目的是让"该结构会被序列化给前端"一眼可辨。
2. **model 包导入一律加 `model` 后缀别名**，如 `usermodel "…/internal/model/user"`，避免与 `service/user`、`repository/user` 包名冲突。

请求体与响应体**只允许**定义在 `model/` 下。handler 与 service 从此处引用类型，禁止就地定义匿名 struct。

## 数据模型

子项目 1 只引入一张业务表。无 sessions 表。

```sql
CREATE TABLE users (
  id            BIGSERIAL PRIMARY KEY,
  username      VARCHAR(64)  NOT NULL UNIQUE,
  password_hash VARCHAR(255) NOT NULL,        -- bcrypt
  display_name  VARCHAR(64)  NOT NULL DEFAULT '',
  role          VARCHAR(16)  NOT NULL DEFAULT 'user',    -- 'admin' | 'user'
  status        VARCHAR(16)  NOT NULL DEFAULT 'active',  -- 'active' | 'disabled'
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);
```

迁移由 golang-migrate 管理，每个变更提供 `.up.sql` 与 `.down.sql`。

## 后端架构

### 分层

依赖方向严格单向，由 wire 装配：

```
handler  →  service  →  repository  →  Postgres
   ↓           ↓            ↓
        三层均只依赖 model/ 中的类型
```

- **repository** 只认识 SQL 与 `model/*/types.go` 中的实体。不做业务判断，不返回 HTTP 概念。
- **service** 只认识业务规则。不认识 `*gin.Context`。
- **handler** 仅做三件事：绑定并校验请求体、调用 service、将结果或错误写为统一响应。

每层对上暴露 interface，wire 注入实现，使 service 测试可注入假 repository 而无需启动数据库。

### wire 装配

`wire.go` 按 Config → pgxpool → Repositories → Services → Handlers → Router 顺序声明 provider set。`wire_gen.go` 由 `wire ./...` 生成并提交入仓库。

### 接口清单

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| POST | `/api/auth/login` | 公开 | 返回 JWT 与用户信息 |
| POST | `/api/auth/logout` | 登录 | 前端清除 token；服务端仅记日志 |
| GET | `/api/auth/me` | 登录 | 当前用户 |
| PUT | `/api/auth/password` | 登录 | 修改自己的密码，需提供旧密码 |
| GET | `/api/users` | admin | 分页列表 |
| POST | `/api/users` | admin | 创建账号 |
| PUT | `/api/users/:id` | admin | 修改 display_name / role / status |
| PUT | `/api/users/:id/password` | admin | 重置密码，不需旧密码 |
| DELETE | `/api/users/:id` | admin | 删除账号 |
| GET | `/api/health` | 公开 | 存活与数据库连通性 |

### 统一响应

```go
// internal/model/common/response.go
type Response struct {
    Code    string `json:"code"`              // "OK" 或错误码，如 "AUTH_INVALID_CREDENTIALS"
    Message string `json:"message,omitempty"` // 仅供日志与调试，不直接展示给用户
    Data    any    `json:"data,omitempty"`
}
```

HTTP 状态码保持语义（401 / 403 / 404 / 422 / 500），`Code` 供前端做精确分支与 i18n 查表。

### 错误处理

`internal/pkg/apperr` 定义 `AppError{Code, HTTPStatus, Internal error}`。service 返回 `AppError`，由统一的错误转换中间件写为 `common.Response`。`Internal` 字段只进日志，绝不出网。

后端不做 i18n：只返回 `Code`，由前端查表得到文案。翻译因此只有一份，新增语言无需改动后端。

### 业务规则

- 状态为 `disabled` 的用户不能登录，已登录的 token 在下一次请求即被拒绝
- admin 不能禁用、降级或删除自己
- 系统中最后一个 active admin 不能被降级、禁用或删除
- 密码使用 bcrypt 哈希，明文密码不进日志

### 环境配置

```
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=123456
DB_NAME=omnigen
DB_SSLMODE=disable
JWT_SECRET=<必填>
JWT_TTL=168h
BOOTSTRAP_ADMIN_USERNAME=admin
BOOTSTRAP_ADMIN_PASSWORD=<必填>
HTTP_PORT=8080
```

Postgres 为本地 Docker 实例，密码 `123456`，需预先创建 `omnigen` 与 `omnigen_test` 两个数据库。

**首启播种**：启动时若 `users` 表中不存在任何 admin，则用 `BOOTSTRAP_ADMIN_*` 创建一个；已存在则跳过且不覆盖。

**`JWT_SECRET` 缺失时拒绝启动**，不生成随机默认值——否则每次重启都会使所有 token 失效且难以排查。

## 前端架构

技术选型：Vite + React 18 + TypeScript + antd 5 + react-router 6 + zustand + axios + react-i18next。

### 认证流转

- `stores/auth.ts`（zustand）持有 `token` 与 `user`；token 同步写入 localStorage 以便刷新页面后恢复
- `api/client.ts` 是唯一的 axios 实例：请求拦截器附加 `Authorization: Bearer`，响应拦截器遇 401 清空 store 并跳转 `/login`
- `ProtectedRoute` 包裹所有业务路由；`AdminRoute` 在其上追加 `role === 'admin'` 校验
- 应用启动时若 localStorage 存在 token，先调用 `/api/auth/me` 验活再渲染，避免持过期 token 闪现主界面

### i18n

react-i18next，`zh-CN` 与 `en` 两份 JSON，以现有 `public/locales/*.json` 文案为迁移起点。语言偏好存 localStorage。antd 自带组件文案通过 `ConfigProvider locale`（`zhCN` / `enUS`）跟随切换。

错误码文案单列 `errors` 命名空间，与后端 `Code` 一一对应。查不到对应 code 时兜底显示通用文案，不将 code 裸露给用户。

### 主题

`theme/index.ts` 导出一份 antd token 配置，`ConfigProvider` 固定 `darkAlgorithm`。所有自定义颜色集中于此，组件中不得出现硬编码色值——这是子项目 2–4 大量新页面进入后唯一能守住视觉一致性的手段。

### 类型

`src/types/` 与后端 `model/` 按域同名对应（`types/user.ts`、`types/auth.ts`），手写维护。子项目 1 接口面很小，不值得引入代码生成工具。

### 界面

具体视觉设计使用 ui-ux-pro-max 产出，方向已定：

- **登录页**：左右分屏。左半屏为品牌区与能力展示（可用静态渐变色块，后续可替换为系统真实生成的作品缩略图）；右半屏为登录表单。窄屏时左半屏隐藏。
- **主界面外壳**：64px 窄图标栏，hover 显示 tooltip，可手动展开为完整侧边栏并记忆状态；顶栏显示当前页名称、语言切换、用户菜单。

### 开发代理

Vite 将 `/api` 代理至 `http://localhost:8080`，前后端各自热重载。

## 测试策略

遵循 TDD：先写测试，确认失败，再写实现。

### 后端

- **repository 层**跑真实 Postgres，使用独立的 `omnigen_test` 库；每个用例在事务中执行并回滚，用例间互不污染
- **service 层**注入假 repository，覆盖业务规则：禁用用户登录被拒、密码错误、admin 不能禁用自己、最后一个 admin 不能被降级或删除
- **handler 层**用 `httptest` 打完整中间件链，覆盖：无 token 返回 401、普通用户访问 admin 接口返回 403、请求体校验失败返回 422
- **端到端冒烟**一条：启动服务 → 播种 admin → 登录 → 建号 → 新号登录 → admin 禁用该号 → 新号请求立即返回 401（专门验证「每请求校验用户状态」这一决策）

### 前端

vitest + @testing-library/react，覆盖：

- 登录表单校验
- auth store 的登录、登出、刷新恢复
- `ProtectedRoute` 未登录时跳转
- `zh-CN` 与 `en` 两份 JSON 的 key 完全对齐（防止旧系统那类漏翻译上线才发现的问题）

## 风险

| 风险 | 应对 |
|---|---|
| 每请求查库校验用户状态，未来若需水平扩展会成为热点 | 单实例自用场景下无问题；确有需要时在 repository 层加短 TTL 缓存，不影响上层 |
| `types.go` 实体被误直接返回，泄露 `password_hash` | handler 层测试断言响应 JSON 不含该字段 |
| 前端 `types/` 手写维护，与后端 model 漂移 | 接口面小；子项目 3 接口显著变多后再评估引入代码生成 |
| 旧系统文案迁移遗漏 | i18n key 对齐测试作为闸门 |
