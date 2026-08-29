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
  | 'f2v'
  | 'l2v'
  | 'optimize_text'
  | 'optimize_vision'

/**
 * 视频类能力，顺序与 catalog.go 的 videoCapabilities 一致（也是左侧导航
 * 里五个视频页面的顺序）。用它判断一个模型的模式是否唯一：wan3.0 之前
 * 每个视频模型恰好只有一种，wan3.0 有五种，提交时必须带 mode。
 */
export const VIDEO_CAPABILITIES: Capability[] = ['t2v', 'i2v', 'r2v', 'f2v', 'l2v']

/**
 * catalog.go 的 VideoProfile：视频模型的"代际"，决定素材面板长什么样。
 * 空串是 happyhorse 系列——这是后端零值，不是"未知"。
 */
export type VideoProfile = '' | 'wan2.7' | 'wan3.0'

export type Protocol = 'dashscope' | 'openai'

/** 与 catalog.go 的 Param* 常量一一对应，供 ParamPanel 查 model.Supports。 */
export const ParamName = {
  ThinkingMode: 'thinking_mode',
  EnableSequential: 'enable_sequential',
  NegativePrompt: 'negative_prompt',
  PromptExtend: 'prompt_extend',
  Seed: 'seed',
  Watermark: 'watermark',
  Audio: 'audio',
} as const

export type ParamName = (typeof ParamName)[keyof typeof ParamName]

/** catalog.go 的 MediaLimits：按 media type 分别计数的上限（wan3.0）。 */
export interface MediaLimits {
  ReferenceImages: number
  ReferenceVideos: number
  ReferenceAudios: number
}

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

  // ── 视频模型专属 ────────────────────────────────────────────────
  // resolution/duration/ratio 的取值范围过去是本仓库里写死两遍的常量
  // （service 的 videoResolutions 等 + web 的 videoParams.ts），前提是
  // "所有视频模型取值都一样"。wan3.0 打破了这个前提，于是它们跟
  // Sizes/MaxN 一样成了目录字段，由后端单向驱动这里的控件。
  VideoProfile: VideoProfile
  Resolutions: string[] | null
  DefaultResolution: string
  DurationMin: number
  DurationMax: number
  DefaultDuration: number
  SmartDuration: boolean
  Ratios: string[] | null
  DefaultRatio: string
  I2VAutoRatio: boolean
  MediaLimits: MediaLimits
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

/** model.VideoCapabilities 的前端等价物（顺序稳定，见 VIDEO_CAPABILITIES）。 */
export function modelVideoCapabilities(model: CatalogModel): Capability[] {
  return VIDEO_CAPABILITIES.filter((c) => modelHasCapability(model, c))
}

/**
 * 提交时是否必须显式带 mode。后端对单能力模型会自行推断，所以只有
 * 多能力模型（wan3.0）才需要——但页面一律带上更简单也更明确，这个函数
 * 主要用于"这个模型在本页面之外还能做别的"这类提示。
 */
export function modelIsMultiModeVideo(model: CatalogModel): boolean {
  return modelVideoCapabilities(model).length > 1
}

// ── 生成任务（generation_tasks，即历史记录） ─────────────────────────────

export type TaskMode = 'imggen' | 'imgedit' | 't2v' | 'i2v' | 'r2v' | 'f2v' | 'l2v'

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

/** 与 server/internal/model/generation/response.go 的 TaskDeleteAllResponse 对应。 */
export interface TaskDeleteAllResponse {
  deleted: number
}

// ── 复用（历史记录页 → 生成页面预填充） ────────────────────────────────────

/**
 * "复用"跨路由传递的预填充数据，历史记录页通过
 * `navigate(path, { state: { reuse } })` 交给对应的生成页面。
 *
 * params 直接就是 task.params——后端落库时存的原始请求参数，图片模式的
 * 字段名（size/n/watermark/seed/thinkingMode/enableSequential/promptExtend/
 * negativePrompt）与 ParamPanelValues 同名，视频模式的字段名
 * （resolution/duration/ratio/watermark/seed/negativePrompt/promptExtend）
 * 与三个视频页面自己的 state 同名，见 server/internal/service/
 * generation_image.go 的 paramsToStorageMap 与 generation_video.go 的
 * videoParamsToStorageMap——调用页面按自己认识的字段名挑取，不需要转换。
 *
 * 不携带任何输入图片/视频 URL——旧系统的"复用"同样从不恢复输入素材（见
 * public/js/history.js 的 reuseHistory 与 reuseImggenNote/reuseOtherNote
 * 文案），hadInput 只用来告诉用户"这条记录曾经带过输入素材，需要重新上传"。
 */
export interface ReuseGenerationState {
  taskId: number
  mode: TaskMode
  model: string
  prompt: string
  params: Record<string, unknown>
  hadInput: boolean
}

/** 通过 `navigate(path, { state })` 传递的 location.state 形状。 */
export interface ReuseLocationState {
  reuse?: ReuseGenerationState
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
  audio?: boolean
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

/** 参考音频（wan3.0 独有），没有 referenceVoice 兄弟字段。 */
export interface VideoMediaAudio {
  url: string
}

/** 与 handler.createVideoTaskRequestBody 的 JSON 字段一一对应。 */
export interface GenerateVideoRequest {
  model: string
  prompt?: string
  /**
   * 后端对单能力模型可以自行推断模式，但五个视频页面一律显式带上：
   * wan3.0 一个 model id 同时具备 t2v/i2v/r2v/f2v/l2v，不带就会被拒。
   */
  mode?: TaskMode
  images?: VideoMediaImage[]
  videos?: VideoMediaVideo[]
  audios?: VideoMediaAudio[]
  taskType?: string
  firstFrame?: string
  lastFrame?: string
  firstClip?: string
  drivingAudio?: string
  /** 文件生视频（f2v）/ 网页生视频（l2v）各自唯一的那条素材。 */
  fileUrl?: string
  linkUrl?: string
  negativePrompt?: string
  promptExtend?: boolean
  seed?: number
  audio?: boolean
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
