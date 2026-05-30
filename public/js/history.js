/* ── OmniGen AI: History management ── */

const HISTORY_KEY = 'hh_history_v1';
const HISTORY_MAX = 100;

function loadHistory() {
  try { return JSON.parse(localStorage.getItem(HISTORY_KEY) || '[]'); }
  catch { return []; }
}
function saveHistory(arr) {
  try { localStorage.setItem(HISTORY_KEY, JSON.stringify(arr.slice(0, HISTORY_MAX))); }
  catch (e) { console.warn('历史记录写入失败', e); }
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
  if (d < 60_000) return '刚刚';
  if (d < 3600_000) return Math.floor(d / 60_000) + ' 分钟前';
  if (d < 86400_000) return Math.floor(d / 3600_000) + ' 小时前';
  if (d < 7 * 86400_000) return Math.floor(d / 86400_000) + ' 天前';
  return new Date(ts).toLocaleString();
}
function fmtAbsTime(ts) { return new Date(ts).toLocaleString(); }
const MODE_LABEL = { r2v: '参考生视频', t2v: '文生视频', i2v: '图生视频', imggen: '图片生成' };

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
    list.innerHTML = '<div class="history-empty">尚无历史记录。提交任务后将在此显示。</div>';
    meta.textContent = '尚无记录';
    return;
  }
  meta.textContent = `共 ${arr.length} 条记录（最多保留 ${HISTORY_MAX} 条）`;
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
      : `<div class="h-thumbs zero">${r.mode === 't2v' ? '文本' : '无缩略图'}</div>`;
  const promptShort = (r.prompt || '(无 prompt)').slice(0, 240);
  const status = r.status || 'PENDING';
  const elapsed = r.endTime ? Math.round((r.endTime - r.submitTime) / 1000) + 's' : '';
  const params = r.params || {};

  let paramText;
  if (isImgMode) {
    paramText = [
      params.size || '',
      params.n ? `${params.n} 张` : '',
      r.imageUrls ? `结果: ${r.imageUrls.length} 张` : '',
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
          <span class="h-mode-tag">${MODE_LABEL[r.mode] || r.mode}${isImgMode && r.subMode ? ' · ' + (r.subMode === 'edit' ? '编辑' : '文生图') : ''}</span>
          <span class="pill ${status}">${status}</span>
          <span class="h-time" title="${fmtAbsTime(r.submitTime)}">${fmtRelTime(r.submitTime)}</span>
        </div>
        <div class="h-prompt">${escapeHTML(promptShort)}</div>
        <div class="h-params">${paramText}</div>
        ${r.errorMsg ? `<div class="h-error">${escapeHTML(r.errorMsg)}</div>` : ''}
      </div>
      <div class="h-actions">
        <button data-action="detail" data-id="${r.id}">详情</button>
        ${canDownload ? `<button data-action="download" data-id="${r.id}">下载</button>` : ''}
        ${canDownload ? `<button data-action="copy" data-id="${r.id}">复制链接</button>` : ''}
        ${(isLive && r.taskId) ? `<button data-action="resume" data-id="${r.id}">查询</button>` : ''}
        <button data-action="reuse" data-id="${r.id}">复用</button>
        <button data-action="delete" data-id="${r.id}" class="danger">删除</button>
      </div>
    </div>
  `;
}

// ---------- History actions ----------
async function onHistoryAction(action, id) {
  const r = loadHistory().find(x => x.id === id);
  if (!r) return;
  if (action === 'delete') {
    if (confirm('删除这条历史？')) deleteHistoryRecord(id);
  } else if (action === 'download') {
    if (r.imageUrls && r.imageUrls.length) {
      window.location.href = `/api/download?url=${encodeURIComponent(r.imageUrls[0])}&filename=omnigen-imggen-${r.id}.png`;
    } else if (r.videoUrl) {
      window.location.href = `/api/download?url=${encodeURIComponent(r.videoUrl)}&filename=omnigen-${r.mode}-${r.id}.mp4`;
    }
  } else if (action === 'copy') {
    const url = (r.imageUrls && r.imageUrls.length) ? r.imageUrls[0] : r.videoUrl;
    if (!url) return;
    try { await navigator.clipboard.writeText(url); alert(r.imageUrls ? '已复制图片链接' : '已复制视频链接'); }
    catch { alert('复制失败：' + url); }
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
      <tr><td style="padding:4px 8px;color:var(--muted);width:120px">模式</td><td>${MODE_LABEL[r.mode] || r.mode}${isImgMode && r.subMode ? ' · ' + (r.subMode === 'edit' ? '图片编辑' : '文生图') : ''} (${r.model || ''})</td></tr>
      <tr><td style="padding:4px 8px;color:var(--muted)">状态</td><td><span class="pill ${r.status || 'PENDING'}">${r.status || 'PENDING'}</span></td></tr>
      ${!isImgMode ? `<tr><td style="padding:4px 8px;color:var(--muted)">task_id</td><td><code>${r.taskId || '(未创建)'}</code></td></tr>` : ''}
      <tr><td style="padding:4px 8px;color:var(--muted)">提交时间</td><td>${fmtAbsTime(r.submitTime)}</td></tr>
      ${r.endTime ? `<tr><td style="padding:4px 8px;color:var(--muted)">结束时间</td><td>${fmtAbsTime(r.endTime)}（用时 ${Math.round((r.endTime-r.submitTime)/1000)}s）</td></tr>` : ''}
      <tr><td style="padding:4px 8px;color:var(--muted)">地域</td><td>${r.region}${r.workspaceId ? ' · ' + r.workspaceId : ''}</td></tr>
      <tr><td style="padding:4px 8px;color:var(--muted);vertical-align:top">参数</td><td><code>${escapeHTML(JSON.stringify(params))}</code></td></tr>
      ${r.imageCount ? `<tr><td style="padding:4px 8px;color:var(--muted)">${isImgMode ? '输入图' : '参考图'}</td><td>${r.imageCount} 张</td></tr>` : ''}
      ${r.imageUrls ? `<tr><td style="padding:4px 8px;color:var(--muted)">生成结果</td><td>${r.imageUrls.length} 张图片</td></tr>` : ''}
    </table>`,
    `<div style="margin-top:12px"><b style="font-size:13px">Prompt</b><div style="background:var(--panel-2);padding:10px;border-radius:6px;font-size:13px;white-space:pre-wrap;margin-top:6px">${escapeHTML(r.prompt || '(无)')}</div></div>`,
    r.videoUrl ? `<div style="margin-top:12px"><b style="font-size:13px">视频</b><video src="${r.videoUrl}" controls style="width:100%;margin-top:6px;border-radius:6px;background:#000"></video></div>` : '',
  ];

  // Render image results for imggen mode
  if (r.imageUrls && r.imageUrls.length) {
    const imgsHTML = r.imageUrls.map((url, i) =>
      `<div style="margin-top:8px"><b style="font-size:13px">图片 ${i + 1}</b><img src="${url}" style="width:100%;margin-top:6px;border-radius:6px;background:#000" /></div>`
    ).join('');
    lines.push(`<div style="margin-top:12px">${imgsHTML}</div>`);
  }

  if (r.errorMsg) {
    lines.push(`<div style="margin-top:12px"><b style="font-size:13px;color:var(--danger)">错误</b><div style="background:var(--panel-2);padding:10px;border-radius:6px;font-size:13px;color:var(--danger);margin-top:6px">${escapeHTML(r.errorMsg)}</div></div>`);
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
    alert(`已切换到「图片生成」并回填 prompt/参数。${r.imageCount ? '注意：输入图片不会保留，需要重新上传。' : ''}`);
    return;
  }

  switchToTab(r.mode);
  if ($('prompt-' + r.mode)) $('prompt-' + r.mode).value = r.prompt || '';
  if (p.resolution && $('resolution-' + r.mode)) $('resolution-' + r.mode).value = p.resolution;
  if (p.ratio && $('ratio-' + r.mode)) $('ratio-' + r.mode).value = p.ratio;
  if (p.duration && $('duration-' + r.mode)) $('duration-' + r.mode).value = p.duration;
  if (typeof p.watermark === 'boolean' && $('watermark-' + r.mode)) $('watermark-' + r.mode).checked = p.watermark;
  if (p.seed != null && $('seed-' + r.mode)) $('seed-' + r.mode).value = p.seed;
  alert(`已切换到「${MODE_LABEL[r.mode]}」并回填 prompt/参数。${r.imageCount ? '注意：图片不会保留，需要重新上传。' : ''}`);
}

