# t8star gpt-image-2 接入 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `gpt-image-2`（t8star，OpenAI chat-completions 协议）在「图片生成」和「图片编辑」两个模块可用，与现有 DashScope 模型并存。

**Architecture:** 新增一个纯函数模块 `lib/providers/t8star.js` 负责拼请求体与解析响应；`server.js` 的 `/api/generate-image` 按模型分流到 DashScope 或 OpenAI 协议，对外请求与响应契约均保持不变（响应新增可选 `note`）；前端把设置页现有的「接入点（Endpoint）」下拉从「DashScope base URL 选择器」升级为「服务商选择器」，新增 t8star 一项，并由此派生出贯穿前端的 provider 概念。

**关键前提（与旧版计划的差异）：** t8star 的 Key 与 base URL **复用现有的 `apiKey` / `endpoint` 两个字段**，
不新增 `t8ApiKey` / `t8BaseUrl`。因此后端路由的解构一行都不用改；代价是前端要按 provider
切换 Key 存储槽、模型列表、参数控件和视频可用性。

**Tech Stack:** Node.js + Express（无框架前端，原生 HTML/CSS/JS），测试用 `node:test` + `node:assert/strict`。

**设计文档:** `docs/superpowers/specs/2026-07-18-t8star-gpt-image-2-design.md`

---

## 背景知识（实现者必读）

这个仓库现有的全部图像模型走 **DashScope 原生协议**，请求体形如
`{ model, input: { messages: [...] }, parameters: {...} }`，响应形如
`{ output: { choices: [{ message: { content: [{ image: "url" }] } }] } }`。

要接入的 t8star 完全不同，是 **OpenAI chat-completions**。以下是**实测**的真实响应，
后续测试夹具直接用它：

```json
{
  "id": "chatcmpl-11e15ef4-b616-412a-9e5a-d9e8e44497b5",
  "object": "chat.completion",
  "created": 1784370851,
  "model": "gpt-image-2-pro",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "![image](https://webstatic.aiproxy.vip/output/0276be3f.png)\n\n给你画好了，一只可爱的小猫。"
    },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 3780, "completion_tokens": 883, "total_tokens": 4663 }
}
```

**要点：图片 URL 藏在 markdown `![image](url)` 里，后面还跟着一段模型说明文字。**
请求的是 `gpt-image-2`，响应回显 `gpt-image-2-pro`——我们存回显值。

实测已确认：`data:` URI 输入可用，多个 `image_url` 块可用，
接口**没有** `size`/`n`/`seed`/`watermark` 参数。

## 文件结构

| 文件 | 职责 | 动作 |
|---|---|---|
| `lib/providers/t8star.js` | 纯函数：拼 payload、解析响应、规范化 base URL。无网络 IO | 新建 |
| `tests/t8star.test.js` | 上述纯函数的单测，全部脱网 | 新建 |
| `server.js` | 模型白名单 + `/api/generate-image` 协议分流 | 修改 |
| `public/index.html` | Endpoint 下拉新增一项；视频页提示条；两个模块的 note 容器；参数控件加标记属性 | 修改 |
| `public/js/app.js` | `getProvider`、双 Key 槽切换、`providerchange` 广播、provider 感知的 `checkAuth` | 修改 |
| `public/js/imggen.js` | 按 provider 换模型列表、控件联动、note 渲染 | 修改 |
| `public/js/imgedit.js` | 同上 | 修改 |
| `public/js/task.js` | `submitTask` 加 provider 闸门，挡住视频提交 | 修改 |
| `public/locales/zh-CN.json`、`en.json` | 新增文案 | 修改 |
| `README.md` | 模型表与配置说明 | 修改 |

---

### Task 1: t8star 协议模块（纯函数 + 单测）

**Files:**
- Create: `lib/providers/t8star.js`
- Test: `tests/t8star.test.js`

- [x] **Step 1: 写失败的测试**

创建 `tests/t8star.test.js`：

