/* ── OmniGen AI: History management ── */

const HISTORY_KEY = 'hh_history_v1';
const HISTORY_MAX = 100;

function loadHistory() {
  try { return JSON.parse(localStorage.getItem(HISTORY_KEY) || '[]'); }
  catch { return []; }
}
function saveHistory(arr) {
  try { localStorage.setItem(HISTORY_KEY, JSON.stringify(arr.slice(0, HISTORY_MAX))); }
  catch (e) { console.warn(t('history.writeFail'), e); }
}
function addHistoryRecord(rec) {
  const arr = loadHistory();
  arr.unshift(rec);
  saveHistory(arr);
  refreshHistoryUI();
  return rec.id;
}
function patchHistoryRecord(id, patch) {
  const arr = loadHistory();
  const idx = arr.findIndex(r => r.id === id);
  if (idx >= 0) {
    Object.assign(arr[idx], patch);
    saveHistory(arr);
    refreshHistoryUI();
  }
}
function deleteHistoryRecord(id) {
  saveHistory(loadHistory().filter(r => r.id !== id));
  refreshHistoryUI();
}
function genHistoryId() {
  return 'h_' + Date.now() + '_' + Math.random().toString(36).slice(2, 7);
}

// ---------- Time formatting ----------
function fmtRelTime(ts) {
  const d = Date.now() - ts;
  if (d < 60_000) return t('history.justNow');
  if (d < 3600_000) return t('history.minutesAgo', { n: Math.floor(d / 60_000) });
  if (d < 86400_000) return t('history.hoursAgo', { n: Math.floor(d / 3600_000) });
  if (d < 7 * 86400_000) return t('history.daysAgo', { n: Math.floor(d / 86400_000) });
  return new Date(ts).toLocaleString();
}
function fmtAbsTime(ts) { return new Date(ts).toLocaleString(); }
function getModeLabel(mode) {
  const key = 'history.modeLabels.' + mode;
  return t(key) || mode;
}

// ---------- UI rendering ----------
function refreshHistoryUI() {
  const arr = loadHistory();
  const badge = $('historyBadge');
  if (arr.length) { badge.style.display = ''; badge.textContent = arr.length; }
  else badge.style.display = 'none';
  const active = document.querySelector('.tab-content[data-tab="history"]')?.classList.contains('active');
  if (active) renderHistoryList();
}
function renderHistoryList() {
  const arr = loadHistory();
  const list = $('historyList');
  const meta = $('historyMeta');
  if (!arr.length) {
    list.innerHTML = '<div class="history-empty">' + t('history.empty') + '</div>';
    meta.textContent = t('history.noRecord');
    return;
  }
  meta.textContent = t('history.metaInfo', { count: arr.length, max: HISTORY_MAX });
  list.innerHTML = arr.map(renderHistoryCard).join('');
  list.querySelectorAll('[data-action]').forEach(btn => {
    btn.addEventListener('click', () => onHistoryAction(btn.dataset.action, btn.dataset.id));
  });
}
function renderHistoryCard(r) {
  const isImgMode = r.mode === 'imggen';
  const thumbsHTML = (r.thumbnails && r.thumbnails.length)
    ? `<div class="h-thumbs ${r.thumbnails.length > 1 ? 'multi' : ''}">
         ${r.thumbnails.slice(0, 4).map(t => `<div class="one"><img src="${t}" /></div>`).join('')}
       </div>`
    : isImgMode && r.imageUrls && r.imageUrls.length
      ? `<div class="h-thumbs"><div class="one"><img src="${r.imageUrls[0]}" /></div></div>`
      : `<div class="h-thumbs zero">${r.mode === 't2v' ? t('history.text') : t('history.noThumb')}</div>`;
  const promptShort = (r.prompt || t('history.noPrompt')).slice(0, 240);
  const status = r.status || 'PENDING';
  const elapsed = r.endTime ? Math.round((r.endTime - r.submitTime) / 1000) + 's' : '';
  const params = r.params || {};

  let paramText;
  if (isImgMode) {
    paramText = [
      params.size || '',
      params.n ? t('history.imageCount', { n: params.n }) : '',
      r.imageUrls ? t('history.resultCount', { n: r.imageUrls.length }) : '',
      elapsed,
    ].filter(Boolean).join(' · ');
  } else {
    paramText = [
      `${params.resolution || ''}`,
      params.ratio ? `${params.ratio}` : '',
      params.duration ? `${params.duration}s` : '',
      r.taskId ? `task:${r.taskId.slice(0, 8)}…` : '',
      elapsed,
    ].filter(Boolean).join(' · ');
  }

  const isLive = ['PENDING', 'RUNNING'].includes(status);
  const canDownload = status === 'SUCCEEDED' && (r.videoUrl || (r.imageUrls && r.imageUrls.length));

  return `
    <div class="h-card">
      ${thumbsHTML}
      <div class="h-body">
        <div class="h-meta">
          <span class="h-mode-tag">${getModeLabel(r.mode)}${isImgMode && r.subMode ? ' · ' + (r.subMode === 'edit' ? t('history.subModeEdit') : t('history.subModeT2i')) : ''}</span>
          <span class="pill ${status}">${status}</span>
          <span class="h-time" title="${fmtAbsTime(r.submitTime)}">${fmtRelTime(r.submitTime)}</span>
        </div>
        <div class="h-prompt">${escapeHTML(promptShort)}</div>
        <div class="h-params">${paramText}</div>
        ${r.errorMsg ? `<div class="h-error">${escapeHTML(r.errorMsg)}</div>` : ''}
      </div>
      <div class="h-actions">
        <button data-action="detail" data-id="${r.id}">${t('history.actionDetail')}</button>
        ${canDownload ? `<button data-action="download" data-id="${r.id}">${t('history.actionDownload')}</button>` : ''}
        ${canDownload ? `<button data-action="copy" data-id="${r.id}">${t('history.actionCopy')}</button>` : ''}
        ${(isLive && r.taskId) ? `<button data-action="resume" data-id="${r.id}">${t('history.actionResume')}</button>` : ''}
        <button data-action="reuse" data-id="${r.id}">${t('history.actionReuse')}</button>
        <button data-action="delete" data-id="${r.id}" class="danger">${t('history.actionDelete')}</button>
      </div>
    </div>
  `;
}

