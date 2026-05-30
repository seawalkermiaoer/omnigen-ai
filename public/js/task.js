/* ── OmniGen AI: Shared task utilities (log, poll, submit, optimize, video) ── */

// ---------- Logging & status ----------
function makeLog(elId) {
  const el = $(elId);
  return (msg, type = '') => {
    const line = document.createElement('div');
    line.className = 'line ' + type;
    const t = new Date().toLocaleTimeString();
    line.innerHTML = `<span class="t">${t}</span>${msg}`;
    el.appendChild(line);
    el.scrollTop = el.scrollHeight;
  };
}
function setStatusHTML(elId, html) { $(elId).innerHTML = html; }

// ---------- Utilities ----------
function readAsDataURL(file) {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(r.result);
    r.onerror = reject;
    r.readAsDataURL(file);
  });
}
async function makeThumb(file, max = 96, quality = 0.7) {
  const dataUrl = await readAsDataURL(file);
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => {
      const r = Math.min(max / img.width, max / img.height, 1);
      const w = Math.round(img.width * r), h = Math.round(img.height * r);
      const c = document.createElement('canvas');
      c.width = w; c.height = h;
      c.getContext('2d').drawImage(img, 0, 0, w, h);
      try { resolve(c.toDataURL('image/jpeg', quality)); } catch { resolve(null); }
    };
    img.onerror = () => resolve(null);
    img.src = dataUrl;
  });
}
function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch]));
}
function escapeAttr(s) { return String(s || '').replace(/"/g, '&quot;'); }

// ---------- Video panel ----------
function renderVideo(ctx, url, data) {
  const out = $('videoOut-' + ctx.kind);
  const video = $('videoEl-' + ctx.kind);
  const meta = $('videoMeta-' + ctx.kind);
  out.style.display = '';
  video.src = url;
  const usage = data.usage || {};
  meta.textContent =
    `时长 ${usage.output_video_duration || data.output?.duration || ''}s · ` +
    `${usage.SR || ''}P · ${usage.ratio || ''} · 视频链接 24 小时内有效`;
  $('downloadBtn-' + ctx.kind).onclick = () => {
    const fname = `omnigen-${ctx.kind}-${Date.now()}.mp4`;
    window.location.href = `/api/download?url=${encodeURIComponent(url)}&filename=${encodeURIComponent(fname)}`;
  };
  $('copyBtn-' + ctx.kind).onclick = async () => {
    try {
      await navigator.clipboard.writeText(url);
      const b = $('copyBtn-' + ctx.kind);
      const old = b.textContent; b.textContent = '已复制 ✓';
      setTimeout(() => b.textContent = old, 1500);
    } catch { alert('复制失败：' + url); }
  };
}

// ---------- Polling ----------
async function pollTask(ctx, taskId, log) {
  const auth = getAuth();
  const start = Date.now();
  while (true) {
    try {
      const params = new URLSearchParams({
        apiKey: auth.apiKey, region: auth.region, workspaceId: auth.workspaceId || ''
      });
      const r = await fetch(`/api/task/${taskId}?` + params);
      const d = await r.json();
      const out = d.output || {};
      const status = out.task_status || 'UNKNOWN';
      const elapsed = Math.round((Date.now() - start) / 1000);
      log(`[${elapsed}s] 状态: ${status}`);
      setStatusHTML('status-' + ctx.kind,
        `<span class="pill ${status}">${status}</span> task_id: <code>${taskId}</code> · 已用时 ${elapsed}s`);

      if (ctx.currentHistoryId) {
        patchHistoryRecord(ctx.currentHistoryId, { status });
      }

      if (status === 'SUCCEEDED') {
        log('✓ 视频生成完成', 'ok');
        renderVideo(ctx, out.video_url, d);
        $('submitBtn-' + ctx.kind).disabled = false;
        if (ctx.currentHistoryId) {
          patchHistoryRecord(ctx.currentHistoryId, {
            status, videoUrl: out.video_url, endTime: Date.now(),
            usage: d.usage || null,
          });
        }
        return;
      }
      if (['FAILED', 'CANCELED', 'UNKNOWN'].includes(status)) {
        const err = out.message || d.message || '未知错误';
        log(`任务${status}: ${err}`, 'err');
        setStatusHTML('status-' + ctx.kind, `<span class="pill ${status}">${status}</span> ${err}`);
        $('submitBtn-' + ctx.kind).disabled = false;
        if (ctx.currentHistoryId) {
          patchHistoryRecord(ctx.currentHistoryId, {
            status, errorMsg: err, endTime: Date.now(),
          });
        }
        return;
      }
    } catch (e) {
      log('轮询异常: ' + e.message, 'err');
    }
    await new Promise(r => setTimeout(r, 15000));
  }
}

// ---------- Submit task ----------
async function submitTask(ctx, payload) {
  const auth = checkAuth();
  if (!auth) return;

  const log = makeLog('log-' + ctx.kind);
  $('log-' + ctx.kind).innerHTML = '';
  $('videoOut-' + ctx.kind).style.display = 'none';
  $('submitBtn-' + ctx.kind).disabled = true;
  setStatusHTML('status-' + ctx.kind, '<span class="pill PENDING">提交中</span> 正在创建任务…');
  log(`提交 ${payload.model} 任务到 ${auth.region}`);

  const historyId = genHistoryId();
  ctx.currentHistoryId = historyId;
  let thumbnails = [];
  if (ctx.kind === 'r2v' && ctx.state.images?.length) {
    thumbnails = ctx.state.images.map(i => i.thumb).filter(Boolean);
  } else if (ctx.kind === 'i2v' && ctx.state.image?.thumb) {
    thumbnails = [ctx.state.image.thumb];
  }
  addHistoryRecord({
    id: historyId,
    mode: ctx.kind,
    model: payload.model,
    status: 'PENDING',
    submitTime: Date.now(),
    region: auth.region,
    workspaceId: auth.workspaceId || '',
    prompt: payload.input?.prompt || '',
    params: payload.parameters || {},
    imageCount: ctx.kind === 'r2v' ? (ctx.state.images?.length || 0)
              : ctx.kind === 'i2v' ? (ctx.state.image ? 1 : 0) : 0,
    thumbnails,
    taskId: null,
  });

  try {
    const res = await fetch('/api/create-task', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...auth, payload }),
    });
    const data = await res.json();
    if (!res.ok || data.code) {
      throw new Error(data.message || data.error || JSON.stringify(data));
    }
    const taskId = data.output?.task_id;
    if (!taskId) throw new Error('未拿到 task_id');
    log('任务已创建：' + taskId, 'ok');
    setStatusHTML('status-' + ctx.kind, `<span class="pill PENDING">PENDING</span> task_id: <code>${taskId}</code>`);
    patchHistoryRecord(historyId, { taskId });
    pollTask(ctx, taskId, log);
  } catch (e) {
    log(e.message, 'err');
    setStatusHTML('status-' + ctx.kind, `<span class="pill FAILED">失败</span> ${e.message}`);
    $('submitBtn-' + ctx.kind).disabled = false;
    patchHistoryRecord(historyId, { status: 'FAILED', errorMsg: e.message, endTime: Date.now() });
  }
}

