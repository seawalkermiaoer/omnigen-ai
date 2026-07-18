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

### 后端

**`lib/providers/t8star.js`（新建）** —— 两个纯函数，不做网络 IO：

- `buildPayload({ model, prompt, images })` → 请求体。`images` 为空时 `content` 用字符串，否则用数组。
- `parseResponse(data)` → `{ images: string[], note: string }`。
  用全局正则 `/!\[[^\]]*\]\((https?:\/\/[^)\s]+)\)/g` 提取全部图链；
  剥离图链后剩余的正文 trim 后作为 `note`。

抽成独立模块的理由：`server.js` 已 666 行；纯函数可脱网单测，
项目已有 `tests/server-utils.test.js` 的先例。

**`server.js` 改动** —— `/api/generate-image` 按协议分支：

- 新增 `MODELS.IMAGE_OPENAI = ['gpt-image-2']`。
- 模型白名单校验放宽为 `MODELS.IMAGE` 与 `MODELS.IMAGE_OPENAI` 的并集。
- 命中 `IMAGE_OPENAI` 时：base URL 取请求体的 `t8BaseUrl`（缺省 `https://ai.t8star.org`），
  Bearer 取 `t8ApiKey`，走 `/v1/chat/completions`，用上述两个纯函数拼装与解析。
- 命中 `MODELS.IMAGE` 时：完全走现有 DashScope 逻辑，不改。

**响应契约不变** —— 仍返回 `{ images, usage, model }`，新增可选 `note`。
因此历史记录、下载、结果渲染全部零改动。

### 前端

**设置页（`index.html` + `app.js`）**

新增两个字段，与现有 DashScope 配置并列：

- `gpt-image-2 API Key` → localStorage `hh_t8_api_key`
- `Base URL` → localStorage `hh_t8_base_url`，默认 `https://ai.t8star.org`

`getAuth()` 追加返回 `t8ApiKey` / `t8BaseUrl`。
两个提交路径都是 `JSON.stringify({ ...auth, ... })`，所以字段自动透传，无需改调用点。

`checkAuth()` 改为模型感知：选中 `gpt-image-2` 时校验 `t8ApiKey`
（而非 `apiKey`），且跳过法兰克福 workspaceId 校验（与地域无关）。

**模型下拉（`imggen.js` / `imgedit.js`）**

`getImggenModels()` 与 `getImgeditModels()` 各追加 `gpt-image-2` 选项。

**控件联动**

选中 `gpt-image-2` 时隐藏该接口不支持的控件：尺寸、数量、水印、种子、
反向提示词、thinking_mode、prompt_extend。
复用现有 `data-imggen-wan` / `data-imgedit-wan` 那套 `style.display` 切换写法，
不引入新机制。数量固定为 1 —— 接口不支持 `n`，也不通过循环调用去凑（YAGNI）。

**note 展示**

模型返回的说明文字（例："我还可以帮你改成：1. 更扁平的纯色 logo…"）
渲染在结果图下方。不展示等于白扔掉一段有用输出。

### i18n

`zh-CN.json` 与 `en.json` 同步新增：设置页两个字段的 label 与说明、
模型下拉选项名、note 区域标题。

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

## 安全备注

联调用的 t8star Key 在对话中以明文出现过，不写入仓库任何文件、不提交。
联调完成后建议在 t8star 后台轮换该 Key。
