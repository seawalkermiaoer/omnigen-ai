/* ── OmniGen AI: Image editing (Image Edit) ── */

const imgedit = {
  kind: 'imgedit',
  state: {
    images: [],     // uploaded input images
  },
  currentHistoryId: null,
};

// ─── Model definitions (image edit only) ─────────────────────────
const IMGEDIT_MODELS = [
  { value: 'qwen-image-edit-plus', label: 'Qwen-Image-Edit-Plus（多图融合，DashScope 协议）' },
  { value: 'qwen-image-edit', label: 'Qwen-Image-Edit（DashScope 协议）' },
  { value: 'wan2.7-image-pro', label: 'Wan 2.7 Image Pro（DashScope 协议）' },
  { value: 'wan2.7-image', label: 'Wan 2.7 Image（DashScope 协议）' },
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

function isWanEditModel(model) {
  return model && model.startsWith('wan2.7');
}

// ─── UI update ──────────────────────────────────────────────────
function updateImgeditUI() {
  const model = $('model-imgedit').value;
  const isWan = isWanEditModel(model);

  // Update model dropdown
  const modelSelect = $('model-imgedit');
  const prevValue = modelSelect.value;
  modelSelect.innerHTML = IMGEDIT_MODELS.map(m => `<option value="${m.value}">${m.label}</option>`).join('');
  if (IMGEDIT_MODELS.some(m => m.value === prevValue)) {
    modelSelect.value = prevValue;
  }

  // Update size options
  updateImgeditSizeOptions();
  // Update count options
  updateImgeditCountOptions();

  // Show/hide wan2.7-specific controls
  const wanControls = document.querySelectorAll('[data-imgedit-wan]');
  wanControls.forEach(el => el.style.display = isWan ? '' : 'none');
}

function updateImgeditSizeOptions() {
  const model = $('model-imgedit').value;
  const isWan = isWanEditModel(model);
  const sizes = isWan ? WAN_SIZES : QWEN_SIZES;
  const sizeSelect = $('size-imgedit');
  sizeSelect.innerHTML = sizes.map(s => `<option value="${s.value}">${s.label}</option>`).join('');
  if (!isWan) {
    sizeSelect.value = '1328*1328';
  }
}

function updateImgeditCountOptions() {
  const model = $('model-imgedit').value;
  const countSelect = $('count-imgedit');
  let options = [];

  if (model === 'qwen-image-edit-plus') {
    options = [1, 2, 3, 4, 5, 6];
  } else if (model === 'qwen-image-edit') {
    options = [1];
  } else if (isWanEditModel(model)) {
    const isSequential = $('sequential-imgedit').checked;
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
$('model-imgedit').addEventListener('change', updateImgeditUI);

// Sequential checkbox changes count options
$('sequential-imgedit').addEventListener('change', updateImgeditCountOptions);

// ─── Image upload (multi-image) ────────────────────────────────
const uploaderImgedit = $('uploader-imgedit');
const fileInputImgedit = $('fileInput-imgedit');

uploaderImgedit.addEventListener('click', () => fileInputImgedit.click());
uploaderImgedit.addEventListener('dragover', e => { e.preventDefault(); uploaderImgedit.classList.add('drag'); });
uploaderImgedit.addEventListener('dragleave', () => uploaderImgedit.classList.remove('drag'));
uploaderImgedit.addEventListener('drop', e => {
  e.preventDefault();
  uploaderImgedit.classList.remove('drag');
  handleImgeditFiles(e.dataTransfer.files);
});
fileInputImgedit.addEventListener('change', () => {
  handleImgeditFiles(fileInputImgedit.files);
  fileInputImgedit.value = '';
});

async function handleImgeditFiles(fileList) {
  const model = $('model-imgedit').value;
  const maxImages = isWanEditModel(model) ? 9 : 3;
  const files = Array.from(fileList).filter(f => f.type.startsWith('image/'));

  for (const file of files) {
    if (imgedit.state.images.length >= maxImages) {
      alert(`当前模型最多支持 ${maxImages} 张输入图片`);
      break;
    }
    if (file.size > 20 * 1024 * 1024) {
      alert(`${file.name} 超过 20MB 限制`);
      continue;
    }
    const base64Url = await readAsDataURL(file);
    const thumb = await makeThumb(file, 96, 0.7);
    imgedit.state.images.push({ file, base64Url, thumb, name: file.name });
  }
  renderImgeditImages();
}

function renderImgeditImages() {
  const container = $('images-imgedit');
  if (!imgedit.state.images.length) {
    container.innerHTML = '';
    return;
  }
  container.innerHTML = imgedit.state.images.map((img, i) => `
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
      imgedit.state.images.splice(idx, 1);
      renderImgeditImages();
    });
  });
}

// ─── Submit (synchronous, no polling) ──────────────────────────
$('submitBtn-imgedit').addEventListener('click', submitImgedit);

async function submitImgedit() {
  const auth = checkAuth();
  if (!auth) return;

  const model = $('model-imgedit').value;
  const prompt = $('prompt-imgedit').value.trim();

  // Validate
  if (imgedit.state.images.length === 0) {
    return alert('请上传至少一张输入图片');
  }
  // wan2.7 region check
  if (isWanEditModel(model) && !['cn-beijing', 'ap-southeast-1'].includes(auth.region)) {
    return alert('Wan 2.7 图片模型仅支持北京和新加坡地域，当前地域为：' + auth.region);
  }

  // Build params
  const params = {
    size: $('size-imgedit').value,
    n: parseInt($('count-imgedit').value, 10) || 1,
    watermark: $('watermark-imgedit').checked,
  };
  const negPrompt = $('negative-imgedit').value.trim();
  if (negPrompt) params.negative_prompt = negPrompt;
  const seedVal = $('seed-imgedit').value.trim();
  if (seedVal) params.seed = parseInt(seedVal, 10);
  if (isWanEditModel(model)) {
    params.thinking_mode = $('thinking-imgedit').checked;
    params.enable_sequential = $('sequential-imgedit').checked;
  }

  // Build images array
  const images = imgedit.state.images.map(img => img.base64Url);

  const log = makeLog('log-imgedit');
  $('log-imgedit').innerHTML = '';
  $('imageOut-imgedit').style.display = 'none';
  $('submitBtn-imgedit').disabled = true;
  setStatusHTML('status-imgedit', '<span class="pill PENDING">生成中</span> 正在调用模型，请稍候…');
  log(`提交 ${model} 编辑任务到 ${auth.region}`);

  // Create history record
  const historyId = genHistoryId();
  imgedit.currentHistoryId = historyId;
  const thumbnails = imgedit.state.images.map(img => img.thumb).filter(Boolean);

  addHistoryRecord({
    id: historyId,
    mode: 'imgedit',
    subMode: 'edit',
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

    log(`✓ 编辑完成，共 ${imageUrls.length} 张图片（耗时 ${elapsed}s）`, 'ok');
    setStatusHTML('status-imgedit', `<span class="pill SUCCEEDED">完成</span> ${imageUrls.length} 张图片 · 耗时 ${elapsed}s`);

    renderImgeditImageResults(imageUrls, data.usage);

    patchHistoryRecord(historyId, {
      status: 'SUCCEEDED',
      imageUrls,
      endTime: Date.now(),
      usage: data.usage || null,
    });
  } catch (e) {
    log(e.message, 'err');
    setStatusHTML('status-imgedit', `<span class="pill FAILED">失败</span> ${escapeHTML(e.message)}`);
    patchHistoryRecord(historyId, { status: 'FAILED', errorMsg: e.message, endTime: Date.now() });
  } finally {
    $('submitBtn-imgedit').disabled = false;
  }
}

// ─── Render image results ───────────────────────────────────────
function renderImgeditImageResults(imageUrls, usage) {
  const out = $('imageOut-imgedit');
  const grid = $('imageGrid-imgedit');
  const meta = $('imageMeta-imgedit');

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
      const fname = `omnigen-imgedit-${Date.now()}-${btn.dataset.download}.png`;
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
$('resetBtn-imgedit').addEventListener('click', () => {
  imgedit.state.images = [];
  $('images-imgedit').innerHTML = '';
  $('prompt-imgedit').value = '';
  $('negative-imgedit').value = '';
  $('optimizeHint-imgedit').textContent = '';
  $('undoBtn-imgedit').style.display = 'none';
  $('seed-imgedit').value = '';
  $('watermark-imgedit').checked = false;
  $('thinking-imgedit').checked = true;
  $('sequential-imgedit').checked = false;
  $('imageOut-imgedit').style.display = 'none';
  $('log-imgedit').innerHTML = '';
  setStatusHTML('status-imgedit', '<span style="color: var(--muted)">上传图片并填写 prompt 后点击「编辑图片」</span>');
  updateImgeditUI();
});

// ─── Initialize UI on load ─────────────────────────────────────
updateImgeditUI();
bindOptimize(imgedit);
