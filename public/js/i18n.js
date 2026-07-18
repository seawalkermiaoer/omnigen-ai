/* ── OmniGen AI: i18n — Lightweight internationalization module ── */

const I18N_LANG_KEY = 'hh_lang';
const SUPPORTED_LOCALES = ['zh-CN', 'en'];
const DEFAULT_LOCALE = 'zh-CN';

let _locale = localStorage.getItem(I18N_LANG_KEY) || DEFAULT_LOCALE;
let _dict = {};
let _fallbackDict = {};

/**
 * Load locale dictionary from JSON file.
 * Falls back to DEFAULT_LOCALE if the requested locale file is missing keys.
 */
async function loadLocale(locale) {
  if (!SUPPORTED_LOCALES.includes(locale)) locale = DEFAULT_LOCALE;

  try {
    const resp = await fetch(`locales/${locale}.json`);
    _dict = await resp.json();
  } catch (e) {
    console.error(`[i18n] Failed to load locale: ${locale}`, e);
    _dict = {};
  }

  // Load fallback (default locale) if current is not default
  if (locale !== DEFAULT_LOCALE) {
    try {
      const resp = await fetch(`locales/${DEFAULT_LOCALE}.json`);
      _fallbackDict = await resp.json();
    } catch { _fallbackDict = {}; }
  } else {
    _fallbackDict = {};
  }

  _locale = locale;
  localStorage.setItem(I18N_LANG_KEY, locale);
}

/**
 * Translate a key with optional parameter interpolation.
 * Usage: t('key') or t('key', { count: 5, name: 'foo' })
 * Keys use dot notation: 'sidebar.title'
 */
function t(key, params) {
  let value = _getNestedValue(_dict, key);
  if (value === undefined || value === null) {
    value = _getNestedValue(_fallbackDict, key);
  }
  if (value === undefined || value === null) {
    // Return key itself as fallback (for debugging)
    return key;
  }
  if (params) {
    return _interpolate(String(value), params);
  }
  return String(value);
}

function _getNestedValue(obj, key) {
  const parts = key.split('.');
  let current = obj;
  for (const part of parts) {
    if (current === undefined || current === null) return undefined;
    current = current[part];
  }
  return current;
}

function _interpolate(str, params) {
  return str.replace(/\{(\w+)\}/g, (_, name) => {
    return params[name] !== undefined ? String(params[name]) : `{${name}}`;
  });
}

/**
 * Translate all DOM elements with [data-i18n] attribute.
 * Supports:
 *   data-i18n="key"              → textContent
 *   data-i18n="attr:key"         → setAttribute(attr, value)
 *   data-i18n-html="key"         → innerHTML
 */
function translateDOM() {
  // textContent translations
  document.querySelectorAll('[data-i18n]').forEach(el => {
    const raw = el.getAttribute('data-i18n');
    const parts = raw.split(':');
    if (parts.length === 2) {
      // attr:key
      const [attr, key] = parts;
      el.setAttribute(attr, t(key));
    } else {
      el.textContent = t(raw);
    }
  });

  // innerHTML translations
  document.querySelectorAll('[data-i18n-html]').forEach(el => {
    const key = el.getAttribute('data-i18n-html');
    el.innerHTML = t(key);
  });

  // placeholder translations
  document.querySelectorAll('[data-i18n-placeholder]').forEach(el => {
    const key = el.getAttribute('data-i18n-placeholder');
    el.placeholder = t(key);
  });

  // title translations
  document.querySelectorAll('[data-i18n-title]').forEach(el => {
    const key = el.getAttribute('data-i18n-title');
    el.title = t(key);
  });

  // Update HTML lang attribute
  document.documentElement.lang = _locale;
}

/**
 * Get current locale.
 */
function getLocale() {
  return _locale;
}

/**
 * Switch locale and re-translate everything.
 */
async function setLocale(locale) {
  await loadLocale(locale);
  translateDOM();
  // Dispatch custom event so other modules can react
  window.dispatchEvent(new CustomEvent('localechange', { detail: { locale } }));
}

// Initialize on load
(async function initI18n() {
  await loadLocale(_locale);
  // Translate DOM when DOM is ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', translateDOM);
  } else {
    translateDOM();
  }
})();
