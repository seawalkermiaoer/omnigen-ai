/* ── OmniGen AI: Tab — 图生视频 (I2V) ── */

const i2v = {
  kind: 'i2v',
  state: {
    image: null,        // first_frame
    lastFrame: null,
    firstClip: '',      // URL
    drivingAudio: '',   // URL
    task: 'first_frame',
  },
};

const modelI2vEl = $('model-i2v');
function isWanI2v() { return modelI2vEl.value === 'wan2.7-i2v-2026-04-25'; }
function getI2vMode() { return isWanI2v() ? 'i2v_wan' : 'i2v'; }

modelI2vEl.addEventListener('change', () => {
  const wan = isWanI2v();
  document.querySelectorAll('[data-i2v-wan]').forEach(el => {
    el.style.display = wan ? '' : 'none';
  });
  if (wan) {
    $('firstFrameLabel-i2v').innerHTML = t('i2v.firstFrameLabelWanFull');
    $('promptHint-i2v').textContent = t('i2v.promptHintWan');
  } else {
    $('firstFrameLabel-i2v').innerHTML = t('i2v.firstFrameLabelFull');
    $('promptHint-i2v').textContent = t('i2v.promptHint');
  }
  applyI2vSlots();
});

// ---------- Task type tabs ----------
document.querySelectorAll('#i2vTaskTabs .seg-tab').forEach(btn => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('#i2vTaskTabs .seg-tab').forEach(b => b.classList.toggle('active', b === btn));
    i2v.state.task = btn.dataset.task;
    applyI2vSlots();
  });
});

function applyI2vSlots() {
  const wan = isWanI2v();
  const task = wan ? i2v.state.task : 'first_frame';
  const showFirstFrame = !wan || ['first_frame', 'first_last'].includes(task);
  const showLastFrame = wan && ['first_last', 'continue'].includes(task);
  const showFirstClip = wan && task === 'continue';
  const showAudio = wan && ['first_frame', 'first_last'].includes(task);
  $('uploader-i2v').closest('[data-slot="first_frame"]').style.display = showFirstFrame ? '' : 'none';
  document.querySelector('[data-slot="last_frame"]').style.display = showLastFrame ? '' : 'none';
  document.querySelector('[data-slot="first_clip"]').style.display = showFirstClip ? '' : 'none';
  document.querySelector('[data-slot="driving_audio"]').style.display = showAudio ? '' : 'none';
}