function resumeHistory(r) {
  if (!r.taskId) return alert('该记录没有 task_id，无法查询');
  const auth = checkAuth();
  if (!auth) return;
  switchToTab(r.mode);
  const ctx = ({ r2v, t2v, i2v })[r.mode];
  if (!ctx) return;
  ctx.currentHistoryId = r.id;
  const log = makeLog('log-' + r.mode);
  $('log-' + r.mode).innerHTML = '';
  log(`复活轮询 task ${r.taskId}…`);
  setStatusHTML('status-' + r.mode, `<span class="pill PENDING">查询中</span> task_id: <code>${r.taskId}</code>`);
  pollTask(ctx, r.taskId, log);
}

// ---------- Toolbar ----------
$('clearHistoryBtn').addEventListener('click', () => {
  if (!loadHistory().length) return;
  if (confirm('确定清空所有历史记录？此操作不可撤销。')) {
    saveHistory([]);
    refreshHistoryUI();
  }
});
$('exportHistoryBtn').addEventListener('click', () => {
  const arr = loadHistory();
  if (!arr.length) return alert('无记录');
  const blob = new Blob([JSON.stringify(arr, null, 2)], { type: 'application/json' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `omnigen-history-${Date.now()}.json`;
  a.click();
  URL.revokeObjectURL(a.href);
});

// Init
refreshHistoryUI();