```js
/**
 * t8star (gpt-image-2) provider unit tests — no network IO.
 */

const { describe, it } = require('node:test');
const assert = require('node:assert/strict');

const { buildPayload, parseResponse, resolveBaseUrl, T8STAR_DEFAULT_BASE } = require('../lib/providers/t8star');

// Real captured response from the live API (see plan doc).
const REAL_T2I_RESPONSE = {
  id: 'chatcmpl-11e15ef4',
  object: 'chat.completion',
  model: 'gpt-image-2-pro',
  choices: [{
    index: 0,
    message: {
      role: 'assistant',
      content: '![image](https://webstatic.aiproxy.vip/output/0276be3f.png)\n\n给你画好了，一只可爱的小猫。',
    },
    finish_reason: 'stop',
  }],
  usage: { prompt_tokens: 3780, completion_tokens: 883, total_tokens: 4663 },
};

describe('buildPayload', () => {
  it('uses a plain string content when there are no images', () => {
    const p = buildPayload({ model: 'gpt-image-2', prompt: '画只猫', images: [] });
    assert.equal(p.model, 'gpt-image-2');
    assert.equal(p.stream, false);
    assert.equal(p.messages.length, 1);
    assert.equal(p.messages[0].role, 'user');
    assert.equal(p.messages[0].content, '画只猫');
  });

  it('uses an array content with one image_url block per image', () => {
    const p = buildPayload({
      model: 'gpt-image-2',
      prompt: '修改这个图片',
      images: ['https://a.com/1.png', 'data:image/png;base64,AAAA'],
    });
    const content = p.messages[0].content;
    assert.ok(Array.isArray(content));
    assert.deepEqual(content[0], { type: 'text', text: '修改这个图片' });
    assert.deepEqual(content[1], { type: 'image_url', image_url: { url: 'https://a.com/1.png' } });
    assert.deepEqual(content[2], { type: 'image_url', image_url: { url: 'data:image/png;base64,AAAA' } });
  });

  it('omits the text block when prompt is empty but images exist', () => {
    const p = buildPayload({ model: 'gpt-image-2', prompt: '', images: ['https://a.com/1.png'] });
    const content = p.messages[0].content;
    assert.equal(content.length, 1);
    assert.equal(content[0].type, 'image_url');
  });

  it('drops falsy entries from the images array', () => {
    const p = buildPayload({ model: 'gpt-image-2', prompt: 'x', images: ['https://a.com/1.png', '', null] });
    assert.equal(p.messages[0].content.length, 2);
  });

  it('treats a non-array images value as no images', () => {
    const p = buildPayload({ model: 'gpt-image-2', prompt: '画只猫', images: undefined });
    assert.equal(p.messages[0].content, '画只猫');
  });
});

describe('parseResponse', () => {
  it('extracts the image url from the real API response', () => {
    const { images } = parseResponse(REAL_T2I_RESPONSE);
    assert.deepEqual(images, ['https://webstatic.aiproxy.vip/output/0276be3f.png']);
  });

  it('returns the prose commentary as note, with the image link stripped', () => {
    const { note } = parseResponse(REAL_T2I_RESPONSE);
    assert.equal(note, '给你画好了，一只可爱的小猫。');
  });

  it('extracts every image when the reply contains several', () => {
    const data = { choices: [{ message: { content: '![a](https://x.com/1.png) ![b](https://x.com/2.png)' } }] };
    assert.deepEqual(parseResponse(data).images, ['https://x.com/1.png', 'https://x.com/2.png']);
  });

  it('returns empty results when the reply has no image link', () => {
    const data = { choices: [{ message: { content: '抱歉，我无法生成。' } }] };
    assert.deepEqual(parseResponse(data), { images: [], note: '抱歉，我无法生成。' });
  });

  it('returns empty results for a malformed response', () => {
    assert.deepEqual(parseResponse({}), { images: [], note: '' });
    assert.deepEqual(parseResponse(null), { images: [], note: '' });
  });

  it('is not stateful across repeated calls', () => {
    assert.deepEqual(parseResponse(REAL_T2I_RESPONSE).images, parseResponse(REAL_T2I_RESPONSE).images);
  });
});

describe('resolveBaseUrl', () => {
  it('falls back to the default base when empty', () => {
    assert.equal(resolveBaseUrl(''), T8STAR_DEFAULT_BASE);
    assert.equal(resolveBaseUrl(null), T8STAR_DEFAULT_BASE);
  });

  it('strips trailing slashes', () => {
    assert.equal(resolveBaseUrl('https://ai.t8star.org///'), 'https://ai.t8star.org');
  });

  it('falls back to the default for a non-http value', () => {
    assert.equal(resolveBaseUrl('not-a-url'), T8STAR_DEFAULT_BASE);
  });
});
```

- [x] **Step 2: 跑测试，确认失败**

Run: `node --test tests/t8star.test.js`
Expected: FAIL —— `Cannot find module '../lib/providers/t8star'`

- [x] **Step 3: 实现模块**

创建 `lib/providers/t8star.js`：

```js
/**
 * t8star (gpt-image-2) provider — OpenAI chat-completions protocol.
 *
 * Pure functions only: no network IO, so they can be unit tested offline.
 * The upstream returns the generated image as a markdown link embedded in the
 * assistant's reply text, followed by prose commentary.
 */

const T8STAR_DEFAULT_BASE = 'https://ai.t8star.org';

/** Matches a markdown image link and captures its URL. */
function imageLinkRegex() {
  // Built fresh on every call: a shared /g regex carries lastIndex between calls.
  return /!\[[^\]]*\]\((https?:\/\/[^)\s]+)\)/g;
}

/**
 * Builds the chat-completions request body.
 * With no images the content is a plain string; with images it is a block array.
 */
function buildPayload({ model, prompt, images }) {
  const list = Array.isArray(images) ? images.filter(Boolean) : [];
  let content;

  if (list.length === 0) {
    content = prompt || '';
  } else {
    content = [];
    if (prompt) content.push({ type: 'text', text: prompt });
    for (const url of list) {
      content.push({ type: 'image_url', image_url: { url } });
    }
  }

  return { model, stream: false, messages: [{ role: 'user', content }] };
}

/**
 * Extracts image URLs and the leftover prose from the assistant reply.
 * Returns { images: string[], note: string }.
 */
function parseResponse(data) {
  const content = data?.choices?.[0]?.message?.content;
  if (typeof content !== 'string') return { images: [], note: '' };

  const images = [];
  const re = imageLinkRegex();
  let m;
  while ((m = re.exec(content)) !== null) images.push(m[1]);

  const note = content.replace(imageLinkRegex(), '').trim();
  return { images, note };
}

/** Normalizes a user-supplied base URL, falling back to the official host. */
function resolveBaseUrl(input) {
  const v = (input || '').trim();
  if (!/^https?:\/\//i.test(v)) return T8STAR_DEFAULT_BASE;
  return v.replace(/\/+$/, '');
}

module.exports = { buildPayload, parseResponse, resolveBaseUrl, T8STAR_DEFAULT_BASE };
```

- [x] **Step 4: 跑测试，确认通过**

Run: `node --test tests/t8star.test.js`
Expected: PASS，19 个断言全绿

- [x] **Step 5: 确认没弄坏现有测试**

Run: `npm test`
Expected: 全部 PASS

- [x] **Step 6: 提交**

```bash
git add lib/providers/t8star.js tests/t8star.test.js
git commit -m "feat: 新增 t8star gpt-image-2 协议模块（拼包与解析）"
```

---

### Task 2: server.js 协议分流

**Files:**
- Modify: `server.js`（`MODELS` 定义处约 166-181 行；`/api/generate-image` 约 322-400 行；文件末尾导出处）
- Modify: `public/locales/zh-CN.json`、`public/locales/en.json`（`server` 段新增一个 key）
- Test: `tests/server-utils.test.js`

- [x] **Step 1: 写失败的测试**

在 `tests/server-utils.test.js` 末尾追加：

```js
// ─── OpenAI-protocol image models ────────────────────────────────
describe('MODELS.IMAGE_OPENAI', () => {
  it('contains gpt-image-2', () => {
    assert.ok(MODELS.IMAGE_OPENAI.includes('gpt-image-2'));
  });

  it('does not overlap with the DashScope image models', () => {
    const overlap = MODELS.IMAGE_OPENAI.filter(m => MODELS.IMAGE.includes(m));
    assert.deepEqual(overlap, []);
  });
});
```

