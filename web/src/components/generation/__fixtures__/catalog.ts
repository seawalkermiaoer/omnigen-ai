import type { CatalogModel } from '@/types/generation'

/**
 * 测试用目录夹具，字段值取自 server/internal/model/catalog/catalog.go 里
 * 对应模型的真实定义，保持两侧同步——如果目录约束变了，这里的测试也应该
 * 跟着改（发现方式是 ParamPanel.test.tsx 断言失败，而不是静默过时）。
 */

/**
 * 视频专属字段的三组预设。
 *
 * catalog.Model 从 wan3.0 起多了 11 个视频专属字段（VideoProfile /
 * Resolutions / Durations / Ratios / MediaLimits …）。Go 结构体没有
 * omitempty，所以它们在每个模型的 JSON 里都存在，包括图片模型——夹具要
 * 忠实反映线上形状就得全部写出来。逐个字面量抄 11 遍会立刻腐坏，所以
 * 按"代际"抽成三组预设，各个夹具展开对应的一组即可。
 *
 * 取值逐字对应 server/internal/model/catalog/catalog.go，改了那边这里也要改。
 */

/** 非视频模型：11 个字段全是零值。 */
export const nonVideoFields = {
  VideoProfile: '' as const,
  Resolutions: null,
  DefaultResolution: '',
  DurationMin: 0,
  DurationMax: 0,
  DefaultDuration: 0,
  SmartDuration: false,
  Ratios: null,
  DefaultRatio: '',
  I2VAutoRatio: false,
  MediaLimits: { ReferenceImages: 0, ReferenceVideos: 0, ReferenceAudios: 0 },
}

const legacyResolutions = ['720P', '1080P']
const legacyRatios = ['16:9', '9:16', '3:4', '4:3', '4:5', '5:4', '1:1']

/** happyhorse / wan2.7 的 t2v 与 r2v：720P·1080P，3~15 秒，七种宽高比。 */
export const legacyVideoFields = {
  ...nonVideoFields,
  Resolutions: legacyResolutions,
  DefaultResolution: '720P',
  DurationMin: 3,
  DurationMax: 15,
  DefaultDuration: 5,
  Ratios: legacyRatios,
  DefaultRatio: '16:9',
}

/** happyhorse / wan2.7 的 i2v：没有 ratio（宽高比由首帧决定）。 */
export const legacyI2VFields = {
  ...legacyVideoFields,
  Ratios: null,
  DefaultRatio: '',
  I2VAutoRatio: true,
}

/** wan3.0：多 480P、2~30 秒、智能时长，ratio 集合不同且 i2v 下同样有效。 */
export const wan30VideoFields = {
  ...nonVideoFields,
  VideoProfile: 'wan3.0' as const,
  Resolutions: ['480P', '720P', '1080P'],
  DefaultResolution: '720P',
  DurationMin: 2,
  DurationMax: 30,
  DefaultDuration: 5,
  SmartDuration: true,
  Ratios: ['adaptive', '16:9', '4:3', '1:1', '3:4', '9:16'],
  DefaultRatio: 'adaptive',
  MediaLimits: { ReferenceImages: 10, ReferenceVideos: 5, ReferenceAudios: 5 },
}

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
  ...nonVideoFields,
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
  ...nonVideoFields,
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
  ...nonVideoFields,
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
  ...nonVideoFields,
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
  ...legacyI2VFields,
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
  ...legacyI2VFields,
  VideoProfile: 'wan2.7',
}

export const allModels: CatalogModel[] = [
  qwenImage,
  qwenImageEdit,
  wanImage,
  gptImage2,
  happyhorseI2V,
  wanI2V,
]