// ---------- Auto-optimize prompt ----------
function bindOptimize(ctx) {
  const btn = $('optimizeBtn-' + ctx.kind);
  const undoBtn = $('undoBtn-' + ctx.kind);
  const labelSpan = btn.querySelector('[data-label]');
  const hint = $('optimizeHint-' + ctx.kind);
  const promptEl = $('prompt-' + ctx.kind);
  let lastBefore = null;

  undoBtn.addEventListener('click', () => {
    if (lastBefore !== null) {
      promptEl.value = lastBefore;
      lastBefore = null;
      undoBtn.style.display = 'none';
      hint.textContent = '已还原';
      hint.className = 'optimize-hint';
    }
  });

  btn.addEventListener('click', async () => {
    const auth = checkAuth();
    if (!auth) return;
    const draft = promptEl.value;
    let images = [];
    let videoCount = 0;
    let mode = ctx.kind;
    if (ctx.kind === 'r2v') {
      images = ctx.state.images.map(i => i.base64Url);
      if (isWanR2v()) {
        mode = 'r2v_wan';
        videoCount = ctx.state.videos.filter(v => v.url).length;
      }
    } else if (ctx.kind === 'i2v') {
      if (ctx.state.image) images.push(ctx.state.image.base64Url);
      if (isWanI2v()) {
        mode = 'i2v_wan';
        if (ctx.state.lastFrame) images.push(ctx.state.lastFrame.base64Url);
        if (ctx.state.firstClip) videoCount = 1;
      }
    }

    btn.disabled = true; btn.classList.add('loading');
    labelSpan.textContent = '优化中…';
    const srcDesc = [
      images.length ? `${images.length} 张图` : '',
      videoCount ? `${videoCount} 个视频` : '',
    ].filter(Boolean).join(' + ');
    hint.textContent = images.length
      ? `${MODEL_NAMES.VISION_OPTIMIZE_LABEL} 正在分析${ctx.kind === 'i2v' ? '首帧图' : srcDesc || ' 图'}并改写 prompt…`
      : `${MODEL_NAMES.TEXT_OPTIMIZE} 正在润色 prompt…`;
    hint.className = 'optimize-hint';
    undoBtn.style.display = 'none';

    try {
      const res = await fetch('/api/optimize-prompt', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...auth, draft, images, mode, videoCount }),
      });
      const data = await res.json();
      if (!res.ok || data.error) throw new Error(data.error || '优化失败');
      lastBefore = draft;
      promptEl.value = data.prompt;
      hint.textContent = `✓ 已用 ${data.model} 优化（${data.prompt.length} 字）`;
      hint.className = 'optimize-hint ok';
      undoBtn.style.display = '';
    } catch (e) {
      hint.textContent = '优化失败：' + e.message;
      hint.className = 'optimize-hint err';
    } finally {
      btn.disabled = false;
      btn.classList.remove('loading');
      labelSpan.textContent = '自动优化';
    }
  });
}

// ---------- Collect generation params ----------
function collectParams(kind) {
  const params = {
    resolution: $('resolution-' + kind).value,
    duration: parseInt($('duration-' + kind).value, 10) || 5,
    watermark: $('watermark-' + kind).checked,
  };
  const ratioEl = $('ratio-' + kind);
  if (ratioEl) params.ratio = ratioEl.value;
  const seedVal = $('seed-' + kind).value.trim();
  if (seedVal) params.seed = parseInt(seedVal, 10);
  return params;
}
