/* ── OmniGen AI: Text-to-Image generation ── */

const imggen = {
  kind: 'imggen',
  state: {},
  currentHistoryId: null,
};

// ─── Model definitions (text-to-image only) ─────────────────────
const IMGGEN_MODELS = [
  { value: 'qwen-image-plus', label: 'Qwen-Image-Plus（高质量，DashScope 协议）' },
  { value: 'qwen-image', label: 'Qwen-Image（快速，DashScope 协议）' },
  { value: 'wan2.7-image-pro', label: 'Wan 2.7 Image Pro（高质量，DashScope 协议）' },
  { value: 'wan2.7-image', label: 'Wan 2.7 Image（基础版，DashScope 协议）' },
];

// Qwen models use these sizes; wan2.7 uses 1K/2K/4K
const QWEN_SIZES = [
  { value: '1664*928', label: '1664×928（16:9）' },
  { value: '1472*1140', label: '1472×1140（4:3）' },
  { value: '1328*1328', label: '1328×1328（1:1）' },
  { value: '1140*1472', label: '1140×1472（3:4）' },
  { value: '928*1664', label: '928×1664（9:16）' },
];
const WAN_SIZES = [
  { value: '1K', label: '1K（默认）' },
  { value: '2K', label: '2K' },
  { value: '4K', label: '4K' },
];

function isWanModel(model) {
  return model && model.startsWith('wan2.7');
}

// ─── UI update ──────────────────────────────────────────────────
function updateImggenUI() {
  const model = $('model-imggen').value;
  const isWan = isWanModel(model);

  // Update model dropdown
  const modelSelect = $('model-imggen');
  const prevValue = modelSelect.value;
  modelSelect.innerHTML = IMGGEN_MODELS.map(m => `<option value="${m.value}">${m.label}</option>`).join('');
  if (IMGGEN_MODELS.some(m => m.value === prevValue)) {
    modelSelect.value = prevValue;
  }

  // Update size options
  updateSizeOptions();
  // Update count options
  updateCountOptions();

  // Show/hide wan2.7-specific controls
  const wanControls = document.querySelectorAll('[data-imggen-wan]');
  wanControls.forEach(el => el.style.display = isWan ? '' : 'none');
}

function updateSizeOptions() {
  const model = $('model-imggen').value;
  const isWan = isWanModel(model);
  const sizes = isWan ? WAN_SIZES : QWEN_SIZES;
  const sizeSelect = $('size-imggen');
  sizeSelect.innerHTML = sizes.map(s => `<option value="${s.value}">${s.label}</option>`).join('');
  if (!isWan) {
    sizeSelect.value = '1328*1328';
  }
}

function updateCountOptions() {
  const model = $('model-imggen').value;
  const countSelect = $('count-imggen');
  let options = [];

  if (model === 'qwen-image-plus' || model === 'qwen-image') {
    options = [1];
  } else if (isWanModel(model)) {
    const isSequential = $('sequential-imggen').checked;
    const max = isSequential ? 12 : 4;
    for (let i = 1; i <= max; i++) options.push(i);
  } else {
    options = [1];
  }

  const prev = parseInt(countSelect.value, 10) || 1;
  countSelect.innerHTML = options.map(n => `<option value="${n}">${n}</option>`).join('');
  if (options.includes(prev)) {
    countSelect.value = prev;
  }
}

// Model change handler
$('model-imggen').addEventListener('change', updateImggenUI);

// Sequential checkbox changes count options
$('sequential-imggen').addEventListener('change', updateCountOptions);

// ─── Submit (synchronous, no polling) ──────────────────────────
$('submitBtn-imggen').addEventListener('click', submitImggen);

