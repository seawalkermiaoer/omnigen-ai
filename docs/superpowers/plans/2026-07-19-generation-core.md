# 配置与生成核心 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** 把旧系统的全部生成能力移植到新栈——配置与密钥落库、模型目录、图片生成/编辑、三种视频生成、上传与 OSS、prompt 优化、任务轮询、结果下载。

**Architecture:** 沿用子项目 1 的 handler → service → repository 分层与 wire 装配。新增 `internal/provider/` 上游客户端层（dashscope / t8star 两种协议），`internal/model/catalog` 单一模型目录，`generation_tasks` 表持久化任务并由后端 worker 轮询。凭证只存服务端，不再经过前端。

**设计文档：** `docs/superpowers/specs/2026-07-19-generation-core-design.md`

---

## 权威参考

**这是一次移植，旧代码就是规格。** 每个任务都必须先读对应的旧实现，不要凭记忆写协议：

| 内容 | 旧代码位置 |
|---|---|
| 全部端点与上游转发 | `server.js` |
| t8star 协议 | `lib/providers/t8star.js` |
| 7 套系统提示词 | `server.js:443-550`（逐字保留） |
| 模型目录 | `server.js:169-184` |
| 参数矩阵 | `public/js/{r2v,i2v,imggen,imgedit}.js` |
| 轮询 | `public/js/task.js:102-152` |
| 真实 t8star 响应夹具 | `docs/superpowers/plans/2026-07-18-t8star-gpt-image-2.md` |

设计文档里列了「必须原样保留的行为」和「明确不移植的旧 bug」——动手前读一遍。

---

# Phase A：配置与密钥（子项目 2）

## Task 1: 加密原语

**Files:** `server/internal/pkg/crypto/crypto.go` + `crypto_test.go`

AES-256-GCM，密钥来自 `APP_ENCRYPTION_KEY`（32 字节 base64，缺失或长度不符则 `config.Load` 拒绝启动）。

```go
func Encrypt(plaintext, aad string) (string, error)   // 返回 iv:tag:ciphertext，三段 base64
func Decrypt(ciphertext, aad string) (string, error)
func Mask(secret string) string                        // sk-abc...wxyz
```

**AAD 是必须的，不是可选的。** 调用方传 setting key。没有 AAD 的话，把 DashScope Key 的密文拷到 t8star Key 那一行，解密会成功并返回一个有效但完全错误的凭证——这是本任务唯一真正的安全要求。

**测试（先写）：**
- 加解密往返
- 每次加密 IV 不同（同一明文两次密文不同）
- 篡改密文任一段 → 解密失败
- **AAD 不匹配 → 解密失败**（用 `Encrypt(s,"dashscope_key")` 的结果去 `Decrypt(c,"t8star_key")`）
- 段数不为 3 → 报格式错误，不 panic
- `Mask` 不泄露中间部分；空串与短串不 panic
- 错误信息里不含明文或密钥

`config.Load` 增加 `APP_ENCRYPTION_KEY` 校验并补测试；`.env.example` 记录生成方式。

## Task 2: app_settings 表与 repository

**Files:** migration `000003_create_app_settings.{up,down}.sql`、`internal/model/setting/{types,request,response}.go`、`internal/repository/setting.go` + 测试

表结构见设计文档。`Get(ctx,key)` / `GetAll(ctx)` / `Upsert(ctx,key,value,isSecret,updatedBy)` / `UpsertMany`（同一事务）。

设置项 key：`dashscope_api_key`、`t8star_api_key`、`region`、`endpoint`、`workspace_id`、`oss_access_key_id`、`oss_access_key_secret`、`oss_bucket`、`oss_region`、`oss_role_arn`。

**响应体只暴露脱敏值。** `SettingResponse` 对 secret 项返回 `{configured: bool, masked: string}`，绝不返回明文——加断言测试。

repository 测试跑真实 Postgres，沿用 `withTx` 回滚隔离。

## Task 3: 模型目录

**Files:** `internal/model/catalog/catalog.go` + 测试

把 `server.js:169-184` 的 `MODELS` **加上散落在前端的视频模型 id** 收敛成一份结构化目录。每个模型描述：id、能力、协议、尺寸集合、数量上限、支持的可选参数、region 限制。

```go
type Capability string // t2i, edit, t2v, i2v, r2v, optimize_text, optimize_vision
type Protocol string   // dashscope, openai

type Model struct {
    ID           string
    Capabilities []Capability
    Protocol     Protocol
    Sizes        []string   // qwen 固定像素 / wan 1K,2K,4K；视频为空
    MaxN         int
    Supports     []string   // thinking_mode, enable_sequential, negative_prompt, prompt_extend, seed, watermark
    Regions      []string   // 空=不限；wan2.7 系列为 [cn-beijing, ap-southeast-1]
    MaxImages    int
}
```

