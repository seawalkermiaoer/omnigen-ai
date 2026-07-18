# 接入 t8star gpt-image-2 图像模型

日期：2026-07-18

## 背景

现有应用的全部图像模型（qwen-image 系列、wan2.7 系列）都走 DashScope 原生协议，
共用一把 DashScope API Key。

现在要接入第三方服务商 t8star（`https://ai.t8star.org`）的 `gpt-image-2` 模型。
它与现有模型有两点根本不同：

1. **协议不同** —— OpenAI chat-completions（`POST /v1/chat/completions`），不是 DashScope 原生。
2. **凭证不同** —— 另一把 Key，与 DashScope Key 无关。

该模型同时支持文生图和图片编辑，因此在「图片生成」和「图片编辑」两个模块都要可用。

## 接口实测结论

以下均为实际调用验证的结果，非推测。

### 请求

```
POST https://ai.t8star.org/v1/chat/completions
Authorization: Bearer <t8star key>
Content-Type: application/json
```

文生图 —— `content` 为字符串：

```json
{ "model": "gpt-image-2", "stream": false,
  "messages": [{ "role": "user", "content": "画只猫" }] }
```

图片编辑 —— `content` 为数组，`text` 块 + 一个或多个 `image_url` 块：

```json
{ "model": "gpt-image-2", "stream": false,
  "messages": [{ "role": "user", "content": [
    { "type": "text", "text": "修改这个图片" },
    { "type": "image_url", "image_url": { "url": "<url or data uri>" } }
  ]}]}
```

### 响应

```json
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "model": "gpt-image-2-pro",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "![image](https://webstatic.aiproxy.vip/output/xxx.png)\n\n给你画好了，一只可爱的小猫。"
    },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 3780, "completion_tokens": 883, "total_tokens": 4663 }
}
```

### 关键约束

| 项 | 结论 | 影响 |
|---|---|---|
| 图片位置 | markdown `![image](url)` 内嵌在正文里，后面跟一段模型说明 | 需正则提取，不能直接取字段 |
| `data:` URI 输入 | **支持**（已用 64×64 PNG base64 实测） | 现有 base64 上传流程无需改动 |
| 多图输入 | **支持**（已用两张图拼图实测） | imgedit 多图融合可用 |
| `size` / `n` / `seed` / `watermark` / `negative_prompt` | **不支持**，聊天接口没有 parameters | 选中该模型时必须隐藏这些控件 |
| `usage` | token 计数，无 `image_count` / `size` | 现有 `usage?.size` 渲染为空串，不会报错 |
| 回显 `model` | 请求 `gpt-image-2`，回显 `gpt-image-2-pro` | 历史记录存回显值，便于追溯实际模型 |

## 架构

### 核心决策：「接入点」下拉升级为服务商选择器

现有的「接入点（Endpoint）」下拉只是 DashScope 的 base URL 选择器 ——
它的值喂给 `resolveEndpoint()`，图片、视频、任务轮询三条链路共用，
且共用设置页里那把 DashScope Key。

t8star 与它们协议不同、Key 不同。把 t8star 放进这个下拉，
等于把它的语义从「DashScope 走哪个地址」改成「用哪家服务商」。
这是本次改动的主要成本所在，取舍如下：

| | 收益 | 代价 |
|---|---|---|
| 放进下拉 | 用户心智一致：一个地方选服务商；不新增 Key 字段 | 下拉值决定协议，模型列表、鉴权、视频可用性都要跟着变 |

选定放进下拉。由此派生出一个贯穿前端的概念 **provider**：

```
endpoint === 'https://ai.t8star.org'  →  provider = 't8star'
其它任何值（含空、蓝星、自定义）        →  provider = 'dashscope'
```

provider 是**从 endpoint 推导**出来的，不单独存 localStorage —— 避免两处状态不一致。

**连带影响：选中 t8star 后，设置页的 API Key 字段代表 t8star 的 Key**，
不是 DashScope 的。两把 Key 分开存（`hh_api_key` / `hh_t8_api_key`），
切换接入点时输入框内容跟着切换，否则用户切回百炼会发现 Key 被覆盖没了。

### 后端

**`lib/providers/t8star.js`（新建）** —— 三个纯函数，不做网络 IO：

- `buildPayload({ model, prompt, images })` → 请求体。`images` 为空时 `content` 用字符串，否则用数组。
- `parseResponse(data)` → `{ images: string[], note: string }`。
  用全局正则 `/!\[[^\]]*\]\((https?:\/\/[^)\s]+)\)/g` 提取全部图链；
  剥离图链后剩余的正文 trim 后作为 `note`。
- `resolveBaseUrl(input)` → 规范化 base URL，非 http 值回落到 `https://ai.t8star.org`。

抽成独立模块的理由：`server.js` 已 666 行；纯函数可脱网单测，
项目已有 `tests/server-utils.test.js` 的先例。

**`server.js` 改动** —— `/api/generate-image` 按协议分支：

