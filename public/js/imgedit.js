/* ── OmniGen AI: Image editing (Image Edit) ── */

const imgedit = {
  kind: 'imgedit',
  state: {
    images: [],     // uploaded input images
  },
  currentHistoryId: null,
};

// ─── Model definitions (image edit only) ─────────────────────────
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

// Qwen models use these sizes; wan2.7 uses 1K/2K/4K
function getQwenEditSizes() {
  return [
    { value: '1664*928', label: '1664×928 (16:9)' },
    { value: '1472*1140', label: '1472×1140 (4:3)' },
    { value: '1328*1328', label: '1328×1328 (1:1)' },
    { value: '1140*1472', label: '1140×1472 (3:4)' },
    { value: '928*1664', label: '928×1664 (9:16)' },
  ];
}
function getWanEditSizes() {
  return [
    { value: '1K', label: t('imggen.size1k') },
    { value: '2K', label: '2K' },
    { value: '4K', label: '4K' },
  ];
}

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
  const imgeditModels = getImgeditModels();
  modelSelect.innerHTML = imgeditModels.map(m => `<option value="${m.value}">${m.label}</option>`).join('');
  if (imgeditModels.some(m => m.value === prevValue)) {
    modelSelect.value = prevValue;
  }

  // Update size options
  updateImgeditSizeOptions();
  // Update count options
  updateImgeditCountOptions();

  // Show/hide wan2.7-specific controls
  const wanControls = document.querySelectorAll('[data-imgedit-wan]');
  wanControls.forEach(el => el.style.display = isWan ? '' : 'none');

  // gpt-image-2 is a chat API: it has no size/count/seed/watermark parameters.
  // Read the rebuilt select, not `model` — right after a provider switch that
  // variable still holds the previous provider's model name.
  const isT8 = isT8Model(modelSelect.value);
  document.querySelectorAll('[data-imgedit-ds]').forEach(el => el.style.display = isT8 ? 'none' : '');
  if (isT8) {
    document.querySelectorAll('[data-imgedit-wan]').forEach(el => el.style.display = 'none');
  }
}

function updateImgeditSizeOptions() {
  const model = $('model-imgedit').value;
  const isWan = isWanEditModel(model);
  const sizes = isWan ? getWanEditSizes() : getQwenEditSizes();
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
window.addEventListener('localechange', () => { updateImgeditUI(); });
window.addEventListener('providerchange', () => { updateImgeditUI(); });

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
      alert(t('imgedit.alertMaxImages', { max: maxImages }));
      break;
    }
    const thumb = await makeThumb(file, 96, 0.7);
    try {
      const uploadUrl = await uploadImageToServer(file);
      imgedit.state.images.push({ file, base64Url: uploadUrl, thumb, name: file.name });
    } catch (e) {
      alert(t('imgedit.alertUploadFail', { name: file.name, msg: e.message }));
    }
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
      <img src="${img.thumb || img.base64Url}" alt="${t('imgedit.imageTag', { n: i + 1 })}" />
      <div class="tag">${t('imgedit.imageTag', { n: i + 1 })}</div>
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
    return alert(t('imgedit.alertNeedImage'));
  }
  // wan2.7 region check
  if (isWanEditModel(model) && !['cn-beijing', 'ap-southeast-1'].includes(auth.region)) {
    return alert(t('imgedit.alertWanRegion', { region: auth.region }));
  }

  // Build params
  // gpt-image-2 is a chat API and takes no generation parameters.
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

  // Build images array
  const images = imgedit.state.images.map(img => img.base64Url);

  const log = makeLog('log-imgedit');
  $('log-imgedit').innerHTML = '';
  $('imageOut-imgedit').style.display = 'none';
  $('submitBtn-imgedit').disabled = true;
  setStatusHTML('status-imgedit', '<span class="pill PENDING">' + t('common.pillPending') + '</span> ' + t('imgedit.generating'));
  log(t('task.submitEditLog', { model, region: auth.region }));

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
    if (!imageUrls.length) throw new Error(t('imgedit.noResult'));

    log(t('imgedit.editDone', { count: imageUrls.length, elapsed }), 'ok');
    setStatusHTML('status-imgedit', '<span class="pill SUCCEEDED">' + t('common.pillSuccess') + '</span> ' + imageUrls.length + ' · ' + elapsed + 's');

    renderImgeditImageResults(imageUrls, data.usage, data.note);

    patchHistoryRecord(historyId, {
      status: 'SUCCEEDED',
      imageUrls,
      endTime: Date.now(),
      usage: data.usage || null,
      // Upstream may echo a more concrete model than we asked for (gpt-image-2-pro).
      model: data.model || model,
    });
  } catch (e) {
    log(e.message, 'err');
    setStatusHTML('status-imgedit', '<span class="pill FAILED">' + t('common.pillFailed') + '</span> ' + escapeHTML(e.message));
    patchHistoryRecord(historyId, { status: 'FAILED', errorMsg: e.message, endTime: Date.now() });
  } finally {
    $('submitBtn-imgedit').disabled = false;
  }
}

// ─── Render image results ───────────────────────────────────────
function renderImgeditImageResults(imageUrls, usage, note) {
  const out = $('imageOut-imgedit');
  const grid = $('imageGrid-imgedit');
  const meta = $('imageMeta-imgedit');
  const noteEl = $('imageNote-imgedit');

  out.style.display = '';
  // textContent, not innerHTML: this is untrusted model output.
  if (note) {
    noteEl.textContent = t('imgedit.noteTitle') + '：' + note;
    noteEl.style.display = '';
  } else {
    noteEl.textContent = '';
    noteEl.style.display = 'none';
  }
  const count = imageUrls.length;
  const sizeInfo = usage?.size || '';
  meta.textContent = t('imgedit.editMeta', { count, sizeInfo: sizeInfo ? ' · ' + sizeInfo : '' });

  grid.innerHTML = imageUrls.map((url, i) => `
    <div class="image-card">
      <img src="${escapeAttr(url)}" alt="${t('imgedit.editImageAlt', { n: i + 1 })}" />
      <div class="actions">
        <button class="primary" data-download="${i}">${t('common.download')}</button>
        <button class="ghost" data-copy="${i}">${t('common.copyLink')}</button>
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
        btn.textContent = t('common.copySuccess');
        setTimeout(() => btn.textContent = old, 1500);
      } catch { alert(t('common.copyFailed', { url })); }
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
  setStatusHTML('status-imgedit', '<span style="color: var(--muted)">' + t('imgedit.statusDefault') + '</span>');
  updateImgeditUI();
});

// ─── Initialize UI on load ─────────────────────────────────────
updateImgeditUI();
bindOptimize(imgedit);
