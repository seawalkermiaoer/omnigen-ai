import { client, unwrap } from './client'
import type { ApiResponse } from '@/types/common'
import type {
  CatalogResponse,
  GenerateImageRequest,
  GenerateVideoRequest,
  GenerationTask,
  ListTasksQuery,
  OptimizePromptRequest,
  OptimizePromptResponse,
  TaskListResponse,
  UploadResult,
} from '@/types/generation'

export const catalogApi = {
  /** GET /api/catalog——前端全部下拉框/参数面板的唯一数据源。 */
  list: () => unwrap<CatalogResponse>(client.get<ApiResponse<CatalogResponse>>('/catalog')),
}

export const uploadApi = {
  /**
   * POST /api/upload，multipart/form-data，字段名固定为 "file"，与
   * server/internal/handler/upload.go 的 uploadFormField 对应。
   */
  upload: (file: File) => {
    const form = new FormData()
    form.append('file', file)
    return unwrap<UploadResult>(
      client.post<ApiResponse<UploadResult>>('/upload', form, {
        headers: { 'Content-Type': 'multipart/form-data' },
      }),
    )
  },
}

/** 拼出 GET /api/download/:taskId/:index 的相对路径，不经过 axios。 */
function downloadPath(taskId: number, index: number): string {
  return `/download/${taskId}/${index}`
}

export const generationApi = {
  generateImage: (req: GenerateImageRequest) =>
    unwrap<GenerationTask>(client.post<ApiResponse<GenerationTask>>('/generate/image', req)),

  generateVideo: (req: GenerateVideoRequest) =>
    unwrap<GenerationTask>(client.post<ApiResponse<GenerationTask>>('/generate/video', req)),

  getTask: (id: number) =>
    unwrap<GenerationTask>(client.get<ApiResponse<GenerationTask>>(`/tasks/${id}`)),

  listTasks: (query: ListTasksQuery) =>
    unwrap<TaskListResponse>(client.get<ApiResponse<TaskListResponse>>('/tasks', { params: query })),

  optimizePrompt: (req: OptimizePromptRequest) =>
    unwrap<OptimizePromptResponse>(
      client.post<ApiResponse<OptimizePromptResponse>>('/optimize-prompt', req),
    ),

  /**
   * 复制链接用：下载走的是"任务 id + 结果序号"这条路径，前端从不持有、
   * 也不需要拼真实上游 URL（见 generation-core-design.md 的下载决策）。
   * 这个链接需要 Authorization 头才能真正打开，复制的意义是"在本应用内
   * 可用的标识"，不是一个可脱离登录态分享的公开 URL。
   */
  downloadLinkPath: downloadPath,

  /**
   * 实际触发下载：GET /api/download/:taskId/:index 需要 Bearer token，
   * 不能用 `<a href>`/`window.location` 直接跳转（浏览器导航不会带上
   * axios 拦截器注入的 Authorization 头）。改为用 axios 取 blob，再用
   * 一次性 <a download> 触发保存。
   */
  downloadResult: async (taskId: number, index: number, filename: string): Promise<void> => {
    const res = await client.get<Blob>(downloadPath(taskId, index), { responseType: 'blob' })
    const blobUrl = URL.createObjectURL(res.data)
    const link = document.createElement('a')
    link.href = blobUrl
    link.download = filename
    document.body.appendChild(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(blobUrl)
  },
}
