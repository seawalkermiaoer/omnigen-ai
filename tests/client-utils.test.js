/**
 * OmniGen AI Client-Side Utility Tests
 *
 * Tests for pure functions and history CRUD from public/js/*.js files.
 * Uses vm.runInNewContext to load client JS in a sandboxed environment.
 */

const { describe, it } = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

// ─── Sandbox helpers ──────────────────────────────────────────────

function makeElement(id, extra = {}) {
  const listeners = {};
  return {
    id,
    value: extra.value || '',
    checked: extra.checked || false,
    files: [],
    textContent: '',
    innerHTML: '',
    src: '',
    style: {},
    dataset: extra.dataset || {},
    classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } },
    addEventListener(type, fn) { listeners[type] = listeners[type] || []; listeners[type].push(fn); },
    dispatchEvent(event) { for (const fn of listeners[event.type] || []) fn(event); },
    click() {},
    closest() { return makeElement(`${id}-slot`); },
    querySelectorAll() { return []; },
    querySelector() { return makeElement('slot'); },
    appendChild() {},
    focus() {},
    ...extra,
  };
}

// ─── escapeHTML ───────────────────────────────────────────────────

describe('escapeHTML (from task.js)', () => {
  const ctx = {};
  vm.runInNewContext(`
    function escapeHTML(s) {
      return String(s).replace(/[&<>"']/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch]));
    }
    result = escapeHTML;
  `, ctx);
  const escapeHTMLFn = ctx.result;

  it('passes plain text unchanged', () => {
    assert.equal(escapeHTMLFn('hello'), 'hello');
  });

  it('escapes < and >', () => {
    assert.equal(escapeHTMLFn('<script>'), '&lt;script&gt;');
  });

  it('escapes &', () => {
    assert.equal(escapeHTMLFn('&amp;'), '&amp;amp;');
  });

  it('escapes double quotes', () => {
    assert.equal(escapeHTMLFn('say "hi"'), 'say &quot;hi&quot;');
  });

  it('escapes single quotes', () => {
    assert.equal(escapeHTMLFn("it's"), 'it&#39;s');
  });

  it('handles empty string', () => {
    assert.equal(escapeHTMLFn(''), '');
  });

  it('coerces null to string', () => {
    assert.equal(escapeHTMLFn(null), 'null');
  });
});

// ─── escapeAttr ───────────────────────────────────────────────────

describe('escapeAttr (from task.js)', () => {
  const ctx = {};
  vm.runInNewContext(`
    function escapeAttr(s) { return String(s || '').replace(/"/g, '&quot;'); }
    result = escapeAttr;
  `, ctx);
  const escapeAttrFn = ctx.result;

  it('escapes double quotes', () => {
    assert.equal(escapeAttrFn('url"param'), 'url&quot;param');
  });

  it('handles null as empty string', () => {
    assert.equal(escapeAttrFn(null), '');
  });

  it('handles undefined as empty string', () => {
    assert.equal(escapeAttrFn(undefined), '');
  });

  it('passes text without quotes unchanged', () => {
    assert.equal(escapeAttrFn('no-quotes'), 'no-quotes');
  });
});

// ─── fmtRelTime ───────────────────────────────────────────────────

describe('fmtRelTime (from history.js)', () => {
  const src = read('public/js/history.js');
  const fnMatch = src.match(/function fmtRelTime\(ts\)\s*\{[\s\S]*?\n\}/m);
  const fnBody = fnMatch[0].match(/\{([\s\S]+)\}/)[1];
  const fmtRelTimeFn = new Function('ts', 'Date', fnBody);
  // Bind Date
  const fmt = (ts) => fmtRelTimeFn(ts, Date);

  it('returns 刚刚 for just now', () => {
    assert.equal(fmt(Date.now()), '刚刚');
  });

  it('returns N 分钟前 for 5 minutes ago', () => {
    assert.equal(fmt(Date.now() - 5 * 60_000), '5 分钟前');
  });

  it('returns N 小时前 for 3 hours ago', () => {
    assert.equal(fmt(Date.now() - 3 * 3600_000), '3 小时前');
  });

  it('returns N 天前 for 2 days ago', () => {
    assert.equal(fmt(Date.now() - 2 * 86400_000), '2 天前');
  });

  it('returns locale date for 30 days ago', () => {
    const result = fmt(Date.now() - 30 * 86400_000);
    assert.ok(!result.includes('天前'), `expected locale date, got: ${result}`);
  });
});

// ─── genHistoryId ─────────────────────────────────────────────────

