/**
 * OmniGen AI API Integration Tests
 *
 * Usage:
 *   npm test                        # runs with .env loaded (local dev)
 *   TOKEN=sk-xxx npm test           # runs with env var (CI / production)
 *
 * Environment:
 *   - Local: reads token from .env file via dotenv
 *   - Production / CI: expects TOKEN env var already set (no .env loaded)
 */

// Load .env only when file exists (local dev); skip silently in production
const dotenvPath = require('path').resolve(__dirname, '..', '.env');
try {
  require('dotenv').config({ path: dotenvPath });
} catch { /* ignore if dotenv not installed in prod */ }

const { describe, it, before, after } = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');

const app = require('../server');

const API_KEY = process.env.token || process.env.TOKEN;
const REGION = process.env.REGION || 'cn-beijing';
const BASE_URL = () => `http://localhost:${server.address().port}`;

let server;

/** Helper: make an HTTP request and return parsed JSON + status */
function request(method, path, body) {
  return new Promise((resolve, reject) => {
    const url = new URL(path, BASE_URL());
    const payload = body ? JSON.stringify(body) : null;
    const req = http.request(
      {
        hostname: url.hostname,
        port: url.port,
        path: url.pathname + url.search,
        method,
        headers: {
          'Content-Type': 'application/json',
          ...(payload ? { 'Content-Length': Buffer.byteLength(payload) } : {}),
        },
      },
      (res) => {
        const chunks = [];
        res.on('data', (c) => chunks.push(c));
        res.on('end', () => {
          const text = Buffer.concat(chunks).toString('utf8');
          let data;
          try { data = JSON.parse(text); } catch { data = text; }
          resolve({ status: res.statusCode, data });
        });
      },
    );
    req.on('error', reject);
    if (payload) req.write(payload);
    req.end();
  });
}

// ─── Setup / Teardown ───────────────────────────────────────────────
before(async () => {
  server = app.listen(0); // random available port
  await new Promise((r) => server.on('listening', r));
  console.log(`  Test server on port ${server.address().port}`);
});

after(() => {
  if (server) server.close();
});

// ─── GET /api/config ────────────────────────────────────────────────
describe('GET /api/config', () => {
  it('should return model configuration', async () => {
    const { status, data } = await request('GET', '/api/config');
    assert.equal(status, 200);
    assert.ok(data.models, 'should have models key');
    assert.ok(Array.isArray(data.models.IMAGE), 'IMAGE should be an array');
    assert.equal(data.models.TEXT_OPTIMIZE, 'qwen3.7-plus');
    assert.ok(data.models.VISION_OPTIMIZE, 'should have VISION_OPTIMIZE');
  });

  it('IMAGE array should contain known models', async () => {
    const { data } = await request('GET', '/api/config');
    const img = data.models.IMAGE;
    assert.ok(img.includes('qwen-image'), 'should include qwen-image');
    assert.ok(img.includes('wan2.7-image'), 'should include wan2.7-image');
  });
});

// ─── POST /api/generate-image ───────────────────────────────────────
describe('POST /api/generate-image — input validation', () => {
  it('400 when apiKey is missing', async () => {
    const { status, data } = await request('POST', '/api/generate-image', { model: 'qwen-image', prompt: 'cat' });
    assert.equal(status, 400);
    assert.match(data.error, /API Key/i);
  });

  it('400 when model is missing', async () => {
    const { status, data } = await request('POST', '/api/generate-image', { apiKey: 'test', prompt: 'cat' });
    assert.equal(status, 400);
    assert.match(data.error, /model/i);
  });

  it('400 for unsupported model', async () => {
    const { status, data } = await request('POST', '/api/generate-image', { apiKey: 'test', model: 'invalid-xyz', prompt: 'cat' });
    assert.equal(status, 400);
    assert.match(data.error, /不支持/);
  });
});

