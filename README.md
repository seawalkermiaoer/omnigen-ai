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
| `happyhorse-1.0-r2v` | 多图参考生视频 | DashScope 原生 |
| `happyhorse-1.0-i2v` | 首帧图生视频 | DashScope 原生 |
| `wan2.7-r2v` | Wan 2.7 参考生视频（图+视频+音色） | DashScope 原生 |
| `wan2.7-i2v-2026-04-25` | Wan 2.7 图生视频（首帧/首尾帧/续写） | DashScope 原生 |
| `qwen3.7-max` | 文本 Prompt 优化 | OpenAI 兼容 ✅ |
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

> 设置保存在浏览器 localStorage，API Key 不会上传到任何第三方。

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

### 视频生成 & Prompt 优化

- **文本 Prompt 优化** (`qwen3.7-max`)：使用 OpenAI 兼容协议 (`/compatible-mode/v1/chat/completions`)
- **多模态 Prompt 优化** (`qwen-vl-max-latest`)：使用 DashScope 原生协议 (`/api/v1/services/aigc/multimodal-generation/generation`)
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