// ---------- History actions ----------
async function onHistoryAction(action, id) {
  const r = loadHistory().find(x => x.id === id);
  if (!r) return;
  if (action === 'delete') {
    if (confirm(t('history.confirmDelete'))) deleteHistoryRecord(id);
  } else if (action === 'download') {
    if (r.imageUrls && r.imageUrls.length) {
      window.location.href = `/api/download?url=${encodeURIComponent(r.imageUrls[0])}&filename=omnigen-imggen-${r.id}.png`;
    } else if (r.videoUrl) {
      window.location.href = `/api/download?url=${encodeURIComponent(r.videoUrl)}&filename=omnigen-${r.mode}-${r.id}.mp4`;
    }
  } else if (action === 'copy') {
    const url = (r.imageUrls && r.imageUrls.length) ? r.imageUrls[0] : r.videoUrl;
    if (!url) return;
    try { await navigator.clipboard.writeText(url); alert(r.imageUrls ? t('history.copiedImage') : t('history.copiedVideo')); }
    catch { alert(t('common.copyFailed', { url })); }
  } else if (action === 'detail') {
    showDetail(r);
  } else if (action === 'reuse') {
    reuseHistory(r);
  } else if (action === 'resume') {
    resumeHistory(r);
  }
}

function showDetail(r) {
  const params = r.params || {};
  const isImgMode = r.mode === 'imggen';
  const lines = [
    `<table style="width:100%;font-size:13px;border-collapse:collapse">
      <tr><td style="padding:4px 8px;color:var(--muted);width:120px">${t('history.detailMode')}</td><td>${getModeLabel(r.mode)}${isImgMode && r.subMode ? ' · ' + (r.subMode === 'edit' ? t('history.subModeEdit') : t('history.subModeT2i')) : ''} (${r.model || ''})</td></tr>
      <tr><td style="padding:4px 8px;color:var(--muted)">${t('history.detailStatus')}</td><td><span class="pill ${r.status || 'PENDING'}">${r.status || 'PENDING'}</span></td></tr>
      ${!isImgMode ? `<tr><td style="padding:4px 8px;color:var(--muted)">task_id</td><td><code>${r.taskId || t('history.detailNotCreated')}</code></td></tr>` : ''}
      <tr><td style="padding:4px 8px;color:var(--muted)">${t('history.detailSubmitTime')}</td><td>${fmtAbsTime(r.submitTime)}</td></tr>
      ${r.endTime ? `<tr><td style="padding:4px 8px;color:var(--muted)">${t('history.detailEndTime')}</td><td>${fmtAbsTime(r.endTime)}（${t('history.detailElapsed')} ${Math.round((r.endTime-r.submitTime)/1000)}s）</td></tr>` : ''}
      <tr><td style="padding:4px 8px;color:var(--muted)">${t('history.detailRegion')}</td><td>${r.region}${r.workspaceId ? ' · ' + r.workspaceId : ''}</td></tr>
      <tr><td style="padding:4px 8px;color:var(--muted);vertical-align:top">${t('history.detailParams')}</td><td><code>${escapeHTML(JSON.stringify(params))}</code></td></tr>
      ${r.imageCount ? `<tr><td style="padding:4px 8px;color:var(--muted)">${isImgMode ? t('history.detailInputImage') : t('history.detailRefImage')}</td><td>${t('history.imageCount', { n: r.imageCount })}</td></tr>` : ''}
      ${r.imageUrls ? `<tr><td style="padding:4px 8px;color:var(--muted)">${t('history.detailResult')}</td><td>${r.imageUrls.length} ${t('history.detailImages')}</td></tr>` : ''}
    </table>`,
    `<div style="margin-top:12px"><b style="font-size:13px">Prompt</b><div style="background:var(--panel-2);padding:10px;border-radius:6px;font-size:13px;white-space:pre-wrap;margin-top:6px">${escapeHTML(r.prompt || t('history.detailNoPrompt'))}</div></div>`,
    r.videoUrl ? `<div style="margin-top:12px"><b style="font-size:13px">${t('history.detailVideo')}</b><video src="${r.videoUrl}" controls style="width:100%;margin-top:6px;border-radius:6px;background:#000"></video></div>` : '',
  ];

  // Render image results for imggen mode
  if (r.imageUrls && r.imageUrls.length) {
    const imgsHTML = r.imageUrls.map((url, i) =>
      `<div style="margin-top:8px"><b style="font-size:13px">${t('history.detailImageN', { n: i + 1 })}</b><img src="${url}" style="width:100%;margin-top:6px;border-radius:6px;background:#000" /></div>`
    ).join('');
    lines.push(`<div style="margin-top:12px">${imgsHTML}</div>`);
  }

  if (r.errorMsg) {
    lines.push(`<div style="margin-top:12px"><b style="font-size:13px;color:var(--danger)">${t('history.detailError')}</b><div style="background:var(--panel-2);padding:10px;border-radius:6px;font-size:13px;color:var(--danger);margin-top:6px">${escapeHTML(r.errorMsg)}</div></div>`);
  }

  $('detailBody').innerHTML = lines.filter(Boolean).join('');
  $('detailModal').classList.add('show');
}