- [x] **Step 2: 跑测试，确认失败**

Run: `node --test tests/server-utils.test.js`
Expected: FAIL —— `Cannot read properties of undefined (reading 'includes')`

- [x] **Step 3: 加 MODELS 条目与模块引用**

在 `server.js` 顶部 require 区（`const multer = require('multer');` 之后）加：

```js
const t8star = require('./lib/providers/t8star');
```

在 `MODELS` 对象里，`IMAGE:` 那一行之后加：

```js
  /** Image models that speak the OpenAI chat-completions protocol (t8star) */
  IMAGE_OPENAI: ['gpt-image-2'],
```

- [x] **Step 4: 跑测试，确认通过**

Run: `node --test tests/server-utils.test.js`
Expected: PASS

- [x] **Step 5: 改造 `/api/generate-image`**

不需要新增 i18n 错误文案 —— t8star 复用 `apiKey` 字段，缺 Key 时现有的
`server.missingApiKey` 文案就是对的。

把 `server.js` 中该路由**开头的解构与校验**替换为（解构列表原样不动）：

```js
    const { apiKey, region, workspaceId, endpoint, model, prompt, images, params } = req.body;
    if (!apiKey) return res.status(400).json({ error: st('server.missingApiKey', lang) });
    if (!model) return res.status(400).json({ error: st('server.missingModel', lang) });

    const isOpenAIImage = MODELS.IMAGE_OPENAI.includes(model);
    if (!isOpenAIImage && !MODELS.IMAGE.includes(model)) {
      return res.status(400).json({ error: st('server.unsupportedImageModel', lang, { model }) });
    }

    // ─── OpenAI chat-completions branch (t8star / gpt-image-2) ───
    // Base URL and key both come from the existing endpoint/apiKey fields:
    // the client picks t8star in the Endpoint dropdown, which swaps both.
    if (isOpenAIImage) {
      const t8Base = t8star.resolveBaseUrl(endpoint);
      const t8Url = `${t8Base}/v1/chat/completions`;
      const t8Payload = t8star.buildPayload({ model, prompt, images });

      console.log(`[generate-image] → ${t8Url}  model=${model}  images=${Array.isArray(images) ? images.length : 0}`);
      const t8r = await forwardJSON(t8Url, 'POST', {
        Authorization: `Bearer ${apiKey}`,
      }, t8Payload, 180000);
      console.log(`[generate-image] ← status=${t8r.status}  response=`, JSON.stringify(t8r.data).slice(0, 500));

      if (t8r.status >= 400) {
        const msg = t8r.data?.error?.message || t8r.data?.message || st('server.upstreamHttpError', lang, { status: t8r.status });
        return res.status(t8r.status).json({ error: msg, raw: t8r.data });
      }
      if (t8r.data?.error) {
        const msg = t8r.data.error.message || st('server.callFailed', lang);
        return res.status(400).json({ error: msg, raw: t8r.data });
      }

      const { images: t8Images, note } = t8star.parseResponse(t8r.data);
      if (t8Images.length === 0) {
        return res.status(502).json({ error: st('server.noImageResult', lang), raw: t8r.data });
      }

      return res.json({
        images: t8Images,
        usage: t8r.data?.usage || {},
        // Upstream echoes the concrete model it ran (e.g. gpt-image-2-pro).
        model: t8r.data?.model || model,
        note,
      });
    }

    // ─── DashScope native branch (unchanged) ─────────────────────
    const base = resolveEndpoint(endpoint, region, workspaceId);
```

注意：原来的 `if (!MODELS.IMAGE.includes(model))` 校验块已被上面的并集校验取代，删掉它。
`const base = resolveEndpoint(...)` 原本在 `MODELS.IMAGE` 校验之前，现在要挪到 t8star 分支之后 ——
否则 t8star 请求会白算一次 DashScope endpoint，region 为空时还可能抛错。
该行以下的原有 DashScope 逻辑（拼 payload、调用、解析）保持不变。

- [x] **Step 6: 起服务做一次真实冒烟**

用你自己的 t8star Key 替换 `<YOUR_KEY>`。注意 `endpoint` 与 `apiKey` 就是 t8star 的：

```bash
node server.js &
sleep 2
curl -s -X POST http://localhost:3000/api/generate-image \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-image-2","prompt":"画只猫","images":[],"apiKey":"<YOUR_KEY>","endpoint":"https://ai.t8star.org"}' | head -c 400
```

Expected: `{"images":["https://webstatic.aiproxy.vip/output/....png"],"usage":{...},"model":"gpt-image-2-pro","note":"..."}`

再验 `endpoint` 缺省时回落到官方地址（去掉 endpoint 字段，应同样成功）：

```bash
curl -s -X POST http://localhost:3000/api/generate-image \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-image-2","prompt":"画只猫","apiKey":"<YOUR_KEY>"}' | head -c 200
```

Expected: 同样出图（`resolveBaseUrl('')` → `https://ai.t8star.org`）

再验错误分支：

```bash
curl -s -X POST http://localhost:3000/api/generate-image \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-image-2","prompt":"画只猫"}'
```

Expected: `{"error":"缺少 API Key"}`

再验 DashScope 分支没被弄坏：

```bash
curl -s -X POST http://localhost:3000/api/generate-image \
  -H 'Content-Type: application/json' \
  -d '{"model":"qwen-image","prompt":"x"}'
```

Expected: `{"error":"缺少 API Key"}`

跑完记得停掉服务：`kill %1`

- [x] **Step 7: 提交**

```bash
git add server.js tests/server-utils.test.js
git commit -m "feat: /api/generate-image 支持 OpenAI 协议分流"
```

---

### Task 3: 接入点下拉新增 t8star，引入 provider 概念

这是本次改动风险最高的一个任务：动的是一个已被三条链路共用的全局设置。
**改完务必按 Step 7 逐条回归**，确认原有百炼/蓝星两种配置行为不变。

**Files:**
- Modify: `public/index.html`（设置面板，约 578-606 行）
- Modify: `public/js/app.js`（约 27-99 行）
- Modify: `public/locales/zh-CN.json`、`public/locales/en.json`

