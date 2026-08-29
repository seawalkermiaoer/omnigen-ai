## 新版

新版前后端位于 `server/`（Go + Gin + wire + Postgres）与 `web/`（React + Vite + antd）。
旧版 `server.js` + `public/` 仍可独立运行（见下方「AIGC 图片生成，视频生成」章节），
两套系统各自监听不同端口，互不干扰。

### 功能

- **账号体系**：JWT 登录、管理员 / 普通用户两种角色、用户管理（新建、改密、禁用）
- **配置与凭证**：DashScope / t8star API Key、地域、接入点、OSS 凭证等全部落库，
  AES-256-GCM 加密存储，仅管理员可写，接口只回传脱敏值——**API Key 不再经过浏览器**，
  这是相对旧版最核心的安全变化
- **模型目录**：单一后端目录驱动前端的模型下拉与参数面板，新增/调整模型不需要改前端代码
- **图片生成 / 图片编辑**：Qwen 与 wan2.7 系列、`gpt-image-2`（t8star 协议），参数面板按所选模型自动增减
- **五种视频生成**：文生视频、图生视频（首帧 / 首尾帧 / 续写，任务类型因模型而异）、参考生视频
  （参考图 / 参考视频 / 参考音频）、文件生视频、网页生视频，服务端持久化任务并轮询上游到终态，
  前端不再自己戳接口
- **wan3.0 全能视频模型**：`wan3.0-video` / `wan3.0-video-prime` 一个 model id 覆盖上述五种能力。
  因此请求体里多了一个 `mode` 字段——单能力模型（happyhorse / wan2.7）后端仍会自行推断，可以不传；
  wan3.0 必须显式指定，否则拒绝。它的 resolution（多 480P）、duration（2~30 秒，`-1` 为智能时长）、
  ratio（含 `adaptive`）取值都与 wan2.7 不同，这些集合已下沉进模型目录，由后端单向驱动前端控件
- **上传与 OSS**：≤12MB 走 base64 内嵌，超出走阿里云 OSS 直传（24 小时签名 URL），
  STS 凭证用 `singleflight` 防并发重复获取
- **Prompt 智能优化**：文本 / 看图两种模式，命中 AccessDenied 时自动降级重试
- **任务下载**：`GET /api/download/:taskId/:index` 登录且仅本人，服务端持有真实上游 URL 并做重定向跳数与域名白名单限制
- **国际化**：中文 / English 双语，界面文案与后端错误码翻译均覆盖
- **历史记录**：按用户分页查询任务列表与状态

### 启动新版

```bash
# 数据库：docker 容器 postgres-17，端口 5432
# 注意：若本机 brew postgresql 在运行会遮蔽该端口，需先 brew services stop postgresql@14

cd server
cp config.yaml.example config.yaml   # 填入 jwt.secret、app_encryption_key、bootstrap.admin_password
make migrate-up
make run               # :8080

cd ../web
npm install
npm run dev            # :5173（端口被占用时 Vite 会自动顺延到 5174~5177，CORS 已放行这个区间）
```

### 配置

后端配置只有一份文件：`server/config.yaml`（已 gitignore，不会被提交；
`server/config.yaml.example` 是提交进仓库的示例，含全部字段与说明）。没有环境
变量覆盖、没有 `.env`——改配置就是编辑这份 YAML 并重启进程。启动时用
`--config` 指定路径，默认是当前工作目录下的 `config.yaml`。

必填三项，缺失或格式不对会在启动时直接拒绝：

| 字段 | 说明 |
|------|------|
| `jwt.secret` | 至少 32 字节，缺失或过短则拒绝启动。生成方式：`openssl rand -base64 32` |
| `app_encryption_key` | base64 编码的 32 字节，缺失或解码后不是 32 字节则拒绝启动。用于加密 `app_settings` 表中的上游 API Key（AES-256-GCM）。生成方式：`openssl rand -base64 32`。**不要随意更换**——更换后已加密存储的旧密钥将无法再解密，需要清空 `app_settings` 表重新配置 |
| `bootstrap.admin_password` | 首次启动且系统内无活跃管理员时，用于自举创建管理员账号 |

