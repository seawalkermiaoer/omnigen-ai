# t8star gpt-image-2 接入 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `gpt-image-2`（t8star，OpenAI chat-completions 协议）在「图片生成」和「图片编辑」两个模块可用，与现有 DashScope 模型并存。

**Architecture:** 新增一个纯函数模块 `lib/providers/t8star.js` 负责拼请求体与解析响应；`server.js` 的 `/api/generate-image` 按模型分流到 DashScope 或 OpenAI 协议，对外响应契约保持 `{ images, usage, model }` 不变（新增可选 `note`）；前端在设置页新增独立的 Key/BaseURL 字段，并在选中该模型时隐藏接口不支持的参数控件。

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
| `public/index.html` | 设置页两个新字段；两个模块的 note 容器；参数控件加标记属性 | 修改 |
| `public/js/app.js` | Key 读写、`getAuth`、`isT8Model`、模型感知的 `checkAuth` | 修改 |
| `public/js/imggen.js` | 模型选项、控件联动、note 渲染 | 修改 |
| `public/js/imgedit.js` | 同上 | 修改 |
| `public/locales/zh-CN.json`、`en.json` | 新增文案 | 修改 |
| `README.md` | 模型表与配置说明 | 修改 |

---

### Task 1: t8star 协议模块（纯函数 + 单测）

**Files:**
- Create: `lib/providers/t8star.js`
- Test: `tests/t8star.test.js`

- [ ] **Step 1: 写失败的测试**

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

- [ ] **Step 2: 跑测试，确认失败**

Run: `node --test tests/t8star.test.js`
Expected: FAIL —— `Cannot find module '../lib/providers/t8star'`

- [ ] **Step 3: 实现模块**

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

- [ ] **Step 4: 跑测试，确认通过**

Run: `node --test tests/t8star.test.js`
Expected: PASS，19 个断言全绿

- [ ] **Step 5: 确认没弄坏现有测试**

Run: `npm test`
Expected: 全部 PASS

- [ ] **Step 6: 提交**

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

- [ ] **Step 1: 写失败的测试**

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

- [ ] **Step 2: 跑测试，确认失败**

Run: `node --test tests/server-utils.test.js`
Expected: FAIL —— `Cannot read properties of undefined (reading 'includes')`

- [ ] **Step 3: 加 MODELS 条目与模块引用**

在 `server.js` 顶部 require 区（`const multer = require('multer');` 之后）加：

```js
const t8star = require('./lib/providers/t8star');
```

在 `MODELS` 对象里，`IMAGE:` 那一行之后加：

```js
  /** Image models that speak the OpenAI chat-completions protocol (t8star) */
  IMAGE_OPENAI: ['gpt-image-2'],
```

- [ ] **Step 4: 跑测试，确认通过**

Run: `node --test tests/server-utils.test.js`
Expected: PASS

- [ ] **Step 5: 加 i18n 错误文案**

`public/locales/zh-CN.json` 的 `server` 段加：

```json
"missingT8ApiKey": "缺少 gpt-image-2 API Key，请在设置中填写",
```

`public/locales/en.json` 的 `server` 段加：

```json
"missingT8ApiKey": "Missing gpt-image-2 API Key — please set it in Settings",
```

- [ ] **Step 6: 改造 `/api/generate-image`**

把 `server.js` 中该路由**开头的解构与校验**替换为：

