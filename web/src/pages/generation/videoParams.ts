/**
 * 视频页面的 resolution/duration/ratio 取值，全部从所选模型的目录条目派生。
 *
 * 这个文件过去是一份写死的常量表，逐字复制 server 的
 * generation_video.go——注释里写的理由是"这几个常量不按模型变化"。
 * wan3.0 让这句话不再成立：它多了 480P、时长是 2~30 而不是 3~15、
 * ratio 集合也不同（有 adaptive、没有 4:5/5:4），而且 i2v 也接受 ratio。
 *
 * 所以取值范围搬进了 catalog.Model（Resolutions/DurationMin/DurationMax/
 * Ratios/DefaultRatio/I2VAutoRatio/SmartDuration），本文件只剩下"从目录
 * 读出来"的取数函数。这样新增视频模型时前端一行都不用改——和模型下拉框、
 * ParamPanel 的做法一致。
 *
 * 每个函数都接受 `model` 可能为 undefined（目录还没加载完），此时返回
 * 一组不会让控件报错的空值/占位值，由调用页面的 loading 态负责遮挡。
 */

import type { CatalogModel, TaskMode } from '@/types/generation'

/** 智能时长哨兵值，对应 catalog.SmartDurationValue。 */
export const SMART_DURATION = -1

export function videoResolutions(model?: CatalogModel): string[] {
  return model?.Resolutions ?? []
}

export function defaultResolution(model?: CatalogModel): string {
  return model?.DefaultResolution ?? ''
}

export function videoRatios(model?: CatalogModel): string[] {
  return model?.Ratios ?? []
}

export function defaultRatio(model?: CatalogModel): string {
  return model?.DefaultRatio ?? ''
}

export function durationMin(model?: CatalogModel): number {
  return model?.DurationMin ?? 1
}

export function durationMax(model?: CatalogModel): number {
  return model?.DurationMax ?? 1
}

export function defaultDuration(model?: CatalogModel): number {
  return model?.DefaultDuration ?? 1
}

export function supportsSmartDuration(model?: CatalogModel): boolean {
  return !!model?.SmartDuration
}

/**
 * 该模型在给定模式下是否接受 ratio 参数。
 *
 * 只有一种情况不接受：i2v 且模型标了 I2VAutoRatio（wan2.7 与 happyhorse，
 * 宽高比完全由首帧决定，后端收到 ratio 会直接报错）。wan3.0 不标这个
 * 旗标——它的默认值 adaptive 本身就是"跟随输入素材"，所以在 i2v 下同样
 * 是一个有意义的可选项。
 */
export function acceptsRatio(model: CatalogModel | undefined, mode: TaskMode): boolean {
  if (!model) return false
  if (mode === 'i2v' && model.I2VAutoRatio) return false
  return (model.Ratios ?? []).length > 0
}

/**
 * 切换模型时把当前的 resolution/duration/ratio 收敛到新模型的合法取值。
 *
 * 用途是"从 wan2.7 切到 wan3.0（或反过来）时表单不能留着上一个模型才认识
 * 的值"——比如在 wan3.0 上选了 480P 再切回 wan2.7，若原样保留，提交就会
 * 被后端拒绝，而用户看到的下拉框里明明显示着一个值。越界一律回落到新
 * 模型的默认值，而不是保留后报错。
 */
export function clampVideoParams(
  model: CatalogModel | undefined,
  mode: TaskMode,
  current: { resolution: string; duration: number; ratio: string },
): { resolution: string; duration: number; ratio: string } {
  if (!model) return current

  const resolutions = videoResolutions(model)
  const resolution = resolutions.includes(current.resolution)
    ? current.resolution
    : defaultResolution(model)

  const inRange = current.duration >= durationMin(model) && current.duration <= durationMax(model)
  const isSmart = current.duration === SMART_DURATION && supportsSmartDuration(model)
  const duration = inRange || isSmart ? current.duration : defaultDuration(model)

  let ratio = ''
  if (acceptsRatio(model, mode)) {
    ratio = videoRatios(model).includes(current.ratio) ? current.ratio : defaultRatio(model)
  }

  return { resolution, duration, ratio }
}