其余字段（`http.port`、`database.*`、`jwt.ttl`、`bootstrap.admin_username`、
`cors.origins`）都有默认值，详见 `config.yaml.example` 里的注释。配置文件里
出现未声明的字段名（typo）会在启动时直接报错，不会被静默忽略。

> 注意：图片上传目前**不做服务端压缩**——旧版 README 曾提到 `sharp` 压缩，但旧代码里
> `sharp` 只是被引入、从未被调用；新版 Go 后端同样没有实现压缩，上传即原图（≤12MB 走
> base64 内嵌，超出走 OSS 直传）。

详见 `docs/superpowers/specs/2026-07-19-generation-core-design.md`。

---

# AIGC 图片生成，视频生成

基于阿里云百炼 DashScope API 的 AIGC 图片与视频生成 Web 应用。

## 功能

| 模块 | 说明 |
|------|------|
| **图片生成 (Image Gen)** | 文生图、图片编辑、多图融合、组图生成 |
| **参考生视频 (R2V)** | 上传 1~9 张参考图 + 可选参考视频，生成参考风格视频 |
| **图生视频 (I2V)** | 上传首帧图（可选尾帧/视频续写），从静态图生成动态视频 |
| **文生视频 (T2V)** | 纯文本 Prompt 生成视频 |
| **历史记录** | 本地保存所有生成任务，支持查看、下载、复用 |
| **Prompt 自动优化** | 调用 Qwen 模型自动润色 Prompt，提升生成质量 |

## 支持的模型

| 模型 | 用途 | 接口协议 |
|------|------|----------|
| `qwen-image-plus` | 高质量文生图 | DashScope 原生 |
| `qwen-image` | 快速文生图 | DashScope 原生 |
| `qwen-image-edit-plus` | 图片编辑 / 多图融合 | DashScope 原生 |
| `qwen-image-edit` | 图片编辑 | DashScope 原生 |
| `wan2.7-image-pro` | 万相 2.7 图像生成（文生图/编辑/组图） | DashScope 原生 |
| `wan2.7-image` | 万相 2.7 图像生成（基础版） | DashScope 原生 |
| `gpt-image-2` | 文生图 / 图片编辑（t8star 中转站） | OpenAI chat-completions |
| `happyhorse-1.1-r2v` | 多图参考生视频 | DashScope 原生 |
| `happyhorse-1.1-i2v` | 首帧图生视频 | DashScope 原生 |
| `happyhorse-1.1-t2v` | 文生视频 | DashScope 原生 |
| `wan2.7-r2v` | Wan 2.7 参考生视频（图+视频+音色） | DashScope 原生 |
| `wan2.7-i2v-2026-04-25` | Wan 2.7 图生视频（首帧/首尾帧/续写） | DashScope 原生 |
| `wan3.0-video` | 万相 3.0 全能视频（文/图/参考/文件/网页生视频） | DashScope 原生 |
| `wan3.0-video-prime` | 万相 3.0 全能视频·高速版，接口与标准版完全一致 | DashScope 原生 |
| `qwen3.7-plus` | 文本 Prompt 优化 | OpenAI 兼容 ✅ |
| `qwen-vl-max-latest` | 多模态 Prompt 优化（看图润色） | DashScope 原生 |

## 技术栈

- **后端**: Node.js + Express
- **前端**: 原生 HTML/CSS/JS（无框架），深色主题 SPA
- **API**: 阿里云百炼 DashScope

## 快速开始

### 1. 安装依赖

```bash
npm install
```

### 2. 启动服务

```bash
npm start
# 默认端口 3000，可通过环境变量修改
PORT=8080 npm start
```

### 3. 打开浏览器