```js
    const { apiKey, region, workspaceId, endpoint, model, prompt, images, params, t8ApiKey, t8BaseUrl } = req.body;
    if (!model) return res.status(400).json({ error: st('server.missingModel', lang) });

    const isOpenAIImage = MODELS.IMAGE_OPENAI.includes(model);
    if (!isOpenAIImage && !MODELS.IMAGE.includes(model)) {
      return res.status(400).json({ error: st('server.unsupportedImageModel', lang, { model }) });
    }

    // ─── OpenAI chat-completions branch (t8star / gpt-image-2) ───
    if (isOpenAIImage) {
      if (!t8ApiKey) return res.status(400).json({ error: st('server.missingT8ApiKey', lang) });

      const t8Base = t8star.resolveBaseUrl(t8BaseUrl);
      const t8Url = `${t8Base}/v1/chat/completions`;
      const t8Payload = t8star.buildPayload({ model, prompt, images });

      console.log(`[generate-image] → ${t8Url}  model=${model}  images=${Array.isArray(images) ? images.length : 0}`);
      const t8r = await forwardJSON(t8Url, 'POST', {
        Authorization: `Bearer ${t8ApiKey}`,
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
    if (!apiKey) return res.status(400).json({ error: st('server.missingApiKey', lang) });
    const base = resolveEndpoint(endpoint, region, workspaceId);
```

注意：原来 `if (!apiKey)` 和 `const base = ...` 在路由最前面，
现在必须移到 DashScope 分支内部——t8star 不需要 DashScope Key。
原来的 `if (!MODELS.IMAGE.includes(model))` 校验块已被上面的并集校验取代，删掉它。
该行以下的原有 DashScope 逻辑（拼 payload、调用、解析）保持不变。

- [ ] **Step 7: 起服务做一次真实冒烟**

用你自己的 t8star Key 替换 `<YOUR_KEY>`：

```bash
node server.js &
sleep 2
curl -s -X POST http://localhost:3000/api/generate-image \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-image-2","prompt":"画只猫","images":[],"t8ApiKey":"<YOUR_KEY>"}' | head -c 400
```

Expected: `{"images":["https://webstatic.aiproxy.vip/output/....png"],"usage":{...},"model":"gpt-image-2-pro","note":"..."}`

再验错误分支：

```bash
curl -s -X POST http://localhost:3000/api/generate-image \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-image-2","prompt":"画只猫"}'
```

Expected: `{"error":"缺少 gpt-image-2 API Key，请在设置中填写"}`

再验 DashScope 分支没被弄坏（无 Key 应报 DashScope 的错，不是 t8 的错）：

```bash
curl -s -X POST http://localhost:3000/api/generate-image \
  -H 'Content-Type: application/json' \
  -d '{"model":"qwen-image","prompt":"x"}'
```

Expected: `{"error":"缺少 API Key"}`

跑完记得停掉服务：`kill %1`

- [ ] **Step 8: 提交**

```bash
git add server.js tests/server-utils.test.js public/locales/zh-CN.json public/locales/en.json
git commit -m "feat: /api/generate-image 支持 OpenAI 协议分流"
```

---

### Task 3: 设置页新增 Key 与 Base URL

**Files:**
- Modify: `public/index.html`（设置面板，约 578-606 行）
- Modify: `public/js/app.js`（约 27-42 行的元素引用与读取、约 92-96 行的保存、约 124-131 行的 `getAuth`）
- Modify: `public/locales/zh-CN.json`、`public/locales/en.json`

- [ ] **Step 1: 加 i18n 文案**

`zh-CN.json` 的 `settings` 段追加：

```json
"t8ApiKeyLabel": "API Key（gpt-image-2 / t8star）",
"t8ApiKeyNote": "gpt-image-2 使用独立于 DashScope 的另一把 Key，同样只存在浏览器 localStorage。",
"t8BaseUrlLabel": "gpt-image-2 Base URL",
"t8BaseUrlNote": "留空则使用官方地址 https://ai.t8star.org"
```

`en.json` 的 `settings` 段追加：

```json
"t8ApiKeyLabel": "API Key (gpt-image-2 / t8star)",
"t8ApiKeyNote": "gpt-image-2 uses its own key, separate from DashScope. Stored only in your browser's localStorage.",
"t8BaseUrlLabel": "gpt-image-2 Base URL",
"t8BaseUrlNote": "Leave empty to use the official host https://ai.t8star.org"
```

`zh-CN.json` 的 `config` 段追加：

```json
"confirmNoT8ApiKey": "尚未填写 gpt-image-2 的 API Key，现在去设置？"
```

`en.json` 的 `config` 段追加：