- [x] **Step 1: 加 i18n 文案**

`zh-CN.json` 的 `settings` 段追加：

```json
"endpointT8star": "t8star 中转站（ai.t8star.org）",
"apiKeyLabelT8": "API Key（t8star）",
"apiKeyNoteT8": "t8star 用的是另一把 Key，与 DashScope Key 分开保存，切换接入点时会自动切换。",
"t8OnlyImageNote": "t8star 只提供 gpt-image-2 图片模型，不支持视频生成。"
```

`en.json` 的 `settings` 段追加：

```json
"endpointT8star": "t8star Proxy (ai.t8star.org)",
"apiKeyLabelT8": "API Key (t8star)",
"apiKeyNoteT8": "t8star uses a separate key from DashScope. Both are stored independently and swap when you change endpoint.",
"t8OnlyImageNote": "t8star only serves the gpt-image-2 image model — video generation is unavailable."
```

`zh-CN.json` 的 `config` 段追加：

```json
"readyT8": "已配置 · t8star{epLabel}"
```

`en.json` 的 `config` 段追加：

```json
"readyT8": "Configured · t8star{epLabel}"
```

- [x] **Step 2: HTML —— 下拉加选项，地域块加包裹层**

在 `public/index.html` 里，Endpoint 下拉的蓝星那一项之后插入：

```html
          <option value="https://ai.t8star.org" data-i18n="settings.endpointT8star">t8star 中转站（ai.t8star.org）</option>
```

地域那一段（`<label ...regionLabel>` + `<select id="region">` + 后面的 regionNote）
目前是三个裸兄弟节点，没法整体显隐。把这三个节点用一个 div 包起来：

```html
        <div id="regionWrap">
          <label data-i18n="settings.regionLabel">地域</label>
          <select id="region">
            ... 四个 option 原样不动 ...
          </select>
          <div style="font-size:11px;color:var(--muted);margin-top:4px" data-i18n="settings.regionNote">注意：API Key、模型、Endpoint 必须同地域，跨地域调用会失败。</div>
        </div>
```

给 API Key 的 label 与说明加 id，好在切换时改文案：

```html
        <label class="first" id="apiKeyLabel" data-i18n="settings.apiKeyLabel">API Key（DashScope）</label>
        <input type="password" id="apiKey" placeholder="sk-xxxxxxxxxxxxxxxx" autocomplete="off" />
        <div id="apiKeyNote" style="font-size:11px;color:var(--muted);margin-top:4px" data-i18n="settings.apiKeyNote">仅保存在你浏览器的 localStorage 里，不会上传到任何第三方。</div>
```

在 Endpoint 说明那个 `<div>` 之后加一条只在 t8star 时显示的提示：

```html
        <div id="t8OnlyImageNote" style="display:none;font-size:11px;color:var(--warn,#c90);margin-top:4px" data-i18n="settings.t8OnlyImageNote">t8star 只提供 gpt-image-2 图片模型，不支持视频生成。</div>
```

- [x] **Step 3: app.js —— provider 判定与双 Key 槽**

把 `public/js/app.js` 里元素引用与初始化那一段（约 27-43 行）改为：

```js
const apiKeyEl = $('apiKey');
const apiKeyLabelEl = $('apiKeyLabel');
const apiKeyNoteEl = $('apiKeyNote');
const regionEl = $('region');
const regionWrap = $('regionWrap');
const workspaceWrap = $('workspaceWrap');
const workspaceIdEl = $('workspaceId');
const endpointEl = $('endpoint');
const customEndpointWrap = $('customEndpointWrap');
const customEndpointEl = $('customEndpoint');
const t8OnlyImageNote = $('t8OnlyImageNote');
const configChip = $('configChip');
const configChipText = $('configChipText');
const setupHint = $('setupHint');

const T8STAR_BASE = 'https://ai.t8star.org';
/** localStorage slot per provider — the two keys never overwrite each other. */
const KEY_SLOT = { dashscope: 'hh_api_key', t8star: 'hh_t8_api_key' };

/**
 * The provider is derived from the endpoint, never stored separately —
 * one source of truth avoids the two drifting apart.
 */
function getProvider() {
  return endpointEl.value === T8STAR_BASE ? 't8star' : 'dashscope';
}
function isT8Provider() { return getProvider() === 't8star'; }

// Endpoint must be restored before the key: the key slot depends on it.
endpointEl.value = localStorage.getItem('hh_endpoint') || '';
customEndpointEl.value = localStorage.getItem('hh_custom_endpoint') || '';
// Tracks which provider's key currently sits in the input box.
let keySlotProvider = getProvider();
apiKeyEl.value = localStorage.getItem(KEY_SLOT[keySlotProvider]) || '';
regionEl.value = localStorage.getItem('hh_region') || 'cn-beijing';
workspaceIdEl.value = localStorage.getItem('hh_ws_id') || '';
toggleCustomEndpoint();
```

- [x] **Step 4: app.js —— 切换接入点时换 Key 槽与显隐**

把现有的 `endpointEl.addEventListener('change', toggleCustomEndpoint);` 换成：

```js
endpointEl.addEventListener('change', onEndpointChange);

function onEndpointChange() {
  const next = getProvider();
  if (next !== keySlotProvider) {
    // Park the current box contents in the old provider's slot before swapping,
    // otherwise switching back would show an empty key.
    localStorage.setItem(KEY_SLOT[keySlotProvider], apiKeyEl.value.trim());
    apiKeyEl.value = localStorage.getItem(KEY_SLOT[next]) || '';
    keySlotProvider = next;
  }
  toggleCustomEndpoint();
  applyProviderFields();
  refreshConfigChip();
}

/** Region and workspace are DashScope-only concepts; hide them for t8star. */
function applyProviderFields() {
  const t8 = isT8Provider();
  regionWrap.style.display = t8 ? 'none' : '';
  t8OnlyImageNote.style.display = t8 ? '' : 'none';
  if (t8) workspaceWrap.style.display = 'none';
  else toggleWorkspace();
  apiKeyLabelEl.textContent = t(t8 ? 'settings.apiKeyLabelT8' : 'settings.apiKeyLabel');
  apiKeyNoteEl.textContent = t(t8 ? 'settings.apiKeyNoteT8' : 'settings.apiKeyNote');
}
applyProviderFields();
```