访问 [http://localhost:3000](http://localhost:3000)

### 4. 配置

在左侧边栏点击 **⚙️ 设置**，填入：

- **API Key** — 从[阿里云百炼控制台](https://bailian.console.aliyun.com/)获取 DashScope API Key
- **地域** — 选择与 API Key 匹配的地域（北京/新加坡/弗吉尼亚/法兰克福）
- **WorkspaceId** — 仅法兰克福地域需要
- **接入点（Endpoint）** — 官方百炼 / 蓝星纪元中转站 / **t8star 中转站** / 自定义

> 设置保存在浏览器 localStorage，API Key 不会上传到任何第三方。

#### 关于 t8star 中转站

接入点选 **t8star 中转站** 后，整个应用切换到该服务商，行为与百炼不同：

- **API Key 字段代表 t8star 的 Key** —— 与 DashScope Key 分开保存，切换接入点时自动切换，互不覆盖
- **地域与 WorkspaceId 不适用**，自动隐藏
- **图片模型只有 `gpt-image-2`**，图片生成与图片编辑都可用（编辑支持多图输入）
- **视频生成不可用** —— t8star 不提供视频接口，三个视频页会给出提示并拦截提交

要同时用两家，回设置页切换接入点即可。

## 项目结构

```
omnigen-ai/
├── server.js              # Express 后端，API 代理
├── package.json
├── .env.example           # 环境变量示例
└── public/
    ├── index.html         # HTML 页面骨架
    ├── css/
    │   └── styles.css     # 全部样式（深色主题 + 响应式）
    └── js/
        ├── app.js         # 核心：初始化、设置、认证、导航
        ├── task.js         # 任务：提交、轮询、优化、视频渲染
        ├── history.js     # 历史记录：CRUD、卡片渲染
        ├── imggen.js      # 图片生成：文生图、图片编辑逻辑
        ├── imgedit.js     # 图片编辑：多图上传与编辑
        ├── r2v.js         # 参考生视频 + 文生视频逻辑
        └── i2v.js         # 图生视频逻辑
```

## API 接口

后端作为 DashScope API 代理，避免前端直接暴露 API Key：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/config` | 返回模型配置信息 |
| POST | `/api/create-task` | 创建视频生成任务（异步） |
| POST | `/api/generate-image` | 图片生成（文生图/图片编辑，同步返回） |
| POST | `/api/optimize-prompt` | AI 优化 Prompt |
| GET | `/api/task/:taskId` | 查询任务状态 |
| GET | `/api/download?url=...` | 代理下载文件（视频/图片） |
| POST | `/api/upload-image` | 图片上传（压缩 + OSS 存储） |

## 接口协议说明

### 图片生成

- **所有图片模型**（Qwen 系列 + wan2.7 系列）统一使用 DashScope 原生协议（Qwen-Image 系列不支持 OpenAI 协议）
  - 请求地址：`/api/v1/services/aigc/multimodal-generation/generation`
  - 请求格式：`{ model, input: { messages: [{ role: "user", content: [{ text }, { image }] }] }, parameters: { size, n, watermark, thinking_mode, seed, enable_sequential, negative_prompt, prompt_extend } }`
  - 响应格式：`{ output: { choices: [{ message: { content: [{ image: "url", type: "image" }] } }] }, usage: { image_count, size } }`
- **`gpt-image-2`**（t8star）使用 OpenAI chat-completions 协议
  - 请求地址：`{接入点}/v1/chat/completions`，默认 `https://ai.t8star.org`
  - 请求格式：`{ model, stream: false, messages: [{ role: "user", content: string | [{ type: "text", text }, { type: "image_url", image_url: { url } }] }] }`
    —— 无图时 `content` 为字符串，有图时为块数组
  - 响应格式：图片 URL 以 markdown `![image](url)` 内嵌在 `choices[0].message.content` 中，需正则提取；剥离图链后剩余的正文作为「模型说明」展示在结果图下方
  - 不支持 `size` / `n` / `seed` / `watermark` / `negative_prompt` 等参数，选中该模型时相关控件自动隐藏
  - 上游会回显更具体的模型名（如 `gpt-image-2-pro`），历史记录存回显值

### 视频生成 & Prompt 优化

- **文本 Prompt 优化** (`qwen3.7-plus`)：使用 OpenAI 兼容协议 (`/compatible-mode/v1/chat/completions`)
- **多模态 Prompt 优化** (`qwen-vl-max-latest`)：使用 DashScope 原生协议 (`/api/v1/services/aigc/multimodal-generation/generation`)
- **Prompt 优化降级**：若账号未开通主优化模型权限并返回 AccessDenied，文本优化会自动尝试 `qwen-plus`，看图优化会自动尝试 `qwen-vl-plus`
- **视频生成任务**：使用 DashScope 原生协议 (`/api/v1/services/aigc/video-generation/video-synthesis`)
- **任务状态查询**：使用 DashScope 原生协议 (`/api/v1/tasks/{taskId}`)

## 地域与 Endpoint

| 地域 | Endpoint |
|------|----------|
| 华北2（北京） | `https://dashscope.aliyuncs.com` |
| 新加坡 | `https://dashscope-intl.aliyuncs.com` |
| 美国（弗吉尼亚） | `https://dashscope-us.aliyuncs.com` |
| 德国（法兰克福） | `https://{workspaceId}.eu-central-1.maas.aliyuncs.com` |

## OSS 大文件上传（可选）

当上传图片 > 12MB 时，应用会自动上传至阿里云 OSS 并返回签名 URL，避免 Base64 编码膨胀问题。

### 环境变量配置

在 `.env` 文件中配置以下 OSS 相关变量（参考 `.env.example`）：

```bash
OSS_ACCESS_KEY_ID=          # RAM 用户 AccessKey ID（必填）
OSS_ACCESS_KEY_SECRET=      # RAM 用户 AccessKey Secret（必填）
OSS_BUCKET=trans-ai-cn      # OSS Bucket 名称（默认 trans-ai-cn）
OSS_REGION=oss-cn-chengdu   # OSS 地域（默认 oss-cn-chengdu）
OSS_ROLE_ARN=               # RAM 角色 ARN（可选，不填则使用直接 AK/SK 模式）
OSS_TOKEN_EXPIRE_SECONDS=3600  # STS Token 有效期（仅 STS 模式，默认 3600 秒）
```

### 两种认证模式

| 模式 | 条件 | 说明 |
|------|------|------|
| **直接 AK/SK** | `OSS_ACCESS_KEY_ID` + `OSS_ACCESS_KEY_SECRET` 已配置，`OSS_ROLE_ARN` 为空 | 本地开发推荐，直接使用 AK/SK 访问 OSS |
| **STS 临时令牌** | 上述 + `OSS_ROLE_ARN` 已配置 | 生产环境推荐，通过 RAM 角色获取临时凭证，更安全 |

### RAM 角色配置（仅 STS 模式）

需要在阿里云 RAM 控制台创建一个角色，授予该角色 `oss:PutObject` 权限，角色策略示例：

```json
{
  "Version": "1",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["oss:PutObject"],
    "Resource": ["acs:oss:*:*:${BUCKET}/omnigen-uploads/*"]
  }]
}
```

同时需要配置信任策略，允许 RAM 用户（`OSS_ACCESS_KEY_ID`）扮演该角色。

### 安全说明

- **直接 AK/SK 模式**：适合本地开发，AK/SK 仅存储在 `.env` 文件中（已 gitignore）
- **STS 临时令牌模式**：服务器通过 `@alicloud/sts20150401` 动态获取临时令牌（有效期 1 小时），定期自动刷新，不暴露永久 AK/SK
- **最小权限**：RAM 角色仅授予 `oss:PutObject` 权限，无读取/删除/列举权限
- **签名 URL**：OSS 对象为私有访问，通过签名 URL 授权（24 小时有效）
- **图像压缩**：使用 `sharp` 进行服务器端图像压缩（最大 4096×4096px，JPEG Q85），大幅降低文件体积

### 上传流程

- **≤12MB**：压缩后 Base64 编码，直接嵌入请求体（与现有流程兼容）
- **>12MB**：压缩后上传至 OSS，返回 HTTPS 签名 URL，由 DashScope 直接访问
- **未配置 OSS**：≤12MB 文件正常工作，>12MB 文件返回清晰错误提示

## 注意事项

- 图片和视频生成链接 24 小时内有效，请及时下载
- Wan 2.7 系列模型（图片和视频）仅支持北京和新加坡地域
- API Key、模型、Endpoint 必须同一地域，跨地域调用会失败
- 图片上传仅保存在本地浏览器，不经过第三方服务器
