const express = require('express');
const path = require('path');
const https = require('https');
const { URL } = require('url');

const app = express();
const PORT = process.env.PORT || 3000;

app.use(express.json({ limit: '200mb' }));
app.use(express.static(path.join(__dirname, 'public')));

// Expose model configuration to the frontend
app.get('/api/config', (_req, res) => {
  res.json({ models: MODELS });
});

const REGION_ENDPOINTS = {
  'cn-beijing': 'https://dashscope.aliyuncs.com',
  'ap-southeast-1': 'https://dashscope-intl.aliyuncs.com',
  'us-east-1': 'https://dashscope-us.aliyuncs.com',
};

// ─── Model configuration ────────────────────────────────────────
// Centralized model names — update here to change models globally.
const MODELS = {
  /** Text-only prompt optimization (OpenAI-compatible) */
  TEXT_OPTIMIZE: 'qwen3.7-max',
  /** Multimodal prompt optimization with reference images (DashScope native) */
  VISION_OPTIMIZE: 'qwen-vl-max-latest',
  /** Display label shown in UI (short form, no "-latest" suffix) */
  VISION_OPTIMIZE_LABEL: 'qwen-vl-max',
  /** Image generation models using OpenAI-compatible protocol */
  IMAGE_OPENAI: ['Qwen-Image-Plus', 'Qwen-Image', 'Qwen-Image-Edit-Plus', 'Qwen-Image-Edit'],
  /** Image generation models using DashScope native protocol */
  IMAGE_DASHSCOPE: ['wan2.7-image-pro', 'wan2.7-image'],
};

function getEndpoint(region, workspaceId) {
  if (region === 'eu-central-1') {
    if (!workspaceId) throw new Error('德国（法兰克福）地域必须提供 WorkspaceId');
    return `https://${workspaceId}.eu-central-1.maas.aliyuncs.com`;
  }
  return REGION_ENDPOINTS[region] || REGION_ENDPOINTS['cn-beijing'];
}

function forwardJSON(targetUrl, method, headers, bodyObj) {
  return new Promise((resolve, reject) => {
    const url = new URL(targetUrl);
    const body = bodyObj ? JSON.stringify(bodyObj) : null;
    const reqHeaders = { ...headers };
    if (body) {
      reqHeaders['Content-Length'] = Buffer.byteLength(body);
      reqHeaders['Content-Type'] = 'application/json';
    }
    const req = https.request(
      {
        hostname: url.hostname,
        port: 443,
        path: url.pathname + url.search,
        method,
        headers: reqHeaders,
      },
      (res) => {
        const chunks = [];
        res.on('data', (c) => chunks.push(c));
        res.on('end', () => {
          const text = Buffer.concat(chunks).toString('utf8');
          let data;
          try { data = JSON.parse(text); } catch { data = { raw: text }; }
          resolve({ status: res.statusCode, data });
        });
      }
    );
    req.on('error', reject);
    if (body) req.write(body);
    req.end();
  });
}

