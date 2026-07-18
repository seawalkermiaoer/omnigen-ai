/**
 * MediaUploader 的尺寸/比例校验逻辑，独立于组件本身以便纯函数单测。
 *
 * i2v 的尺寸规则按模型不同（happyhorse 最小边 300px、比例 0.4~2.5；wan
 * 最小边 240px、比例 0.125~8），且宽高都要达到最小值——不是取短边，见
 * server/internal/model/catalog/catalog.go MinImageEdge 的文档注释与旧
 * i2v.js:145 `w < minSize || h < minSize`。这里只realize 通用规则，具体
 * 阈值由调用方以 props 形式传入（来自目录），不在这里硬编码任何模型 id。
 */

export interface DimensionConstraints {
  /** 宽、高都必须达到的最小像素值；0 或 undefined 表示不限制。 */
  minEdge?: number
  /** 宽高比（宽/高）允许区间；ratioMin/ratioMax 任一为空表示不限制比例。 */
  ratioMin?: number
  ratioMax?: number
}

export type DimensionValidation =
  | { ok: true }
  | { ok: false; reason: 'min-edge'; width: number; height: number; minEdge: number }
  | { ok: false; reason: 'ratio'; ratio: number; ratioMin: number; ratioMax: number }

/** 纯函数：给定实际宽高与约束，判断是否合规。不做任何 I/O。 */
export function validateDimensions(
  width: number,
  height: number,
  constraints: DimensionConstraints,
): DimensionValidation {
  const { minEdge, ratioMin, ratioMax } = constraints

  if (minEdge && (width < minEdge || height < minEdge)) {
    return { ok: false, reason: 'min-edge', width, height, minEdge }
  }

  if (ratioMin != null && ratioMax != null && ratioMin > 0 && ratioMax > 0) {
    const ratio = width / height
    if (ratio < ratioMin || ratio > ratioMax) {
      return { ok: false, reason: 'ratio', ratio, ratioMin, ratioMax }
    }
  }

  return { ok: true }
}

/** 用 <img> 解码本地文件，读出真实像素宽高——校验必须在上传前完成。 */
export function loadImageDimensions(file: File): Promise<{ width: number; height: number }> {
  return new Promise((resolve, reject) => {
    const objectUrl = URL.createObjectURL(file)
    const img = new Image()
    img.onload = () => {
      URL.revokeObjectURL(objectUrl)
      resolve({ width: img.naturalWidth, height: img.naturalHeight })
    }
    img.onerror = () => {
      URL.revokeObjectURL(objectUrl)
      reject(new Error('IMAGE_DECODE_ERROR'))
    }
    img.src = objectUrl
  })
}