function reuseHistory(r) {
  const p = r.params || {};
  if (r.mode === 'imggen') {
    switchToTab('imggen');
    if (r.subMode && imggen) {
      imggen.state.mode = r.subMode;
      document.querySelectorAll('#imggenModeTabs .seg-tab').forEach(b =>
        b.classList.toggle('active', b.dataset.mode === r.subMode)
      );
    }
    if ($('prompt-imggen')) $('prompt-imggen').value = r.prompt || '';
    if (r.model && $('model-imggen')) {
      if (typeof updateImggenUI === 'function') updateImggenUI();
      $('model-imggen').value = r.model;
      if (typeof updateImggenUI === 'function') updateImggenUI();
    }
    if (p.size && $('size-imggen')) $('size-imggen').value = p.size;
    if (p.n != null && $('count-imggen')) $('count-imggen').value = p.n;
    if (typeof p.watermark === 'boolean' && $('watermark-imggen')) $('watermark-imggen').checked = p.watermark;
    if (typeof p.prompt_extend === 'boolean' && $('prompt-extend-imggen')) $('prompt-extend-imggen').checked = p.prompt_extend;
    if (p.seed != null && $('seed-imggen')) $('seed-imggen').value = p.seed;
    if ($('negative-imggen')) $('negative-imggen').value = p.negative_prompt || '';
    alert(t('history.reuseImggen', { note: r.imageCount ? t('history.reuseImggenNote') : '' }));
    return;
  }

  switchToTab(r.mode);
  if ($('prompt-' + r.mode)) $('prompt-' + r.mode).value = r.prompt || '';
  if (p.resolution && $('resolution-' + r.mode)) $('resolution-' + r.mode).value = p.resolution;
  if (p.ratio && $('ratio-' + r.mode)) $('ratio-' + r.mode).value = p.ratio;
  if (p.duration && $('duration-' + r.mode)) $('duration-' + r.mode).value = p.duration;
  if (typeof p.watermark === 'boolean' && $('watermark-' + r.mode)) $('watermark-' + r.mode).checked = p.watermark;
  if (p.seed != null && $('seed-' + r.mode)) $('seed-' + r.mode).value = p.seed;
  alert(t('history.reuseOther', { mode: getModeLabel(r.mode), note: r.imageCount ? t('history.reuseOtherNote') : '' }));
}

function resumeHistory(r) {
  if (!r.taskId) return alert(t('history.noTaskIdToQuery'));
  const auth = checkAuth();
  if (!auth) return;
  switchToTab(r.mode);
  const ctx = ({ r2v, t2v, i2v })[r.mode];
  if (!ctx) return;
  ctx.currentHistoryId = r.id;
  const log = makeLog('log-' + r.mode);
  $('log-' + r.mode).innerHTML = '';
  log(t('history.resumingPoll', { taskId: r.taskId }));
  setStatusHTML('status-' + r.mode, `<span class="pill PENDING">${t('history.querying')}</span> task_id: <code>${r.taskId}</code>`);
  pollTask(ctx, r.taskId, log);
}

// ---------- Toolbar ----------
$('clearHistoryBtn').addEventListener('click', () => {
  if (!loadHistory().length) return;
  if (confirm(t('history.confirmClear'))) {
    saveHistory([]);
    refreshHistoryUI();
  }
});
$('exportHistoryBtn').addEventListener('click', () => {
  const arr = loadHistory();
  if (!arr.length) return alert(t('history.noRecord'));
  const blob = new Blob([JSON.stringify(arr, null, 2)], { type: 'application/json' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `omnigen-history-${Date.now()}.json`;
  a.click();
  URL.revokeObjectURL(a.href);
});

// Init
refreshHistoryUI();
