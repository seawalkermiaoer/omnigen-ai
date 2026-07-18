import { useState } from 'react'
import { App, Button, Flex, Typography, Upload } from 'antd'
import {
  ArrowLeftOutlined,
  ArrowRightOutlined,
  DeleteOutlined,
  LoadingOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'

import { uploadApi } from '@/api/generation'
import { useApiError } from '@/hooks/useApiError'
import { loadImageDimensions, validateDimensions, type DimensionValidation } from './mediaValidation'

const { Text } = Typography

export interface MediaUploaderItem {
  /** React key，同时用作重排/删除时定位条目。 */
  uid: string
  /** POST /api/upload 返回的 URL（data URI 或 OSS 签名 URL），已经是生成请求可直接使用的值。 */
  url: string
  name: string
}

export interface MediaUploaderProps {
  value: MediaUploaderItem[]
  onChange: (items: MediaUploaderItem[]) => void
  /** 该模型允许的最大图片数——来自目录 Model.MaxImages（r2v 还需调用方另减已用视频数）。 */
  maxCount: number
  /** 宽、高都要达到的最小像素值；0/undefined 表示不限制（非 i2v 模型）。 */
  minEdge?: number
  /** 宽高比（宽/高）允许区间；两者任一为空表示不限制。 */
  ratioMin?: number
  ratioMax?: number
  disabled?: boolean
  accept?: string
}

function describeValidationError(
  result: Extract<DimensionValidation, { ok: false }>,
  t: (key: string, opts?: Record<string, unknown>) => string,
): string {
  if (result.reason === 'min-edge') {
    return t('generation.mediaMinEdgeError', {
      width: result.width,
      height: result.height,
      minEdge: result.minEdge,
    })
  }
  return t('generation.mediaRatioError', {
    ratio: result.ratio.toFixed(2),
    ratioMin: result.ratioMin.toFixed(2),
    ratioMax: result.ratioMax.toFixed(2),
  })
}

/**
 * 多图上传：排序、删除、缩略图，尺寸/比例校验在上传前完成——不合规的图片
 * 从不会打一次 POST /api/upload，与旧 i2v.js 的行为一致（见其
 * bindSingleImageUploader 里 handleFile 的校验顺序）。
 *
 * minEdge/ratioMin/ratioMax 由调用方按所选模型从目录读出后传入，本组件不
 * 内置任何模型特定的阈值——happyhorse 的 300px/0.4~2.5 与 wan 的
 * 240px/0.125~8 都是调用方决定的，不是这里硬编码的。
 */
export default function MediaUploader({
  value,
  onChange,
  maxCount,
  minEdge,
  ratioMin,
  ratioMax,
  disabled,
  accept = 'image/*',
}: MediaUploaderProps) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const { notify } = useApiError()
  const [uploadingCount, setUploadingCount] = useState(0)

  const atCapacity = value.length >= maxCount

  const handleBeforeUpload = async (file: File): Promise<boolean | string> => {
    if (atCapacity) {
      void message.error(t('generation.mediaMaxCountReached', { max: maxCount }))
      return Upload.LIST_IGNORE
    }

    let dimensions: { width: number; height: number }
    try {
      dimensions = await loadImageDimensions(file)
    } catch {
      void message.error(t('generation.mediaDecodeError'))
      return Upload.LIST_IGNORE
    }

    const result = validateDimensions(dimensions.width, dimensions.height, {
      minEdge,
      ratioMin,
      ratioMax,
    })
    if (!result.ok) {
      void message.error(describeValidationError(result, t))
      return Upload.LIST_IGNORE
    }

    setUploadingCount((c) => c + 1)
    try {
      const uploaded = await uploadApi.upload(file)
      onChange([...value, { uid: `${Date.now()}-${file.name}`, url: uploaded.url, name: file.name }])
    } catch (err) {
      notify(err)
    } finally {
      setUploadingCount((c) => c - 1)
    }
    return Upload.LIST_IGNORE
  }

  const moveItem = (index: number, direction: -1 | 1) => {
    const target = index + direction
    if (target < 0 || target >= value.length) return
    const next = [...value]
    const [item] = next.splice(index, 1)
    next.splice(target, 0, item)
    onChange(next)
  }

  const removeItem = (uid: string) => {
    onChange(value.filter((item) => item.uid !== uid))
  }

  return (
    <Flex vertical gap={8} data-testid="media-uploader">
      <Flex wrap gap={12}>
        {value.map((item, index) => (
          <div
            key={item.uid}
            data-testid={`media-item-${index}`}
            style={{
              position: 'relative',
              width: 96,
              height: 96,
              borderRadius: 8,
              overflow: 'hidden',
              border: '1px solid var(--omnigen-border, #26262c)',
            }}
          >
            <img
              src={item.url}
              alt={t('generation.mediaThumbnailAlt', { n: index + 1 })}
              style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }}
            />
            <Flex
              justify="space-between"
              align="center"
              style={{
                position: 'absolute',
                left: 0,
                right: 0,
                bottom: 0,
                padding: '2px 4px',
                background: 'rgba(0,0,0,0.55)',
              }}
            >
              <Button
                type="text"
                size="small"
                autoInsertSpace={false}
                disabled={disabled || index === 0}
                aria-label={t('generation.mediaMoveLeft')}
                icon={<ArrowLeftOutlined aria-hidden />}
                onClick={() => moveItem(index, -1)}
              />
              <Button
                type="text"
                size="small"
                danger
                autoInsertSpace={false}
                disabled={disabled}
                aria-label={t('generation.mediaRemove')}
                icon={<DeleteOutlined aria-hidden />}
                onClick={() => removeItem(item.uid)}
              />
              <Button
                type="text"
                size="small"
                autoInsertSpace={false}
                disabled={disabled || index === value.length - 1}
                aria-label={t('generation.mediaMoveRight')}
                icon={<ArrowRightOutlined aria-hidden />}
                onClick={() => moveItem(index, 1)}
              />
            </Flex>
          </div>
        ))}

        <Upload
          accept={accept}
          multiple
          showUploadList={false}
          beforeUpload={handleBeforeUpload}
          disabled={disabled || atCapacity}
        >
          <div
            style={{
              width: 96,
              height: 96,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              flexDirection: 'column',
              gap: 4,
              border: '1px dashed var(--omnigen-border, #26262c)',
              borderRadius: 8,
              cursor: disabled || atCapacity ? 'not-allowed' : 'pointer',
              opacity: disabled || atCapacity ? 0.5 : 1,
            }}
          >
            {uploadingCount > 0 ? <LoadingOutlined aria-hidden /> : <PlusOutlined aria-hidden />}
            <Text type="secondary" style={{ fontSize: 12 }}>
              {t('generation.mediaUpload')}
            </Text>
          </div>
        </Upload>
      </Flex>

      <Text type="secondary" style={{ fontSize: 12 }} data-testid="media-count-hint">
        {t('generation.mediaCountHint', { count: value.length, max: maxCount })}
      </Text>
    </Flex>
  )
}