```json
"confirmNoT8ApiKey": "No gpt-image-2 API Key configured. Open settings now?"
```

- [ ] **Step 2: 加设置页字段**

在 `public/index.html` 里，找到 WorkspaceId 那个 `<div>` 块的结束标签之后、
`<label data-i18n="settings.languageLabel">` 之前，插入：

```html
        <label data-i18n="settings.t8ApiKeyLabel">API Key（gpt-image-2 / t8star）</label>
        <input type="password" id="t8ApiKey" placeholder="sk-xxxxxxxxxxxxxxxx" autocomplete="off" />
        <div style="font-size:11px;color:var(--muted);margin-top:4px" data-i18n="settings.t8ApiKeyNote">gpt-image-2 使用独立于 DashScope 的另一把 Key，同样只存在浏览器 localStorage。</div>

        <label data-i18n="settings.t8BaseUrlLabel">gpt-image-2 Base URL</label>
        <input type="text" id="t8BaseUrl" placeholder="https://ai.t8star.org" autocomplete="off" />
        <div style="font-size:11px;color:var(--muted);margin-top:4px" data-i18n="settings.t8BaseUrlNote">留空则使用官方地址 https://ai.t8star.org</div>
```

- [ ] **Step 3: app.js 读取与保存**

在 `public/js/app.js` 里 `const endpointEl = $('endpoint');` 附近追加元素引用：

```js
const t8ApiKeyEl = $('t8ApiKey');
const t8BaseUrlEl = $('t8BaseUrl');
```

在 `endpointEl.value = localStorage.getItem('hh_endpoint') || '';` 附近追加初始化：

```js
t8ApiKeyEl.value = localStorage.getItem('hh_t8_api_key') || '';
t8BaseUrlEl.value = localStorage.getItem('hh_t8_base_url') || '';
```

在保存设置的函数里，`localStorage.setItem('hh_custom_endpoint', ...)` 之后追加：

```js
  localStorage.setItem('hh_t8_api_key', t8ApiKeyEl.value.trim());
  localStorage.setItem('hh_t8_base_url', t8BaseUrlEl.value.trim());
```

- [ ] **Step 4: getAuth 带上新字段**

把 `getAuth()` 改为：

```js
function getAuth() {
  return {
    apiKey: apiKeyEl.value.trim(),
    region: regionEl.value,
    workspaceId: workspaceIdEl.value.trim(),
    endpoint: getEndpointUrl(),
    t8ApiKey: t8ApiKeyEl.value.trim(),
    t8BaseUrl: t8BaseUrlEl.value.trim(),
  };
}
```

两个提交路径都是 `JSON.stringify({ ...auth, ... })`，因此新字段自动透传，调用点无需改动。

- [ ] **Step 5: 手动验证**

Run: `node server.js`，浏览器打开 http://localhost:3000 → 设置页
Expected: 出现两个新输入框；填入值 → 保存 → 刷新页面 → 值仍在

浏览器控制台执行 `getAuth()`，Expected: 返回对象含 `t8ApiKey` 与 `t8BaseUrl`

- [ ] **Step 6: 提交**

```bash
git add public/index.html public/js/app.js public/locales/zh-CN.json public/locales/en.json
git commit -m "feat: 设置页新增 gpt-image-2 独立 Key 与 Base URL"
```

---

### Task 4: 模型感知的鉴权校验

**Files:**
- Modify: `public/js/app.js`（`checkAuth`，约 132-146 行）

- [ ] **Step 1: 加模型判定与改造 checkAuth**

在 `public/js/app.js` 的 `getAuth()` 之后加：

```js
/** True when the model uses the t8star OpenAI-protocol endpoint. */
function isT8Model(model) {
  return model === 'gpt-image-2';
}
```

把 `checkAuth` 改为：

