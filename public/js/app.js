/* ── OmniGen AI: Core app, settings, navigation ── */

const $ = (id) => document.getElementById(id);

// Model names fetched from server; fallback values used before load completes.
let MODEL_NAMES = { TEXT_OPTIMIZE: 'qwen3.7-plus', VISION_OPTIMIZE_LABEL: 'qwen-vl-max' };

(async function loadModelConfig() {
  try {
    const resp = await fetch('/api/config');
    const cfg = await resp.json();
    MODEL_NAMES = {
      TEXT_OPTIMIZE: cfg.models.TEXT_OPTIMIZE,
      VISION_OPTIMIZE_LABEL: cfg.models.VISION_OPTIMIZE_LABEL,
      IMAGE: cfg.models.IMAGE,
    };
    document.querySelectorAll('[data-model-title="vision"]').forEach(el => {
      el.title = t('r2v.optimizeTitle');
    });
    document.querySelectorAll('[data-model-title="text"]').forEach(el => {
      el.title = t('t2v.optimizeTitle');
    });
  } catch (e) { /* use fallback defaults */ }
})();

// ---------- Settings & Auth ----------
const apiKeyEl = $('apiKey');
const apiKeyLabelEl = $('apiKeyLabel');
const apiKeyNoteEl = $('apiKeyNote');
const regionEl = $('region');
const regionWrap = $('regionWrap');
const workspaceWrap = $('workspaceWrap');
const workspaceIdEl = $('workspaceId');
const endpointEl = $('endpoint');
const customEndpointWrap = $('customEndpointWrap');
const customEndpointEl = $('customEndpoint');
const t8OnlyImageNote = $('t8OnlyImageNote');
const configChip = $('configChip');
const configChipText = $('configChipText');
const setupHint = $('setupHint');

const T8STAR_BASE = 'https://ai.t8star.org';
/** localStorage slot per provider — the two keys never overwrite each other. */
const KEY_SLOT = { dashscope: 'hh_api_key', t8star: 'hh_t8_api_key' };

/**
 * The provider is derived from the endpoint, never stored separately —
 * one source of truth keeps the two from drifting apart.
 */
function getProvider() {
  return endpointEl.value === T8STAR_BASE ? 't8star' : 'dashscope';
}
function isT8Provider() { return getProvider() === 't8star'; }

/** True when the model speaks the t8star OpenAI chat protocol. */
function isT8Model(model) {
  return model === 'gpt-image-2';
}

// Endpoint must be restored before the key: the key slot depends on it.
endpointEl.value = localStorage.getItem('hh_endpoint') || '';
customEndpointEl.value = localStorage.getItem('hh_custom_endpoint') || '';
// Tracks which provider's key currently sits in the input box.
let keySlotProvider = getProvider();
apiKeyEl.value = localStorage.getItem(KEY_SLOT[keySlotProvider]) || '';
regionEl.value = localStorage.getItem('hh_region') || 'cn-beijing';
workspaceIdEl.value = localStorage.getItem('hh_ws_id') || '';
toggleCustomEndpoint();

const REGION_LABELS = {
  'cn-beijing': t('regions.cn-beijing'),
  'ap-southeast-1': t('regions.ap-southeast-1'),
  'us-east-1': t('regions.us-east-1'),
  'eu-central-1': t('regions.eu-central-1'),
};

regionEl.addEventListener('change', toggleWorkspace);
function toggleWorkspace() {
  workspaceWrap.style.display = regionEl.value === 'eu-central-1' ? '' : 'none';
}
toggleWorkspace();

endpointEl.addEventListener('change', onEndpointChange);
function toggleCustomEndpoint() {
  customEndpointWrap.style.display = endpointEl.value === 'custom' ? '' : 'none';
}

function onEndpointChange() {
  const next = getProvider();
  if (next !== keySlotProvider) {
    // Park the current box contents in the old provider's slot before swapping,
    // otherwise switching back would show an empty key.
    localStorage.setItem(KEY_SLOT[keySlotProvider], apiKeyEl.value.trim());
    apiKeyEl.value = localStorage.getItem(KEY_SLOT[next]) || '';
    keySlotProvider = next;
  }
  toggleCustomEndpoint();
  applyProviderFields();
  refreshConfigChip();
}

