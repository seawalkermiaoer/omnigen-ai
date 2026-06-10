const { describe, it } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

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
});
