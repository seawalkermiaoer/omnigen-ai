/**
 * t8star (gpt-image-2) provider unit tests — no network IO.
 */

const { describe, it } = require('node:test');
const assert = require('node:assert/strict');

const { buildPayload, parseResponse, resolveBaseUrl, T8STAR_DEFAULT_BASE } = require('../lib/providers/t8star');

// Real captured response from the live API (see the design doc).
const REAL_T2I_RESPONSE = {
  id: 'chatcmpl-4ea9bde7-cf96-4f0a-9e04-630fa4db661b',
  object: 'chat.completion',
  model: 'gpt-image-2-pro',
  choices: [{
    index: 0,
    message: {
      role: 'assistant',
      content: '![image](https://webstatic.aiproxy.vip/output/2a67146d.png)\n\n好了，给你一只猫。',
    },
    finish_reason: 'stop',
  }],
  usage: { prompt_tokens: 3704, completion_tokens: 1221, total_tokens: 4925 },
};

describe('buildPayload', () => {
  it('uses a plain string content when there are no images', () => {
    const p = buildPayload({ model: 'gpt-image-2', prompt: '画只猫', images: [] });
    assert.equal(p.model, 'gpt-image-2');
    assert.equal(p.stream, false);
    assert.equal(p.messages.length, 1);
    assert.equal(p.messages[0].role, 'user');
    assert.equal(p.messages[0].content, '画只猫');
  });

  it('treats a missing images field the same as an empty list', () => {
    const p = buildPayload({ model: 'gpt-image-2', prompt: '画只猫' });
    assert.equal(p.messages[0].content, '画只猫');
  });

  it('uses an array content with one image_url block per image', () => {
    const p = buildPayload({
      model: 'gpt-image-2',
      prompt: '修改这个图片',
      images: ['https://a.com/1.png', 'data:image/png;base64,AAAA'],
    });
    const content = p.messages[0].content;
    assert.ok(Array.isArray(content));
    assert.equal(content.length, 3);
    assert.deepEqual(content[0], { type: 'text', text: '修改这个图片' });
    assert.deepEqual(content[1], { type: 'image_url', image_url: { url: 'https://a.com/1.png' } });
    assert.deepEqual(content[2], { type: 'image_url', image_url: { url: 'data:image/png;base64,AAAA' } });
  });

  it('omits the text block when there is no prompt', () => {
    const p = buildPayload({ model: 'gpt-image-2', prompt: '', images: ['https://a.com/1.png'] });
    const content = p.messages[0].content;
    assert.equal(content.length, 1);
    assert.equal(content[0].type, 'image_url');
  });

  it('drops falsy entries from the images list', () => {
    const p = buildPayload({ model: 'gpt-image-2', prompt: 'x', images: ['https://a.com/1.png', '', null] });
    assert.equal(p.messages[0].content.length, 2);
  });
});

describe('parseResponse', () => {
  it('extracts the image URL from the real captured response', () => {
    const { images } = parseResponse(REAL_T2I_RESPONSE);
    assert.deepEqual(images, ['https://webstatic.aiproxy.vip/output/2a67146d.png']);
  });

  it('strips the image link and keeps the prose as the note', () => {
    const { note } = parseResponse(REAL_T2I_RESPONSE);
    assert.equal(note, '好了，给你一只猫。');
  });

  it('extracts every image when the reply contains several', () => {
    const { images } = parseResponse({
      choices: [{ message: { content: '![image](https://a.com/1.png)\n![image](https://a.com/2.png)\n说明' } }],
    });
    assert.deepEqual(images, ['https://a.com/1.png', 'https://a.com/2.png']);
  });

  it('returns an empty note when the reply is only an image', () => {
    const { images, note } = parseResponse({
      choices: [{ message: { content: '![image](https://a.com/1.png)' } }],
    });
    assert.deepEqual(images, ['https://a.com/1.png']);
    assert.equal(note, '');
  });

  it('returns no images when the reply has no markdown link', () => {
    const { images, note } = parseResponse({
      choices: [{ message: { content: '抱歉，我无法生成这张图片。' } }],
    });
    assert.deepEqual(images, []);
    assert.equal(note, '抱歉，我无法生成这张图片。');
  });

  it('returns empty results for a malformed response', () => {
    assert.deepEqual(parseResponse({}), { images: [], note: '' });
    assert.deepEqual(parseResponse(null), { images: [], note: '' });
  });

  it('is not stateful across repeated calls', () => {
    assert.deepEqual(parseResponse(REAL_T2I_RESPONSE).images, parseResponse(REAL_T2I_RESPONSE).images);
  });
});

describe('resolveBaseUrl', () => {
  it('falls back to the default base when empty', () => {
    assert.equal(resolveBaseUrl(''), T8STAR_DEFAULT_BASE);
    assert.equal(resolveBaseUrl(null), T8STAR_DEFAULT_BASE);
    assert.equal(resolveBaseUrl(undefined), T8STAR_DEFAULT_BASE);
  });

  it('strips trailing slashes', () => {
    assert.equal(resolveBaseUrl('https://ai.t8star.org///'), 'https://ai.t8star.org');
  });

  it('falls back to the default for a non-http value', () => {
    assert.equal(resolveBaseUrl('not-a-url'), T8STAR_DEFAULT_BASE);
  });

  it('honours a custom http base', () => {
    assert.equal(resolveBaseUrl('http://localhost:8080/'), 'http://localhost:8080');
  });
});
