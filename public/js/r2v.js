/* ── OmniGen AI: Tab — 参考生视频 (R2V) ── */

const r2v = {
  kind: 'r2v',
  state: { images: [], videos: [] },
};

const modelR2vEl = $('model-r2v');
function isWanR2v() { return modelR2vEl.value === 'wan2.7-r2v'; }
function getR2vMode() { return isWanR2v() ? 'r2v_wan' : 'r2v'; }
function maxImagesForR2v() { return isWanR2v() ? Math.max(0, 5 - r2v.state.videos.length) : 9; }

modelR2vEl.addEventListener('change', () => {
  const wan = isWanR2v();
  document.querySelectorAll('[data-r2v-wan]').forEach(el => {
    el.style.display = wan ? '' : 'none';
  });
  $('imageLabel-r2v').innerHTML = wan
    ? t('r2v.imageLabelWanFull')
    : t('r2v.imageLabelFull');
  $('prompt-r2v').placeholder = wan
    ? t('r2v.promptPlaceholderWan')
    : t('r2v.promptPlaceholder');
  renderImages();
});

// ---------- Image upload ----------
const uploaderEl = $('uploader-r2v');
const fileInputEl = $('fileInput-r2v');
const imagesEl = $('images-r2v');

uploaderEl.addEventListener('click', () => fileInputEl.click());
uploaderEl.addEventListener('dragover', (e) => { e.preventDefault(); uploaderEl.classList.add('drag'); });
uploaderEl.addEventListener('dragleave', () => uploaderEl.classList.remove('drag'));
uploaderEl.addEventListener('drop', (e) => {
  e.preventDefault();
  uploaderEl.classList.remove('drag');
  handleFiles(e.dataTransfer.files);
});
fileInputEl.addEventListener('change', (e) => handleFiles(e.target.files));

async function handleFiles(fileList) {
  const log = makeLog('log-r2v');
  const wan = isWanR2v();
  const accept = wan ? /^image\/(jpeg|png|bmp|webp)$/ : /^image\/(jpeg|png|webp)$/;
  const files = Array.from(fileList).filter(f => accept.test(f.type));
  if (!files.length) return;
  const limit = maxImagesForR2v();
  for (const f of files) {
    if (r2v.state.images.length >= limit) {
      log(t('r2v.alertMaxImages', { limit, extra: wan ? '（图+视频 ≤5）' : '' }), 'err');
      break;
    }
    const thumb = await makeThumb(f, 64);
    log(t('common.uploading', { name: f.name }));
    try {
      const uploadUrl = await uploadImageToServer(f);
      r2v.state.images.push({ file: f, dataUrl: thumb || '', base64Url: uploadUrl, thumb, voiceUrl: '' });
      log(t('common.uploadDone', { name: f.name }), 'ok');
    } catch (e) {
      log(t('common.uploadFailed', { name: f.name, msg: e.message }), 'err');
    }
  }
  renderImages();
  fileInputEl.value = '';
}

function renderImages() {
  const wan = isWanR2v();
  imagesEl.innerHTML = '';
  r2v.state.images.forEach((img, i) => {
    const wrap = document.createElement('div');
    wrap.className = 'img-wrap';
    wrap.innerHTML = `
      <div class="img-card">
        <span class="tag">${wan ? t('r2v.imageTag', { n: i + 1 }) : t('r2v.imageTagBracket', { n: i + 1 })}</span>
        <button class="remove" title="${t('r2v.delete')}">×</button>
        <img src="${img.dataUrl}" alt="img${i+1}" />
        <div class="order">
          <button data-act="left" title="${t('r2v.moveLeft')}">←</button>
          <button data-act="right" title="${t('r2v.moveRight')}">→</button>
        </div>
      </div>
      ${wan ? `<input type="text" class="voice-input" placeholder="${t('r2v.voiceUrlPlaceholder')}" value="${img.voiceUrl || ''}" />` : ''}
    `;
    wrap.querySelector('.remove').addEventListener('click', () => {
      r2v.state.images.splice(i, 1); renderImages();
    });
    wrap.querySelector('[data-act="left"]').addEventListener('click', () => {
      if (i > 0) { [r2v.state.images[i-1], r2v.state.images[i]] = [r2v.state.images[i], r2v.state.images[i-1]]; renderImages(); }
    });
    wrap.querySelector('[data-act="right"]').addEventListener('click', () => {
      if (i < r2v.state.images.length - 1) { [r2v.state.images[i], r2v.state.images[i+1]] = [r2v.state.images[i+1], r2v.state.images[i]]; renderImages(); }
    });
    if (wan) {
      const vi = wrap.querySelector('.voice-input');
      vi.addEventListener('change', () => { r2v.state.images[i].voiceUrl = vi.value.trim(); });
    }
    imagesEl.appendChild(wrap);
  });
}