`toggleWorkspace()` 里的法兰克福判断保持不变，但把调用点收敛进 `applyProviderFields()`，
避免 t8star 下地域仍是法兰克福时把 WorkspaceId 框漏出来。

- [x] **Step 5: app.js —— 保存、配置条、providerchange 广播**

把保存设置的处理器改为：

```js
$('settingsSave').addEventListener('click', () => {
  localStorage.setItem(KEY_SLOT[getProvider()], apiKeyEl.value.trim());
  localStorage.setItem('hh_region', regionEl.value);
  localStorage.setItem('hh_ws_id', workspaceIdEl.value.trim());
  localStorage.setItem('hh_endpoint', endpointEl.value);
  localStorage.setItem('hh_custom_endpoint', customEndpointEl.value.trim());
  refreshConfigChip();
  // Model lists, parameter controls and the video gate all key off the provider.
  window.dispatchEvent(new Event('providerchange'));
  switchToTab('r2v');
});
```

注意 `switchToTab('r2v')` —— 保存后会跳到视频页。t8star 下那一页是不可用的，
Task 4 的提示条正好在那里给出说明，不必改这个跳转。

在 `refreshConfigChip()` 里，把设置文本那一段改为：

```js
  if (hasKey) {
    configChip.classList.add('ok');
    configChipText.textContent = isT8Provider()
      ? t('config.readyT8', { epLabel })
      : t('config.ready', { region, epLabel });
    setupHint.style.display = 'none';
  } else {
```

`localechange` 监听里补一句 `applyProviderFields()`，否则切语言后 Key 的 label
会退回 HTML 里的 `data-i18n` 默认值：

```js
window.addEventListener('localechange', () => {
  applyProviderFields();
  refreshConfigChip();
});
```

- [x] **Step 6: getAuth 不动**

`getAuth()` 保持原样 —— `apiKey` 与 `endpoint` 已经承载了 t8star 所需的全部信息。
这是方案 B 的红利：所有提交路径的 `JSON.stringify({ ...auth, ... })` 一行都不用改。

- [x] **Step 7: 手动回归（重点）**

Run: `node server.js`，浏览器打开 http://localhost:3000 → 设置页

1. 接入点选「t8star 中转站」
   Expected: 地域块与 WorkspaceId 消失；Key 的 label 变成「API Key（t8star）」；出现橙色的「不支持视频生成」提示
2. 填入 t8star Key → 保存 → 刷新页面 → 回设置页
   Expected: 接入点仍是 t8star，Key 仍在
3. 接入点切回「官方百炼」
   Expected: 地域块回来；Key 框显示的是**原来那把 DashScope Key**，不是 t8star 的
4. 再切回 t8star
   Expected: Key 框显示 t8star 的 Key（两把互不覆盖 —— 这一条最容易写错，务必确认）
5. 接入点选「自定义…」，填 `https://x.example.com`
   Expected: 行为与改动前一致，仍按 DashScope 处理，地域块显示
6. 地域选「德国（法兰克福）」→ 接入点切 t8star
   Expected: WorkspaceId 框不出现（不是只隐藏地域却漏出 Workspace）

- [x] **Step 8: 提交**

```bash
git add public/index.html public/js/app.js public/locales/zh-CN.json public/locales/en.json
git commit -m "feat: 接入点下拉新增 t8star，按服务商切换 Key 与配置项"
```

---

### Task 4: provider 感知的鉴权与视频模块闸门

**Files:**
- Modify: `public/js/app.js`（`checkAuth`）
- Modify: `public/js/task.js`（`submitTask`，约 155 行）
- Modify: `public/index.html`（三个视频页顶部提示条）
- Modify: `public/locales/zh-CN.json`、`public/locales/en.json`

- [x] **Step 1: 加 i18n 文案**

`zh-CN.json` 的 `config` 段追加：

```json
"t8NoVideo": "当前接入点是 t8star，只支持 gpt-image-2 图片生成。视频功能请到设置里把接入点切回官方百炼。"
```

`en.json` 的 `config` 段追加：

```json
"t8NoVideo": "The current endpoint is t8star, which only serves gpt-image-2 image generation. Switch the endpoint back to Official Bailian in Settings to use video."
```

- [x] **Step 2: checkAuth 跳过 t8star 无关的校验**

把 `public/js/app.js` 的 `checkAuth` 改为：

```js
function checkAuth(needsWs = true) {
  const a = getAuth();
  if (!a.apiKey) {
    if (confirm(t('config.confirmNoApiKey'))) openSettings();
    return null;
  }
  // Region and workspace are DashScope concepts — t8star has neither.
  if (needsWs && !isT8Provider() && a.region === 'eu-central-1' && !a.workspaceId) {
    if (confirm(t('config.confirmWorkspace'))) openSettings();
    return null;
  }
  return a;
}
```

签名不变 —— provider 从 endpoint 推导，不需要调用方传模型。
所有现有调用点（`checkAuth()` / `checkAuth(false)`）零改动。

- [x] **Step 3: 视频提交闸门**

`task.js` 的 `submitTask()` 是 t2v / i2v / r2v 三个视频页的**唯一**提交入口，
一道闸门就能全挡住。把开头改为：

```js
async function submitTask(ctx, payload) {
  // t8star speaks only the image chat API; DashScope video endpoints do not exist there.
  if (isT8Provider()) {
    alert(t('config.t8NoVideo'));
    return;
  }
  const auth = checkAuth();
  if (!auth) return;
```

同一文件里轮询用的 `checkAuth()`（约 234 行）不用加闸门 ——
提交都进不去，就不会有待轮询的任务。

- [x] **Step 4: 视频页常驻提示条**

点了才被拒的体验不好。在 `public/index.html` 的 t2v、i2v、r2v 三个面板里，
各自的 `<h2>` 之后插入一条默认隐藏的提示（`KIND` 分别替换为 `t2v` / `i2v` / `r2v`）：