app.post('/api/create-task', async (req, res) => {
  try {
    const { apiKey, region, workspaceId, payload } = req.body;
    if (!apiKey) return res.status(400).json({ error: '缺少 API Key' });
    if (!payload) return res.status(400).json({ error: '缺少 payload' });

    const endpoint = getEndpoint(region, workspaceId);
    const url = `${endpoint}/api/v1/services/aigc/video-generation/video-synthesis`;
    const r = await forwardJSON(url, 'POST', {
      Authorization: `Bearer ${apiKey}`,
      'X-DashScope-Async': 'enable',
    }, payload);
    res.status(r.status).json(r.data);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// ─── Image generation (synchronous) ─────────────────────────────
app.post('/api/generate-image', async (req, res) => {
  try {
    const { apiKey, region, workspaceId, model, prompt, images, params } = req.body;
    if (!apiKey) return res.status(400).json({ error: '缺少 API Key' });
    if (!model) return res.status(400).json({ error: '缺少 model' });

    const endpoint = getEndpoint(region, workspaceId);
    const isQwen = MODELS.IMAGE_OPENAI.includes(model);
    const isDashScope = MODELS.IMAGE_DASHSCOPE.includes(model);
    if (!isQwen && !isDashScope) {
      return res.status(400).json({ error: '不支持的图片模型: ' + model });
    }

    let url, payload;
    const p = params || {};

    if (isQwen) {
      // OpenAI-compatible protocol
      url = `${endpoint}/compatible-mode/v1/images/generations`;
      payload = { model, prompt };
      if (p.size) payload.size = p.size;
      if (p.n != null) payload.n = p.n;
      if (p.negative_prompt) payload.negative_prompt = p.negative_prompt;
      if (p.watermark != null) payload.watermark = p.watermark;
      if (p.prompt_extend != null) payload.prompt_extend = p.prompt_extend;
      if (p.seed != null) payload.seed = p.seed;
      // Attach input images for edit models
      if (Array.isArray(images) && images.length > 0) {
        payload.image = images[0];
        if (images[1]) payload.image2 = images[1];
        if (images[2]) payload.image3 = images[2];
      }
    } else {
      // DashScope native protocol (wan2.7 series)
      url = `${endpoint}/api/v1/services/aigc/multimodal-generation/generation`;
      const content = [];
      if (Array.isArray(images)) {
        images.forEach(img => content.push({ image: img }));
      }
      if (prompt) content.push({ text: prompt });
      payload = {
        model,
        input: {
          messages: [{ role: 'user', content }],
        },
        parameters: {},
      };
      if (p.size) payload.parameters.size = p.size;
      if (p.n != null) payload.parameters.n = p.n;
      if (p.watermark != null) payload.parameters.watermark = p.watermark;
      if (p.thinking_mode != null) payload.parameters.thinking_mode = p.thinking_mode;
      if (p.seed != null) payload.parameters.seed = p.seed;
      if (p.enable_sequential != null) payload.parameters.enable_sequential = p.enable_sequential;
    }

    const r = await forwardJSON(url, 'POST', {
      Authorization: `Bearer ${apiKey}`,
    }, payload);

    // Error handling: both OpenAI and DashScope native formats
    if (r.data && (r.data.code || r.data.error)) {
      const msg = r.data.message || r.data.error?.message || r.data.code || '调用失败';
      return res.status(r.status || 400).json({ error: msg });
    }

    // Normalize response to unified format: { images: [...], usage: {...}, model }
    let imageUrls = [];
    let usage = {};

    if (isQwen) {
      // OpenAI response: { data: [{ url, b64_json }], usage: {...} }
      if (Array.isArray(r.data?.data)) {
        imageUrls = r.data.data.map(d => d.url || d.b64_json).filter(Boolean);
      }
      usage = r.data?.usage || {};
    } else {
      // DashScope native response: { output: { choices: [{ message: { content: [{ image, type }] } }] }, usage: {...} }
      const choices = r.data?.output?.choices;
      if (Array.isArray(choices) && choices.length > 0) {
        const msgContent = choices[0]?.message?.content;
        if (Array.isArray(msgContent)) {
          imageUrls = msgContent
            .filter(c => c.image || c.type === 'image')
            .map(c => c.image)
            .filter(Boolean);
        }
      }
      usage = r.data?.usage || {};
    }

    if (imageUrls.length === 0) {
      return res.status(502).json({ error: '模型未返回图片结果', raw: r.data });
    }

    res.json({ images: imageUrls, usage, model });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

const SYSTEM_PROMPTS = {
  r2v: `你是 OmniGen AI 参考生视频（happyhorse-1.0-r2v）的资深 prompt 工程师。

【任务】根据用户的草稿和多张参考图，输出一段电影感强、可直接调用模型的中文 prompt。

【硬性规则】
1. 用户上传的参考图按顺序对应 [Image 1]、[Image 2]、[Image 3] …，你必须用该标记在 prompt 中指代图中的具体对象。例：「[Image 1]中身着红色旗袍的女性」。
2. 必须明确指出每张参考图里被借用的元素（人物、物体、配饰、场景），不要笼统说"参考这张图"。
3. 至少描述 1 个镜头运动（推近/拉远/环绕/侧移/低角度仰拍等）和 1 个明确的动作。
4. 加入光影、节奏、情绪等电影化细节，但避免堆砌形容词。
5. 长度不超过 2500 个中文字符（含标点）。
6. 仅输出最终 prompt 本身，不要前后加任何解释、标题、引号或 Markdown。
7. 用户原 prompt 中已含的核心意图必须保留，仅做润色和扩展。`,

  t2v: `你是 OmniGen AI 文生视频（happyhorse-1.0-t2v）的资深 prompt 工程师。

【任务】根据用户的草稿，输出一段画面感与运动感俱佳的中文 prompt，用于纯文生视频。

【硬性规则】
1. 不存在参考图，禁止使用 [Image 1] 之类的标记。
2. 必须包含主体（人/物/场景）的具体细节、明确动作、至少 1 个镜头语言（推近/拉远/环绕/俯拍/仰拍/手持跟随等）。
3. 加入光影、时间、天气、氛围、节奏等电影化细节，但避免堆砌形容词。
4. 长度不超过 2500 个中文字符。
5. 仅输出最终 prompt 本身，不要任何解释、标题、引号或 Markdown。
6. 用户原 prompt 的核心意图必须保留，仅做润色和扩展。`,

  r2v_wan: `你是阿里云百炼 Wan 2.7 参考生视频（wan2.7-r2v）的资深 prompt 工程师。

【任务】根据用户草稿、参考图与参考视频，输出一段电影感强、可直接送入 wan2.7-r2v 的中文 prompt。

【硬性规则】
1. 参考图按上传顺序对应「图1、图2、图3…」，参考视频按上传顺序对应「视频1、视频2…」。**图和视频分别计数**，可同时存在「图1」「视频1」。你必须使用这种标记指代素材里的具体对象，例「图1中身着红色旗袍的女性」「视频1中的男主角」。
2. 必须明确指出每个参考素材里被借用的元素（人物、动物、物体、场景），不要笼统说"参考这张图/这个视频"。
3. 至少含 1 个镜头运动（推近/拉远/环绕/侧移/低角度仰拍等）和 1 个明确的动作；多素材时建议描写素材之间的互动。
4. 加入光影、节奏、情绪等电影化细节，但避免堆砌形容词。
5. 长度不超过 5000 个中文字符。
6. 仅输出最终 prompt 本身，不要前后加任何解释、标题、引号或 Markdown。
7. 用户原 prompt 的核心意图必须保留，仅做润色和扩展。`,

  i2v_wan: `你是阿里云百炼 Wan 2.7 图生视频（wan2.7-i2v）的资深 prompt 工程师。

【背景】用户的素材组合可能是以下之一：
- 首帧生视频：仅首帧图（可选 driving_audio）
- 首尾帧生视频：首帧+尾帧（可选 driving_audio）
- 视频续写：首段视频片段（可选 last_frame 限定结尾）

你能直接看到的是 **图像素材**（首帧、尾帧）。视频片段、音频以 URL 形式存在，看不到。

【任务】根据所提供素材与用户草稿，输出一段中文 prompt 描述视频中要发生的内容。

【硬性规则】
1. 不要使用 [Image N] 或 图N 之类的标记，直接描述画面与动作。
2. 描述要与素材语义匹配：
   - 仅首帧：描述从首帧开始接下来发生什么。
   - 首帧+尾帧：描述从首帧自然过渡到尾帧的中间过程。
   - 视频续写：描述首段视频结束后接下来发生什么。
   - 含 driving_audio：动作/口型应与音频节奏对齐（如果用户提示是 rap/演唱）。
3. 至少含 1 个明确动作或运动；推荐加入镜头变化（推近/拉远/环绕/手持跟随等）。
4. 描述应与已知图像物理一致，不要凭空出现素材里没有的人或物（除非用户草稿明确要求）。
5. 长度不超过 5000 字符。
6. 仅输出最终 prompt 本身，不要解释、标题、引号、Markdown。
7. 用户原 prompt 的核心意图必须保留，仅做润色与扩展。
8. 若用户什么素材都没传，按一般运动镜头描述输出。`,

  i2v: `你是 OmniGen AI 图生视频-首帧（happyhorse-1.0-i2v）的资深 prompt 工程师。

【背景】用户上传了 1 张首帧图像，作为视频的第一帧；你看到的图就是该首帧。

【任务】根据首帧图与用户草稿，输出一段描述"从该首帧开始接下来发生什么"的中文 prompt。

【硬性规则】
1. prompt 描述的是从首帧开始的动作、运动与变化，不要复述首帧本身的静态画面（首帧已经存在了，不必再描述其外观）。
2. 禁止使用 [Image N] 标记，全文只有一张图。
3. 必须包含至少 1 个明确的动作或运动；推荐加入镜头变化（推近/拉远/环绕/手持跟随等）。
4. 描述应与首帧画面物理一致——不要凭空出现首帧里没有的人或物（除非用户草稿明确要求）。
5. 长度不超过 2500 个中文字符。
6. 仅输出最终 prompt 本身，不要任何解释、标题、引号或 Markdown。
7. 用户原 prompt 的核心意图必须保留，仅做润色和扩展。
8. 若用户未上传首帧（仅有草稿），按通用运动镜头描述输出，不要凭空假设画面内容。`,
};

app.post('/api/optimize-prompt', async (req, res) => {
  try {
    const { apiKey, region, workspaceId, draft, images, mode, videoCount } = req.body;
    if (!apiKey) return res.status(400).json({ error: '缺少 API Key' });

    const endpoint = getEndpoint(region, workspaceId);
    const hasImages = Array.isArray(images) && images.length > 0;
    const vCount = Number.isInteger(videoCount) ? videoCount : 0;
    const sysPrompt = SYSTEM_PROMPTS[mode] || SYSTEM_PROMPTS.t2v;

    const sourceDesc = (() => {
      const parts = [];
      if (hasImages) parts.push(`${images.length} 张参考图（${mode === 'r2v_wan' ? '对应「图1」～「图' + images.length + '」' : '对应 [Image 1]…[Image ' + images.length + ']'}）`);
      if (vCount > 0) parts.push(`${vCount} 个参考视频（对应「视频1」～「视频${vCount}」，仅以 URL 形式存在，模型看不到画面）`);
      return parts.length ? `用户提供了 ${parts.join('、')}。` : '';
    })();

    const draftText = (draft && draft.trim())
      ? `${sourceDesc}\n\n用户草稿：\n${draft}\n\n请基于以上信息，输出优化后的 prompt。`
      : (hasImages || vCount > 0
          ? `${sourceDesc}\n\n用户没有提供草稿。请自由构思一段短视频的镜头描述。`
          : `用户没有提供草稿。请自由构思一段 5 秒视频的镜头描述。`);

    let url, payload, model, isCompat = false;
    if (hasImages) {
      // 有参考图 → 多模态原生接口
      model = MODELS.VISION_OPTIMIZE;
      url = `${endpoint}/api/v1/services/aigc/multimodal-generation/generation`;
      const userContent = images.map(img => ({ image: img }));
      userContent.push({ text: draftText });
      payload = {
        model,
        input: {
          messages: [
            { role: 'system', content: [{ text: sysPrompt }] },
            { role: 'user', content: userContent },
          ],
        },
        parameters: { result_format: 'message' },
      };
    } else {
      // 无图（文生视频，或 i2v 优化时还没传首帧）→ OpenAI 兼容
      model = MODELS.TEXT_OPTIMIZE;
      url = `${endpoint}/compatible-mode/v1/chat/completions`;
      isCompat = true;
      payload = {
        model,
        messages: [
          { role: 'system', content: sysPrompt },
          { role: 'user', content: draftText },
        ],
      };
    }

    const r = await forwardJSON(url, 'POST', {
      Authorization: `Bearer ${apiKey}`,
    }, payload);

    // 错误响应：DashScope 原生用 r.data.code；OpenAI 兼容用 r.data.error
    if (r.data && (r.data.code || r.data.error)) {
      const msg = r.data.message || r.data.error?.message || r.data.code || '调用失败';
      return res.status(r.status || 400).json({ error: msg });
    }

    // 解析 choices：兼容两种返回结构
    const choices = isCompat ? r.data?.choices : r.data?.output?.choices;
    if (!choices || !choices.length) {
      return res.status(502).json({ error: '模型未返回结果', raw: r.data });
    }
    const content = choices[0]?.message?.content;
    let text = '';
    if (typeof content === 'string') text = content.trim();
    else if (Array.isArray(content)) text = content.map(c => c.text || '').join('').trim();
    if (!text) return res.status(502).json({ error: '解析模型输出失败', raw: r.data });

    res.json({ prompt: text, model });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

app.get('/api/task/:taskId', async (req, res) => {
  try {
    const { apiKey, region, workspaceId } = req.query;
    const { taskId } = req.params;
    if (!apiKey) return res.status(400).json({ error: '缺少 API Key' });

    const endpoint = getEndpoint(region, workspaceId);
    const url = `${endpoint}/api/v1/tasks/${taskId}`;
    const r = await forwardJSON(url, 'GET', {
      Authorization: `Bearer ${apiKey}`,
    }, null);
    res.status(r.status).json(r.data);
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

app.get('/api/download', (req, res) => {
  const target = req.query.url;
  const filename = req.query.filename || `omnigen-${Date.now()}.mp4`;
  if (!target) return res.status(400).send('missing url');
  try {
    const url = new URL(target);
    https.get({
      hostname: url.hostname,
      path: url.pathname + url.search,
      headers: { 'User-Agent': 'omnigen-web/1.0' },
    }, (upstream) => {
      if (upstream.statusCode >= 300 && upstream.statusCode < 400 && upstream.headers.location) {
        res.redirect(`/api/download?url=${encodeURIComponent(upstream.headers.location)}&filename=${encodeURIComponent(filename)}`);
        return;
      }
      res.setHeader('Content-Type', upstream.headers['content-type'] || 'application/octet-stream');
      if (upstream.headers['content-length']) {
        res.setHeader('Content-Length', upstream.headers['content-length']);
      }
      res.setHeader('Content-Disposition', `attachment; filename="${filename}"`);
      upstream.pipe(res);
    }).on('error', (e) => res.status(502).send('download failed: ' + e.message));
  } catch (e) {
    res.status(400).send('invalid url');
  }
});

app.listen(PORT, () => {
  console.log(`OmniGen AI Web running at http://localhost:${PORT}`);
});
