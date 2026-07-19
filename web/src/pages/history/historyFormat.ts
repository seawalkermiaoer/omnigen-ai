import type { TFunction } from 'i18next'

import type { GenerationTask, ReuseGenerationState, TaskMode } from '@/types/generation'

/**
 * 图片家族 vs 视频家族的模式分组——历史记录页渲染参数摘要、下载文件后缀
 * 都要按这个分组分支，不能靠 `mode === 'imggen'` 单点判断。
 *
 * 这是对旧 public/js/history.js 一个真实 bug 的修复：`renderHistoryCard`
 * 里 `isImgMode = r.mode === 'imggen'` 让 imgedit 记录被错误地落进"否则"
 * 分支，渲染 resolution/ratio/duration（全部 undefined）而不是 size/count。
 * imgedit 与 imggen 共用同一张结果表（图片），必须一起分到图片家族。
 */
export const IMAGE_MODES: ReadonlySet<TaskMode> = new Set(['imggen', 'imgedit'])
export const VIDEO_MODES: ReadonlySet<TaskMode> = new Set(['t2v', 'i2v', 'r2v'])

export function isImageMode(mode: TaskMode): boolean {
  return IMAGE_MODES.has(mode)
}

/** 复用时应该跳去的生成页面路由，与 App.tsx 的路由表一一对应。 */
export const MODE_PATH: Record<TaskMode, string> = {
  imggen: '/imggen',
  imgedit: '/imgedit',
  t2v: '/t2v',
  i2v: '/i2v',
  r2v: '/r2v',
}

const MINUTE = 60_000
const HOUR = 3_600_000
const DAY = 86_400_000

/** 相对时间文案，超过 7 天回退为本地化的绝对时间——照抄旧 history.js 的 fmtRelTime 阈值。 */
export function formatRelativeTime(iso: string, t: TFunction, now: number = Date.now()): string {
  const ts = new Date(iso).getTime()
  if (!Number.isFinite(ts)) return iso
  const diff = now - ts
  if (diff < MINUTE) return t('history.justNow')
  if (diff < HOUR) return t('history.minutesAgo', { n: Math.floor(diff / MINUTE) })
  if (diff < DAY) return t('history.hoursAgo', { n: Math.floor(diff / HOUR) })
  if (diff < 7 * DAY) return t('history.daysAgo', { n: Math.floor(diff / DAY) })
  return new Date(ts).toLocaleString()
}

export function formatAbsoluteTime(iso: string): string {
  const ts = new Date(iso).getTime()
  return Number.isFinite(ts) ? new Date(ts).toLocaleString() : iso
}

const TERMINAL_STATUSES = new Set(['SUCCEEDED', 'FAILED', 'CANCELED'])

/** 终态任务的耗时（秒）；仍在飞的任务没有意义，返回 null。 */
export function elapsedSeconds(task: GenerationTask): number | null {
  if (!TERMINAL_STATUSES.has(task.status)) return null
  const start = new Date(task.createdAt).getTime()
  const end = new Date(task.updatedAt).getTime()
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return null
  return Math.round((end - start) / 1000)
}

/**
 * 参数摘要：图片家族展示 size/数量/结果张数，视频家族展示
 * resolution/ratio/duration——这正是要按 isImageMode 分组，而不是旧代码
 * `mode === 'imggen'` 那种漏掉 imgedit 的写法。
 */
export function paramSummary(task: GenerationTask, t: TFunction): string {
  const p = task.params ?? {}
  const parts: string[] = []

  if (isImageMode(task.mode)) {
    if (typeof p.size === 'string' && p.size) parts.push(p.size)
    if (typeof p.n === 'number') parts.push(t('history.imageCount', { n: p.n }))
    if (task.resultUrls.length > 0) parts.push(t('history.resultCount', { n: task.resultUrls.length }))
  } else {
    if (typeof p.resolution === 'string' && p.resolution) parts.push(p.resolution)
    if (typeof p.ratio === 'string' && p.ratio) parts.push(p.ratio)
    if (typeof p.duration === 'number') parts.push(`${p.duration}s`)
  }

  const elapsed = elapsedSeconds(task)
  if (elapsed !== null) parts.push(`${elapsed}s`)

  return parts.join(' · ')
}

/**
 * 失败原因的展示文案——优先按 errorCode 查 `errors.<code>` 翻译表，命中
 * 才用；查不到才回退到后端给的 errorMessage 原文，最终兜底通用文案。
 * 与 useApiError.toMessage 同样的"未知 code 绝不裸露"原则，但这里没有
 * ApiError 实例可用（数据来自已经落库的任务，不是这次请求抛出的异常）。
 */
export function errorText(task: GenerationTask, t: TFunction): string | null {
  if (task.status !== 'FAILED') return null
  if (task.errorCode) {
    const key = `errors.${task.errorCode}`
    const text = t(key)
    if (text !== key) return text
  }
  return task.errorMessage || t('errors.UNKNOWN')
}

export function modeLabel(mode: TaskMode, t: TFunction): string {
  return t(`history.modeLabels.${mode}`)
}

export function resultExtension(mode: TaskMode): string {
  return VIDEO_MODES.has(mode) ? 'mp4' : 'png'
}

/** 组装"复用"要通过 router state 带给目标生成页面的数据。 */
export function buildReuseState(task: GenerationTask): { reuse: ReuseGenerationState } {
  return {
    reuse: {
      taskId: task.id,
      mode: task.mode,
      model: task.model,
      prompt: task.prompt,
      params: task.params,
      hadInput: task.inputUrls.length > 0,
    },
  }
}