```html
        <div class="meta" id="t8VideoWarn-KIND" style="display:none;color:var(--warn,#c90);margin-bottom:8px" data-i18n="config.t8NoVideo">当前接入点是 t8star，只支持 gpt-image-2 图片生成。视频功能请到设置里把接入点切回官方百炼。</div>
```

在 `public/js/task.js` 末尾加刷新逻辑，并挂到两个事件上：

```js
/** Shows the "video unavailable" banner on the video tabs when t8star is selected. */
function refreshT8VideoWarnings() {
  const show = isT8Provider();
  ['t2v', 'i2v', 'r2v'].forEach(kind => {
    const el = document.getElementById('t8VideoWarn-' + kind);
    if (el) el.style.display = show ? '' : 'none';
  });
}
window.addEventListener('providerchange', refreshT8VideoWarnings);
window.addEventListener('localechange', refreshT8VideoWarnings);
refreshT8VideoWarnings();
```

- [x] **Step 5: 手动验证**

Run: `node server.js`，浏览器打开 http://localhost:3000

1. 接入点设为 t8star → 保存
   Expected: 跳到视频页后，页面顶部就有橙色提示条
2. 在视频页填内容点提交
   Expected: 弹出「当前接入点是 t8star…」，不发请求（Network 面板确认无 `/api/create-task`）
3. 接入点切回官方百炼 → 保存
   Expected: 提示条消失；视频提交恢复正常
4. 地域设法兰克福、不填 WorkspaceId，接入点为百炼，提交视频
   Expected: 仍弹出原来的 WorkspaceId 提示（这条老校验没被误伤）

- [x] **Step 6: 提交**

```bash
git add public/js/app.js public/js/task.js public/index.html public/locales/zh-CN.json public/locales/en.json
git commit -m "feat: t8star 接入点下跳过地域校验并挡住视频提交"
```

---

### Task 5: 图片生成模块接入

**Files:**
- Modify: `public/index.html`（imggen 面板，约 405-461 行）
- Modify: `public/js/imggen.js`
- Modify: `public/locales/zh-CN.json`、`public/locales/en.json`

- [x] **Step 1: 加 i18n 文案**

`zh-CN.json` 的 `imggen` 段追加：

```json
"modelGptImage2": "gpt-image-2（t8star，仅支持 Prompt）",
"noteTitle": "模型说明"
```

`en.json` 的 `imggen` 段追加：

```json
"modelGptImage2": "gpt-image-2 (t8star, prompt only)",
"noteTitle": "Model notes"
```

- [x] **Step 2: HTML 标记不支持的控件 + 加 note 容器**

在 `public/index.html` 的 imggen 面板中：

给反向提示词的 `<label>` 和 `<textarea id="negative-imggen">` 各加 `data-imggen-ds` 属性：

```html
        <label data-imggen-ds data-i18n-html="imggen.negativeLabelFull">反向提示词 <span style="color: var(--muted)">（可选，描述不希望出现的内容）</span></label>
        <textarea data-imggen-ds id="negative-imggen" rows="2" data-i18n-placeholder="imggen.negativePlaceholder" placeholder="例：低分辨率、模糊、多余的手指、错误比例…" style="min-height:60px"></textarea>
```

给「生成参数」面板里那两个 `<div class="row-2">` 加同一属性：

```html
        <div class="row-2" data-imggen-ds>
```

（两处都要加：尺寸/数量那一个，Seed/水印那一个。）

在 `imageOut-imggen` 容器内，`imageGrid-imggen` 之后加 note 容器：

```html
        <div class="image-out" id="imageOut-imggen" style="display:none">
          <div class="meta" id="imageMeta-imggen"></div>
          <div class="image-grid" id="imageGrid-imggen"></div>
          <div class="meta" id="imageNote-imggen" style="display:none;white-space:pre-wrap;margin-top:8px"></div>
        </div>
```

- [x] **Step 3: 模型列表按 provider 切换**

不是追加一项 —— t8star 不认 qwen/wan 模型名，列出来全是必然报错的选项。
把 `public/js/imggen.js` 的 `getImggenModels()` 改为：

```js
function getImggenModels() {
  // t8star serves exactly one image model; DashScope models are not routable there.
  if (isT8Provider()) {
    return [{ value: 'gpt-image-2', label: t('imggen.modelGptImage2') }];
  }
  return [
    { value: 'qwen-image-plus', label: t('imggen.modelQwenPlus') },
    { value: 'qwen-image', label: t('imggen.modelQwen') },
    { value: 'wan2.7-image-pro', label: t('imggen.modelWanPro') },
    { value: 'wan2.7-image', label: t('imggen.modelWan') },
  ];
}
```

再加一个模型判定（`imggen.js` 与 `imgedit.js` 共用，放 `app.js` 更合适 ——
放在 `getProvider()` 旁边）：

```js
/** True when the model speaks the t8star OpenAI chat protocol. */
function isT8Model(model) {
  return model === 'gpt-image-2';
}
```

切换接入点后模型下拉必须重建，否则会残留上一个 provider 的选项。
在 `public/js/imggen.js` 现有的 `localechange` 监听旁边加：

```js
window.addEventListener('providerchange', () => { updateImggenUI(); });
```

`updateImggenUI()` 开头会读 `$('model-imggen').value` 再重建 `innerHTML`，
而 `models.some(m => m.value === prevValue)` 判不中时会落到列表第一项 ——
provider 切换后旧模型名自然被丢弃，这个行为正是我们要的，不用额外处理。

- [x] **Step 4: 控件联动**

在 `updateImggenUI()` 函数体末尾（`wanControls.forEach(...)` 那行之后）追加。
注意要用**重建后的**下拉当前值，不能用函数开头那个 `model` 变量 ——
provider 刚切换时它还是旧模型名：

```js
  // gpt-image-2 is a chat API: it has no size/count/seed/watermark parameters.
  const isT8 = isT8Model(modelSelect.value);
  document.querySelectorAll('[data-imggen-ds]').forEach(el => el.style.display = isT8 ? 'none' : '');
  if (isT8) {
    document.querySelectorAll('[data-imggen-wan]').forEach(el => el.style.display = 'none');
  }
```

- [x] **Step 5: 提交时跳过不支持的参数**