// ---------- Single image uploader ----------
function bindSingleImageUploader(opts) {
  const { uploaderId, fileInputId, previewId, imgElId, infoId, removeId, errorId, accept, onLoad, onClear } = opts;
  const uploader = $(uploaderId);
  const fileInput = $(fileInputId);
  const preview = $(previewId);
  const imgEl = $(imgElId);
  const info = $(infoId);
  const errorEl = $(errorId);
  uploader.addEventListener('click', () => fileInput.click());
  uploader.addEventListener('dragover', (e) => { e.preventDefault(); uploader.classList.add('drag'); });
  uploader.addEventListener('dragleave', () => uploader.classList.remove('drag'));
  uploader.addEventListener('drop', (e) => {
    e.preventDefault();
    uploader.classList.remove('drag');
    handleFile(e.dataTransfer.files[0]);
  });
  fileInput.addEventListener('change', (e) => handleFile(e.target.files[0]));
  $(removeId).addEventListener('click', () => {
    onClear();
    preview.style.display = 'none';
    uploader.style.display = '';
    errorEl.style.display = 'none';
    errorEl.textContent = '';
  });

  function showI2vUploadError(message) {
    const log = makeLog('log-i2v');
    log(message, 'err');
    errorEl.textContent = message;
    errorEl.style.display = 'block';
    setStatusHTML('status-i2v', '<span class="pill FAILED">' + t('common.uploadFailedGeneric') + '</span> ' + escapeHTML(message));
    preview.style.display = 'none';
    uploader.style.display = '';
    onClear();
  }

  function clearFileInput() {
    fileInput.value = '';
  }

  function getValidationLimits() {
    return {
      minSize: Number(opts.minSize),
      ratioMin: Number(opts.ratioMin),
      ratioMax: Number(opts.ratioMax),
    };
  }

  function loadImageDimensions(src) {
    return new Promise((resolve, reject) => {
      const img = new Image();
      img.onload = () => resolve({ w: img.naturalWidth, h: img.naturalHeight });
      img.onerror = () => reject(new Error(t('i2v.imageDecodeError')));
      img.src = src;
    });
  }

  async function inspectLocalImage(file) {
    const localUrl = URL.createObjectURL(file);
    try {
      return await loadImageDimensions(localUrl);
    } finally {
      URL.revokeObjectURL(localUrl);
    }
  }

  function rejectSelectedFile(message) {
    showI2vUploadError(message);
    clearFileInput();
  }

  async function handleFile(file) {
    const log = makeLog('log-i2v');
    if (!file) return;
    if (!accept.test(file.type)) {
      rejectSelectedFile(t('i2v.fileTypeError'));
      return;
    }
    errorEl.style.display = 'none';
    errorEl.textContent = '';
    let dimensions;
    try {
      dimensions = await inspectLocalImage(file);
    } catch (e) {
      rejectSelectedFile(e.message);
      return;
    }
    const { minSize, ratioMin, ratioMax } = getValidationLimits();
    const { w, h } = dimensions;
    if (w < minSize || h < minSize) {
      rejectSelectedFile(t('i2v.sizeError', { w, h, minSize }));
      return;
    }
    const r = w / h;
    if (r < ratioMin || r > ratioMax) {
      rejectSelectedFile(t('i2v.ratioError', { r: r.toFixed(2), ratioMin: ratioMin.toFixed(2), ratioMax: ratioMax.toFixed(2) }));
      return;
    }

    let uploadUrl;
    try {
      uploadUrl = await uploadImageToServer(file);
    } catch (e) {
      rejectSelectedFile(t('i2v.uploadFail', { name: file.name, msg: e.message }));
      return;
    }
    const thumb = await makeThumb(file, 96);
    imgEl.src = uploadUrl;
    info.textContent = `${w}×${h} · ${(file.size/1024/1024).toFixed(2)} MB`;
    errorEl.style.display = 'none';
    errorEl.textContent = '';
    preview.style.display = '';
    uploader.style.display = 'none';
    onLoad({ file, dataUrl: uploadUrl, base64Url: uploadUrl, w, h, thumb });
    clearFileInput();
  }
}

// First frame
bindSingleImageUploader({
  uploaderId: 'uploader-i2v',
  fileInputId: 'fileInput-i2v',
  previewId: 'singleImg-i2v',
  imgElId: 'singleImgEl-i2v',
  infoId: 'singleImgInfo-i2v',
  removeId: 'singleImgRemove-i2v',
  errorId: 'uploadError-i2v',
  accept: /^image\/(jpeg|png|bmp|webp)$/,
  get minSize() { return isWanI2v() ? 240 : 300; },
  get ratioMin() { return isWanI2v() ? 1/8 : 1/2.5; },
  get ratioMax() { return isWanI2v() ? 8 : 2.5; },
  onLoad: (img) => { i2v.state.image = img; },
  onClear: () => { i2v.state.image = null; },
});

// Last frame (wan only)
bindSingleImageUploader({
  uploaderId: 'uploader-last-i2v',
  fileInputId: 'fileInput-last-i2v',
  previewId: 'singleImg-last-i2v',
  imgElId: 'singleImgEl-last-i2v',
  infoId: 'singleImgInfo-last-i2v',
  removeId: 'singleImgRemove-last-i2v',
  errorId: 'uploadError-last-i2v',
  accept: /^image\/(jpeg|png|bmp|webp)$/,
  minSize: 240,
  ratioMin: 1/8,
  ratioMax: 8,
  onLoad: (img) => { i2v.state.lastFrame = img; },
  onClear: () => { i2v.state.lastFrame = null; },
});

