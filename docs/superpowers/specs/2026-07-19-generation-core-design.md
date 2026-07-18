# OmniGen AI 改造 · 子项目 2+3：配置与生成核心

- 日期：2026-07-19
- 分支：`rewrite-react-go`
- 前置：子项目 1「地基与登录」已完成（见 `2026-07-18-rewrite-foundation-auth-design.md`）

## 范围

一次性完成原计划的子项目 2（配置与密钥）与子项目 3（生成核心）。做完之后旧系统的**全部生成能力**都在新栈上跑通，只剩子项目 4（历史记录落库）。

**本文覆盖的功能**：应用配置与密钥落库、模型目录、图片生成、图片编辑、文生视频、图生视频、参考生视频、图片上传（含 OSS 大文件路径）、prompt 智能优化、异步任务轮询、结果下载。

## 权威参考

旧系统的线路格式就是本次移植的规格。实现时以下列源码为准，不要凭记忆：

| 内容 | 位置 |
|---|---|
| 全部后端端点与上游转发 | `server.js` |
| t8star 协议 | `lib/providers/t8star.js` |
| 7 套 prompt 系统提示词 | `server.js:443-550` |
| 模型目录 | `server.js:169-184` |
| 各视图参数矩阵 | `public/js/{r2v,i2v,imggen,imgedit}.js` |
| 任务轮询 | `public/js/task.js:102-152` |
| DashScope 协议参考 | `docs/dashscope_image.md` |

## 必须原样保留的行为

这些是产品行为，不是实现细节，移植时不得"顺手优化"：

1. **两个协议的 content 顺序是相反的。** DashScope 图片生成是 `[{image}...,{text}]`（图在前）；t8star 是 `[{type:'text'},{type:'image_url'}...]`（文在前）。且 t8star 在无图时 `content` 是**裸字符串**而非单元素数组。
2. **7 套系统提示词逐字保留**，包括其中的中文标点与换行。它们是产品内容。
3. **参数写入的判空条件不一致且有意义**：`size`/`negative_prompt` 用真值判断，`n`/`watermark`/`seed`/`thinking_mode`/`enable_sequential`/`prompt_extend` 用 `!= null`（因此 `0` 与 `false` 会被发送）。
4. **prompt 优化的降级链**：仅在 AccessDenied 时重试，模型外层、endpoint 内层，最多 2×2 次。`isAccessDeniedResponse` 的两个正则不同（code 查 `AccessDenied` 无空格，message 查 `Access denied` 有空格），不得统一。
5. **i2v 的客户端图片校验**：宽和高**都**要达到下限（不是短边），比例区间按模型区分（happyhorse 300px / 0.4–2.5，wan 240px / 0.125–8）。
6. **12MB 上传阈值**：`<=` 走 base64 data URI，`>` 走 OSS + 24 小时签名 URL。
7. **`ratio` 对 i2v 不发送**（自动跟随首帧）。

## 明确不原样移植的旧系统 bug

| 旧行为 | 位置 | 新行为 |
|---|---|---|
| imgedit 无图时发 `mode='imgedit'`，不匹配任何提示词 key，静默退化为 `t2v` 视频提示词 | `task.js:263` | 图片编辑一律用 `imggen_edit` |
| `isImgMode` 只判 `=== 'imggen'`，imgedit 历史按视频布局渲染出一堆 undefined | `history.js` | 按 mode 归类为图片/视频两族 |
| `UNKNOWN` 状态被当作终态，一次畸形响应永久杀死轮询 | `task.js:139` | `UNKNOWN` 计入连续失败计数，连续 N 次才终止 |
| STS 凭证缓存无并发保护 | `server.js:105` | Go 侧用 `singleflight` |
| 轮询无上限、无超时，`while(true)` | `task.js:102` | 设置最大时长与最大轮询次数 |
| `sharp` 被引入但从未调用，README 宣称的压缩不存在 | `server.js` | 不实现压缩，也不宣称 |