// ---------- Video references ----------
function renderVideos() {
  const list = $('videos-r2v');
  if (!list) return;
  list.innerHTML = '';
  r2v.state.videos.forEach((v, i) => {
    const row = document.createElement('div');
    row.className = 'video-row has-tag';
    row.innerHTML = `
      <span class="vid-num">${t('r2v.videoNum', { n: i + 1 })}</span>
      <input type="text" class="v-url" placeholder="${t('r2v.videoUrlPlaceholder')}" value="${escapeAttr(v.url)}" />
      <input type="text" class="v-voice" placeholder="${t('r2v.voiceUrlShort')}" value="${escapeAttr(v.voiceUrl || '')}" />
      <button class="remove-vid" title="${t('r2v.delete')}">×</button>
    `;
    row.querySelector('.v-url').addEventListener('change', (e) => {
      r2v.state.videos[i].url = e.target.value.trim();
    });
    row.querySelector('.v-voice').addEventListener('change', (e) => {
      r2v.state.videos[i].voiceUrl = e.target.value.trim();
    });
    row.querySelector('.remove-vid').addEventListener('click', () => {
      r2v.state.videos.splice(i, 1); renderVideos();
    });
    list.appendChild(row);
  });
}

$('addVideoBtn').addEventListener('click', () => {
  const totalMedia = r2v.state.images.length + r2v.state.videos.length;
  if (totalMedia >= 5) { alert(t('r2v.alertMaxMedia')); return; }
  r2v.state.videos.push({ url: '', voiceUrl: '' });
  renderVideos();
});

// ---------- Submit ----------
$('submitBtn-r2v').addEventListener('click', () => {
  const prompt = $('prompt-r2v').value.trim();
  if (!prompt) { alert(t('common.pleasePrompt')); return; }
  const wan = isWanR2v();
  const validVideos = r2v.state.videos.filter(v => v.url);

  if (wan) {
    const auth = getAuth();
    if (!['cn-beijing', 'ap-southeast-1'].includes(auth.region)) {
      alert(t('r2v.alertWanRegion')); return;
    }
    if (!r2v.state.images.length && !validVideos.length) {
      alert(t('r2v.alertNeedMedia')); return;
    }
    if (r2v.state.images.length + validVideos.length > 5) {
      alert(t('r2v.alertMaxMediaStrict')); return;
    }
  } else {
    if (!r2v.state.images.length) { alert(t('r2v.alertNeedImage')); return; }
  }

  const media = [];
  r2v.state.images.forEach(img => {
    const m = { type: 'reference_image', url: img.base64Url };
    if (wan && img.voiceUrl) m.reference_voice = img.voiceUrl;
    media.push(m);
  });
  if (wan) {
    validVideos.forEach(v => {
      const m = { type: 'reference_video', url: v.url };
      if (v.voiceUrl) m.reference_voice = v.voiceUrl;
      media.push(m);
    });
  }

  const params = collectParams('r2v');
  const input = { prompt, media };
  if (wan) {
    params.prompt_extend = $('prompt-extend-r2v').checked;
    const negative = $('negative-r2v').value.trim();
    if (negative) input.negative_prompt = negative;
  }

  const payload = {
    model: wan ? 'wan2.7-r2v' : 'happyhorse-1.1-r2v',
    input,
    parameters: params,
  };
  submitTask(r2v, payload);
});

// ---------- Reset ----------
$('resetBtn-r2v').addEventListener('click', () => {
  r2v.state.images = [];
  r2v.state.videos = [];
  renderImages();
  renderVideos();
  $('prompt-r2v').value = '';
  $('seed-r2v').value = '';
  if ($('negative-r2v')) $('negative-r2v').value = '';
  $('videoOut-r2v').style.display = 'none';
  setStatusHTML('status-r2v', '<span style="color: var(--muted)">' + t('common.resetDone') + '</span>');
  $('log-r2v').innerHTML = '';
});

bindOptimize(r2v);

// ---------- T2V (Text-to-Video) ----------
const t2v = { kind: 't2v', state: {} };

$('submitBtn-t2v').addEventListener('click', () => {
  const prompt = $('prompt-t2v').value.trim();
  if (!prompt) { alert(t('common.pleasePrompt')); return; }
  const payload = {
    model: 'happyhorse-1.1-t2v',
    input: { prompt },
    parameters: collectParams('t2v'),
  };
  submitTask(t2v, payload);
});
$('resetBtn-t2v').addEventListener('click', () => {
  $('prompt-t2v').value = '';
  $('seed-t2v').value = '';
  $('videoOut-t2v').style.display = 'none';
  setStatusHTML('status-t2v', '<span style="color: var(--muted)">' + t('common.resetDone') + '</span>');
  $('log-t2v').innerHTML = '';
});
bindOptimize(t2v);
