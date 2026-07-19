import { Descriptions, Modal, Tag, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

import { ResultPanel } from '@/components/generation'
import type { GenerationTask } from '@/types/generation'
import { formatAbsoluteTime, modeLabel, paramSummary } from './historyFormat'

const { Paragraph, Text } = Typography

const STATUS_COLOR: Record<GenerationTask['status'], string> = {
  PENDING: 'default',
  RUNNING: 'processing',
  SUCCEEDED: 'success',
  FAILED: 'error',
  CANCELED: 'default',
}

export interface HistoryDetailModalProps {
  task: GenerationTask | null
  onClose: () => void
}

/**
 * "详情"弹窗：完整参数表 + 结果内联渲染。
 *
 * 结果渲染直接复用 ResultPanel（子项目 3 的组件）——它已经处理好图片网格
 * /视频播放器、逐项下载、复制链接、失败态与 note 展示，历史页不需要为
 * "已经落库的任务"重新实现一遍这些逻辑，与实时生成页面看到的结果长得
 *完全一样。
 */
export default function HistoryDetailModal({ task, onClose }: HistoryDetailModalProps) {
  const { t } = useTranslation()

  if (!task) return null

  return (
    <Modal
      open={!!task}
      onCancel={onClose}
      footer={null}
      width={720}
      title={t('history.detailTitle', { id: task.id })}
      data-testid="history-detail-modal"
      destroyOnHidden
    >
      <Descriptions
        size="small"
        column={1}
        bordered
        styles={{ label: { width: 140 } }}
        data-testid="history-detail-descriptions"
      >
        <Descriptions.Item label={t('history.detailMode')}>
          <Tag>{modeLabel(task.mode, t)}</Tag> {task.model}
        </Descriptions.Item>
        <Descriptions.Item label={t('history.detailStatus')}>
          <Tag color={STATUS_COLOR[task.status]}>{t(`generation.status.${task.status}`)}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label={t('history.detailTaskId')}>{task.id}</Descriptions.Item>
        {task.upstreamTaskId && (
          <Descriptions.Item label={t('history.detailUpstreamTaskId')}>
            <Text code>{task.upstreamTaskId}</Text>
          </Descriptions.Item>
        )}
        <Descriptions.Item label={t('history.detailCreatedAt')}>
          {formatAbsoluteTime(task.createdAt)}
        </Descriptions.Item>
        <Descriptions.Item label={t('history.detailUpdatedAt')}>
          {formatAbsoluteTime(task.updatedAt)}
        </Descriptions.Item>
        <Descriptions.Item label={t('history.detailParamSummary')}>
          {paramSummary(task, t) || t('common.empty')}
        </Descriptions.Item>
        <Descriptions.Item label={t('history.detailParams')}>
          <Text code style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
            {JSON.stringify(task.params ?? {}, null, 2)}
          </Text>
        </Descriptions.Item>
        {task.inputUrls.length > 0 && (
          <Descriptions.Item label={t('history.detailInputCount')}>
            {t('history.imageCount', { n: task.inputUrls.length })}
          </Descriptions.Item>
        )}
      </Descriptions>

      <Paragraph style={{ marginTop: 16, marginBottom: 4 }}>
        <Text strong>{t('history.detailPrompt')}</Text>
      </Paragraph>
      <Paragraph
        style={{ background: 'var(--omnigen-bg-elevated, #17171b)', padding: 10, borderRadius: 6, whiteSpace: 'pre-wrap' }}
        data-testid="history-detail-prompt"
      >
        {task.prompt || t('history.detailNoPrompt')}
      </Paragraph>

      <div style={{ marginTop: 16 }}>
        <ResultPanel task={task} />
      </div>
    </Modal>
  )
}
