#!/usr/bin/env node

try { require('dotenv').config(); } catch { /* dotenv optional */ }

const https = require('https');

const REGION_ENDPOINTS = {
  'cn-beijing': 'https://dashscope.aliyuncs.com',
  'ap-southeast-1': 'https://dashscope-intl.aliyuncs.com',
  'us-east-1': 'https://dashscope-us.aliyuncs.com',
};

function argValue(name) {
  const prefix = `--${name}=`;
  const item = process.argv.slice(2).find((arg) => arg.startsWith(prefix));
  return item ? item.slice(prefix.length) : '';
}

function getEndpoint(region, workspaceId) {
  if (region === 'eu-central-1') {
    if (!workspaceId) throw new Error('eu-central-1 requires WORKSPACE_ID or --workspace-id');
    return `https://${workspaceId}.eu-central-1.maas.aliyuncs.com`;
  }
  return REGION_ENDPOINTS[region] || REGION_ENDPOINTS['cn-beijing'];
}

function resolveEndpoint(endpoint, region, workspaceId) {
  if (endpoint && endpoint.startsWith('http')) return endpoint.replace(/\/+$/, '');
  return getEndpoint(region, workspaceId);
}

function postJSON(targetUrl, headers, bodyObj, timeoutMs = 60000) {
  return new Promise((resolve, reject) => {
    const url = new URL(targetUrl);
    const body = JSON.stringify(bodyObj);
    const req = https.request({
      hostname: url.hostname,
      port: 443,
      path: url.pathname + url.search,
      method: 'POST',
      timeout: timeoutMs,
      headers: {
        ...headers,
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(body),
      },
    }, (res) => {
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => {
        const text = Buffer.concat(chunks).toString('utf8');
        let data;
        try { data = JSON.parse(text); } catch { data = { raw: text }; }
        resolve({ status: res.statusCode, data });
      });
    });
    req.on('timeout', () => {
      req.destroy();
      reject(new Error(`request timeout: ${url.hostname}${url.pathname}`));
    });
    req.on('error', reject);
    req.write(body);
    req.end();
  });
}

async function main() {
  const apiKey = argValue('api-key')
    || process.env.token
    || process.env.TOKEN
    || process.env.DASHSCOPE_API_KEY
    || process.env.API_KEY;
  if (!apiKey) {
    throw new Error('Missing API key. Set TOKEN/token/DASHSCOPE_API_KEY/API_KEY or pass --api-key=sk-...');
  }

  const model = 'qwen3.7-plus';
  const region = argValue('region') || process.env.REGION || 'cn-beijing';
  const workspaceId = argValue('workspace-id') || process.env.WORKSPACE_ID || process.env.WORKSPACEID || '';
  const endpoint = argValue('endpoint') || process.env.ENDPOINT || process.env.DASHSCOPE_ENDPOINT || '';
  const prompt = argValue('prompt') || process.argv.slice(2).filter((arg) => !arg.startsWith('--')).join(' ')
    || '用一句中文描述一只猫在草地上奔跑的电影镜头。';

  const base = resolveEndpoint(endpoint, region, workspaceId);
  const url = `${base}/compatible-mode/v1/chat/completions`;
  const payload = {
    model,
    messages: [
      { role: 'system', content: '你是一个简洁的中文视频 prompt 优化助手。' },
      { role: 'user', content: prompt },
    ],
  };

  console.log(`[qwen3.7-plus smoke] endpoint=${url}`);
  console.log(`[qwen3.7-plus smoke] region=${region}${workspaceId ? ` workspaceId=${workspaceId}` : ''}`);
  console.log(`[qwen3.7-plus smoke] apiKey=${apiKey.slice(0, 6)}...${apiKey.slice(-4)}`);

  const { status, data } = await postJSON(url, {
    Authorization: `Bearer ${apiKey}`,
  }, payload);

  console.log(`[qwen3.7-plus smoke] status=${status}`);
  if (status >= 400 || data.error || data.code) {
    console.log(JSON.stringify(data, null, 2));
    process.exitCode = 1;
    return;
  }

  const content = data.choices?.[0]?.message?.content;
  console.log(`[qwen3.7-plus smoke] model=${data.model || model}`);
  if (data.usage) console.log(`[qwen3.7-plus smoke] usage=${JSON.stringify(data.usage)}`);
  console.log('\n--- content ---');
  console.log(typeof content === 'string' ? content : JSON.stringify(content, null, 2));
}

main().catch((error) => {
  console.error(`[qwen3.7-plus smoke] ${error.message}`);
  process.exit(1);
});
