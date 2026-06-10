const { describe, it } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.resolve(__dirname, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

describe('watermark UI defaults', () => {
  it('uses the same add-watermark label on every page', () => {
    const html = read('public/index.html');
    const labels = Array.from(html.matchAll(/<label for="watermark-[^"]+"[^>]*>(.*?)<\/label>/g))
      .map((match) => match[1]);

    assert.deepEqual(labels, [
      '添加水印',
      '添加水印',
      '添加水印',
      '添加水印',
      '添加水印',
    ]);
  });

  it('keeps OmniGen AIStudio as the default watermark brand', () => {
    const taskJs = read('public/js/task.js');

    assert.match(taskJs, /DEFAULT_WATERMARK_TEXT\s*=\s*['"]OmniGen AIStudio['"]/);
  });
});

describe('i2v image upload feedback', () => {
  it('shows visible feedback when an uploaded frame fails validation', () => {
    const html = read('public/index.html');
    const js = read('public/js/i2v.js');

    assert.match(html, /id="uploadError-i2v"/);
    assert.match(html, /id="uploadError-last-i2v"/);
    assert.match(js, /showI2vUploadError/);
    assert.match(js, /errorId/);
    assert.match(js, /errorEl\.textContent = message/);
    assert.match(js, /宽高比 .* 超出范围/);
    assert.match(js, /图像尺寸 .* 不满足/);
    assert.match(js, /fileInput\.value = ''/);
  });

  it('shows ratio feedback immediately without uploading an invalid frame', async () => {
    const js = read('public/js/i2v.js');
    const { elements, uploadCalls } = runI2vUploader(js, { naturalWidth: 2047, naturalHeight: 550 });
    const fileInput = elements.get('fileInput-i2v');

    fileInput.value = '/fake/wide.png';
    fileInput.files = [{ name: 'wide.png', type: 'image/png', size: 128 * 1024 }];
    fileInput.dispatchEvent({ type: 'change', target: fileInput });

    await flushAsync();

    assert.equal(uploadCalls.count, 0);
    assert.match(elements.get('uploadError-i2v').textContent, /宽高比 3\.72 超出范围/);
    assert.equal(elements.get('uploadError-i2v').style.display, 'block');
    assert.match(elements.get('status-i2v').innerHTML, /上传失败/);
    assert.equal(fileInput.value, '');
  });
});

function flushAsync() {
  return new Promise((resolve) => setImmediate(resolve));
}

function createElement(id, extra = {}) {
  const listeners = {};
  const element = {
    id,
    value: extra.value || '',
    files: [],
    textContent: '',
    innerHTML: '',
    src: '',
    style: {},
    dataset: extra.dataset || {},
    classList: { add() {}, remove() {}, toggle() {} },
    addEventListener(type, listener) {
      listeners[type] = listeners[type] || [];
      listeners[type].push(listener);
    },
    dispatchEvent(event) {
      for (const listener of listeners[event.type] || []) listener(event);
    },
    click() {},
    closest() { return createElement(`${id}-slot`); },
    querySelectorAll() { return []; },
    ...extra,
  };
  return element;
}

function runI2vUploader(js, imageSize) {
  const uploadCalls = { count: 0 };
  const elements = new Map();
  const ids = [
    'model-i2v', 'firstFrameLabel-i2v', 'promptHint-i2v',
    'uploader-i2v', 'fileInput-i2v', 'singleImg-i2v', 'singleImgEl-i2v',
    'singleImgInfo-i2v', 'singleImgRemove-i2v', 'uploadError-i2v',
    'uploader-last-i2v', 'fileInput-last-i2v', 'singleImg-last-i2v',
    'singleImgEl-last-i2v', 'singleImgInfo-last-i2v', 'singleImgRemove-last-i2v',
    'uploadError-last-i2v', 'firstClip-i2v', 'drivingAudio-i2v',
    'submitBtn-i2v', 'prompt-i2v', 'prompt-extend-i2v', 'negative-i2v',
    'resetBtn-i2v', 'seed-i2v', 'videoOut-i2v', 'log-i2v', 'status-i2v',
  ];
  for (const id of ids) elements.set(id, createElement(id));
  elements.get('model-i2v').value = 'happyhorse-1.0-i2v';

  const document = {
    querySelectorAll(selector) {
      if (selector === '#i2vTaskTabs .seg-tab') {
        return [
          createElement('tab-first-frame', { dataset: { task: 'first_frame' } }),
          createElement('tab-first-last', { dataset: { task: 'first_last' } }),
        ];
      }
      return [];
    },
    querySelector() { return createElement('slot'); },
  };

  const context = {
    $: (id) => elements.get(id),
    document,
    Image: class FakeImage {
      set src(value) {
        this._src = value;
        this.naturalWidth = imageSize.naturalWidth;
        this.naturalHeight = imageSize.naturalHeight;
        queueMicrotask(() => this.onload && this.onload());
      }
      get src() { return this._src; }
    },
    URL: {
      createObjectURL() { return 'blob:wide.png'; },
      revokeObjectURL() {},
    },
    uploadImageToServer() {
      uploadCalls.count += 1;
      return new Promise(() => {});
    },
    makeLog() { return () => {}; },
    setStatusHTML(id, html) { elements.get(id).innerHTML = html; },
    escapeHTML(value) { return String(value); },
    makeThumb() { return Promise.resolve('thumb'); },
    bindOptimize() {},
    getAuth() { return { region: 'cn-beijing' }; },
    collectParams() { return {}; },
    submitTask() {},
    alert() {},
    queueMicrotask,
  };

  vm.runInNewContext(js, context);
  return { elements, uploadCalls };
}