describe('POST /api/generate-image — real generation', { skip: !API_KEY && 'TOKEN not set' }, () => {
  it('qwen-image text-to-image should return image URLs', { timeout: 200_000 }, async () => {
    const { status, data } = await request('POST', '/api/generate-image', {
      apiKey: API_KEY,
      region: REGION,
      model: 'qwen-image',
      prompt: 'a cute orange cat sitting on a chair',
      params: { size: '1024*1024', n: 1, watermark: false, prompt_extend: true },
    });
    assert.equal(status, 200, `expected 200 but got ${status}: ${JSON.stringify(data)}`);
    assert.ok(Array.isArray(data.images), 'should have images array');
    assert.ok(data.images.length >= 1, 'should return at least 1 image');
    assert.ok(data.images[0].startsWith('http'), 'image URL should start with http');
    assert.equal(data.model, 'qwen-image');
    assert.ok(data.usage, 'should have usage info');
  });

  it('wan2.7-image text-to-image should return image URLs', { timeout: 200_000 }, async () => {
    const { status, data } = await request('POST', '/api/generate-image', {
      apiKey: API_KEY,
      region: REGION,
      model: 'wan2.7-image',
      prompt: 'a beautiful sunset over the ocean, photorealistic',
      params: { size: '1K', n: 1, watermark: false },
    });
    assert.equal(status, 200, `expected 200 but got ${status}: ${JSON.stringify(data)}`);
    assert.ok(data.images.length >= 1);
    assert.equal(data.model, 'wan2.7-image');
  });

  it('qwen-image with negative_prompt should succeed', { timeout: 200_000 }, async () => {
    const { status, data } = await request('POST', '/api/generate-image', {
      apiKey: API_KEY,
      region: REGION,
      model: 'qwen-image',
      prompt: 'a red rose in a glass vase',
      params: { size: '1024*1024', n: 1, negative_prompt: 'blurry, low quality', prompt_extend: false },
    });
    assert.equal(status, 200, `expected 200 but got ${status}: ${JSON.stringify(data)}`);
    assert.ok(data.images.length >= 1);
  });

  it('invalid apiKey should return auth error from upstream', { timeout: 30_000 }, async () => {
    const { status, data } = await request('POST', '/api/generate-image', {
      apiKey: 'sk-invalid-key-12345',
      region: REGION,
      model: 'qwen-image',
      prompt: 'cat',
      params: { size: '1024*1024', n: 1 },
    });
    assert.ok(status >= 400, `expected error status, got ${status}`);
    assert.ok(data.error, 'should have error message');
  });
});

// ─── POST /api/optimize-prompt ──────────────────────────────────────
describe('POST /api/optimize-prompt — input validation', () => {
  it('400 when apiKey is missing', async () => {
    const { status, data } = await request('POST', '/api/optimize-prompt', { draft: 'cat' });
    assert.equal(status, 400);
    assert.match(data.error, /API Key/i);
  });
});

describe('POST /api/optimize-prompt — real call', { skip: !API_KEY && 'TOKEN not set' }, () => {
  it('text prompt optimization (t2v mode) should return optimized prompt', { timeout: 60_000 }, async () => {
    const { status, data } = await request('POST', '/api/optimize-prompt', {
      apiKey: API_KEY,
      region: REGION,
      draft: '一只猫在草地上跑',
      mode: 't2v',
    });
    assert.equal(status, 200, `expected 200 but got ${status}: ${JSON.stringify(data)}`);
    assert.ok(data.prompt, 'should return prompt text');
    assert.ok(data.prompt.length > 10, 'optimized prompt should be longer than original');
    assert.ok(data.model, 'should return model name');
  });
});

// ─── POST /api/create-task ──────────────────────────────────────────
describe('POST /api/create-task — input validation', () => {
  it('400 when apiKey is missing', async () => {
    const { status, data } = await request('POST', '/api/create-task', { payload: {} });
    assert.equal(status, 400);
    assert.match(data.error, /API Key/i);
  });

  it('400 when payload is missing', async () => {
    const { status, data } = await request('POST', '/api/create-task', { apiKey: 'test' });
    assert.equal(status, 400);
    assert.match(data.error, /payload/i);
  });
});

// ─── GET /api/task/:taskId ──────────────────────────────────────────
describe('GET /api/task/:taskId — input validation', () => {
  it('400 when apiKey query is missing', async () => {
    const { status, data } = await request('GET', '/api/task/fake-id-123');
    assert.equal(status, 400);
    assert.match(data.error, /API Key/i);
  });
});

// ─── GET /api/download ──────────────────────────────────────────────
describe('GET /api/download', () => {
  it('400 when url is missing', async () => {
    const { status } = await request('GET', '/api/download');
    assert.equal(status, 400);
  });

  it('400 for invalid url', async () => {
    const { status } = await request('GET', '/api/download?url=not-a-url');
    assert.equal(status, 400);
  });
});

// ─── Static files ───────────────────────────────────────────────────
describe('Static files', () => {
  it('GET / should return HTML', async () => {
    const { status, data } = await request('GET', '/');
    assert.equal(status, 200);
    assert.match(String(data), /<!doctype|<html/i);
  });
});

// ─── POST /api/upload-image ─────────────────────────────────────────
// Utils for generating test images
async function makeTestJpeg(width = 100, height = 100, quality = 95) {
  const sharp = require('sharp');
  return await sharp({
    create: { width, height, channels: 3, background: { r: 128, g: 128, b: 128 } },
  }).jpeg({ quality }).toBuffer();
}

async function makeTestPng(width = 100, height = 100) {
  const sharp = require('sharp');
  return await sharp({
    create: { width, height, channels: 3, background: { r: 128, g: 128, b: 128 } },
  }).png().toBuffer();
}

