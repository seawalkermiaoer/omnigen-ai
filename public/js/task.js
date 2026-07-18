/* ── OmniGen AI: Shared task utilities (log, poll, submit, optimize, video) ── */

const DEFAULT_WATERMARK_TEXT = 'OmniGen AIStudio';

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

// Upload file to server for compression + optional OSS storage.
// Returns a URL: either data:... Base64 or https://... signed OSS URL.
async function uploadImageToServer(file) {
  let lastError;
  for (let attempt = 0; attempt <= 2; attempt++) {
    try {
      const formData = new FormData();
      formData.append('file', file);
      const res = await fetch('/api/upload-image', { method: 'POST', body: formData });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || t('common.uploadFailedGeneric') + ' (' + res.status + ')');
      return data.url;
    } catch (e) {
      lastError = e;
      if (attempt < 2) await new Promise(r => setTimeout(r, 1000 * (attempt + 1)));
    }
  }
  throw lastError;
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

function collectWatermarkParams(kind) {
  const enabled = $('watermark-' + kind).checked;
  return { watermark: enabled };
}

// ---------- Video panel ----------
function renderVideo(ctx, url, data) {
  const out = $('videoOut-' + ctx.kind);
  const video = $('videoEl-' + ctx.kind);
  const meta = $('videoMeta-' + ctx.kind);
  out.style.display = '';
  video.src = url;
  const usage = data.usage || {};
  meta.textContent = t('task.videoMeta', {
    duration: usage.output_video_duration || data.output?.duration || '',
    sr: usage.SR || '',
    ratio: usage.ratio || ''
  });
  $('downloadBtn-' + ctx.kind).onclick = () => {
    const fname = `omnigen-${ctx.kind}-${Date.now()}.mp4`;
    window.location.href = `/api/download?url=${encodeURIComponent(url)}&filename=${encodeURIComponent(fname)}`;
  };
  $('copyBtn-' + ctx.kind).onclick = async () => {
    try {
      await navigator.clipboard.writeText(url);
      const b = $('copyBtn-' + ctx.kind);
      const old = b.textContent; b.textContent = t('common.copySuccess');
      setTimeout(() => b.textContent = old, 1500);
    } catch { alert(t('common.copyFailed', { url })); }
  };
}

// ---------- Polling ----------
async function pollTask(ctx, taskId, log) {
  const auth = getAuth();
  const start = Date.now();
  while (true) {
    try {
      const params = new URLSearchParams({
        apiKey: auth.apiKey, region: auth.region, workspaceId: auth.workspaceId || '', endpoint: auth.endpoint || ''
      });
      const r = await fetch(`/api/task/${taskId}?` + params);
      const d = await r.json();
      const out = d.output || {};
      const status = out.task_status || 'UNKNOWN';
      const elapsed = Math.round((Date.now() - start) / 1000);
      log(`[${elapsed}s] ${t('task.status', { status })}`);
      setStatusHTML('status-' + ctx.kind,
        `<span class="pill ${status}">${status}</span> task_id: <code>${taskId}</code> · ${t('task.elapsed', { elapsed })}`);

      if (ctx.currentHistoryId) {
        patchHistoryRecord(ctx.currentHistoryId, { status });
      }

      if (status === 'SUCCEEDED') {
        log(t('task.videoDone'), 'ok');
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
        const err = out.message || d.message || t('task.unknownError');
        log(t('task.taskFailed', { status, err }), 'err');
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
      log(t('task.pollError', { msg: e.message }), 'err');
    }
    await new Promise(r => setTimeout(r, 15000));
  }
}

// ---------- Submit task ----------
async function submitTask(ctx, payload) {
  // t8star speaks only the image chat API; DashScope video endpoints do not exist there.
  if (isT8Provider()) {
    alert(t('config.t8NoVideo'));
    return;
  }
  const auth = checkAuth();
  if (!auth) return;

  const log = makeLog('log-' + ctx.kind);
  $('log-' + ctx.kind).innerHTML = '';
  $('videoOut-' + ctx.kind).style.display = 'none';
  $('submitBtn-' + ctx.kind).disabled = true;
  setStatusHTML('status-' + ctx.kind, `<span class="pill PENDING">${t('task.submitting')}</span> ${t('task.submitting')}`);
  log(t('task.submitLog', { model: payload.model, region: auth.region }));

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
    if (!taskId) throw new Error(t('task.noTaskId'));
    log(t('task.taskCreated', { taskId }), 'ok');
    setStatusHTML('status-' + ctx.kind, `<span class="pill PENDING">PENDING</span> task_id: <code>${taskId}</code>`);
    patchHistoryRecord(historyId, { taskId });
    pollTask(ctx, taskId, log);
  } catch (e) {
    log(e.message, 'err');
    setStatusHTML('status-' + ctx.kind, `<span class="pill FAILED">${t('common.pillFailed')}</span> ${e.message}`);
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
      hint.textContent = t('common.removed');
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
    } else if (ctx.kind === 'imggen') {
      mode = 't2i';
    } else if (ctx.kind === 'imgedit') {
      if (ctx.state.images.length) {
        images = ctx.state.images.map(i => i.base64Url);
        mode = 'imggen_edit';
      }
    }

    btn.disabled = true; btn.classList.add('loading');
    labelSpan.textContent = t('common.optimizing');
    const srcDesc = [
      images.length ? t('task.optimizeSource', { count: images.length }) : '',
      videoCount ? t('task.optimizeSourceVideo', { count: videoCount }) : '',
    ].filter(Boolean).join(' + ');
    hint.textContent = images.length
      ? t('task.optimizeAnalyzing', { model: MODEL_NAMES.VISION_OPTIMIZE_LABEL, target: ctx.kind === 'i2v' ? t('task.optimizeFirstFrame') : srcDesc || t('task.optimizeSource', { count: '' }).trim() })
      : t('task.optimizePolishing', { model: MODEL_NAMES.TEXT_OPTIMIZE });
    hint.className = 'optimize-hint';
    undoBtn.style.display = 'none';

    try {
      const res = await fetch('/api/optimize-prompt', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...auth, draft, images, mode, videoCount }),
      });
      const data = await res.json();
      if (!res.ok || data.error) throw new Error(data.error || t('common.optimizeFailed'));
      lastBefore = draft;
      promptEl.value = data.prompt;
      hint.textContent = t('common.optimizeDone', { model: data.model, count: data.prompt.length });
      hint.className = 'optimize-hint ok';
      undoBtn.style.display = '';
    } catch (e) {
      hint.textContent = t('common.optimizeFailedDetail', { msg: e.message });
      hint.className = 'optimize-hint err';
    } finally {
      btn.disabled = false;
      btn.classList.remove('loading');
      labelSpan.textContent = t('common.autoOptimize');
    }
  });
}

// ---------- Collect generation params ----------
function collectParams(kind) {
  const params = {
    resolution: $('resolution-' + kind).value,
    duration: parseInt($('duration-' + kind).value, 10) || 5,
    ...collectWatermarkParams(kind),
  };
  const ratioEl = $('ratio-' + kind);
  if (ratioEl) params.ratio = ratioEl.value;
  const seedVal = $('seed-' + kind).value.trim();
  if (seedVal) params.seed = parseInt(seedVal, 10);
  return params;
}

// ---------- Provider gate (video tabs) ----------
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
