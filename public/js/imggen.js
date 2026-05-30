/* ── OmniGen AI: Image generation (Text-to-Image & Image Edit) ── */

const imggen = {
  kind: 'imggen',
  state: {
    mode: 't2i',   // 't2i' or 'edit'
    images: [],     // uploaded input images (edit mode only)
  },
  currentHistoryId: null,
};

// ─── Model definitions ─────────────────────────────────────────
const IMGGEN_MODELS = {
  t2i: [
    { value: 'qwen-image-plus', label: 'Qwen-Image-Plus（高质量，DashScope 协议）' },
    { value: 'qwen-image', label: 'Qwen-Image（快速，DashScope 协议）' },
    { value: 'wan2.7-image-pro', label: 'Wan 2.7 Image Pro（高质量，DashScope 协议）' },
    { value: 'wan2.7-image', label: 'Wan 2.7 Image（基础版，DashScope 协议）' },
  ],
  edit: [
    { value: 'qwen-image-edit-plus', label: 'Qwen-Image-Edit-Plus（多图融合，DashScope 协议）' },
    { value: 'qwen-image-edit', label: 'Qwen-Image-Edit（DashScope 协议）' },
    { value: 'wan2.7-image-pro', label: 'Wan 2.7 Image Pro（DashScope 协议）' },
    { value: 'wan2.7-image', label: 'Wan 2.7 Image（DashScope 协议）' },
  ],
};

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

// ─── Mode toggle ───────────────────────────────────────────────
const imggenModeTabs = $('imggenModeTabs');
imggenModeTabs.querySelectorAll('.seg-tab').forEach(btn => {
  btn.addEventListener('click', () => {
    imggenModeTabs.querySelectorAll('.seg-tab').forEach(b => b.classList.toggle('active', b === btn));
    imggen.state.mode = btn.dataset.mode;
    imggen.state.images = [];
    updateImggenUI();
  });
});

