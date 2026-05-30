/* ── OmniGen AI: Core app, settings, navigation ── */

const $ = (id) => document.getElementById(id);

// Model names fetched from server; fallback values used before load completes.
let MODEL_NAMES = { TEXT_OPTIMIZE: 'qwen3.7-max', VISION_OPTIMIZE_LABEL: 'qwen-vl-max' };

(async function loadModelConfig() {
  try {
    const resp = await fetch('/api/config');
    const cfg = await resp.json();
    MODEL_NAMES = {
      TEXT_OPTIMIZE: cfg.models.TEXT_OPTIMIZE,
      VISION_OPTIMIZE_LABEL: cfg.models.VISION_OPTIMIZE_LABEL,
      IMAGE_OPENAI: cfg.models.IMAGE_OPENAI,
      IMAGE_DASHSCOPE: cfg.models.IMAGE_DASHSCOPE,
    };
    document.querySelectorAll('[data-model-title="vision"]').forEach(el => {
      el.title = `用 ${MODEL_NAMES.VISION_OPTIMIZE_LABEL} 看图后润色 prompt`;
    });
    document.querySelectorAll('[data-model-title="text"]').forEach(el => {
      el.title = `用 ${MODEL_NAMES.TEXT_OPTIMIZE} 润色 prompt`;
    });
  } catch (e) { /* use fallback defaults */ }
})();

// ---------- Settings & Auth ----------
const apiKeyEl = $('apiKey');
const regionEl = $('region');
const workspaceWrap = $('workspaceWrap');
const workspaceIdEl = $('workspaceId');
const configChip = $('configChip');
const configChipText = $('configChipText');
const setupHint = $('setupHint');

apiKeyEl.value = localStorage.getItem('hh_api_key') || '';
regionEl.value = localStorage.getItem('hh_region') || 'cn-beijing';
workspaceIdEl.value = localStorage.getItem('hh_ws_id') || '';

const REGION_LABELS = {
  'cn-beijing': '北京',
  'ap-southeast-1': '新加坡',
  'us-east-1': '弗吉尼亚',
  'eu-central-1': '法兰克福',
};

regionEl.addEventListener('change', toggleWorkspace);
function toggleWorkspace() {
  workspaceWrap.style.display = regionEl.value === 'eu-central-1' ? '' : 'none';
}
toggleWorkspace();

function refreshConfigChip() {
  const hasKey = !!apiKeyEl.value.trim();
  const region = REGION_LABELS[regionEl.value] || regionEl.value;
  if (hasKey) {
    configChip.classList.add('ok');
    configChipText.textContent = `已就绪 · ${region}`;
    setupHint.style.display = 'none';
  } else {
    configChip.classList.remove('ok');
    configChipText.textContent = '未配置 API Key';
    setupHint.style.display = '';
  }
}
refreshConfigChip();

function openSettings() { switchToTab('settings'); apiKeyEl.focus(); }

configChip.addEventListener('click', openSettings);
$('setupHintBtn').addEventListener('click', openSettings);
$('settingsSave').addEventListener('click', () => {
  localStorage.setItem('hh_api_key', apiKeyEl.value.trim());
  localStorage.setItem('hh_region', regionEl.value);
  localStorage.setItem('hh_ws_id', workspaceIdEl.value.trim());
  refreshConfigChip();
  switchToTab('r2v');
});

function getAuth() {
  return {
    apiKey: apiKeyEl.value.trim(),
    region: regionEl.value,
    workspaceId: workspaceIdEl.value.trim(),
  };
}
function checkAuth(needsWs = true) {
  const a = getAuth();
  if (!a.apiKey) {
    if (confirm('尚未配置 API Key。是否现在去设置？')) openSettings();
    return null;
  }
  if (needsWs && a.region === 'eu-central-1' && !a.workspaceId) {
    if (confirm('法兰克福地域需提供 WorkspaceId。是否现在去设置？')) openSettings();
    return null;
  }
  return a;
}

// ---------- Navigation ----------
function switchToTab(name) {
  document.querySelectorAll('.sidebar-item').forEach(b => b.classList.toggle('active', b.dataset.tab === name));
  document.querySelectorAll('.tab-content').forEach(c => c.classList.toggle('active', c.dataset.tab === name));
}

document.querySelectorAll('.sidebar-item').forEach(btn => {
  btn.addEventListener('click', () => {
    const tab = btn.dataset.tab;
    document.querySelectorAll('.sidebar-item').forEach(b => b.classList.toggle('active', b === btn));
    document.querySelectorAll('.tab-content').forEach(c => c.classList.toggle('active', c.dataset.tab === tab));
    if (tab === 'history') renderHistoryList();
  });
});

// ---------- Detail modal ----------
$('detailClose').addEventListener('click', () => $('detailModal').classList.remove('show'));
$('detailModal').addEventListener('click', (e) => { if (e.target.id === 'detailModal') $('detailModal').classList.remove('show'); });
