/**
 * 生成核心（子项目 2+3）的类型定义。
 *
 * CatalogModel 字段名与顺序严格对应 server/internal/model/catalog/catalog.go
 * 的 Model 结构体——该结构体没有 json tag，Go 默认按导出字段原名序列化，
 * 因此这里的字段名是大写开头，不是常见的 camelCase。不要"顺手"改成
 * camelCase：那会让运行时收到的 JSON 与类型静默对不上。
 *
 * 数组字段（Sizes/Supports/Regions）在目录里为空时，Go 的 nil slice 序列化
 * 成 JSON null 而不是 []，因此类型里显式允许 null，调用方一律用
 * `model.Sizes ?? []` 之类的写法兜底，不能假设一定是数组。
 */

// ── 目录（GET /api/catalog） ────────────────────────────────────────────

export type Capability =
  | 't2i'
  | 'edit'
  | 't2v'
  | 'i2v'
  | 'r2v'
  | 'optimize_text'
  | 'optimize_vision'

export type Protocol = 'dashscope' | 'openai'

/** 与 catalog.go 的 Param* 常量一一对应，供 ParamPanel 查 model.Supports。 */
export const ParamName = {
  ThinkingMode: 'thinking_mode',
  EnableSequential: 'enable_sequential',
  NegativePrompt: 'negative_prompt',
  PromptExtend: 'prompt_extend',
  Seed: 'seed',
  Watermark: 'watermark',
} as const

export type ParamName = (typeof ParamName)[keyof typeof ParamName]

export interface CatalogModel {
  ID: string
  Capabilities: Capability[] | null
  Protocol: Protocol
  Sizes: string[] | null
  MaxN: number
  SequentialMaxN: number
  Supports: string[] | null
  Regions: string[] | null
  MaxImages: number
  MinImageEdge: number
  RatioMin: number
  RatioMax: number
}

export interface CatalogResponse {
  models: CatalogModel[]
}

/** model.HasCapability 的前端等价物。 */
export function modelHasCapability(model: CatalogModel, capability: Capability): boolean {
  return (model.Capabilities ?? []).includes(capability)
}

/** model.SupportsParam 的前端等价物。 */
export function modelSupportsParam(model: CatalogModel, param: ParamName): boolean {
  return (model.Supports ?? []).includes(param)
}

// ── 生成任务（generation_tasks，即历史记录） ─────────────────────────────

export type TaskMode = 'imggen' | 'imgedit' | 't2v' | 'i2v' | 'r2v'

export type TaskStatus = 'PENDING' | 'RUNNING' | 'SUCCEEDED' | 'FAILED' | 'CANCELED'

/** 与 server/internal/model/generation/response.go 的 TaskResponse 对应。 */
export interface GenerationTask {
  id: number
  mode: TaskMode
  model: string
  status: TaskStatus
  upstreamTaskId?: string
  prompt: string
  params: Record<string, unknown>
  inputUrls: string[]
  resultUrls: string[]
  usage?: Record<string, unknown>
  note?: string
  errorCode?: string
  errorMessage?: string
  createdAt: string
  updatedAt: string
}

export interface TaskListResponse {
  total: number
  items: GenerationTask[]
}

export interface ListTasksQuery {
  page: number
  pageSize: number
}

// ── ParamPanel ───────────────────────────────────────────────────────────

/**
 * ParamPanel 的受控值——字段名与 GenerateImageRequest/GenerateVideoRequest
 * 的对应字段同名，页面（Task 16）直接 `{...request, ...paramValues}` 拼装
 * 请求体即可，不需要再做一次字段名转换。
 */
export interface ParamPanelValues {
  size?: string
  n?: number
  thinkingMode?: boolean
  enableSequential?: boolean
  negativePrompt?: string
  seed?: number
  watermark?: boolean
  promptExtend?: boolean
}

// ── 图片生成/编辑（POST /api/generate/image） ────────────────────────────

/**
 * 与 handler.generateImageRequestBody 的 JSON 字段一一对应。字段是否生效
 * 取决于所选模型的 Protocol/Supports/Sizes——这里不做任何模型判断，
 * ParamPanel 只负责按目录约束产出合法的取值子集。
 */
export interface GenerateImageRequest {
  model: string
  prompt: string
  images?: string[]
  size?: string
  negativePrompt?: string
  n?: number
  watermark?: boolean
  seed?: number
  thinkingMode?: boolean
  enableSequential?: boolean
  promptExtend?: boolean
}

// ── 视频生成（POST /api/generate/video） ─────────────────────────────────

export interface VideoMediaImage {
  url: string
  referenceVoice?: string
}

export interface VideoMediaVideo {
  url: string
  referenceVoice?: string
}

/** 与 handler.createVideoTaskRequestBody 的 JSON 字段一一对应。 */
export interface GenerateVideoRequest {
  model: string
  prompt?: string
  images?: VideoMediaImage[]
  videos?: VideoMediaVideo[]
  taskType?: string
  firstFrame?: string
  lastFrame?: string
  firstClip?: string
  drivingAudio?: string
  negativePrompt?: string
  promptExtend?: boolean
  seed?: number
  resolution?: string
  duration?: number
  ratio?: string
  watermark?: boolean
}

// ── 上传（POST /api/upload） ─────────────────────────────────────────────

export interface UploadResult {
  url: string
  key?: string
  size: number
}

// ── prompt 优化（POST /api/optimize-prompt） ─────────────────────────────

/** 与 generation.OptimizeMode 的 7 个取值对应（imgedit 是历史别名，服务端会解析）。 */
export type OptimizeMode =
  | 'r2v'
  | 't2v'
  | 'r2v_wan'
  | 'i2v_wan'
  | 'i2v'
  | 't2i'
  | 'imggen_edit'

export interface OptimizePromptRequest {
  draft: string
  images?: string[]
  mode: OptimizeMode
  videoCount?: number
}

export interface OptimizePromptResponse {
  prompt: string
  model: string
}
