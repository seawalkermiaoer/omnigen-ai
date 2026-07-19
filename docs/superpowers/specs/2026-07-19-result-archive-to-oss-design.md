# 生成结果归档到 OSS

- 日期：2026-07-19
- 分支：`rewrite-react-go`
- 前置：生成核心已跑通（2026-07-19 首次真实出图，task id 10）；`internal/pkg/ossx` 已用于参考图上传

## 背景与真正的问题

数据库 `generation_tasks.result_urls` 里存的是**上游返回的 URL**：

```
https://webstatic.aiproxy.vip/output/8ddfbc12-....png     （t8star）
https://dashscope-result-*.oss-*.aliyuncs.com/....        （DashScope）
```

这类上游结果链接是短期有效的。也就是说**历史记录会烂掉**——过一段时间回去看，老任务的图和视频全是坏链，而「历史记录」这个功能的全部价值就建立在结果还能打开上。

用户最初提的是「复制链接复制出来的应该是 OSS 链接」，但那只是症状。当前「复制链接」按钮复制的是：

```
http://localhost:5173/api/download/{taskId}/{index}
```

这个地址需要 `Authorization: Bearer` 头才能打开（见 `web/src/api/generation.ts` 的
`downloadLinkPath` 注释，那里明确写了「不是一个可脱离登录态分享的公开 URL」）。
所以粘贴到任何地方都打不开——按钮名叫「复制链接」，复制出来的却不是一个链接。

两件事同一个根因：**结果没有落到我们自己掌控的存储里**。

## 已确定的决策

| 决策 | 结论 | 理由 |
|---|---|---|
| 链接性质 | 对象设为 public-read，永久公开 URL | 「复制链接」的语义就是拿到一个能发出去的地址。签名 URL 24 小时后失效，分享场景基本不可用 |
| 历史数据 | 不回填，只对新任务生效 | 老任务的上游链接大概率已经失效，回填多半也抓不回来，不值得为此写一次性迁移 |
| 覆盖范围 | 图片与视频都归档 | 视频同样面临上游链接失效 |

**public-read 的代价要说清楚**：拿到 URL 的任何人无需登录即可查看该结果，
且 bucket 需允许公共读。这是用户明确选择的权衡，不是默认值。

## 架构

新增 `internal/service/result_archive.go`，一个只做一件事的服务：
**把一组上游结果 URL 抓下来、传到 OSS、返回替换后的 URL 列表。**

```go
type ResultArchiver interface {
    Archive(ctx context.Context, taskID int64, urls []string) []string
}
```

### 绝不把成功变成失败

`Archive` **不返回 error**。这是刻意的。

归档是一个事后增强动作，此时上游已经真的出了图、token 已经真的扣了、
配额已经真的扣了。如果 OSS 没配、网络抖了、或者对象存储拒绝了这次写入，
正确的行为是**保留原始上游 URL 并记录日志**，而不是把一次已经付过钱的成功
生成翻译成一个失败——那样用户既损失了钱又损失了结果，且会重试，再付一次钱。

逐条降级，不是整批降级：三张图里第二张传失败，另外两张仍然用 OSS 链接，
第二张退回上游链接。

### 抓取上游内容的安全约束

`Archive` 会对上游 URL 发起 GET，这是一条新的「服务端按 URL 取内容」路径，
必须复用 `handler.IsAllowedResultHost` 那份域名白名单——这正是当初
`/api/download` 从「无鉴权开放代理」改造过来时防的 SSRF，不能在这里开一个新口子。

- 初始 URL 与**每一跳重定向**都校验 host（与 download.go 同样的执行路径）
- 重定向上限沿用 download.go 的 `maxDownloadRedirects`
- 响应体有大小上限 `maxArchiveBytes`，超过即放弃归档、退回上游 URL

### 流式上传，不把视频读进内存

现有 `ossx.Store.Put(ctx, key, body []byte, contentType)` 把整个文件读进内存。
参考图（≤50MB）可以接受，视频不行。

给 `Store` 增加一个方法，接 `io.Reader` 而不是 `[]byte`：

```go
// PutPublic 以 public-read ACL 上传，返回该对象的永久公开 URL。
PutPublic(ctx context.Context, key string, body io.Reader, contentType string) (string, error)
```

