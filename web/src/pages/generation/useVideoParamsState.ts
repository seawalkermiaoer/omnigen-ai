import { useEffect, useState } from 'react'

import type { CatalogModel, TaskMode } from '@/types/generation'
import { clampVideoParams } from './videoParams'

export interface VideoParamsState {
  resolution: string
  duration: number
  ratio: string
  setResolution: (v: string) => void
  setDuration: (v: number) => void
  setRatio: (v: string) => void
}

/**
 * 五个视频页面共用的 resolution/duration/ratio 状态。
 *
 * 存在的理由是这三个值不再有全局默认值：它们的合法取值属于所选模型
 * （见 videoParams.ts），所以"初始化成什么"和"切模型后变成什么"都得等
 * 目录加载完、模型选定之后才知道。把这段同步逻辑放进 hook，是为了让
 * 五个页面不必各写一遍——写五遍的话，漏改一个页面的表现是：从 wan3.0
 * 切回 wan2.7 后下拉框里还留着 480P，点提交才被后端拒绝。
 *
 * 初始值刻意是空串 / 0 这种非法值，而不是猜一个默认值：clampVideoParams
 * 会把任何非法值收敛到当前模型的默认值，于是"首次选中模型"和"切换模型"
 * 走的是同一条代码路径，不存在只在其中一条上生效的 bug。
 *
 * 也正因为收敛只淘汰非法值，"复用历史记录"的预填充可以照常在选中模型的
 * 同一轮里 setResolution/setDuration/setRatio——只要那些值对该模型合法就
 * 会被保留下来，不合法的本来也不该恢复。
 */
export function useVideoParamsState(model: CatalogModel | undefined, mode: TaskMode): VideoParamsState {
  const [resolution, setResolution] = useState('')
  const [duration, setDuration] = useState(0)
  const [ratio, setRatio] = useState('')

  useEffect(() => {
    if (!model) return
    const next = clampVideoParams(model, mode, { resolution, duration, ratio })
    if (next.resolution !== resolution) setResolution(next.resolution)
    if (next.duration !== duration) setDuration(next.duration)
    if (next.ratio !== ratio) setRatio(next.ratio)
    // 只依赖模型 id 与模式：resolution/duration/ratio 自身不进依赖数组，
    // 否则用户每改一次控件都会重跑一次收敛。收敛只需要在"换了模型"这个
    // 时刻发生一次，用户随后的输入由控件自身的 min/max/options 约束。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [model?.ID, mode])

  return { resolution, duration, ratio, setResolution, setDuration, setRatio }
}
