import { client, unwrap, LONG_RUNNING_TIMEOUT } from './client'
import { useAuthStore } from '@/stores/auth'
import type { ApiResponse } from '@/types/common'
import type {
  CatalogResponse,
  GenerateImageRequest,
  GenerateVideoRequest,
  GenerationTask,
  ListTasksQuery,
  OptimizePromptRequest,
  OptimizePromptResponse,
  TaskDeleteAllResponse,
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
        // 上传耗时随文件大小和上行带宽变化，很容易超过 15s 的全局默认值。
        timeout: LONG_RUNNING_TIMEOUT,
      }),
    )
  },
}

/** 拼出 GET /api/download/:taskId/:index 的相对路径，不经过 axios。 */
function downloadPath(taskId: number, index: number): string {
  return `/download/${taskId}/${index}`
}

/**
 * 配额相关动作成功后，顺带把 authStore 缓存的 user 刷新一遍——topbar
 * 剩余额度显示的是 user.quotaTotal/quotaUsed 这份登录时的快照，生成一次
 * 或删掉一条任务都会在服务端改变 quota_used（ConsumeQuota/
 * RefundQuotaForTask），但本地快照不会跟着自动变，不刷新就要等到下次
 * 整页刷新或重新登录才会更新，中途一直显示旧数字。
 *
 * 不在这里 await：这只是"顺带"刷新，不应该让调用方多等一次网络往返才
 * 能拿到生成/删除本身的结果；refreshUser 内部失败也是静默忽略的，不会
 * 抛出来打断调用方的 then/catch 链。也不放进 client.ts 的拦截器统一处理
 * ——那样等于给"所有请求"都建一层配额缓存失效机制，而只有生成与删除
 * 这两类动作真的会改配额，见 stores/auth.ts 里 refreshUser 的注释。
 */
function refreshQuota(): void {
  void useAuthStore.getState().refreshUser()
}

export const generationApi = {
  // 图片生成是同步接口：上游整个出图过程（实测 gpt-image-2 约 40s）都发生在
  // 这一次 HTTP 请求里，必须给足超时。视频生成不同——它提交后立刻返回
  // task id，真正的等待在后续轮询里，所以沿用默认超时。
  generateImage: (req: GenerateImageRequest) =>
    unwrap<GenerationTask>(
      client.post<ApiResponse<GenerationTask>>('/generate/image', req, {
        timeout: LONG_RUNNING_TIMEOUT,
      }),
    ).then(
      (result) => {
        refreshQuota()
        return result
      },
    ),

  generateVideo: (req: GenerateVideoRequest) =>
    unwrap<GenerationTask>(client.post<ApiResponse<GenerationTask>>('/generate/video', req)).then(
      (result) => {
        refreshQuota()
        return result
      },
    ),

  getTask: (id: number) =>
    unwrap<GenerationTask>(client.get<ApiResponse<GenerationTask>>(`/tasks/${id}`)),

  listTasks: (query: ListTasksQuery) =>
    unwrap<TaskListResponse>(client.get<ApiResponse<TaskListResponse>>('/tasks', { params: query })),

  /** DELETE /api/tasks/:id——按 id 删一条，服务端已按当前用户过滤，不存在/别人的任务一律 TASK_NOT_FOUND。 */
  deleteTask: (id: number) =>
    unwrap<null>(client.delete<ApiResponse<null>>(`/tasks/${id}`)).then((result) => {
      refreshQuota()
      return result
    }),

  /** DELETE /api/tasks——清空当前用户的全部历史，返回删除行数供"清空全部"确认。 */
  deleteAllTasks: () =>
    unwrap<TaskDeleteAllResponse>(client.delete<ApiResponse<TaskDeleteAllResponse>>('/tasks')).then(
      (result) => {
        refreshQuota()
        return result
      },
    ),

  // 优化要等上游大模型把整段提示词写完（实测约 19s），同样超过默认超时。
  optimizePrompt: (req: OptimizePromptRequest) =>
    unwrap<OptimizePromptResponse>(
      client.post<ApiResponse<OptimizePromptResponse>>('/optimize-prompt', req, {
        timeout: LONG_RUNNING_TIMEOUT,
      }),
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