## 安全问题（必须修，不移植）

1. **API Key 出现在 query string**（`GET /api/task/:taskId?apiKey=...`），会进访问日志。新版凭证不出前端——见下。
2. **`/api/download` 是无鉴权开放代理**，且无限跟随重定向。新版：要求登录、限制跳数、按上游域名白名单。
3. **`resolveEndpoint` 会把用户的 API Key 转发到任意 `http(s)://` 主机**（SSRF）。新版 endpoint 取自服务端配置，不接受客户端传入。

## 核心架构决策

### 决策 1：凭证不再经过前端

旧系统把 API Key 存在浏览器 localStorage，每个请求由前端带上。新版**凭证只存在于服务端**：

- `app_settings` 表保存 API Key（AES-256-GCM 加密）、region、endpoint、workspaceId、OSS 配置
- 只有 admin 能写；普通用户能读到"是否已配置"和脱敏后的尾号，读不到明文
- 生成请求前端只发业务参数，凭证由后端在转发时注入

这直接消掉了上面三个安全问题里的两个，也是把它做成多用户系统的前提——共用一套 Key 的模型下，Key 本来就不该发给每个用户。

**加密方案**：AES-256-GCM，密钥来自 `APP_ENCRYPTION_KEY` 环境变量（32 字节，`openssl rand -base64 32` 生成），缺失则拒绝启动。密文存储格式 `iv:authTag:ciphertext`，三段 base64。**加密时传入 setting key 作为 AAD**，把密文绑定到它所属的行——否则把 DashScope Key 的密文拷到 t8star Key 那一列，解密照样成功并返回一个有效但错误的凭证。

### 决策 2：模型目录收敛到服务端单一来源

旧系统的模型列表存在三份，且视频模型完全不在目录里、服务端不校验。新版：

- `internal/model/catalog` 定义全部模型：id、能力（t2i/edit/t2v/i2v/r2v/optimize）、协议（dashscope/openai）、参数约束（尺寸集合、数量上限、是否支持 thinking_mode 等）、region 限制
- `GET /api/catalog` 返回它，前端所有下拉框由它渲染
- 提交时服务端按目录校验 model 与参数组合，非法组合返回 `VALIDATION_FAILED`

wan2.7 系列仅限 `cn-beijing` / `ap-southeast-1` 的限制写进目录，前后端共用同一份判断。

### 决策 3：任务轮询改为服务端持久化

旧系统由浏览器 `while(true)` 每 15 秒轮询，关掉标签页任务就失联，刷新页面靠 localStorage 里的 taskId 手动"继续轮询"。

新版：`generation_tasks` 表持久化任务，后端起一个 worker 轮询上游并更新表；前端轮询自己的后端（或后续接 SSE）。好处是关掉浏览器任务照样跑完，且为子项目 4 的历史记录省掉一次重构——历史记录本质上就是这张表。

轮询策略：间隔 15 秒（与旧系统一致），最长 30 分钟，连续 5 次 `UNKNOWN` 或网络失败才判定失败。

### 决策 4：上游客户端按协议分层

```
internal/provider/
├── provider.go       # ImageProvider / VideoProvider / OptimizeProvider 接口
├── dashscope/        # 原生协议：video-synthesis、tasks、multimodal-generation、compatible-mode
└── t8star/           # OpenAI 兼容：chat/completions + markdown 正则抠图 URL
```

service 层依赖接口，按配置里的 endpoint 选实现。t8star 不支持视频——由目录层表达，不是在调用处硬编码告警。

## 数据模型

