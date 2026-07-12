/**
 * OmniGen AI Server Utility Tests
 *
 * Tests for resolveEndpoint, getEndpoint, REGION_ENDPOINTS, and MODELS config.
 */

const { describe, it } = require('node:test');
const assert = require('node:assert/strict');

const app = require('../server');
const resolveEndpoint = app._resolveEndpoint;
const getEndpoint = app._getEndpoint;
const REGION_ENDPOINTS = app._REGION_ENDPOINTS;
const MODELS = app._MODELS;

// ─── resolveEndpoint ──────────────────────────────────────────────
describe('resolveEndpoint', () => {
  it('uses custom endpoint as-is', () => {
    const result = resolveEndpoint('https://my.proxy.com', 'cn-beijing');
    assert.equal(result, 'https://my.proxy.com');
  });

  it('strips single trailing slash', () => {
    const result = resolveEndpoint('https://my.proxy.com/', 'cn-beijing');
    assert.equal(result, 'https://my.proxy.com');
  });

  it('strips multiple trailing slashes', () => {
    const result = resolveEndpoint('https://my.proxy.com///', 'cn-beijing');
    assert.equal(result, 'https://my.proxy.com');
  });

  it('falls back to region for non-http string', () => {
    const result = resolveEndpoint('not-a-url', 'cn-beijing');
    assert.equal(result, 'https://dashscope.aliyuncs.com');
  });

  it('falls back to region for null endpoint', () => {
    const result = resolveEndpoint(null, 'ap-southeast-1');
    assert.equal(result, 'https://dashscope-intl.aliyuncs.com');
  });

  it('falls back to region for empty string', () => {
    const result = resolveEndpoint('', 'us-east-1');
    assert.equal(result, 'https://dashscope-us.aliyuncs.com');
  });
});

// ─── getEndpoint ──────────────────────────────────────────────────
describe('getEndpoint', () => {
  it('cn-beijing returns dashscope base', () => {
    assert.equal(getEndpoint('cn-beijing'), 'https://dashscope.aliyuncs.com');
  });

  it('ap-southeast-1 returns intl endpoint', () => {
    assert.equal(getEndpoint('ap-southeast-1'), 'https://dashscope-intl.aliyuncs.com');
  });

  it('us-east-1 returns us endpoint', () => {
    assert.equal(getEndpoint('us-east-1'), 'https://dashscope-us.aliyuncs.com');
  });

  it('eu-central-1 with workspaceId returns workspace URL', () => {
    const result = getEndpoint('eu-central-1', 'ws-123');
    assert.equal(result, 'https://ws-123.eu-central-1.maas.aliyuncs.com');
  });

  it('eu-central-1 without workspaceId throws', () => {
    assert.throws(() => getEndpoint('eu-central-1'), /WorkspaceId/i);
  });

  it('unknown region falls back to cn-beijing', () => {
    assert.equal(getEndpoint('unknown-region'), 'https://dashscope.aliyuncs.com');
  });
});

// ─── REGION_ENDPOINTS ─────────────────────────────────────────────
describe('REGION_ENDPOINTS', () => {
  it('has exactly 3 known regions', () => {
    assert.equal(Object.keys(REGION_ENDPOINTS).length, 3);
  });

  it('all values are https dashscope URLs', () => {
    for (const url of Object.values(REGION_ENDPOINTS)) {
      assert.match(url, /^https:\/\/dashscope/);
    }
  });
});

// ─── MODELS configuration ─────────────────────────────────────────
describe('MODELS configuration', () => {
  it('IMAGE array contains all expected models', () => {
    const expected = [
      'qwen-image-plus', 'qwen-image',
      'qwen-image-edit-plus', 'qwen-image-edit',
      'wan2.7-image-pro', 'wan2.7-image',
    ];
    for (const m of expected) {
      assert.ok(MODELS.IMAGE.includes(m), `IMAGE should include ${m}`);
    }
  });

  it('TEXT_OPTIMIZE uses qwen3.7-plus', () => {
    assert.equal(MODELS.TEXT_OPTIMIZE, 'qwen3.7-plus');
  });

  it('VISION_OPTIMIZE is a non-empty string', () => {
    assert.ok(typeof MODELS.VISION_OPTIMIZE === 'string' && MODELS.VISION_OPTIMIZE.length > 0);
  });

  it('VISION_OPTIMIZE_LABEL differs from VISION_OPTIMIZE', () => {
    assert.equal(MODELS.VISION_OPTIMIZE_LABEL, 'qwen-vl-max');
    assert.equal(MODELS.VISION_OPTIMIZE, 'qwen-vl-max-latest');
    assert.notEqual(MODELS.VISION_OPTIMIZE_LABEL, MODELS.VISION_OPTIMIZE);
  });
});