function updateImggenUI() {
  const mode = imggen.state.mode;
  const model = $('model-imggen').value;
  const isWan = isWanModel(model);
  const isEdit = mode === 'edit';

  // Update model dropdown
  const modelSelect = $('model-imggen');
  const prevValue = modelSelect.value;
  const models = IMGGEN_MODELS[mode];
  modelSelect.innerHTML = models.map(m => `<option value="${m.value}">${m.label}</option>`).join('');
  // Try to preserve previous selection if it exists in new list
  if (models.some(m => m.value === prevValue)) {
    modelSelect.value = prevValue;
  }

  // Show/hide image upload area
  const editSlot = document.querySelector('[data-slot="edit_images"]');
  editSlot.style.display = isEdit ? '' : 'none';

  // Clear uploaded images when switching modes
  if (!isEdit) {
    imggen.state.images = [];
    $('images-imggen').innerHTML = '';
  }

  // Update prompt placeholder
  const hint = $('promptHint-imggen');
  if (isEdit) {
    hint.textContent = '（用「图1」「图2」「图3」指代上方图片）';
  } else {
    hint.textContent = '（描述你想生成的图片）';
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
  // Select middle option by default for Qwen (1:1), first for wan
  if (!isWan) {
    sizeSelect.value = '1328*1328';
  }
}

function updateCountOptions() {
  const model = $('model-imggen').value;
  const mode = imggen.state.mode;
  const countSelect = $('count-imggen');
  let options = [];

  if (model === 'qwen-image-plus' || model === 'qwen-image') {
    // Qwen T2I: n fixed at 1
    options = [1];
  } else if (model === 'qwen-image-edit-plus') {
    // Qwen Edit: 1-6
    options = [1, 2, 3, 4, 5, 6];
  } else if (model === 'qwen-image-edit') {
    // Qwen Edit: n fixed at 1
    options = [1];
  } else if (isWanModel(model)) {
    // wan2.7: 1-4 for single mode, 1-12 for sequential
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

// ─── Image upload (multi-image) ────────────────────────────────
const uploaderImggen = $('uploader-imggen');
const fileInputImggen = $('fileInput-imggen');

uploaderImggen.addEventListener('click', () => fileInputImggen.click());
uploaderImggen.addEventListener('dragover', e => { e.preventDefault(); uploaderImggen.classList.add('drag'); });
uploaderImggen.addEventListener('dragleave', () => uploaderImggen.classList.remove('drag'));
uploaderImggen.addEventListener('drop', e => {
  e.preventDefault();
  uploaderImggen.classList.remove('drag');
  handleImggenFiles(e.dataTransfer.files);
});
fileInputImggen.addEventListener('change', () => {
  handleImggenFiles(fileInputImggen.files);
  fileInputImggen.value = '';
});

async function handleImggenFiles(fileList) {
  const model = $('model-imggen').value;
  const maxImages = isWanModel(model) ? 9 : 3;
  const files = Array.from(fileList).filter(f => f.type.startsWith('image/'));

  for (const file of files) {
    if (imggen.state.images.length >= maxImages) {
      alert(`当前模型最多支持 ${maxImages} 张输入图片`);
      break;
    }
    if (file.size > 20 * 1024 * 1024) {
      alert(`${file.name} 超过 20MB 限制`);
      continue;
    }
    const base64Url = await readAsDataURL(file);
    const thumb = await makeThumb(file, 96, 0.7);
    imggen.state.images.push({ file, base64Url, thumb, name: file.name });
  }
  renderImggenImages();
}

function renderImggenImages() {
  const container = $('images-imggen');
  if (!imggen.state.images.length) {
    container.innerHTML = '';
    return;
  }
  container.innerHTML = imggen.state.images.map((img, i) => `
    <div class="img-card" data-idx="${i}">
      <img src="${img.thumb || img.base64Url}" alt="图${i + 1}" />
      <div class="tag">图${i + 1}</div>
      <button class="remove" data-remove="${i}">×</button>
    </div>
  `).join('');
  container.querySelectorAll('[data-remove]').forEach(btn => {
    btn.addEventListener('click', e => {
      e.stopPropagation();
      const idx = parseInt(btn.dataset.remove, 10);
      imggen.state.images.splice(idx, 1);
      renderImggenImages();
    });
  });
}

// ─── Submit (synchronous, no polling) ──────────────────────────
$('submitBtn-imggen').addEventListener('click', submitImggen);

async function submitImggen() {
  const auth = checkAuth();
  if (!auth) return;

  const model = $('model-imggen').value;
  const prompt = $('prompt-imggen').value.trim();
  const mode = imggen.state.mode;

  // Validate
  if (!prompt && mode === 't2i') {
    return alert('请输入 Prompt');
  }
  if (mode === 'edit' && imggen.state.images.length === 0) {
    return alert('请上传至少一张输入图片');
  }
  // wan2.7 region check
  if (isWanModel(model) && !['cn-beijing', 'ap-southeast-1'].includes(auth.region)) {
    return alert('Wan 2.7 图片模型仅支持北京和新加坡地域，当前地域为：' + auth.region);
  }

  // Build params
  const params = {
    size: $('size-imggen').value,
    n: parseInt($('count-imggen').value, 10) || 1,
    watermark: $('watermark-imggen').checked,
    prompt_extend: $('prompt-extend-imggen').checked,
  };
  const negPrompt = $('negative-imggen').value.trim();
  if (negPrompt) params.negative_prompt = negPrompt;
  const seedVal = $('seed-imggen').value.trim();
  if (seedVal) params.seed = parseInt(seedVal, 10);
  if (isWanModel(model)) {
    params.thinking_mode = $('thinking-imggen').checked;
    params.enable_sequential = $('sequential-imggen').checked;
  }

  // Build images array
  const images = mode === 'edit'
    ? imggen.state.images.map(img => img.base64Url)
    : [];

  const log = makeLog('log-imggen');
  $('log-imggen').innerHTML = '';
  $('imageOut-imggen').style.display = 'none';
  $('submitBtn-imggen').disabled = true;
  setStatusHTML('status-imggen', '<span class="pill PENDING">生成中</span> 正在调用模型，请稍候…');
  log(`提交 ${model} 任务到 ${auth.region}`);

  // Create history record
  const historyId = genHistoryId();
  imggen.currentHistoryId = historyId;
  const thumbnails = mode === 'edit'
    ? imggen.state.images.map(img => img.thumb).filter(Boolean)
    : [];

  addHistoryRecord({
    id: historyId,
    mode: 'imggen',
    subMode: mode,
    model,
    status: 'PENDING',
    submitTime: Date.now(),
    region: auth.region,
    workspaceId: auth.workspaceId || '',
    prompt,
    params,
    imageCount: images.length,
    thumbnails,
  });

  try {
    const startTime = Date.now();
    const res = await fetch('/api/generate-image', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...auth, model, prompt, images, params }),
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

    renderImageResults(imageUrls, data.usage);

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
function renderImageResults(imageUrls, usage) {
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
  imggen.state.images = [];
  $('images-imggen').innerHTML = '';
  $('prompt-imggen').value = '';
  $('negative-imggen').value = '';
  $('seed-imggen').value = '';
  $('watermark-imggen').checked = false;
  $('prompt-extend-imggen').checked = true;
  $('thinking-imggen').checked = true;
  $('sequential-imggen').checked = false;
  $('imageOut-imggen').style.display = 'none';
  $('log-imggen').innerHTML = '';
  setStatusHTML('status-imggen', '<span style="color: var(--muted)">填写参数后点击「生成图片」</span>');
  updateImggenUI();
});

// ─── Initialize UI on load ─────────────────────────────────────
updateImggenUI();
