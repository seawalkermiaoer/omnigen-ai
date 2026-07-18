import type { CatalogModel } from '@/types/generation'

/**
 * 测试用目录夹具，字段值取自 server/internal/model/catalog/catalog.go 里
 * 对应模型的真实定义，保持两侧同步——如果目录约束变了，这里的测试也应该
 * 跟着改（发现方式是 ParamPanel.test.tsx 断言失败，而不是静默过时）。
 */

export const qwenImage: CatalogModel = {
  ID: 'qwen-image',
  Capabilities: ['t2i'],
  Protocol: 'dashscope',
  Sizes: ['1664*928', '1472*1140', '1328*1328', '1140*1472', '928*1664'],
  MaxN: 1,
  SequentialMaxN: 0,
  Supports: ['negative_prompt', 'seed', 'watermark'],
  Regions: null,
  MaxImages: 0,
  MinImageEdge: 0,
  RatioMin: 0,
  RatioMax: 0,
}

export const qwenImageEdit: CatalogModel = {
  ID: 'qwen-image-edit',
  Capabilities: ['edit'],
  Protocol: 'dashscope',
  Sizes: ['1664*928', '1472*1140', '1328*1328', '1140*1472', '928*1664'],
  MaxN: 1,
  SequentialMaxN: 0,
  Supports: ['negative_prompt', 'seed', 'watermark'],
  Regions: null,
  MaxImages: 3,
  MinImageEdge: 0,
  RatioMin: 0,
  RatioMax: 0,
}

export const wanImage: CatalogModel = {
  ID: 'wan2.7-image',
  Capabilities: ['t2i', 'edit'],
  Protocol: 'dashscope',
  Sizes: ['1K', '2K', '4K'],
  MaxN: 4,
  SequentialMaxN: 12,
  Supports: ['thinking_mode', 'enable_sequential', 'negative_prompt', 'seed', 'watermark'],
  Regions: ['cn-beijing', 'ap-southeast-1'],
  MaxImages: 9,
  MinImageEdge: 0,
  RatioMin: 0,
  RatioMax: 0,
}

export const gptImage2: CatalogModel = {
  ID: 'gpt-image-2',
  Capabilities: ['t2i', 'edit'],
  Protocol: 'openai',
  Sizes: null,
  MaxN: 1,
  SequentialMaxN: 0,
  Supports: null,
  Regions: null,
  MaxImages: 3,
  MinImageEdge: 0,
  RatioMin: 0,
  RatioMax: 0,
}

export const happyhorseI2V: CatalogModel = {
  ID: 'happyhorse-1.1-i2v',
  Capabilities: ['i2v'],
  Protocol: 'dashscope',
  Sizes: null,
  MaxN: 0,
  SequentialMaxN: 0,
  Supports: ['seed', 'watermark'],
  Regions: null,
  MaxImages: 1,
  MinImageEdge: 300,
  RatioMin: 0.4,
  RatioMax: 2.5,
}

export const wanI2V: CatalogModel = {
  ID: 'wan2.7-i2v-2026-04-25',
  Capabilities: ['i2v'],
  Protocol: 'dashscope',
  Sizes: null,
  MaxN: 0,
  SequentialMaxN: 0,
  Supports: ['negative_prompt', 'prompt_extend', 'seed', 'watermark'],
  Regions: ['cn-beijing', 'ap-southeast-1'],
  MaxImages: 2,
  MinImageEdge: 240,
  RatioMin: 0.125,
  RatioMax: 8,
}

export const allModels: CatalogModel[] = [
  qwenImage,
  qwenImageEdit,
  wanImage,
  gptImage2,
  happyhorseI2V,
  wanI2V,
]