- 新增 `MODELS.IMAGE_OPENAI = ['gpt-image-2']`。
- 模型白名单校验放宽为 `MODELS.IMAGE` 与 `MODELS.IMAGE_OPENAI` 的并集。
- 命中 `IMAGE_OPENAI` 时：base URL 取**现有的 `endpoint` 字段**（经 `resolveBaseUrl` 规范化），
  Bearer 取**现有的 `apiKey` 字段**，走 `/v1/chat/completions`，用上述纯函数拼装与解析。
- 命中 `MODELS.IMAGE` 时：完全走现有 DashScope 逻辑，不改。

**请求契约也不变** —— 方案 B 的一个实际好处：`endpoint` 与 `apiKey` 都是请求体里已有的字段，
后端不需要新增 `t8ApiKey` / `t8BaseUrl`，路由开头的解构一行都不用动。

**响应契约不变** —— 仍返回 `{ images, usage, model }`，新增可选 `note`。
因此历史记录、下载、结果渲染全部零改动。

### 前端

**设置页（`index.html` + `app.js`）**

- Endpoint 下拉新增一项：`t8star 中转站（ai.t8star.org）`，value 为 `https://ai.t8star.org`。
- 选中 t8star 时：隐藏「地域」与「WorkspaceId」两块（对该服务商无意义），
  API Key 的 label 与说明文案切换为 t8star 版本。
- 两把 Key 分开持久化。切换下拉时把当前输入框内容写回**原 provider** 的槽位，
  再载入**新 provider** 的槽位。
- 保存设置后广播 `providerchange` 事件 —— 模型下拉、控件显隐、视频页提示都靠它刷新。
  现有模块已经在监听 `localechange` 做同类刷新，沿用同一套写法即可。

`getAuth()` 不变（`apiKey` + `endpoint` 已够用）。
`checkAuth()` 增加：provider 为 t8star 时跳过法兰克福 workspaceId 校验（与地域无关）。

**模型下拉（`imggen.js` / `imgedit.js`）—— 按 provider 切换整个列表**

不是「追加一个选项」，而是**换一份列表**：

- provider = t8star → 只有 `gpt-image-2`
- provider = dashscope → 现有四项，不变

理由：t8star 不认 qwen/wan 模型名，列出来就是一堆必然报错的选项。

**控件联动**

选中 `gpt-image-2` 时隐藏该接口不支持的控件：尺寸、数量、水印、种子、
反向提示词、thinking_mode、prompt_extend。
复用现有 `data-imggen-wan` / `data-imgedit-wan` 那套 `style.display` 切换写法，
不引入新机制。数量固定为 1 —— 接口不支持 `n`，也不通过循环调用去凑（YAGNI）。

**视频模块闸门**

t2v / i2v / r2v 三个视频页与任务轮询都走 DashScope 协议，t8star 不提供。
所有视频提交都收敛在 `task.js` 的 `submitTask()` 一个入口，
在那里加一道 provider 闸门：provider 为 t8star 时直接 alert 并 return，
文案明确指引「切回官方百炼」。同时在三个视频页顶部显示一条常驻提示条，
让用户在点提交之前就知道，而不是点了才被拒。

**note 展示**

模型返回的说明文字（例："我还可以帮你改成：1. 更扁平的纯色 logo…"）
渲染在结果图下方。不展示等于白扔掉一段有用输出。

### i18n

`zh-CN.json` 与 `en.json` 同步新增：Endpoint 新选项名、t8star 版的 API Key label
与说明、模型下拉选项名、note 区域标题、视频页不可用提示条与拦截 alert 文案。

## 错误处理

- t8star 返回 OpenAI 错误结构 `{ error: { message, type, code } }`，
  现有代码已有 `r.data?.error?.message` 分支，直接复用。
- 正文中匹配不到任何图链时，返回 502 与现有 `server.noImageResult` 文案一致，
  并带上 `raw` 便于排查。
- 超时沿用现有 180s。

## 测试

`tests/` 下新增 `t8star.test.js`，全部脱网，覆盖：

- `buildPayload`：无图时 `content` 为字符串；有图时为数组且 `image_url` 块数量正确。
- `parseResponse`：单图正常提取；多图全部提取；note 正确剥离图链；
  无图链时返回空数组。

用真实响应样本（本文档「响应」一节）作为测试夹具。

## 不做的事

- 不支持 `n > 1` 的多图生成（接口无此能力，不做客户端循环）。
- 不把 t8star Key 放进 `.env`（与现有「用户自带 Key、存 localStorage」的模式保持一致）。
- 不改动任何现有 DashScope 模型的行为。
- **不支持「同时用两家」** —— 接入点是全局单选，选了 t8star 就用不了 qwen/wan 和视频。
  这是方案 B 的固有代价，已知并接受；要并用得回设置页切一下。
- 不为 t8star 做视频能力探测或降级重试 —— 它就是不提供，直接挡住比试错友好。

## 安全备注

联调用的 t8star Key 在对话中以明文出现过，不写入仓库任何文件、不提交。
联调完成后建议在 t8star 后台轮换该 Key。
