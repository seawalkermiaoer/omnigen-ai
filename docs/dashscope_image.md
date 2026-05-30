# 阿里云百炼（DashScope）图像生成 API 技术文档

> 本文档整理了阿里云百炼平台所有图像生成相关API接口，包括文生图、图生图、图像编辑等能力。
> 
> **协议说明**：支持 OpenAI 协议的接口使用 OpenAI 标准格式，不支持的接口使用 DashScope 专有协议格式并单独标注。

---

## 目录

1. [概述](#1-概述)
2. [认证方式](#2-认证方式)
3. [OpenAI 协议兼容接口](#3-openai-协议兼容接口)
4. [DashScope 专有协议接口](#4-dashscope-专有协议接口)
5. [异步任务接口](#5-异步任务接口)
6. [错误码参考](#6-错误码参考)
7. [SDK 使用说明](#7-sdk-使用说明)

---

## 1. 概述

### 1.1 支持的能力

| 能力 | 说明 |
|------|------|
| 文生图（Text-to-Image） | 根据文本描述生成图像 |
| 图生图（Image-to-Image） | 根据输入图像和文本指令编辑图像 |
| 多图融合 | 参考多张输入图像生成新图像 |
| 组图生成 | 一次性生成多张相关联的图像 |
| 图像编辑 | 局部编辑、风格迁移等 |

### 1.2 地域与接入地址

| 地域 | API Key | Base URL (OpenAI 兼容) | 说明 |
|------|---------|--------------------------|------|
| 华北2（北京） | 独立 | `https://dashscope.aliyuncs.com/compatible-mode/v1` | 中国大陆用户 |
| 美国（弗吉尼亚） | 独立 | `https://dashscope-us.aliyuncs.com/compatible-mode/v1` | 海外用户 |
| 新加坡 | 独立 | `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` | 国际版 |
| 德国（法兰克福） | 独立 | `https://{WorkspaceId}.eu-central-1.maas.aliyuncs.com/compatible-mode/v1` | 欧洲用户 |

> ⚠️ **注意**：不同地域的 API Key 和 Base URL 不能混用，否则会导致鉴权失败。

### 1.3 通用限制

| 限制项 | 说明 |
|--------|------|
| 图像 URL 有效期 | 生成的图像 URL 有效期为 24 小时，请及时保存 |
| 输入图像格式 | JPEG、JPG、PNG、BMP、TIFF、WEBP |
| 输入图像大小 | ≤ 10MB（部分接口支持 ≤ 20MB） |
| 输入图像分辨率 | 384~5000 像素（各模型略有不同） |
| 计费规则 | 按成功生成的图像张数计费，调用失败不收费 |

---

## 2. 认证方式

所有 API 请求需要在 HTTP Header 中携带认证信息：

```http
Authorization: Bearer YOUR_DASHSCOPE_API_KEY
Content-Type: application/json
```

- API Key 获取：登录阿里云百炼控制台，在「API Key 管理」中创建
- 不同地域使用不同的 API Key，不可混用

---

## 3. OpenAI 协议兼容接口

> ✅ **本节所有接口均支持 OpenAI 标准协议**，可直接使用 OpenAI SDK 调用，只需修改 `base_url` 和 `api_key`。

### 3.1 接口地址

```
POST https://dashscope.aliyuncs.com/compatible-mode/v1/images/generations
```

### 3.2 通用请求格式（OpenAI 标准）

```json
{
  "model": "模型名称",
  "prompt": "文本提示词",
  "n": 1,
  "size": "1024*1024",
  "negative_prompt": "反向提示词（可选）",
  "seed": 12345,
  "extra_body": {
    "provider": {
      "enable_image_base64": false,
      "enable_image_origin_data": true
    }
  }
}
```

### 3.3 通用响应格式（OpenAI 标准）

```json
{
  "created": 1736123456,
  "data": [
    {
      "url": "https://dashscope-result-xxx.oss-cn-xxx.aliyuncs.com/...",
      "b64_json": "iVBORw0KGgoAAAANSUhEUgAA..."
    }
  ],
  "usage": {
    "total_tokens": 0,
    "input_tokens": 0,
    "output_tokens": 0,
    "image_count": 1
  },
  "provider": "阿里云百炼",
  "model": "模型名称"
}
```

---

### 3.4 模型列表（支持 OpenAI 协议）

#### 3.4.1 Qwen-Image-Plus（文生图）

| 参数 | 类型 | 必填 | 说明 | 默认值 |
|------|------|------|------|--------|
| `model` | string | 是 | 模型名称 | `Qwen-Image-Plus` |
| `prompt` | string | 是 | 文本提示词，≤800 字符 | - |
| `negative_prompt` | string | 否 | 反向提示词，≤500 字符 | - |
| `size` | string | 否 | 输出分辨率 `宽*高` | `1328*1328` |
| `n` | integer | 否 | 生成数量 | `1`（固定为1） |
| `prompt_extend` | bool | 否 | 是否开启 prompt 智能改写 | `true` |
| `watermark` | bool | 否 | 是否添加水印 | `false` |
| `seed` | integer | 否 | 随机种子 `[0, 2147483647]` | - |

**可选尺寸**：

| size 值 | 宽高比 |
|---------|--------|
| `1664*928` | 16:9 |
| `1472*1140` | 4:3 |
| `1328*1328` | 1:1 |
| `1140*1472` | 3:4 |
| `928*1664` | 9:16 |

**请求示例**：

```bash
curl --location 'https://dashscope.aliyuncs.com/compatible-mode/v1/images/generations' \
--header 'Content-Type: application/json' \
--header "Authorization: Bearer $DASHSCOPE_API_KEY" \
--data '{
    "model": "Qwen-Image-Plus",
    "prompt": "一只坐着的橘黄色的猫，表情愉悦，活泼可爱，逼真准确",
    "negative_prompt": "低分辨率、错误、最差质量",
    "size": "1328*1328",
    "prompt_extend": true,
    "watermark": false,
    "seed": 12345
}'
```

---

#### 3.4.2 Qwen-Image（文生图）

参数与 `Qwen-Image-Plus` 完全一致，仅 `model` 参数值为 `Qwen-Image`。

---

#### 3.4.3 Qwen-Image-Edit-Plus（图生图/多图融合）✅ 支持 OpenAI 协议

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | string | 是 | `Qwen-Image-Edit-Plus` |
| `prompt` | string | 是 | 图像编辑指令，≤800 字符；多图时用「图1」「图2」指代 |
| `image` | string/array | 是 | 输入图像，支持 1-3 张，URL 或 Base64 |
| `image2` | string | 否 | 第二张参考图 |
| `image3` | string | 否 | 第三张参考图 |
| `size` | string | 否 | 输出分辨率，默认与输入图同比例 |
| `n` | integer | 否 | 生成数量，可选 1-6 | `1` |
| `prompt_extend` | bool | 否 | 是否开启 prompt 智能改写 | `true` |
| `watermark` | bool | 否 | 是否添加水印 | `false` |
| `seed` | integer | 否 | 随机种子 | - |

**请求示例（单图编辑）**：

```bash
curl --location 'https://dashscope.aliyuncs.com/compatible-mode/v1/images/generations' \
--header 'Content-Type: application/json' \
--header "Authorization: Bearer $DASHSCOPE_API_KEY" \
--data '{
    "model": "Qwen-Image-Edit-Plus",
    "prompt": "生成一张符合深度图的图像，遵循以下描述：一辆红色的破旧的自行车停在一条泥泞的小路上",
    "image": "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250925/fpakfo/image36.webp",
    "negative_prompt": "低分辨率、错误、最差质量",
    "size": "1024*1024",
    "n": 1,
    "prompt_extend": true,
    "watermark": false,
    "seed": 12345
}'
```

**请求示例（多图融合）**：

```bash
curl --location 'https://dashscope.aliyuncs.com/compatible-mode/v1/images/generations' \
--header 'Content-Type: application/json' \
--header "Authorization: Bearer $DASHSCOPE_API_KEY" \
--data '{
    "model": "Qwen-Image-Edit-Plus",
    "prompt": "图1中的女生穿着图2中的黑色裙子按图3的姿势坐下，保持其服装、发型和表情不变",
    "image": "data:image/jpeg;base64,GDU7MtCZz...",
    "image2": "data:image/jpeg;base64,ABC123...",
    "image3": "data:image/jpeg;base64,XYZ789...",
    "negative_prompt": "低分辨率、错误、最差质量",
    "n": 2,
    "prompt_extend": true,
    "watermark": false,
    "seed": 12345
}'
```

---

#### 3.4.4 Qwen-Image-Edit（图生图）✅ 支持 OpenAI 协议

参数与 `Qwen-Image-Edit-Plus` 类似，但 `n` 固定为 1。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | string | 是 | `Qwen-Image-Edit` |
| `prompt` | string | 是 | 图像编辑指令 |
| `image` | string/array | 是 | 输入图像，支持 1-3 张 |
| `size` | string | 否 | 输出分辨率，范围 [512, 2048] 像素 | 与输入图同比例 |
| `n` | integer | 否 | 生成数量 | **固定为 1** |
| `watermark` | bool | 否 | 是否添加水印 | `false` |
| `seed` | integer | 否 | 随机种子 | - |

---

#### 3.4.5 Wan2.5-T2I-Preview（文生图）✅ 支持 OpenAI 协议

| 参数 | 类型 | 必填 | 说明 | 默认值 |
|------|------|------|------|--------|
| `model` | string | 是 | `Wan2.5-T2I-Preview` | - |
| `prompt` | string | 是 | 正向提示词，≤2000 字符 | - |
| `negative_prompt` | string | 否 | 反向提示词，≤500 字符 | - |
| `size` | string | 否 | 输出分辨率 `宽*高` | `1280*1280` |
| `n` | integer | 否 | 生成数量，1~4 | `1` |
| `prompt_extend` | bool | 否 | 是否开启 prompt 智能改写 | `false` |
| `watermark` | bool | 否 | 是否添加水印 | `false` |
| `seed` | integer | 否 | 随机种子 | - |

**尺寸限制**：总像素范围 [768*768, 1440*1440]，宽高比范围 [1:4, 4:1]

---

#### 3.4.6 Wan2.5-I2I-Preview（图生图/多参考图生图）✅ 支持 OpenAI 协议

| 参数 | 类型 | 必填 | 说明 | 默认值 |
|------|------|------|------|--------|
| `model` | string | 是 | `Wan2.5-I2I-Preview` | - |
| `prompt` | string | 是 | 正向提示词，≤2000 字符 | - |
| `image` | string/array | 是 | 输入图像，支持 1-3 张 | - |
| `image2` | string | 否 | 第二张参考图 | - |
| `image3` | string | 否 | 第三张参考图 | - |
| `negative_prompt` | string | 否 | 反向提示词 | - |
| `size` | string | 否 | 输出分辨率 | `1280*1280` |
| `n` | integer | 否 | 生成数量，1~4 | `1` |
| `watermark` | bool | 否 | 是否添加水印 | `false` |
| `seed` | integer | 否 | 随机种子 | - |

---

### 3.5 使用 OpenAI SDK 调用示例

#### Python

```python
from openai import OpenAI

client = OpenAI(
    api_key="YOUR_DASHSCOPE_API_KEY",
    base_url="https://dashscope.aliyuncs.com/compatible-mode/v1"
)

response = client.images.generate(
    model="Qwen-Image-Plus",
    prompt="一只坐着的橘黄色的猫，表情愉悦，活泼可爱",
    size="1328*1328",
    n=1,
    extra_body={
        "prompt_extend": True,
        "watermark": False
    }
)

print(response.data[0].url)
```

#### Node.js

```javascript
import OpenAI from 'openai';

const client = new OpenAI({
  apiKey: 'YOUR_DASHSCOPE_API_KEY',
  baseURL: 'https://dashscope.aliyuncs.com/compatible-mode/v1'
});

const response = await client.images.generate({
  model: 'Qwen-Image-Plus',
  prompt: '一只坐着的橘黄色的猫，表情愉悦，活泼可爱',
  size: '1328*1328',
  n: 1,
  extra_body: {
    prompt_extend: true,
    watermark: false
  }
});

console.log(response.data[0].url);
```

---

## 4. DashScope 专有协议接口

> ⚠️ **本节接口不支持 OpenAI 标准协议**，需要使用 DashScope 专有请求格式。

---

### 4.1 wan2.7-image-pro / wan2.7-image（万相 2.7 图像生成）❌ 不支持 OpenAI 协议

#### 4.1.1 接口地址

| 地域 | 同步接口地址 | 异步接口地址 |
|------|-------------|-------------|
| 北京 | `POST https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation` | `POST https://dashscope.aliyuncs.com/api/v1/services/aigc/image-generation/generation` |
| 新加坡 | `POST https://dashscope-intl.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation` | `POST https://dashscope-intl.aliyuncs.com/api/v1/services/aigc/image-generation/generation` |

#### 4.1.2 请求头

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `Content-Type` | string | 是 | `application/json` |
| `Authorization` | string | 是 | `Bearer $DASHSCOPE_API_KEY` |
| `X-DashScope-Async` | string | 异步必填 | 设置为 `enable` |

#### 4.1.3 请求体（同步/异步通用）

```json
{
  "model": "wan2.7-image-pro",
  "input": {
    "messages": [
      {
        "role": "user",
        "content": [
          {"text": "一间有着精致窗户的花店，漂亮的木质门，摆放着花朵"}
        ]
      }
    ]
  },
  "parameters": {
    "size": "2K",
    "n": 1,
    "watermark": false,
    "thinking_mode": true
  }
}
```

#### 4.1.4 请求参数说明

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | string | 是 | `wan2.7-image-pro` 或 `wan2.7-image` |
| `input.messages` | array | 是 | 请求内容，仅支持单轮 |
| `input.messages[].role` | string | 是 | 固定值：`user` |
| `input.messages[].content` | array | 是 | 内容数组，支持 `text` 和 `image` |
| `input.messages[].content[].text` | string | 否 | 提示词，≤5000 字符 |
| `input.messages[].content[].image` | string | 否 | 输入图像，支持 1-9 张 |
| `parameters.size` | string | 否 | `1K`/`2K`/`4K` 或自定义宽高 | `2K` |
| `parameters.n` | integer | 否 | 生成数量，单图 1-4，组图 1-12 | 单图 1，组图 12 |
| `parameters.enable_sequential` | bool | 否 | 是否开启组图模式 | `false` |
| `parameters.thinking_mode` | bool | 否 | 是否开启思考模式（仅单图无图输入时生效） | `true` |
| `parameters.watermark` | bool | 否 | 是否添加水印 | `false` |
| `parameters.seed` | integer | 否 | 随机种子 `[0, 2147483647]` | - |

#### 4.1.5 输入图像格式

| 格式 | 说明 |
|------|------|
| 公网 URL | `http://` 或 `https://` 开头的公网可访问地址 |
| Base64 | `data:{MIME_type};base64,{base64_data}` |
| 分辨率 | 宽高范围 [240, 8000] 像素，宽高比 [1:8, 8:1] |
| 文件大小 | ≤ 20MB |
| 支持格式 | JPEG、JPG、PNG（不支持透明通道）、BMP、WEBP |

#### 4.1.6 响应格式

**成功响应**：

```json
{
  "output": {
    "choices": [
      {
        "finish_reason": "stop",
        "message": {
          "content": [
            {
              "image": "https://xxx.png?Expires=xxx",
              "type": "image"
            }
          ],
          "role": "assistant"
        }
      }
    ],
    "finished": true
  },
  "usage": {
    "image_count": 1,
    "size": "1488*704"
  },
  "request_id": "71dfc3c6-f796-9972-97e4-bc4efc4faxxx"
}
```

#### 4.1.7 调用示例

**文生图**：

```bash
curl --location 'https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation' \
--header 'Content-Type: application/json' \
--header "Authorization: Bearer $DASHSCOPE_API_KEY" \
--data '{
    "model": "wan2.7-image-pro",
    "input": {
        "messages": [
            {
                "role": "user",
                "content": [
                    {"text": "一间有着精致窗户的花店，漂亮的木质门，摆放着花朵"}
                ]
            }
        ]
    },
    "parameters": {
        "size": "2K",
        "n": 1,
        "watermark": false,
        "thinking_mode": true
    }
}'
```

**图像编辑（多图输入）**：

```bash
curl --location 'https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation' \
--header 'Content-Type: application/json' \
--header "Authorization: Bearer $DASHSCOPE_API_KEY" \
--data '{
    "model": "wan2.7-image-pro",
    "input": {
        "messages": [
            {
                "role": "user",
                "content": [
                    {"image": "https://xxx/car.webp"},
                    {"image": "https://xxx/paint.webp"},
                    {"text": "把图2的涂鸦喷绘在图1的汽车上"}
                ]
            }
        ]
    },
    "parameters": {
        "size": "2K",
        "n": 1,
        "watermark": false
    }
}'
```

**组图生成**：

```bash
curl --location 'https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation' \
--header 'Content-Type: application/json' \
--header "Authorization: Bearer $DASHSCOPE_API_KEY" \
--data '{
    "model": "wan2.7-image-pro",
    "input": {
        "messages": [
            {
                "role": "user",
                "content": [
                    {"text": "电影感组图，记录同一只流浪橘猫，特征必须前后一致。第一张：春天，橘猫穿梭在盛开的樱花树下；第二张：夏天，橘猫在老街的树荫下乘凉避暑；第三张：秋天，橘猫踩在满地的金色落叶上；第四张：冬天，橘猫在雪地上走留下足迹。"}
                ]
            }
        ]
    },
    "parameters": {
        "enable_sequential": true,
        "n": 4,
        "size": "2K"
    }
}'
```

---

### 4.2 wanx-v1（万相 V1 文生图）❌ 不支持 OpenAI 协议

#### 4.2.1 接口地址（异步）

```
POST https://dashscope.aliyuncs.com/api/v1/services/aigc/text2image/image-synthesis
```

> ⚠️ 该模型仅支持异步调用，必须设置 `X-DashScope-Async: enable` 请求头。

#### 4.2.2 请求参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | string | 是 | `wanx-v1` |
| `input.prompt` | string | 是 | 正向提示词，≤800 字符 |
| `input.negative_prompt` | string | 否 | 反向提示词，≤500 字符 |
| `input.ref_img` | string | 否 | 参考图 URL |
| `parameters.style` | string | 否 | 图像风格，见下方风格列表 |
| `parameters.size` | string | 否 | 输出分辨率 | `1024*1024` |
| `parameters.n` | integer | 否 | 生成数量，1~4 | `4` |
| `parameters.seed` | integer | 否 | 随机种子 |
| `parameters.ref_strength` | float | 否 | 参考图相似度 `[0.0, 1.0]` |
| `parameters.ref_mode` | string | 否 | `repaint`（内容）/ `refonly`（风格） |

**支持的图像风格**：

| 值 | 说明 |
|-----|------|
| `<auto>` | 随机 |
| `<photography>` | 摄影 |
| `<portrait>` | 人像写真 |
| `<3d_cartoon>` | 3D 卡通 |
| `<anime>` | 动画 |
| `<oil_painting>` | 油画 |
| `<watercolor>` | 水彩 |
| `<sketch>` | 素描 |
| `<chinese_painting>` | 中国画 |
| `<flat_illustration>` | 扁平插画 |

#### 4.2.3 请求示例

```bash
curl --location 'https://dashscope.aliyuncs.com/api/v1/services/aigc/text2image/image-synthesis' \
--header 'Content-Type: application/json' \
--header "Authorization: Bearer $DASHSCOPE_API_KEY" \
--header 'X-DashScope-Async: enable' \
--data '{
    "model": "wanx-v1",
    "input": {
        "prompt": "一间有着精致窗户的花店，漂亮的木质门，摆放着花朵",
        "negative_prompt": "低分辨率、错误、最差质量"
    },
    "parameters": {
        "style": "<anime>",
        "size": "1024*1024",
        "n": 4,
        "seed": 12345
    }
}'
```

#### 4.2.4 响应（创建任务）

```json
{
  "output": {
    "task_status": "PENDING",
    "task_id": "0385dc79-5ff8-4d82-bcb6-xxxxxx"
  },
  "request_id": "4909100c-7b5a-9f92-bfe5-xxxxxx"
}
```

---

## 5. 异步任务接口

### 5.1 查询任务结果

适用于所有异步调用的模型（`wan2.7` 系列、`wanx-v1` 等）。

**接口地址**：

```
GET https://dashscope.aliyuncs.com/api/v1/tasks/{task_id}
```

**请求头**：

```
Authorization: Bearer YOUR_DASHSCOPE_API_KEY
```

**响应示例（成功）**：

```json
{
  "request_id": "85eaba38-0185-99d7-8d16-4d9135238846",
  "output": {
    "task_id": "86ecf553-d340-4e21-af6e-a0c6a421c010",
    "task_status": "SUCCEEDED",
    "results": [
      {
        "url": "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/123/a1.png"
      }
    ],
    "task_metrics": {
      "TOTAL": 2,
      "SUCCEEDED": 2,
      "FAILED": 0
    }
  },
  "usage": {
    "image_count": 2
  }
}
```

**任务状态说明**：

| 状态 | 说明 |
|------|------|
| `PENDING` | 排队中 |
| `RUNNING` | 处理中 |
| `SUCCEEDED` | 成功 |
| `FAILED` | 失败 |
| `CANCELED` | 已取消 |

### 5.2 取消任务

```
POST https://dashscope.aliyuncs.com/api/v1/tasks/{task_id}/cancel
```

---

## 6. 错误码参考

| 错误码 | 说明 |
|--------|------|
| `InvalidApiKey` | API Key 无效或未提供 |
| `InvalidParameter` | 参数错误 |
| `InvalidParameter.SizeNotSupported` | 尺寸不支持 |
| `InternalError.Timeout` | 内部超时 |
| `RateLimitExceeded` | 触发限流 |
| `QuotaExceeded` | 配额用尽 |

**错误响应格式**：

```json
{
  "error": {
    "message": "Invalid parameter",
    "code": "invalid_param"
  }
}
```

或（DashScope 专有格式）：

```json
{
  "code": "InvalidParameter",
  "message": "num_images_per_prompt must be 1",
  "request_id": "a4d78a5f-655f-9639-8437-xxxxxx"
}
```

---

## 7. SDK 使用说明

### 7.1 Python SDK

**安装**：

```bash
pip install dashscope>=1.25.15
```

**使用示例（图像生成）**：

```python
import dashscope
from dashscope import ImageGeneration

dashscope.api_key = "YOUR_DASHSCOPE_API_KEY"

# 同步调用
result = ImageGeneration.call(
    model="Qwen-Image-Plus",
    prompt="一只坐着的橘黄色的猫",
    size="1328*1328"
)
print(result)

# 异步调用
task_id = ImageGeneration.async_call(
    model="wan2.7-image-pro",
    prompt="一间有着精致窗户的花店"
)
result = ImageGeneration.wait(task_id)
print(result)
```

### 7.2 Java SDK

**Maven 依赖**：

```xml
<dependency>
    <groupId>com.alibaba</groupId>
    <artifactId>dashscope-java</artifactId>
    <version>2.22.13</version>
</dependency>
```

### 7.3 图像输入方式（SDK）

SDK 支持三种图像输入方式：

1. **公网 URL**：直接传入 HTTP/HTTPS 地址
2. **本地文件**：格式为 `file:///绝对路径/文件名`
3. **Base64 编码**：通过内置函数转换

---

## 8. 附录

### 8.1 模型选择建议

| 场景 | 推荐模型 |
|------|----------|
| 高质量文生图 | `Qwen-Image-Plus` / `wan2.7-image-pro` |
| 快速文生图 | `Qwen-Image` / `wan2.7-image` |
| 图像编辑/图生图 | `Qwen-Image-Edit-Plus` |
| 多图融合 | `Qwen-Image-Edit-Plus` / `Wan2.5-I2I-Preview` |
| 组图生成 | `wan2.7-image-pro`（开启 `enable_sequential`） |
| 风格化文生图 | `wanx-v1`（指定 `style` 参数） |

### 8.2 协议兼容性总结

| 模型 | OpenAI 协议 | DashScope 专有协议 | 推荐格式 |
|------|:------------:|:------------------:|----------|
| Qwen-Image-Plus | ✅ | ✅ | OpenAI |
| Qwen-Image | ✅ | ✅ | OpenAI |
| Qwen-Image-Edit-Plus | ✅ | ✅ | OpenAI |
| Qwen-Image-Edit | ✅ | ✅ | OpenAI |
| Wan2.5-T2I-Preview | ✅ | ✅ | OpenAI |
| Wan2.5-I2I-Preview | ✅ | ✅ | OpenAI |
| wan2.7-image-pro | ❌ | ✅ | DashScope 专有 |
| wan2.7-image | ❌ | ✅ | DashScope 专有 |
| wanx-v1 | ❌ | ✅ | DashScope 专有 |

> **说明**：标记为 ❌ 的模型目前官方文档未提供 OpenAI 协议调用方式，仅支持 DashScope 专有协议格式。

### 8.3 相关文档

- 阿里云百炼官方文档：https://help.aliyun.com/zh/model-studio/
- DashScope API 参考：https://help.aliyun.com/zh/model-studio/model-api-reference/
- 模型价格文档：https://help.aliyun.com/zh/model-studio/model-pricing
- 错误码文档：https://help.aliyun.com/zh/model-studio/error-code

---

*文档生成时间：2026-05-30*
*数据来源：阿里云百炼官方文档及 AI Ping 文档*