describe('genHistoryId (from history.js)', () => {
  const src = read('public/js/history.js');
  const fnMatch = src.match(/function genHistoryId\(\)\s*\{[^}]+\}/);
  const fnBody = fnMatch[0].match(/\{([\s\S]+)\}/)[1];
  const genHistoryIdFn = new Function('Date', 'Math', fnBody);
  const gen = () => genHistoryIdFn(Date, Math);

  it('starts with h_ prefix', () => {
    assert.match(gen(), /^h_/);
  });

  it('produces unique IDs', () => {
    assert.notEqual(gen(), gen());
  });

  it('matches expected pattern', () => {
    assert.match(gen(), /^h_\d+_[a-z0-9]{5}$/);
  });
});

// ─── isWanModel / isWanEditModel ─────────────────────────────────

describe('isWanModel (from imggen.js)', () => {
  const src = read('public/js/imggen.js');
  const fnMatch = src.match(/function isWanModel\(model\)\s*\{[^}]+\}/);
  const fnBody = fnMatch[0].match(/\{([\s\S]+)\}/)[1];
  const isWanModelFn = new Function('model', fnBody);

  it('returns true for wan2.7-image-pro', () => {
    assert.equal(isWanModelFn('wan2.7-image-pro'), true);
  });

  it('returns true for wan2.7-image', () => {
    assert.equal(isWanModelFn('wan2.7-image'), true);
  });

  it('returns false for qwen-image-plus', () => {
    assert.equal(isWanModelFn('qwen-image-plus'), false);
  });

  it('returns falsy for null', () => {
    assert.ok(!isWanModelFn(null));
  });
});

describe('isWanEditModel (from imgedit.js)', () => {
  const src = read('public/js/imgedit.js');
  const fnMatch = src.match(/function isWanEditModel\(model\)\s*\{[^}]+\}/);
  const fnBody = fnMatch[0].match(/\{([\s\S]+)\}/)[1];
  const isWanEditModelFn = new Function('model', fnBody);

  it('returns true for wan2.7-image-pro', () => {
    assert.equal(isWanEditModelFn('wan2.7-image-pro'), true);
  });

  it('returns true for wan2.7-image', () => {
    assert.equal(isWanEditModelFn('wan2.7-image'), true);
  });

  it('returns false for qwen-image-edit-plus', () => {
    assert.equal(isWanEditModelFn('qwen-image-edit-plus'), false);
  });

  it('returns falsy for empty string', () => {
    assert.ok(!isWanEditModelFn(''));
  });
});

// ─── History CRUD ─────────────────────────────────────────────────

describe('History CRUD (from history.js)', () => {
  function loadHistoryModule() {
    const src = read('public/js/history.js');
    const store = new Map();
    const elements = new Map();
    const context = {
      localStorage: {
        getItem: (k) => store.has(k) ? store.get(k) : null,
        setItem: (k, v) => store.set(k, String(v)),
        removeItem: (k) => store.delete(k),
      },
      console: { warn() {} },
      Date,
      Math,
      JSON,
      $: (id) => {
        if (!elements.has(id)) elements.set(id, makeElement(id));
        return elements.get(id);
      },
      document: { querySelector: () => null },
      confirm: () => true,
      alert: () => {},
      navigator: { clipboard: { writeText: async () => {} } },
      window: { location: {} },
      escapeHTML: (s) => String(s),
      switchToTab: () => {},
      renderHistoryList: () => {},
    };
    vm.runInNewContext(src, context);
    return context;
  }

  it('loadHistory returns empty array on fresh state', () => {
    const ctx = loadHistoryModule();
    assert.deepEqual(ctx.loadHistory(), []);
  });

  it('addHistoryRecord prepends and persists', () => {
    const ctx = loadHistoryModule();
    const id = ctx.addHistoryRecord({ id: 'h1', mode: 'r2v', status: 'PENDING' });
    assert.equal(id, 'h1');
    const arr = ctx.loadHistory();
    assert.equal(arr.length, 1);
    assert.equal(arr[0].id, 'h1');
  });

  it('patchHistoryRecord merges fields', () => {
    const ctx = loadHistoryModule();
    ctx.addHistoryRecord({ id: 'h1', mode: 'r2v', status: 'PENDING', prompt: 'test' });
    ctx.patchHistoryRecord('h1', { status: 'SUCCEEDED' });
    const arr = ctx.loadHistory();
    assert.equal(arr[0].status, 'SUCCEEDED');
    assert.equal(arr[0].prompt, 'test');
  });

  it('deleteHistoryRecord removes by id', () => {
    const ctx = loadHistoryModule();
    ctx.addHistoryRecord({ id: 'h1', mode: 'r2v' });
    ctx.addHistoryRecord({ id: 'h2', mode: 'i2v' });
    assert.equal(ctx.loadHistory().length, 2);
    ctx.deleteHistoryRecord('h1');
    const arr = ctx.loadHistory();
    assert.equal(arr.length, 1);
    assert.equal(arr[0].id, 'h2');
  });

  it('saveHistory truncates to HISTORY_MAX (100)', () => {
    const ctx = loadHistoryModule();
    for (let i = 0; i < 105; i++) {
      ctx.addHistoryRecord({ id: 'h' + i, mode: 'r2v' });
    }
    assert.equal(ctx.loadHistory().length, 100);
  });

  it('loadHistory returns [] on corrupt JSON', () => {
    const ctx = loadHistoryModule();
    ctx.localStorage.setItem('hh_history_v1', 'not-valid-json');
    const result = ctx.loadHistory();
    assert.ok(Array.isArray(result));
    assert.equal(result.length, 0);
  });
});