阿里云 SDK 的 `bucket.PutObject` 本就接受 `io.Reader`，直接把 HTTP 响应体
接过去即可，全程不落地、不整体缓冲。

公开 URL 形如：`https://{bucket}.{region}.aliyuncs.com/{key}`
（`region` 是 `oss-cn-chengdu` 这种短形式，与 `ossx.Config.endpoint()` 一致）。

**保留现有的 `Put` 与 `SignedURL` 不动**——参考图上传走的是私有 + 签名 URL 这条路，
它的安全模型没有变，不应该被这次改动顺带公开化。

### 对象键

```
results/{taskID}/{index}-{8位随机}{ext}
```

带随机段是为了让键不可枚举：public-read 之下，可预测的键意味着任何人都能
遍历出别人的生成结果。`{ext}` 从 Content-Type 推导，复用 download.go 里已有的
`extFromContentType` / `sanitizeExt`。

## 挂载点

| 位置 | 时机 |
|---|---|
| `internal/service/generation_image.go` | 上游同步返回图片之后、把结果写进 `generation_tasks` 之前 |
| `internal/worker/poller.go` | 视频任务轮询到 SUCCEEDED、把结果写进 `generation_tasks` 之前 |

两处都是「拿到 result URLs、写库前」这一个点，归档在写库前完成，
**数据库里从一开始就只存最终 URL**，不存在「先存上游、再更新成 OSS」的中间态，
也就不需要考虑并发更新与部分更新。

## 下载路径必须同步放行 OSS 域名

**这是最容易漏、漏了就当场全线坏掉的一点。**

`server/internal/handler/download.go` 的 `allowedResultHostSuffixes` 是一份
上游域名白名单。归档之后 `result_urls` 里存的是 OSS 域名，而「下载」按钮
走的仍然是 `GET /api/download/:taskId/:index`——**白名单里没有 OSS 域名，
所有新任务的下载会全部被拒**。

必须把当前配置的 OSS bucket 域名一并放行。注意它是**运行时配置**
（bucket/region 存在 `app_settings` 里），不是编译期常量，所以不能简单地
往那个字符串切片里加一条，需要让 host 校验能读到当前 OSS 配置。

## 前端

「复制链接」改为直接复制 `task.resultUrls[index]` 本身——那已经是一个
可公开访问的 OSS 地址，正是这个按钮该给出的东西。

「下载」保持走 `/api/download/:taskId/:index` 不变：它带鉴权、有
Content-Disposition、文件名是我们拼的，体验优于让浏览器直接打开一个 OSS 裸链。

老任务的 `resultUrls` 仍是上游链接，复制出来可能已失效——这比复制一个
需要 Bearer 头、必然打不开的内部路径仍然更有用，不做特殊处理。

## 测试要点

- OSS 未配置时，生成仍然成功，`result_urls` 退回上游 URL，且**不返回错误**
- 上游抓取失败（404 / 超时 / 超过 `maxArchiveBytes`）时同上
- 三条结果中第二条归档失败 → 第一、三条是 OSS URL，第二条是上游 URL（逐条降级，不是整批）
- 归档成功时写进库的是 OSS URL，且**再没有任何一条上游 URL 残留**
- 抓取上游时的 host 白名单：不在白名单的 host 直接拒绝，**重定向到白名单外的 host 同样拒绝**（初始 URL 与每一跳都要单独测）
- 对象键不可枚举：同一 taskID/index 两次归档产生不同的键
- `PutPublic` 上传时确实带了 public-read ACL，且返回的 URL 形状正确
- `/api/download` 对 OSS 域名放行，且**这份放行不能宽到把整个 `*.aliyuncs.com` 都打开**
- 视频归档全程不整体缓冲（用一个会在被整体读入时报错的 reader 断言流式）
- 前端：「复制链接」复制的是 `resultUrls[index]`，不是 `/api/download/...`

## 明确不做

- 回填历史任务
- OSS 对象的生命周期/过期清理策略
- 图片压缩、转码、缩略图
- 结果去重（同一张图被生成两次会存两份）
- 让用户选择「这次结果不要归档」
