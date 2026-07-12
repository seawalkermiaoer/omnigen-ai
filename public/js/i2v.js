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
    $('firstFrameLabel-i2v').innerHTML = '首帧图像 <span style="color: var(--muted)">（JPG/PNG/BMP/WEBP，宽高 240~8000px，宽高比 1:8~8:1，≤50MB）</span>';
    $('promptHint-i2v').textContent = '（可选，描述视频内容；首尾帧任务可描述过渡过程）';
  } else {
    $('firstFrameLabel-i2v').innerHTML = '首帧图像 <span style="color: var(--muted)">（必填，1 张，JPG/PNG/WEBP，宽高 ≥300px，宽高比 1:2.5~2.5:1，≤50MB）</span>';
    $('promptHint-i2v').textContent = '（可选，描述"接下来发生什么"）';
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
    setStatusHTML('status-i2v', `<span class="pill FAILED">上传失败</span> ${escapeHTML(message)}`);
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
      img.onerror = () => reject(new Error('图像无法解码，请换一张图片'));
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
      rejectSelectedFile('文件类型不支持，请上传 JPG/PNG/WEBP 图片');
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
      rejectSelectedFile(`图像尺寸 ${w}×${h} 不满足（宽高需 ≥${minSize}px），请换一张更大的图片`);
      return;
    }
    const r = w / h;
    if (r < ratioMin || r > ratioMax) {
      rejectSelectedFile(`宽高比 ${r.toFixed(2)} 超出范围（要求 ${ratioMin.toFixed(2)}~${ratioMax.toFixed(2)}），请换一张比例更接近视频画面的图片`);
      return;
    }

    let uploadUrl;
    try {
      uploadUrl = await uploadImageToServer(file);
    } catch (e) {
      rejectSelectedFile(`${file.name} 上传失败：${e.message}`);
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
      alert('wan2.7-i2v 仅支持北京 / 新加坡地域，请到设置中切换。'); return;
    }
    const t = i2v.state.task;
    const media = [];
    if (t === 'first_frame' || t === 'first_last') {
      if (!i2v.state.image) { alert('该任务需要首帧图'); return; }
      media.push({ type: 'first_frame', url: i2v.state.image.base64Url });
    }
    if (t === 'first_last') {
      if (!i2v.state.lastFrame) { alert('首尾帧任务需要尾帧图'); return; }
      media.push({ type: 'last_frame', url: i2v.state.lastFrame.base64Url });
    }
    if (t === 'continue') {
      if (!i2v.state.firstClip) { alert('视频续写任务需要首段视频 URL'); return; }
      media.push({ type: 'first_clip', url: i2v.state.firstClip });
      if (i2v.state.lastFrame) media.push({ type: 'last_frame', url: i2v.state.lastFrame.base64Url });
    }
    if ((t === 'first_frame' || t === 'first_last') && i2v.state.drivingAudio) {
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
  if (!i2v.state.image) { alert('请先上传首帧图'); return; }
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
  setStatusHTML('status-i2v', '<span style="color: var(--muted)">已重置</span>');
  $('log-i2v').innerHTML = '';
});

bindOptimize(i2v);