`submitImggen()` 开头的 `checkAuth()` 调用**保持原样** ——
provider 从 endpoint 推导，不需要传模型进去。

把「Build params」那一段改为：

```js
  // Build params — gpt-image-2 takes no generation parameters.
  const params = {};
  if (!isT8Model(model)) {
    params.size = $('size-imggen').value;
    params.n = parseInt($('count-imggen').value, 10) || 1;
    Object.assign(params, collectWatermarkParams('imggen'));
    const negPrompt = $('negative-imggen').value.trim();
    if (negPrompt) params.negative_prompt = negPrompt;
    const seedVal = $('seed-imggen').value.trim();
    if (seedVal) params.seed = parseInt(seedVal, 10);
    if (isWanModel(model)) {
      params.thinking_mode = $('thinking-imggen').checked;
      params.enable_sequential = $('sequential-imggen').checked;
    }
  }
```

在 `renderImggenImageResults(imageUrls, data.usage);` 那一行改为传入 note：

```js
    renderImggenImageResults(imageUrls, data.usage, data.note);
```

历史记录里存实际跑的模型（服务端回显的 `gpt-image-2-pro`）——把 `patchHistoryRecord` 的成功分支改为：

```js
    patchHistoryRecord(historyId, {
      status: 'SUCCEEDED',
      imageUrls,
      endTime: Date.now(),
      usage: data.usage || null,
      model: data.model || model,
    });
```

- [x] **Step 6: 渲染 note**

把 `renderImggenImageResults` 的签名与开头改为：

```js
function renderImggenImageResults(imageUrls, usage, note) {
  const out = $('imageOut-imggen');
  const grid = $('imageGrid-imggen');
  const meta = $('imageMeta-imggen');
  const noteEl = $('imageNote-imggen');

  out.style.display = '';
  if (note) {
    noteEl.textContent = t('imggen.noteTitle') + '：' + note;
    noteEl.style.display = '';
  } else {
    noteEl.textContent = '';
    noteEl.style.display = 'none';
  }
```

（函数其余部分不变。用 `textContent` 而非 `innerHTML`，避免上游文本被当作 HTML 注入。）

- [x] **Step 7: 手动验证**

Run: `node server.js`，浏览器打开 http://localhost:3000

1. 接入点为官方百炼时打开「图片生成」
   Expected: 模型下拉是原来四项，`gpt-image-2` **不出现**
2. 设置页切到 t8star + 填 Key → 保存 → 回「图片生成」
   Expected: 下拉**只剩** `gpt-image-2`（不用刷新页面 —— 验证 `providerchange` 生效）；
   尺寸/数量/Seed/水印/反向提示词全部消失
3. Prompt 填「画只猫」→ 点生成
   Expected: 约 30-60s 后出图，图下方显示「模型说明：给你画好了…」
4. 设置页切回官方百炼 → 回「图片生成」
   Expected: 下拉恢复四项且选中第一项，上述控件全部恢复显示
5. 用 `qwen-image` 生成一次
   Expected: 行为与改动前一致

- [x] **Step 8: 提交**

```bash
git add public/index.html public/js/imggen.js public/locales/zh-CN.json public/locales/en.json
git commit -m "feat: 图片生成模块接入 gpt-image-2"
```

---

### Task 6: 图片编辑模块接入

**Files:**
- Modify: `public/index.html`（imgedit 面板，约 498-554 行）
- Modify: `public/js/imgedit.js`
- Modify: `public/locales/zh-CN.json`、`public/locales/en.json`

做法与 Task 5 完全对称，但**不要跳读**——以下是这个模块自己的完整改动。

- [x] **Step 1: 加 i18n 文案**

`zh-CN.json` 的 `imgedit` 段追加：

```json
"modelGptImage2": "gpt-image-2（t8star，支持多图输入）",
"noteTitle": "模型说明"
```

`en.json` 的 `imgedit` 段追加：

```json
"modelGptImage2": "gpt-image-2 (t8star, multi-image input)",
"noteTitle": "Model notes"
```

- [x] **Step 2: HTML 标记控件 + note 容器**

给反向提示词的 `<label>` 与 `<textarea id="negative-imgedit">` 加 `data-imgedit-ds`：

```html
        <label data-imgedit-ds data-i18n-html="imgedit.negativeLabelFull">反向提示词 <span style="color: var(--muted)">（可选，描述不希望出现的内容）</span></label>
        <textarea data-imgedit-ds id="negative-imgedit" rows="2" data-i18n-placeholder="imgedit.negativePlaceholder" placeholder="例：低分辨率、模糊、多余的手指、错误比例…" style="min-height:60px"></textarea>
```

给「生成参数」面板里两个 `<div class="row-2">` 都加：

```html
        <div class="row-2" data-imgedit-ds>
```

note 容器：

```html
        <div class="image-out" id="imageOut-imgedit" style="display:none">
          <div class="meta" id="imageMeta-imgedit"></div>
          <div class="image-grid" id="imageGrid-imgedit"></div>
          <div class="meta" id="imageNote-imgedit" style="display:none;white-space:pre-wrap;margin-top:8px"></div>
        </div>
```

- [x] **Step 3: 模型列表按 provider 切换**

把 `public/js/imgedit.js` 的 `getImgeditModels()` 改为：

```js
function getImgeditModels() {
  // t8star serves exactly one image model; DashScope models are not routable there.
  if (isT8Provider()) {
    return [{ value: 'gpt-image-2', label: t('imgedit.modelGptImage2') }];
  }
  return [
    { value: 'qwen-image-edit-plus', label: t('imgedit.modelQwenEditPlus') },
    { value: 'qwen-image-edit', label: t('imgedit.modelQwenEdit') },
    { value: 'wan2.7-image-pro', label: t('imgedit.modelWanPro') },
    { value: 'wan2.7-image', label: t('imgedit.modelWan') },
  ];
}
```

在 `public/js/imgedit.js` 现有的 `localechange` 监听旁边加：

```js
window.addEventListener('providerchange', () => { updateImgeditUI(); });
```

- [x] **Step 4: 控件联动**