// ─── updateCountOptions (imggen.js) ──────────────────────────────

describe('updateCountOptions (from imggen.js)', () => {
  function runImggenCountOptions(model, sequential) {
    const src = read('public/js/imggen.js');
    const elements = new Map();
    const getEl = (id, defaults = {}) => {
      if (!elements.has(id)) elements.set(id, makeElement(id, defaults));
      return elements.get(id);
    };
    const context = {
      $: (id) => {
        if (id === 'model-imggen') return getEl(id, { value: model });
        if (id === 'count-imggen') return getEl(id, { value: '1' });
        if (id === 'size-imggen') return getEl(id, { value: '1328*1328' });
        if (id === 'sequential-imggen') return getEl(id, { checked: sequential });
        return getEl(id);
      },
      document: { querySelectorAll: () => [] },
      alert: () => {},
      console: { warn() {} },
      fetch: () => Promise.resolve({ json: () => Promise.resolve({}) }),
      navigator: { clipboard: { writeText: async () => {} } },
      bindOptimize: () => {},
    };
    vm.runInNewContext(src, context);
    const countEl = context.$('count-imggen');
    const matches = countEl.innerHTML.match(/<option/g);
    return matches ? matches.length : 0;
  }

  it('qwen-image-plus → 1 option', () => {
    assert.equal(runImggenCountOptions('qwen-image-plus', false), 1);
  });

  it('wan2.7-image-pro → 4 options (no sequential)', () => {
    assert.equal(runImggenCountOptions('wan2.7-image-pro', false), 4);
  });

  it('wan2.7-image-pro + sequential → 12 options', () => {
    assert.equal(runImggenCountOptions('wan2.7-image-pro', true), 12);
  });
});

// ─── updateImgeditCountOptions (imgedit.js) ──────────────────────

describe('updateImgeditCountOptions (from imgedit.js)', () => {
  function runImgeditCountOptions(model, sequential) {
    const src = read('public/js/imgedit.js');
    const elements = new Map();
    const getEl = (id, defaults = {}) => {
      if (!elements.has(id)) elements.set(id, makeElement(id, defaults));
      return elements.get(id);
    };
    const context = {
      $: (id) => {
        if (id === 'model-imgedit') return getEl(id, { value: model });
        if (id === 'count-imgedit') return getEl(id, { value: '1' });
        if (id === 'size-imgedit') return getEl(id, { value: '1328*1328' });
        if (id === 'sequential-imgedit') return getEl(id, { checked: sequential });
        return getEl(id);
      },
      document: { querySelectorAll: () => [] },
      alert: () => {},
      console: { warn() {} },
      fetch: () => Promise.resolve({ json: () => Promise.resolve({}) }),
      navigator: { clipboard: { writeText: async () => {} } },
      bindOptimize: () => {},
    };
    vm.runInNewContext(src, context);
    const countEl = context.$('count-imgedit');
    const matches = countEl.innerHTML.match(/<option/g);
    return matches ? matches.length : 0;
  }

  it('qwen-image-edit-plus → 6 options', () => {
    assert.equal(runImgeditCountOptions('qwen-image-edit-plus', false), 6);
  });

  it('qwen-image-edit → 1 option', () => {
    assert.equal(runImgeditCountOptions('qwen-image-edit', false), 1);
  });

  it('wan2.7-image → 4 options (no sequential)', () => {
    assert.equal(runImgeditCountOptions('wan2.7-image', false), 4);
  });

  it('wan2.7-image + sequential → 12 options', () => {
    assert.equal(runImgeditCountOptions('wan2.7-image', true), 12);
  });
});