async function makeTestWebp(width = 100, height = 100) {
  const sharp = require('sharp');
  return await sharp({
    create: { width, height, channels: 3, background: { r: 128, g: 128, b: 128 } },
  }).webp().toBuffer();
}

/** Helper: upload a buffer as multipart/form-data */
function uploadFile(fileName, buffer, mime) {
  return new Promise((resolve, reject) => {
    const boundary = '----TestBoundary' + Date.now();
    const buf = Buffer.from(buffer);
    const body = Buffer.concat([
      Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="${fileName}"\r\nContent-Type: ${mime}\r\n\r\n`),
      buf,
      Buffer.from(`\r\n--${boundary}--\r\n`),
    ]);

    const url = new URL('/api/upload-image', BASE_URL());
    const req = http.request(
      {
        hostname: url.hostname,
        port: url.port,
        path: url.pathname,
        method: 'POST',
        headers: {
          'Content-Type': `multipart/form-data; boundary=${boundary}`,
          'Content-Length': body.length,
        },
      },
      (res) => {
        const chunks = [];
        res.on('data', (c) => chunks.push(c));
        res.on('end', () => {
          const text = Buffer.concat(chunks).toString('utf8');
          let data;
          try { data = JSON.parse(text); } catch { data = text; }
          resolve({ status: res.statusCode, data });
        });
      },
    );
    req.on('error', reject);
    req.write(body);
    req.end();
  });
}

describe('POST /api/upload-image', () => {
  it('400 when no file is provided', async () => {
    const { status } = await request('POST', '/api/upload-image', {});
    assert.equal(status, 400);
  });

  it('400 for invalid file type', async () => {
    const { status } = await uploadFile('test.txt', Buffer.from('hello world'), 'text/plain');
    assert.equal(status, 400);
  });

  it('should return data URL for small JPEG', async () => {
    const buf = await makeTestJpeg(100, 100);
    const { status, data } = await uploadFile('test.jpg', buf, 'image/jpeg');
    assert.equal(status, 200);
    assert.ok(data.url.startsWith('data:image/jpeg;base64,'));
  });

  it('should return data URL for small PNG (preserving alpha if present)', async () => {
    const buf = await makeTestPng(100, 100);
    const { status, data } = await uploadFile('test.png', buf, 'image/png');
    assert.equal(status, 200);
    assert.ok(data.url.startsWith('data:image/png;base64,'));
  });

  it('should compress large images and return data URL when ≤12MB', { timeout: 30_000 }, async () => {
    // Generate a large JPEG (~15MB raw, but after sharp compression should be small)
    const buf = await makeTestJpeg(5000, 5000, 100);
    const { status, data } = await uploadFile('large.jpg', buf, 'image/jpeg');
    assert.equal(status, 200);
    assert.ok(data.url.startsWith('data:image/jpeg;base64,') || data.url.startsWith('data:image/png;base64,'));
  });

  it('should return data URL for small WebP', async () => {
    const buf = await makeTestWebp(100, 100);
    const { status, data } = await uploadFile('test.webp', buf, 'image/webp');
    assert.equal(status, 200);
    assert.ok(data.url.startsWith('data:image/webp;base64,'));
  });

  it('should return data URL for small BMP', async () => {
    // Use raw bytes with BMP mime — server stores buffer as-is
    const buf = Buffer.alloc(1024, 0xAB);
    const { status, data } = await uploadFile('test.bmp', buf, 'image/bmp');
    assert.equal(status, 200);
    assert.ok(data.url.startsWith('data:image/bmp;base64,'));
  });

  it('size field matches actual buffer length', async () => {
    const buf = await makeTestJpeg(200, 200);
    const { status, data } = await uploadFile('sized.jpg', buf, 'image/jpeg');
    assert.equal(status, 200);
    assert.equal(data.size, buf.length);
  });

  it('exactly 12MB buffer returns base64 (not OSS)', { timeout: 30_000 }, async () => {
    // 12 * 1024 * 1024 = 12582912 bytes, server uses <= threshold
    const buf = Buffer.alloc(12 * 1024 * 1024, 0xFF);
    const { status, data } = await uploadFile('exact12mb.jpg', buf, 'image/jpeg');
    assert.equal(status, 200);
    assert.ok(data.url.startsWith('data:image/jpeg;base64,'), 'should return base64 for exactly 12MB');
  });

  it('over 50MB returns 413', { timeout: 30_000 }, async () => {
    const buf = Buffer.alloc(50 * 1024 * 1024 + 1, 0);
    const { status } = await uploadFile('over50mb.jpg', buf, 'image/jpeg');
    assert.equal(status, 413);
  });
});
