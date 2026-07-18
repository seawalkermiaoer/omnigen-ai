/* ── OmniGen AI: Text-to-Image generation ── */

const imggen = {
  kind: 'imggen',
  state: {},
  currentHistoryId: null,
};

// ─── Model definitions (text-to-image only) ─────────────────────
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

// Qwen models use these sizes; wan2.7 uses 1K/2K/4K
function getQwenSizes() {
  return [
    { value: '1664*928', label: '1664×928 (16:9)' },
    { value: '1472*1140', label: '1472×1140 (4:3)' },
    { value: '1328*1328', label: '1328×1328 (1:1)' },
    { value: '1140*1472', label: '1140×1472 (3:4)' },
    { value: '928*1664', label: '928×1664 (9:16)' },
  ];
}
function getWanSizes() {
  return [
    { value: '1K', label: t('imggen.size1k') },
    { value: '2K', label: '2K' },
    { value: '4K', label: '4K' },
  ];
}

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
  const models = getImggenModels();
  modelSelect.innerHTML = models.map(m => `<option value="${m.value}">${m.label}</option>`).join('');
  if (models.some(m => m.value === prevValue)) {
    modelSelect.value = prevValue;
  }

  // Update size options
  updateSizeOptions();
  // Update count options
  updateCountOptions();

  // Show/hide wan2.7-specific controls
  const wanControls = document.querySelectorAll('[data-imggen-wan]');
  wanControls.forEach(el => el.style.display = isWan ? '' : 'none');

  // gpt-image-2 is a chat API: it has no size/count/seed/watermark parameters.
  // Read the rebuilt select, not `model` — right after a provider switch that
  // variable still holds the previous provider's model name.
  const isT8 = isT8Model(modelSelect.value);
  document.querySelectorAll('[data-imggen-ds]').forEach(el => el.style.display = isT8 ? 'none' : '');
  if (isT8) {
    document.querySelectorAll('[data-imggen-wan]').forEach(el => el.style.display = 'none');
  }
}

function updateSizeOptions() {
  const model = $('model-imggen').value;
  const isWan = isWanModel(model);
  const sizes = isWan ? getWanSizes() : getQwenSizes();
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
window.addEventListener('localechange', () => { updateImggenUI(); });
window.addEventListener('providerchange', () => { updateImggenUI(); });

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
    return alert(t('common.pleaseInputPrompt'));
  }
  // wan2.7 region check
  if (isWanModel(model) && !['cn-beijing', 'ap-southeast-1'].includes(auth.region)) {
    return alert(t('imggen.alertWanRegion', { region: auth.region }));
  }

  // Build params — gpt-image-2 is a chat API and takes no generation parameters.
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

  const log = makeLog('log-imggen');
  $('log-imggen').innerHTML = '';
  $('imageOut-imggen').style.display = 'none';
  $('submitBtn-imggen').disabled = true;
  setStatusHTML('status-imggen', '<span class="pill PENDING">' + t('common.pillPending') + '</span> ' + t('imggen.generating'));
  log(t('task.submitLog', { model, region: auth.region }));

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
    if (!imageUrls.length) throw new Error(t('imggen.noResult'));

    log(t('imggen.genDone', { count: imageUrls.length, elapsed }), 'ok');
    setStatusHTML('status-imggen', '<span class="pill SUCCEEDED">' + t('common.pillSuccess') + '</span> ' + imageUrls.length + ' · ' + elapsed + 's');

    renderImggenImageResults(imageUrls, data.usage, data.note);

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
    setStatusHTML('status-imggen', '<span class="pill FAILED">' + t('common.pillFailed') + '</span> ' + escapeHTML(e.message));
    patchHistoryRecord(historyId, { status: 'FAILED', errorMsg: e.message, endTime: Date.now() });
  } finally {
    $('submitBtn-imggen').disabled = false;
  }
}

// ─── Render image results ───────────────────────────────────────
function renderImggenImageResults(imageUrls, usage, note) {
  const out = $('imageOut-imggen');
  const grid = $('imageGrid-imggen');
  const meta = $('imageMeta-imggen');
  const noteEl = $('imageNote-imggen');

  out.style.display = '';
  // textContent, not innerHTML: this is untrusted model output.
  if (note) {
    noteEl.textContent = t('imggen.noteTitle') + '：' + note;
    noteEl.style.display = '';
  } else {
    noteEl.textContent = '';
    noteEl.style.display = 'none';
  }
  const count = imageUrls.length;
  const sizeInfo = usage?.size || '';
  meta.textContent = t('imggen.genMeta', { count, sizeInfo: sizeInfo ? ' · ' + sizeInfo : '' });

  grid.innerHTML = imageUrls.map((url, i) => `
    <div class="image-card">
      <img src="${escapeAttr(url)}" alt="${t('imggen.genImageAlt', { n: i + 1 })}" />
      <div class="actions">
        <button class="primary" data-download="${i}">${t('common.download')}</button>
        <button class="ghost" data-copy="${i}">${t('common.copyLink')}</button>
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
        btn.textContent = t('common.copySuccess');
        setTimeout(() => btn.textContent = old, 1500);
      } catch { alert(t('common.copyFailed', { url })); }
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
  setStatusHTML('status-imggen', '<span style="color: var(--muted)">' + t('imggen.statusDefault') + '</span>');
  updateImggenUI();
});

// ─── Initialize UI on load ─────────────────────────────────────
updateImggenUI();
bindOptimize(imggen);