/** Region and workspace are DashScope-only concepts; hide them for t8star. */
function applyProviderFields() {
  const t8 = isT8Provider();
  regionWrap.style.display = t8 ? 'none' : '';
  t8OnlyImageNote.style.display = t8 ? '' : 'none';
  if (t8) workspaceWrap.style.display = 'none';
  else toggleWorkspace();
  // Swap the data-i18n key too, not just the text: translateDOM() runs on load
  // and on every locale change, and would otherwise reset these to the DashScope
  // wording straight after we set it.
  const labelKey = t8 ? 'settings.apiKeyLabelT8' : 'settings.apiKeyLabel';
  const noteKey = t8 ? 'settings.apiKeyNoteT8' : 'settings.apiKeyNote';
  apiKeyLabelEl.setAttribute('data-i18n', labelKey);
  apiKeyNoteEl.setAttribute('data-i18n', noteKey);
  apiKeyLabelEl.textContent = t(labelKey);
  apiKeyNoteEl.textContent = t(noteKey);
}
applyProviderFields();

function refreshConfigChip() {
  const hasKey = !!apiKeyEl.value.trim();
  const region = REGION_LABELS[regionEl.value] || regionEl.value;
  let epLabel = '';
  if (endpointEl.value === 'custom') {
    const ce = customEndpointEl.value.trim();
    if (ce) {
      try { epLabel = ' · ' + new URL(ce).hostname; } catch { epLabel = t('config.custom'); }
    }
  } else if (endpointEl.value) {
    try { epLabel = ' · ' + new URL(endpointEl.value).hostname; } catch { /* */ }
  }
  if (hasKey) {
    configChip.classList.add('ok');
    configChipText.textContent = isT8Provider()
      ? t('config.readyT8', { epLabel })
      : t('config.ready', { region, epLabel });
    setupHint.style.display = 'none';
  } else {
    configChip.classList.remove('ok');
    configChipText.textContent = t('config.noApiKey');
    setupHint.style.display = '';
  }
}
refreshConfigChip();

function openSettings() { switchToTab('settings'); apiKeyEl.focus(); }

configChip.addEventListener('click', openSettings);
$('setupHintBtn').addEventListener('click', openSettings);
$('settingsSave').addEventListener('click', () => {
  localStorage.setItem(KEY_SLOT[getProvider()], apiKeyEl.value.trim());
  localStorage.setItem('hh_region', regionEl.value);
  localStorage.setItem('hh_ws_id', workspaceIdEl.value.trim());
  localStorage.setItem('hh_endpoint', endpointEl.value);
  localStorage.setItem('hh_custom_endpoint', customEndpointEl.value.trim());
  refreshConfigChip();
  // Model lists, parameter controls and the video gate all key off the provider.
  window.dispatchEvent(new Event('providerchange'));
  switchToTab('r2v');
});

// Language switcher
const langSelect = $('langSelect');
if (langSelect) {
  langSelect.value = getLocale();
  langSelect.addEventListener('change', async () => {
    await setLocale(langSelect.value);
    refreshConfigChip();
  });
}

window.addEventListener('localechange', () => {
  applyProviderFields();
  refreshConfigChip();
});

/**
 * Resolves the effective base URL for API requests.
 * Returns empty string if using official endpoints (server will compute from region).
 */
function getEndpointUrl() {
  if (endpointEl.value === 'custom') return customEndpointEl.value.trim();
  return endpointEl.value; // '' means official
}

function getAuth() {
  return {
    apiKey: apiKeyEl.value.trim(),
    region: regionEl.value,
    workspaceId: workspaceIdEl.value.trim(),
    endpoint: getEndpointUrl(),
  };
}
function checkAuth(needsWs = true) {
  const a = getAuth();
  if (!a.apiKey) {
    if (confirm(t('config.confirmNoApiKey'))) openSettings();
    return null;
  }
  // Region and workspace are DashScope concepts — t8star has neither.
  if (needsWs && !isT8Provider() && a.region === 'eu-central-1' && !a.workspaceId) {
    if (confirm(t('config.confirmWorkspace'))) openSettings();
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