**测试：** 每个旧系统出现过的 model id 都在目录里（含视频三种）；wan2.7 系列带 region 限制；`qwen-image-edit-plus` MaxN=6、`qwen-image-edit` MaxN=1、wan 系列 MaxN=4（sequential 时 12）；`gpt-image-2` 协议为 openai 且不具备任何视频能力；按能力查询返回正确集合。

## Task 4: 设置 service + handler + catalog 接口

**Files:** `internal/service/setting.go`、`internal/handler/setting.go`、`internal/handler/catalog.go` + 测试，router 注册

- `GET /api/settings`（登录）返回脱敏配置
- `PUT /api/settings`（admin）写入，secret 项加密，AAD 用 setting key
- `POST /api/settings/test`（admin）用当前配置试调上游最轻量的接口验证凭证
- `GET /api/catalog`（登录）返回目录

**测试：** 普通用户 PUT 得 403；响应体不含明文密钥（断言不出现 `sk-`）；写入后读回是脱敏值；空字符串表示"不修改"而非"清空"（否则前端提交表单会把没动的密钥抹掉——这是个真陷阱）。

## Task 5: 设置页（前端）

**Files:** `web/src/pages/SettingsPage.tsx`、`web/src/api/setting.ts`、`web/src/types/setting.ts` + 测试，路由与导航接入（admin only）

密钥输入框 placeholder 显示脱敏值，留空表示不修改。"测试连接"按钮调 `/api/settings/test`。

---

# Phase B：上游与生成（子项目 3）

## Task 6: provider 接口与 dashscope 客户端

**Files:** `internal/provider/provider.go`、`internal/provider/dashscope/*.go` + 测试

接口：
```go
type ImageProvider interface { GenerateImage(ctx, req) (*ImageResult, error) }
type VideoProvider interface { CreateVideoTask(ctx, req) (string, error); PollTask(ctx, taskID) (*TaskResult, error) }
type OptimizeProvider interface { Optimize(ctx, req) (string, string, error) }
```

dashscope 实现四条上游路径（见设计文档表）。**严格照抄参数判空条件**——`size`/`negative_prompt` 真值判断，其余 `!= null`。region → base URL 映射含 eu-central-1 的 workspaceId 模板；缺 workspaceId 返回 `VALIDATION_FAILED` 而非 500（旧系统这里是 500，是个 wart）。

**测试用 `httptest.Server` 断言发出的请求体字节**，不是断言函数被调用：
- 图片生成的 content 数组是**图在前文在后**
- `n=0` / `watermark=false` / `seed=0` 会被发送
- `size=""` / `negative_prompt=""` 会被省略
- 视频创建带 `X-DashScope-Async: enable`，图片生成不带
- 超时按配置生效

## Task 7: t8star 客户端

**Files:** `internal/provider/t8star/*.go` + 测试

照抄 `lib/providers/t8star.js` 的三个函数。Go 正则无状态，不需要 JS 那套重建 regex 的规避。

**测试必须用真实响应夹具**（从 `docs/superpowers/plans/2026-07-18-t8star-gpt-image-2.md` 取），断言：
- content 是**文在前图在后**（与 dashscope 相反）
- 无图时 content 是**裸字符串**而非数组
- 从真实报文抠出全部图片 URL
- `note` 是剥离 markdown 图片链接后 trim 的散文
- 多次调用结果稳定（JS 那个 `lastIndex` 坑的回归测试）

## Task 8: prompt 优化 service

**Files:** `internal/service/optimize.go`、`internal/model/generation/prompts.go` + 测试

7 套系统提示词**逐字**从 `server.js:443-550` 搬过来。`sourceDesc` / `draftText` 的三种分支拼装逻辑照抄。

降级链：模型外层、endpoint 内层，仅 AccessDenied 触发。`isAccessDeniedResponse` 的两个正则保持差异（code 无空格 / message 有空格）。

**修掉旧 bug**：图片编辑一律用 `imggen_edit`，不再有无图时退化成 `t2v` 的路径。

**测试：** 七个 mode 各自选中正确提示词；有图走 vision 协议、无图走 compatible-mode；AccessDenied 触发降级且顺序正确、次数不超 4；非 AccessDenied 错误立即返回不重试；未知 mode 的处理。

## Task 9: 上传与 OSS

**Files:** `internal/service/upload.go`、`internal/pkg/ossx/*.go` + 测试，`internal/handler/upload.go`

12MB 阈值、base64 data URI vs OSS、24 小时签名 URL、object key 格式 `omnigen-uploads/<epoch-ms>-<16hex>.<ext>`。

**STS 凭证缓存用 `golang.org/x/sync/singleflight`**——旧系统没有并发保护，两个大文件同时上传会重复 AssumeRole。