$('firstClip-i2v').addEventListener('change', (e) => { i2v.state.firstClip = e.target.value.trim(); });
$('drivingAudio-i2v').addEventListener('change', (e) => { i2v.state.drivingAudio = e.target.value.trim(); });

// ---------- Submit ----------
$('submitBtn-i2v').addEventListener('click', () => {
  const prompt = $('prompt-i2v').value.trim();
  const wan = isWanI2v();

  if (wan) {
    const auth = getAuth();
    if (!['cn-beijing', 'ap-southeast-1'].includes(auth.region)) {
      alert(t('i2v.alertWanRegion')); return;
    }
    const taskType = i2v.state.task;
    const media = [];
    if (taskType === 'first_frame' || taskType === 'first_last') {
      if (!i2v.state.image) { alert(t('i2v.alertNeedFirstFrame')); return; }
      media.push({ type: 'first_frame', url: i2v.state.image.base64Url });
    }
    if (taskType === 'first_last') {
      if (!i2v.state.lastFrame) { alert(t('i2v.alertNeedLastFrame')); return; }
      media.push({ type: 'last_frame', url: i2v.state.lastFrame.base64Url });
    }
    if (taskType === 'continue') {
      if (!i2v.state.firstClip) { alert(t('i2v.alertNeedFirstClip')); return; }
      media.push({ type: 'first_clip', url: i2v.state.firstClip });
      if (i2v.state.lastFrame) media.push({ type: 'last_frame', url: i2v.state.lastFrame.base64Url });
    }
    if ((taskType === 'first_frame' || taskType === 'first_last') && i2v.state.drivingAudio) {
      media.push({ type: 'driving_audio', url: i2v.state.drivingAudio });
    }
    const params = collectParams('i2v');
    params.prompt_extend = $('prompt-extend-i2v').checked;
    const input = { media };
    if (prompt) input.prompt = prompt;
    const negative = $('negative-i2v').value.trim();
    if (negative) input.negative_prompt = negative;
    submitTask(i2v, {
      model: 'wan2.7-i2v-2026-04-25',
      input,
      parameters: params,
    });
    return;
  }

  // OmniGen AI i2v
  if (!i2v.state.image) { alert(t('i2v.alertUploadFirst')); return; }
  submitTask(i2v, {
    model: 'happyhorse-1.1-i2v',
    input: {
      media: [{ type: 'first_frame', url: i2v.state.image.base64Url }],
      ...(prompt ? { prompt } : {}),
    },
    parameters: collectParams('i2v'),
  });
});

// ---------- Reset ----------
$('resetBtn-i2v').addEventListener('click', () => {
  i2v.state.image = null;
  i2v.state.lastFrame = null;
  i2v.state.firstClip = '';
  i2v.state.drivingAudio = '';
  $('singleImg-i2v').style.display = 'none';
  $('uploader-i2v').style.display = '';
  $('uploadError-i2v').style.display = 'none';
  $('uploadError-i2v').textContent = '';
  $('singleImg-last-i2v').style.display = 'none';
  $('uploader-last-i2v').style.display = '';
  $('uploadError-last-i2v').style.display = 'none';
  $('uploadError-last-i2v').textContent = '';
  $('firstClip-i2v').value = '';
  $('drivingAudio-i2v').value = '';
  $('prompt-i2v').value = '';
  $('seed-i2v').value = '';
  if ($('negative-i2v')) $('negative-i2v').value = '';
  $('videoOut-i2v').style.display = 'none';
  setStatusHTML('status-i2v', '<span style="color: var(--muted)">' + t('common.resetDone') + '</span>');
  $('log-i2v').innerHTML = '';
});

bindOptimize(i2v);
