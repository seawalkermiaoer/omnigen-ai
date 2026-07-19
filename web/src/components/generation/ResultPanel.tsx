import { useState } from 'react'
import { Alert, App, Button, Empty, Flex, Image, Spin, Tag, Typography } from 'antd'
import { CopyOutlined, DownloadOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'

import { generationApi } from '@/api/generation'
import { useApiError } from '@/hooks/useApiError'
import type { GenerationTask, TaskStatus } from '@/types/generation'

const { Text, Paragraph } = Typography

export interface ResultPanelProps {
  task?: GenerationTask | null
  /** 视频任务提交后到终态之间由调用页面轮询驱动；图片任务是同步接口，通常不需要。 */
  polling?: boolean
}

const VIDEO_MODES = new Set(['t2v', 'i2v', 'r2v'])

const STATUS_COLOR: Record<TaskStatus, string> = {
  PENDING: 'default',
  RUNNING: 'processing',
  SUCCEEDED: 'success',
  FAILED: 'error',
  CANCELED: 'default',
}

function resultExtension(isVideo: boolean): string {
  return isVideo ? 'mp4' : 'png'
}

/**
 * 结果展示区：图片网格 / 视频播放器，逐项下载与复制链接，以及 t8star 的
 * note 文案（存在时展示）。
 *
 * 下载走 GET /api/download/:taskId/:index：它带鉴权、有 Content-Disposition，
 * 文件名由服务端拼，体验优于让浏览器直接打开一个裸链。
 *
 * 复制链接则直接给出 task.resultUrls[index] 本身。之前复制的是上面那条内部
 * 下载路径，可它需要 Authorization 头才能打开，粘到任何地方都是死链——一个
 * 叫"复制链接"的按钮给出打不开的东西没有意义。结果归档到我方 OSS 后
 * resultUrls 是永久公开地址，正是该复制的东西；老任务里存的上游地址即便可能
 * 已过期，也仍比一条必然打不开的内部路径有用。
 */
export default function ResultPanel({ task, polling }: ResultPanelProps) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const { notify } = useApiError()
  const [downloadingIndex, setDownloadingIndex] = useState<number | null>(null)

  if (!task) {
    return (
      <Empty
        data-testid="result-panel-empty"
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description={t('generation.resultEmpty')}
      />
    )
  }

  const isVideo = VIDEO_MODES.has(task.mode)
  const ext = resultExtension(isVideo)

  const handleDownload = async (index: number) => {
    setDownloadingIndex(index)
    try {
      await generationApi.downloadResult(task.id, index, `omnigen-${task.mode}-${task.id}-${index}.${ext}`)
    } catch (err) {
      notify(err)
    } finally {
      setDownloadingIndex(null)
    }
  }

  const handleCopyLink = async (index: number) => {
    const url = task.resultUrls[index]
    try {
      await navigator.clipboard.writeText(url)
      void message.success(t('generation.resultCopySuccess'))
    } catch {
      void message.error(t('generation.resultCopyFailed'))
    }
  }

  return (
    <Flex vertical gap={12} data-testid="result-panel">
      <Flex align="center" gap={8}>
        <Tag color={STATUS_COLOR[task.status]} data-testid="result-status">
          {t(`generation.status.${task.status}`)}
        </Tag>
        {(polling || task.status === 'PENDING' || task.status === 'RUNNING') && <Spin size="small" />}
      </Flex>

      {task.status === 'FAILED' && (
        <Alert
          type="error"
          showIcon
          message={task.errorMessage || t('errors.UNKNOWN')}
          data-testid="result-error"
        />
      )}

      {task.note && (
        <Alert
          type="info"
          showIcon
          message={t('generation.resultNoteTitle')}
          description={<Paragraph style={{ marginBottom: 0 }}>{task.note}</Paragraph>}
          data-testid="result-note"
        />
      )}

      {task.status === 'SUCCEEDED' && task.resultUrls.length > 0 && (
        <>
          {isVideo ? (
            <Flex vertical gap={8} data-testid="result-video">
              {/* eslint-disable-next-line jsx-a11y/media-has-caption */}
              <video
                src={task.resultUrls[0]}
                controls
                style={{ width: '100%', maxHeight: 480, borderRadius: 8 }}
              />
              <Flex gap={8}>
                <Button
                  icon={<DownloadOutlined aria-hidden />}
                  loading={downloadingIndex === 0}
                  autoInsertSpace={false}
                  onClick={() => void handleDownload(0)}
                >
                  {t('generation.resultDownload')}
                </Button>
                <Button
                  icon={<CopyOutlined aria-hidden />}
                  autoInsertSpace={false}
                  onClick={() => void handleCopyLink(0)}
                >
                  {t('generation.resultCopyLink')}
                </Button>
              </Flex>
            </Flex>
          ) : (
            <Flex wrap gap={16} data-testid="result-image-grid">
              {task.resultUrls.map((url, index) => (
                <Flex vertical gap={8} key={`${url}-${index}`} style={{ width: 200 }}>
                  <Image
                    src={url}
                    alt={t('generation.resultImageAlt', { n: index + 1 })}
                    style={{ width: '100%', borderRadius: 8, objectFit: 'cover' }}
                  />
                  <Flex gap={8}>
                    <Button
                      size="small"
                      icon={<DownloadOutlined aria-hidden />}
                      loading={downloadingIndex === index}
                      autoInsertSpace={false}
                      onClick={() => void handleDownload(index)}
                    >
                      {t('generation.resultDownload')}
                    </Button>
                    <Button
                      size="small"
                      icon={<CopyOutlined aria-hidden />}
                      autoInsertSpace={false}
                      onClick={() => void handleCopyLink(index)}
                    >
                      {t('generation.resultCopyLink')}
                    </Button>
                  </Flex>
                </Flex>
              ))}
            </Flex>
          )}
        </>
      )}

      {task.usage && Object.keys(task.usage).length > 0 && (
        <Text type="secondary" style={{ fontSize: 12 }} data-testid="result-usage">
          {Object.entries(task.usage)
            .map(([k, v]) => `${k}: ${String(v)}`)
            .join(' · ')}
        </Text>
      )}
    </Flex>
  )
}