```sql
-- 子项目 2
CREATE TABLE app_settings (
  key         VARCHAR(64) PRIMARY KEY,
  value       TEXT NOT NULL,           -- 敏感项为 iv:tag:ciphertext
  is_secret   BOOLEAN NOT NULL DEFAULT false,
  updated_by  BIGINT REFERENCES users(id),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 子项目 3
CREATE TABLE generation_tasks (
  id            BIGSERIAL PRIMARY KEY,
  user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  mode          VARCHAR(16) NOT NULL,   -- imggen|imgedit|t2v|i2v|r2v
  model         VARCHAR(64) NOT NULL,
  status        VARCHAR(16) NOT NULL,   -- PENDING|RUNNING|SUCCEEDED|FAILED|CANCELED
  upstream_task_id VARCHAR(128),        -- 视频异步任务；图片同步则为空
  prompt        TEXT NOT NULL DEFAULT '',
  params        JSONB NOT NULL DEFAULT '{}',
  input_urls    JSONB NOT NULL DEFAULT '[]',
  result_urls   JSONB NOT NULL DEFAULT '[]',
  usage         JSONB,
  note          TEXT,                   -- t8star 的模型散文
  error_code    VARCHAR(64),
  error_message TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_tasks_user_created ON generation_tasks (user_id, created_at DESC);
CREATE INDEX idx_tasks_polling ON generation_tasks (status) WHERE status IN ('PENDING','RUNNING');
```

`generation_tasks` 同时就是子项目 4 的历史记录表——不再单独设计。

## 接口

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/api/catalog` | 登录 | 模型目录与参数约束 |
| GET | `/api/settings` | 登录 | 配置（密钥脱敏，仅返回尾号与是否已配置） |
| PUT | `/api/settings` | admin | 写配置，密钥加密落库 |
| POST | `/api/settings/test` | admin | 用当前配置试调上游，验证凭证有效 |
| POST | `/api/upload` | 登录 | 图片上传，≤12MB 返 data URI，否则 OSS 签名 URL |
| POST | `/api/generate/image` | 登录 | 同步图片生成/编辑，落 `generation_tasks` |
| POST | `/api/generate/video` | 登录 | 创建异步视频任务，返回本地 task id |
| GET | `/api/tasks/:id` | 登录（仅本人） | 任务状态与结果 |
| GET | `/api/tasks` | 登录（仅本人） | 分页任务列表（即历史记录） |
| POST | `/api/optimize-prompt` | 登录 | prompt 智能优化 |
| GET | `/api/download/:taskId/:index` | 登录（仅本人） | 按任务下载结果，服务端持有真实 URL |

下载改为按任务 id + 结果序号，前端拿不到也不需要拼真实上游 URL——顺带解决了开放代理问题。

## 前端

- `pages/generation/` 下五个页面共用一套组件：模型选择器（由目录驱动）、参数面板（由目录约束渲染）、素材上传区、prompt 输入 + 优化按钮、结果展示区
- 参数面板不为每个页面手写——由目录里的参数约束描述生成，否则五个页面会长出五份几乎相同又互相漂移的表单代码
- 任务提交后跳转到结果区并轮询 `/api/tasks/:id`
- 设置页仅 admin 可见（复用子项目 1 的 `AdminRoute`）

## 测试策略

沿用子项目 1：TDD，repository 层跑真实 Postgres 并在事务中回滚，service 层注入假 provider，handler 层 `httptest` 打完整中间件链。

**provider 层用录制的真实上游响应做夹具**——`docs/superpowers/plans/2026-07-18-t8star-gpt-image-2.md` 里有一份真实的 t8star 响应可直接复用。协议解析是这次移植最容易出错的地方，必须用真实报文而不是手编的样本测。

关键断言：
- 两个协议的 content 顺序各自正确
- t8star 的 markdown 正则能从真实响应里抠出 URL，且 `note` 是剥离后的散文
- AccessDenied 降级链的顺序与次数
- 加密：AAD 绑定生效——把 A 列的密文换到 B 列必须解密失败
- 参数判空：`n=0`、`watermark=false`、`seed=0` 必须被发送，`size=''`、`negative_prompt=''` 必须被省略
- 凭证不出现在任何响应体或日志里