**测试：** 阈值边界（正好 12MB 走 base64，多 1 字节走 OSS）；MIME 白名单；50MB 上限；OSS 未配置时 >12MB 返回明确错误；singleflight 生效（并发 10 次只 AssumeRole 一次）。

## Task 10: generation_tasks 表与 repository

**Files:** migration `000004_create_generation_tasks.{up,down}.sql`、`internal/model/generation/{types,request,response}.go`、`internal/repository/task.go` + 测试

表结构见设计文档。方法：`Create`、`GetByID`、`GetByIDForUser`（归属过滤）、`ListForUser`（分页）、`UpdateStatus`、`UpdateResult`、`ClaimPending`（供 worker 取待轮询任务）。

**测试：** 归属隔离（用户 A 取不到用户 B 的任务，返回 NOT_FOUND 而非 FORBIDDEN，不泄露存在性）；`ClaimPending` 只返回 PENDING/RUNNING；JSONB 字段往返。

## Task 11: 图片生成 service + handler

**Files:** `internal/service/generation_image.go`、`internal/handler/generation.go` + 测试

按目录校验 model 与参数组合 → 取配置里的凭证 → 选 provider → 调用 → 落 `generation_tasks`（同步完成，直接 SUCCEEDED/FAILED）。

**测试：** 目录校验拦住非法组合（如 `qwen-image` 要 n=2）；wan2.7 在 us-east-1 被拒；凭证未配置时返回明确错误码；响应体不含凭证。

## Task 12: 视频任务 service + 轮询 worker

**Files:** `internal/service/generation_video.go`、`internal/worker/poller.go` + 测试

创建任务落库并返回**本地 task id**（不是上游 id）。worker 定期 `ClaimPending`，按 15 秒间隔轮询上游，更新表。

**修掉旧 bug**：`UNKNOWN` 不再是终态——连续 5 次 UNKNOWN/网络失败才判失败；任务最长 30 分钟后超时终止。

**测试：** 状态机流转；连续 UNKNOWN 不会立刻终止但达阈值会；超时终止；worker 优雅停机不丢任务。

## Task 13: 下载接口

**Files:** `internal/handler/download.go` + 测试

`GET /api/download/:taskId/:index`，登录且仅本人。服务端从任务记录里取真实 URL 转发，前端拿不到上游 URL。

**修掉旧 bug**：限制重定向跳数（最多 3 跳），按上游域名白名单，要求登录。

**测试：** 他人任务返回 404；越界 index 返回 422；重定向跳数超限被拒；非白名单域名被拒。

## Task 14: wire 装配与路由

**Files:** `internal/wire.go`、`wire_gen.go`、`internal/router/router.go`、`cmd/server/main.go`

注册全部新 provider/service/handler，启动轮询 worker 并接入优雅停机。`make wire` 生成并提交。

**验证：** 起服务跑完整 curl 走查——配置写入 → 目录 → 上传 → 图片生成 → 视频创建 → 轮询到终态 → 下载。

## Task 15: 前端生成页面共用组件

**Files:** `web/src/components/generation/*`、`web/src/api/generation.ts`、`web/src/types/generation.ts` + 测试

- `ModelSelect` — 由目录驱动，按能力过滤
- `ParamPanel` — **由目录里的参数约束渲染**，不为五个页面各写一份表单
- `MediaUploader` — 多图上传、排序、删除、缩略图；i2v 的尺寸与比例校验按模型取限制
- `PromptInput` — 输入 + 优化按钮 + 撤销
- `ResultPanel` — 图片网格 / 视频播放器、下载、复制链接

参数面板由目录生成是这一段的关键——否则五个页面会长出五份几乎相同又互相漂移的表单。

## Task 16: 五个生成页面

**Files:** `web/src/pages/generation/{ImageGenPage,ImageEditPage,T2VPage,I2VPage,R2VPage}.tsx` + 测试，路由替换占位页

按参数矩阵接线。i2v 的三种任务类型子标签、r2v 的参考视频 URL 与 reference_voice、各自的校验规则。

**测试：** 每页的必填校验；i2v 图片尺寸/比例校验按模型切换；wan region 限制提示；提交后轮询到结果。

## Task 17: 联调与回归

起后端与前端，浏览器实测五种生成各跑通一次（需要真实 API Key——若无凭证，明确报告哪些项未验证，不要假装通过）。两侧全量测试、`tsc -b --force`、`npm run build`、`go vet`、`gofmt`。更新 README。

---

## 完成标准

- [ ] 后端 `go test ./...` / `go vet` / `gofmt -l` 全绿
- [ ] 前端 `npm test` / `npx tsc -b --force` / `npm run build` 全绿
- [ ] 五种生成方式在浏览器实测跑通（或明确列出未验证项与原因）
- [ ] 凭证不出现在任何响应体、日志、前端代码或 localStorage
- [ ] 旧系统 `npm start`（:3000）仍可独立运行
