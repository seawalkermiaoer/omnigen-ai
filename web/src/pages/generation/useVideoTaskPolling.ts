import { useEffect, useRef, useState } from 'react'

import { generationApi } from '@/api/generation'
import { useApiError } from '@/hooks/useApiError'
import type { GenerationTask, TaskStatus } from '@/types/generation'

const POLL_INTERVAL_MS = 3000

const TERMINAL_STATUSES: ReadonlySet<TaskStatus> = new Set(['SUCCEEDED', 'FAILED', 'CANCELED'])

/**
 * 视频生成是异步的：POST /api/generate/video 只落一行 PENDING 状态的本地
 * 任务并立刻返回，真正推进任务到终态的轮询在后端 worker 里（见
 * server/internal/worker/poller.go，Task 12）。这个 hook 只是定期
 * GET /api/tasks/:id 把最新状态拉到前端展示，本身不驱动、也不重试任何
 * 上游调用——纯只读轮询。
 *
 * 与旧系统的关键差异：旧版 task.js 把任务 id 存在浏览器内存里，关闭标签页
 * 即丢失、用户必须守着页面等。这里返回的 id 是服务端持久化的本地任务 id
 * （GenerationTask.id），意味着即使中途离开这个页面，任务仍在继续；本 hook
 * 只负责"当前这次打开页面期间要不要继续拉状态"，不负责任务本身的生命周期。
 */
export function useVideoTaskPolling() {
  const [task, setTask] = useState<GenerationTask | null>(null)
  const [polling, setPolling] = useState(false)
  const { notify } = useApiError()
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(
    () => () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    },
    [],
  )

  const scheduleNext = (id: number) => {
    timerRef.current = setTimeout(() => void tick(id), POLL_INTERVAL_MS)
  }

  const tick = async (id: number) => {
    try {
      const next = await generationApi.getTask(id)
      setTask(next)
      if (TERMINAL_STATUSES.has(next.status)) {
        setPolling(false)
        return
      }
      scheduleNext(id)
    } catch (err) {
      // 轮询失败就停止——不用错误状态覆盖已经展示的任务快照，只是不再继续拉。
      setPolling(false)
      notify(err)
    }
  }

  /** 提交成功后调用：把 POST /api/generate/video 的返回值当作第一帧展示，随后按需继续轮询。 */
  const start = (initialTask: GenerationTask) => {
    if (timerRef.current) clearTimeout(timerRef.current)
    setTask(initialTask)
    if (TERMINAL_STATUSES.has(initialTask.status)) {
      setPolling(false)
      return
    }
    setPolling(true)
    scheduleNext(initialTask.id)
  }

  const reset = () => {
    if (timerRef.current) clearTimeout(timerRef.current)
    setTask(null)
    setPolling(false)
  }

  return { task, polling, start, reset }
}