在 `updateImgeditUI()` 函数体末尾（`wanControls.forEach(...)` 之后）追加。
同 Task 5，用重建后的下拉当前值：

```js
  // gpt-image-2 is a chat API: it has no size/count/seed/watermark parameters.
  const isT8 = isT8Model(modelSelect.value);
  document.querySelectorAll('[data-imgedit-ds]').forEach(el => el.style.display = isT8 ? 'none' : '');
  if (isT8) {
    document.querySelectorAll('[data-imgedit-wan]').forEach(el => el.style.display = 'none');
  }
```

- [x] **Step 5: 提交时跳过不支持的参数**

`submitImgedit()` 开头的 `checkAuth()` 调用**保持原样**。

把构建 params 的那一段包进条件里：

```js
  // Build params — gpt-image-2 takes no generation parameters.
  const params = {};
  if (!isT8Model(model)) {
    params.size = $('size-imgedit').value;
    params.n = parseInt($('count-imgedit').value, 10) || 1;
    Object.assign(params, collectWatermarkParams('imgedit'));
    const negPrompt = $('negative-imgedit').value.trim();
    if (negPrompt) params.negative_prompt = negPrompt;
    const seedVal = $('seed-imgedit').value.trim();
    if (seedVal) params.seed = parseInt(seedVal, 10);
    if (isWanEditModel(model)) {
      params.thinking_mode = $('thinking-imgedit').checked;
      params.enable_sequential = $('sequential-imgedit').checked;
    }
  }
```

渲染调用改为传 note：

```js
    renderImgeditImageResults(imageUrls, data.usage, data.note);
```

成功分支的历史记录存回显模型：

```js
    patchHistoryRecord(historyId, {
      status: 'SUCCEEDED',
      imageUrls,
      endTime: Date.now(),
      usage: data.usage || null,
      model: data.model || model,
    });
```

- [x] **Step 6: 渲染 note**

把 `renderImgeditImageResults` 的签名与开头改为：

```js
function renderImgeditImageResults(imageUrls, usage, note) {
  const out = $('imageOut-imgedit');
  const grid = $('imageGrid-imgedit');
  const meta = $('imageMeta-imgedit');
  const noteEl = $('imageNote-imgedit');

  out.style.display = '';
  if (note) {
    noteEl.textContent = t('imgedit.noteTitle') + '：' + note;
    noteEl.style.display = '';
  } else {
    noteEl.textContent = '';
    noteEl.style.display = 'none';
  }
```

（其余部分不变。）

- [x] **Step 7: 手动验证**

Run: `node server.js`，浏览器打开 http://localhost:3000 → 设置页切 t8star → 图片编辑

1. 模型下拉只有 `gpt-image-2`
   Expected: 尺寸/数量/Seed/水印/反向提示词消失，上传区与 Prompt 仍在
2. 上传 1 张图，Prompt 填「把背景改成蓝色」→ 点编辑
   Expected: 出图 + 模型说明（验证 base64 data URI 链路通）
3. 上传 2 张图，Prompt 填「把这两张图并排拼成一张」→ 点编辑
   Expected: 出图（验证多图链路通）
4. 设置页切回官方百炼，模型选 `qwen-image-edit`，编辑一次
   Expected: 行为与改动前一致
5. 打开历史记录
   Expected: t8star 那两条记录的模型显示为 `gpt-image-2-pro`，
   且与 DashScope 的记录混排显示正常（历史记录不区分 provider，这是有意的）

- [x] **Step 8: 提交**

```bash
git add public/index.html public/js/imgedit.js public/locales/zh-CN.json public/locales/en.json
git commit -m "feat: 图片编辑模块接入 gpt-image-2"
```

---

### Task 7: 文档更新与全量回归

**Files:**
- Modify: `README.md`

- [x] **Step 1: 更新模型表**

在 README「支持的模型」表格里，`wan2.7-image` 那一行之后插入：

```markdown
| `gpt-image-2` | 文生图 / 图片编辑（t8star） | OpenAI 兼容 ✅ |
```

- [x] **Step 2: 更新配置说明**

在 README「### 4. 配置」小节的列表末尾追加：

```markdown
- **接入点（Endpoint）** — 除官方百炼与蓝星中转站外，新增 **t8star 中转站**。
  选中 t8star 后：
  - API Key 字段代表 t8star 的 Key（与 DashScope Key 分开保存，切换接入点时自动切换）
  - 地域与 WorkspaceId 不适用，自动隐藏
  - 图片模型只有 `gpt-image-2`；视频生成不可用
```

- [x] **Step 3: 更新接口协议说明**

在 README「### 图片生成」小节末尾追加：

```markdown
- **`gpt-image-2`**（t8star）使用 OpenAI chat-completions 协议
  - 请求地址：`{base}/v1/chat/completions`
  - 请求格式：`{ model, stream: false, messages: [{ role: "user", content: string | [{ type: "text", text }, { type: "image_url", image_url: { url } }] }] }`
  - 响应格式：图片 URL 以 markdown `![image](url)` 内嵌在 `choices[0].message.content` 中，需正则提取；正文剩余部分作为模型说明展示
  - 不支持 `size` / `n` / `seed` / `watermark` / `negative_prompt` 等参数，选中该模型时相关控件自动隐藏
```

- [x] **Step 4: 全量回归**

Run: `npm test`
Expected: 全部 PASS

Run: `node -e "require('./server.js')"`
Expected: 无报错退出（语法与加载检查）

- [x] **Step 5: 提交**

```bash
git add README.md
git commit -m "docs: README 补充 gpt-image-2 模型与协议说明"
```

---

## 完成标准

- `npm test` 全绿，含新增的 `tests/t8star.test.js`
- 接入点选 t8star 后，图片生成模块能出图，模型说明正常展示
- 接入点选 t8star 后，图片编辑模块单图、多图均能出图
- **两把 Key 互不覆盖**：t8star ↔ 百炼来回切换，各自的 Key 都还在
- **切回官方百炼后一切如初**：模型下拉恢复四项，参数控件恢复，视频可提交
- t8star 下视频提交被拦截并给出明确指引，不会发出必然失败的请求
- 未填 Key 时给出明确提示而非 500
- 仓库中不含任何真实 API Key