```js
function checkAuth(needsWs = true, model = null) {
  const a = getAuth();
  // t8star models use their own key and are region-independent.
  if (isT8Model(model)) {
    if (!a.t8ApiKey) {
      if (confirm(t('config.confirmNoT8ApiKey'))) openSettings();
      return null;
    }
    return a;
  }
  if (!a.apiKey) {
    if (confirm(t('config.confirmNoApiKey'))) openSettings();
    return null;
  }
  if (needsWs && a.region === 'eu-central-1' && !a.workspaceId) {
    if (confirm(t('config.confirmWorkspace'))) openSettings();
    return null;
  }
  return a;
}
```

`app.js` 在 `imggen.js` / `imgedit.js` 之前加载，所以 `isT8Model` 对它们全局可见。
参数带默认值，现有 `checkAuth()` / `checkAuth(false)` 的调用点行为不变。

- [ ] **Step 2: 手动验证**

浏览器控制台执行：
```js
checkAuth(true, 'gpt-image-2')
```
Expected: t8 Key 已填时返回 auth 对象；清空 t8 Key 后弹出「尚未填写 gpt-image-2 的 API Key」

```js
checkAuth()
```
Expected: 行为与改动前一致（校验 DashScope Key）

- [ ] **Step 3: 提交**

```bash
git add public/js/app.js
git commit -m "feat: checkAuth 按模型选择校验对应的 API Key"
```

---

### Task 5: 图片生成模块接入

**Files:**
- Modify: `public/index.html`（imggen 面板，约 405-461 行）
- Modify: `public/js/imggen.js`
- Modify: `public/locales/zh-CN.json`、`public/locales/en.json`

- [ ] **Step 1: 加 i18n 文案**

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

- [ ] **Step 2: HTML 标记不支持的控件 + 加 note 容器**

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

- [ ] **Step 3: 模型列表加选项**

在 `public/js/imggen.js` 的 `getImggenModels()` 返回数组末尾追加：

```js
    { value: 'gpt-image-2', label: t('imggen.modelGptImage2') },
```

- [ ] **Step 4: 控件联动**

在 `updateImggenUI()` 函数体末尾（`wanControls.forEach(...)` 那行之后）追加：

```js
  // gpt-image-2 is a chat API: it has no size/count/seed/watermark parameters.
  const isT8 = isT8Model(model);
  document.querySelectorAll('[data-imggen-ds]').forEach(el => el.style.display = isT8 ? 'none' : '');
  if (isT8) {
    document.querySelectorAll('[data-imggen-wan]').forEach(el => el.style.display = 'none');
  }
```

- [ ] **Step 5: 提交时跳过不支持的参数**

在 `submitImggen()` 中，把开头的

```js
  const auth = checkAuth();
  if (!auth) return;

  const model = $('model-imggen').value;
```

改为（先读 model 再校验）：

```js
  const model = $('model-imggen').value;
  const auth = checkAuth(true, model);
  if (!auth) return;
```

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

- [ ] **Step 6: 渲染 note**

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

- [ ] **Step 7: 手动验证**

Run: `node server.js`，浏览器打开 http://localhost:3000 → 图片生成

1. 模型选 `gpt-image-2`
   Expected: 尺寸/数量/Seed/水印/反向提示词全部消失
2. Prompt 填「画只猫」→ 点生成
   Expected: 约 30-60s 后出图，图下方显示「模型说明：给你画好了…」
3. 模型切回 `qwen-image`
   Expected: 上述控件全部恢复显示
4. 用 `qwen-image` 生成一次
   Expected: 行为与改动前一致

- [ ] **Step 8: 提交**

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

- [ ] **Step 1: 加 i18n 文案**

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

- [ ] **Step 2: HTML 标记控件 + note 容器**

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

- [ ] **Step 3: 模型列表加选项**

在 `public/js/imgedit.js` 的 `getImgeditModels()` 返回数组末尾追加：

```js
    { value: 'gpt-image-2', label: t('imgedit.modelGptImage2') },
```

- [ ] **Step 4: 控件联动**

在 `updateImgeditUI()` 函数体末尾（`wanControls.forEach(...)` 之后）追加：