async function submitImggen() {
  const auth = checkAuth();
  if (!auth) return;

  const model = $('model-imggen').value;
  const prompt = $('prompt-imggen').value.trim();

  // Validate
  if (!prompt) {
    return alert('请输入 Prompt');
  }
  // wan2.7 region check
  if (isWanModel(model) && !['cn-beijing', 'ap-southeast-1'].includes(auth.region)) {
    return alert('Wan 2.7 图片模型仅支持北京和新加坡地域，当前地域为：' + auth.region);
  }

  // Build params
  const params = {
    size: $('size-imggen').value,
    n: parseInt($('count-imggen').value, 10) || 1,
    ...collectWatermarkParams('imggen'),
  };
  const negPrompt = $('negative-imggen').value.trim();
  if (negPrompt) params.negative_prompt = negPrompt;
  const seedVal = $('seed-imggen').value.trim();
  if (seedVal) params.seed = parseInt(seedVal, 10);
  if (isWanModel(model)) {
    params.thinking_mode = $('thinking-imggen').checked;
    params.enable_sequential = $('sequential-imggen').checked;
  }

  const log = makeLog('log-imggen');
  $('log-imggen').innerHTML = '';
  $('imageOut-imggen').style.display = 'none';
  $('submitBtn-imggen').disabled = true;
  setStatusHTML('status-imggen', '<span class="pill PENDING">生成中</span> 正在调用模型，请稍候…');
  log(`提交 ${model} 任务到 ${auth.region}`);

  // Create history record
  const historyId = genHistoryId();
  imggen.currentHistoryId = historyId;

  addHistoryRecord({
    id: historyId,
    mode: 'imggen',
    subMode: 't2i',
    model,
    status: 'PENDING',
    submitTime: Date.now(),
    region: auth.region,
    workspaceId: auth.workspaceId || '',
    prompt,
    params,
    imageCount: 0,
    thumbnails: [],
  });

  try {
    const startTime = Date.now();
    const res = await fetch('/api/generate-image', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...auth, model, prompt, images: [], params }),
    });
    const data = await res.json();
    const elapsed = Math.round((Date.now() - startTime) / 1000);

    if (!res.ok || data.error) {
      throw new Error(data.error || JSON.stringify(data));
    }

    const imageUrls = data.images || [];
    if (!imageUrls.length) throw new Error('模型未返回图片结果');

    log(`✓ 生成完成，共 ${imageUrls.length} 张图片（耗时 ${elapsed}s）`, 'ok');
    setStatusHTML('status-imggen', `<span class="pill SUCCEEDED">完成</span> ${imageUrls.length} 张图片 · 耗时 ${elapsed}s`);

    renderImggenImageResults(imageUrls, data.usage);

    patchHistoryRecord(historyId, {
      status: 'SUCCEEDED',
      imageUrls,
      endTime: Date.now(),
      usage: data.usage || null,
    });
  } catch (e) {
    log(e.message, 'err');
    setStatusHTML('status-imggen', `<span class="pill FAILED">失败</span> ${escapeHTML(e.message)}`);
    patchHistoryRecord(historyId, { status: 'FAILED', errorMsg: e.message, endTime: Date.now() });
  } finally {
    $('submitBtn-imggen').disabled = false;
  }
}

// ─── Render image results ───────────────────────────────────────
function renderImggenImageResults(imageUrls, usage) {
  const out = $('imageOut-imggen');
  const grid = $('imageGrid-imggen');
  const meta = $('imageMeta-imggen');

  out.style.display = '';
  const count = imageUrls.length;
  const sizeInfo = usage?.size || '';
  meta.textContent = `生成了 ${count} 张图片${sizeInfo ? ' · ' + sizeInfo : ''} · 链接 24 小时内有效`;

  grid.innerHTML = imageUrls.map((url, i) => `
    <div class="image-card">
      <img src="${escapeAttr(url)}" alt="生成图片 ${i + 1}" />
      <div class="actions">
        <button class="primary" data-download="${i}">下载</button>
        <button class="ghost" data-copy="${i}">复制链接</button>
      </div>
    </div>
  `).join('');

  grid.querySelectorAll('[data-download]').forEach(btn => {
    btn.addEventListener('click', () => {
      const url = imageUrls[parseInt(btn.dataset.download, 10)];
      const fname = `omnigen-imggen-${Date.now()}-${btn.dataset.download}.png`;
      window.location.href = `/api/download?url=${encodeURIComponent(url)}&filename=${encodeURIComponent(fname)}`;
    });
  });

  grid.querySelectorAll('[data-copy]').forEach(btn => {
    btn.addEventListener('click', async () => {
      const url = imageUrls[parseInt(btn.dataset.copy, 10)];
      try {
        await navigator.clipboard.writeText(url);
        const old = btn.textContent;
        btn.textContent = '已复制 ✓';
        setTimeout(() => btn.textContent = old, 1500);
      } catch { alert('复制失败：' + url); }
    });
  });
}

// ─── Reset ──────────────────────────────────────────────────────
$('resetBtn-imggen').addEventListener('click', () => {
  $('prompt-imggen').value = '';
  $('negative-imggen').value = '';
  $('optimizeHint-imggen').textContent = '';
  $('undoBtn-imggen').style.display = 'none';
  $('seed-imggen').value = '';
  $('watermark-imggen').checked = false;
  $('thinking-imggen').checked = true;
  $('sequential-imggen').checked = false;
  $('imageOut-imggen').style.display = 'none';
  $('log-imggen').innerHTML = '';
  setStatusHTML('status-imggen', '<span style="color: var(--muted)">填写 prompt 后点击「生成图片」</span>');
  updateImggenUI();
});

// ─── Initialize UI on load ─────────────────────────────────────
updateImggenUI();
bindOptimize(imggen);