```js
  // gpt-image-2 is a chat API: it has no size/count/seed/watermark parameters.
  const isT8 = isT8Model(model);
  document.querySelectorAll('[data-imgedit-ds]').forEach(el => el.style.display = isT8 ? 'none' : '');
  if (isT8) {
    document.querySelectorAll('[data-imgedit-wan]').forEach(el => el.style.display = 'none');
  }
```

- [ ] **Step 5: 提交时跳过不支持的参数**

在 `submitImgedit()` 中，把开头的

```js
  const auth = checkAuth();
  if (!auth) return;

  const model = $('model-imgedit').value;
```

改为（先读 model 再校验）：

```js
  const model = $('model-imgedit').value;
  const auth = checkAuth(true, model);
  if (!auth) return;
```

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

- [ ] **Step 6: 渲染 note**

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

- [ ] **Step 7: 手动验证**

Run: `node server.js`，浏览器打开 http://localhost:3000 → 图片编辑

1. 模型选 `gpt-image-2`
   Expected: 尺寸/数量/Seed/水印/反向提示词消失，上传区与 Prompt 仍在
2. 上传 1 张图，Prompt 填「把背景改成蓝色」→ 点编辑
   Expected: 出图 + 模型说明（验证 base64 data URI 链路通）
3. 上传 2 张图，Prompt 填「把这两张图并排拼成一张」→ 点编辑
   Expected: 出图（验证多图链路通）
4. 模型切回 `qwen-image-edit`，编辑一次
   Expected: 行为与改动前一致
5. 打开历史记录
   Expected: 新记录的模型显示为 `gpt-image-2-pro`

- [ ] **Step 8: 提交**

```bash
git add public/index.html public/js/imgedit.js public/locales/zh-CN.json public/locales/en.json
git commit -m "feat: 图片编辑模块接入 gpt-image-2"
```

---

### Task 7: 文档更新与全量回归

**Files:**
- Modify: `README.md`

- [ ] **Step 1: 更新模型表**

在 README「支持的模型」表格里，`wan2.7-image` 那一行之后插入：

```markdown
| `gpt-image-2` | 文生图 / 图片编辑（t8star） | OpenAI 兼容 ✅ |
```

- [ ] **Step 2: 更新配置说明**

在 README「### 4. 配置」小节的列表末尾追加：

```markdown
- **API Key（gpt-image-2 / t8star）** — 使用 `gpt-image-2` 模型时必填，与 DashScope Key 相互独立
- **gpt-image-2 Base URL** — 可选，留空则用官方地址 `https://ai.t8star.org`
```

- [ ] **Step 3: 更新接口协议说明**

在 README「### 图片生成」小节末尾追加：

```markdown
- **`gpt-image-2`**（t8star）使用 OpenAI chat-completions 协议
  - 请求地址：`{base}/v1/chat/completions`
  - 请求格式：`{ model, stream: false, messages: [{ role: "user", content: string | [{ type: "text", text }, { type: "image_url", image_url: { url } }] }] }`
  - 响应格式：图片 URL 以 markdown `![image](url)` 内嵌在 `choices[0].message.content` 中，需正则提取；正文剩余部分作为模型说明展示
  - 不支持 `size` / `n` / `seed` / `watermark` / `negative_prompt` 等参数，选中该模型时相关控件自动隐藏
```

- [ ] **Step 4: 全量回归**

Run: `npm test`
Expected: 全部 PASS

Run: `node -e "require('./server.js')"`
Expected: 无报错退出（语法与加载检查）

- [ ] **Step 5: 提交**

```bash
git add README.md
git commit -m "docs: README 补充 gpt-image-2 模型与协议说明"
```

---

## 完成标准

- `npm test` 全绿，含新增的 `tests/t8star.test.js`
- 图片生成模块用 `gpt-image-2` 能出图，模型说明正常展示
- 图片编辑模块用 `gpt-image-2` 单图、多图均能出图
- 切换到任一 DashScope 模型，行为与接入前完全一致
- 未填 t8star Key 时给出明确提示而非 500
- 仓库中不含任何真实 API Key
