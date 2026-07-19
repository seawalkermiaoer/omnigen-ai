import { useCallback, useEffect, useRef, useState } from 'react'
import { App, Button, Empty, Flex, Input, Modal, Space, Table, Tag, Tooltip, Typography } from 'antd'
import {
  ClearOutlined,
  CopyOutlined,
  DeleteOutlined,
  DownloadOutlined,
  ExportOutlined,
  EyeOutlined,
  RedoOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import type { ColumnsType } from 'antd/es/table'

import { generationApi } from '@/api/generation'
import { useApiError } from '@/hooks/useApiError'
import type { GenerationTask, TaskStatus } from '@/types/generation'
import HistoryDetailModal from './history/HistoryDetailModal'
import {
  buildReuseState,
  errorText,
  formatAbsoluteTime,
  formatRelativeTime,
  MODE_PATH,
  modeLabel,
  paramSummary,
  resultExtension,
} from './history/historyFormat'

const { Title, Text, Paragraph } = Typography

const STATUS_COLOR: Record<TaskStatus, string> = {
  PENDING: 'default',
  RUNNING: 'processing',
  SUCCEEDED: 'success',
  FAILED: 'error',
  CANCELED: 'default',
}

// 与 useVideoTaskPolling 的 POLL_INTERVAL_MS 保持一致的节奏，没有特别理由
// 让历史页比实时生成页面的轮询更快或更慢。
const POLL_INTERVAL_MS = 3000

// "清空全部"是不可逆的服务端操作，用一个固定的确认词（不做翻译，就像
// GitHub 删除仓库要求手打仓库名那样）而不是简单的"确定/取消"两个按钮，
// 逼用户多一步有意识的操作。
const CLEAR_CONFIRM_WORD = 'DELETE'

/**
 * 历史记录页——public/js/history.js 的复刻，但去掉了两样旧系统的包袱：
 *
 * 1. 不再有"继续轮询"按钮。旧系统里任务轮询循环跑在浏览器里，关闭标签页
 *    就断了，只能靠用户手动点"继续轮询"重新接上。新系统的轮询在服务端
 *    worker 里跑（server/internal/worker/poller.go），本页面只是定期把
 *    在飞任务的最新状态拉过来展示，见下面的轮询 effect。
 * 2. 不再有内联 base64 缩略图。旧系统把每条记录的缩略图存成 data URI 塞进
 *    localStorage，写满配额时 saveHistory 只能 console.warn 静默丢弃。
 *    这里的结果一律从服务端返回的真实 URL 渲染（ResultPanel），历史记录
 *    本身也是服务端分页查询，不再有 localStorage 100 条的硬上限。
 */
export default function HistoryPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { message, modal } = App.useApp()
  const { notify } = useApiError()

  const [items, setItems] = useState<GenerationTask[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [loading, setLoading] = useState(false)

  const [detailTask, setDetailTask] = useState<GenerationTask | null>(null)
  const [downloadingId, setDownloadingId] = useState<number | null>(null)

  const [clearOpen, setClearOpen] = useState(false)
  const [clearInput, setClearInput] = useState('')
  const [clearSubmitting, setClearSubmitting] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const resp = await generationApi.listTasks({ page, pageSize })
      setItems(resp.items)
      setTotal(resp.total)
    } catch (err) {
      notify(err)
    } finally {
      setLoading(false)
    }
  }, [page, pageSize, notify])

  useEffect(() => {
    void load()
  }, [load])

  // ---- 在飞任务自动轮询 ----------------------------------------------------
  // 只要当前页里还有 PENDING/RUNNING 的任务，定期把它们的最新状态拉回来；
  // 全部进入终态后自然停止，不需要用户做任何事。effect 依赖 items 本身，
  // 每次拉回新状态都会重新判断是否还需要下一轮，天然形成一个自我调度的
  // 轮询循环（与 useVideoTaskPolling 的 scheduleNext/tick 是同一个模式）。
  const pollTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(
    () => () => {
      if (pollTimerRef.current) clearTimeout(pollTimerRef.current)
    },
    [],
  )

  useEffect(() => {
    if (pollTimerRef.current) {
      clearTimeout(pollTimerRef.current)
      pollTimerRef.current = null
    }
    const inFlight = items.filter((i) => i.status === 'PENDING' || i.status === 'RUNNING')
    if (inFlight.length === 0) return

    pollTimerRef.current = setTimeout(() => {
      void Promise.all(inFlight.map((task) => generationApi.getTask(task.id).catch(() => null))).then(
        (results) => {
          setItems((prev) => prev.map((item) => results.find((r) => r && r.id === item.id) ?? item))
        },
      )
    }, POLL_INTERVAL_MS)
  }, [items])

  const handleDelete = (task: GenerationTask) => {
    modal.confirm({
      title: t('common.delete'),
      content: t('history.deleteConfirmContent'),
      okText: t('common.confirm'),
      cancelText: t('common.cancel'),
      okButtonProps: { danger: true, autoInsertSpace: false },
      cancelButtonProps: { autoInsertSpace: false },
      onOk: async () => {
        try {
          await generationApi.deleteTask(task.id)
          void message.success(t('history.deleteSuccess'))
          await load()
        } catch (err) {
          notify(err)
        }
      },
    })
  }

  const handleClearAll = async () => {
    setClearSubmitting(true)
    try {
      const res = await generationApi.deleteAllTasks()
      void message.success(t('history.clearSuccess', { count: res.deleted }))
      setClearOpen(false)
      setClearInput('')
      setPage(1)
      await load()
    } catch (err) {
      notify(err)
    } finally {
      setClearSubmitting(false)
    }
  }

  const handleDownload = async (task: GenerationTask) => {
    setDownloadingId(task.id)
    try {
      await generationApi.downloadResult(
        task.id,
        0,
        `omnigen-${task.mode}-${task.id}-0.${resultExtension(task.mode)}`,
      )
    } catch (err) {
      notify(err)
    } finally {
      setDownloadingId(null)
    }
  }

  const handleCopyLink = async (task: GenerationTask) => {
    const url = `${window.location.origin}/api${generationApi.downloadLinkPath(task.id, 0)}`
    try {
      await navigator.clipboard.writeText(url)
      void message.success(t('generation.resultCopySuccess'))
    } catch {
      void message.error(t('generation.resultCopyFailed'))
    }
  }

  // "复用"把任务的 model/prompt/params 通过 router state 带去对应的生成
  // 页面——机制细节与"为什么能扛住目录还没加载完"见 historyFormat.ts 的
  // buildReuseState 注释，以及各生成页面里读取 location.state.reuse 的
  // effect（Task 16 各页面都补了这一段）。
  const handleReuse = (task: GenerationTask) => {
    navigate(MODE_PATH[task.mode], { state: buildReuseState(task) })
  }

  const handleExport = () => {
    if (items.length === 0) {
      void message.info(t('history.exportEmpty'))
      return
    }
    const blob = new Blob([JSON.stringify(items, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `omnigen-history-${Date.now()}.json`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  }

  const columns: ColumnsType<GenerationTask> = [
    {
      title: t('history.columnTask'),
      key: 'task',
      width: 190,
      render: (_, task) => (
        <Flex vertical gap={4}>
          <Tag>{modeLabel(task.mode, t)}</Tag>
          <Text type="secondary" style={{ fontSize: 12 }}>
            {task.model}
          </Text>
        </Flex>
      ),
    },
    {
      title: t('history.columnStatus'),
      key: 'status',
      width: 110,
      render: (_, task) => (
        <Tag color={STATUS_COLOR[task.status]} data-testid={`history-status-${task.id}`}>
          {t(`generation.status.${task.status}`)}
        </Tag>
      ),
    },
    {
      title: t('history.columnTime'),
      key: 'time',
      width: 130,
      render: (_, task) => (
        <Tooltip title={formatAbsoluteTime(task.createdAt)}>
          <span data-testid={`history-time-${task.id}`}>{formatRelativeTime(task.createdAt, t)}</span>
        </Tooltip>
      ),
    },
    {
      title: t('history.columnContent'),
      key: 'content',
      render: (_, task) => {
        const failure = errorText(task, t)
        return (
          <Flex vertical gap={4}>
            <Paragraph
              ellipsis={{ rows: 2 }}
              style={{ marginBottom: 0, maxWidth: 480 }}
              data-testid={`history-prompt-${task.id}`}
            >
              {task.prompt || t('history.detailNoPrompt')}
            </Paragraph>
            <Text type="secondary" style={{ fontSize: 12 }} data-testid={`history-params-${task.id}`}>
              {paramSummary(task, t)}
            </Text>
            {failure && (
              <Text type="danger" style={{ fontSize: 12 }} data-testid={`history-error-${task.id}`}>
                {failure}
              </Text>
            )}
          </Flex>
        )
      },
    },
    {
      title: t('common.actions'),
      key: 'actions',
      width: 260,
      render: (_, task) => {
        const canDownload = task.status === 'SUCCEEDED' && task.resultUrls.length > 0
        return (
          <Space size="small" wrap>
            <Button
              size="small"
              icon={<EyeOutlined aria-hidden />}
              data-testid={`history-detail-${task.id}`}
              onClick={() => setDetailTask(task)}
            >
              {t('history.actionDetail')}
            </Button>
            {canDownload && (
              <Button
                size="small"
                icon={<DownloadOutlined aria-hidden />}
                loading={downloadingId === task.id}
                onClick={() => void handleDownload(task)}
              >
                {t('history.actionDownload')}
              </Button>
            )}
            {canDownload && (
              <Button size="small" icon={<CopyOutlined aria-hidden />} onClick={() => void handleCopyLink(task)}>
                {t('history.actionCopy')}
              </Button>
            )}
            <Button
              size="small"
              icon={<RedoOutlined aria-hidden />}
              data-testid={`history-reuse-${task.id}`}
              onClick={() => handleReuse(task)}
            >
              {t('history.actionReuse')}
            </Button>
            <Button
              size="small"
              danger
              icon={<DeleteOutlined aria-hidden />}
              data-testid={`history-delete-${task.id}`}
              onClick={() => handleDelete(task)}
            >
              {t('history.actionDelete')}
            </Button>
          </Space>
        )
      },
    },
  ]

  return (
    <div>
      <Flex justify="space-between" align="center" style={{ marginBottom: 16 }}>
        <Title level={3} style={{ margin: 0 }}>
          {t('nav.history')}
        </Title>
        <Space>
          <Button icon={<ExportOutlined aria-hidden />} onClick={handleExport}>
            {t('history.exportJson')}
          </Button>
          <Button
            danger
            icon={<ClearOutlined aria-hidden />}
            disabled={total === 0}
            onClick={() => setClearOpen(true)}
          >
            {t('history.clearAll')}
          </Button>
        </Space>
      </Flex>

      <Table<GenerationTask>
        rowKey="id"
        size="middle"
        loading={loading}
        columns={columns}
        dataSource={items}
        locale={{
          emptyText: (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={t('history.empty')}
              style={{ padding: '32px 0' }}
            />
          ),
        }}
        pagination={{
          current: page,
          pageSize,
          total,
          showSizeChanger: true,
          onChange: (p, ps) => {
            setPage(p)
            setPageSize(ps)
          },
        }}
      />

      <HistoryDetailModal task={detailTask} onClose={() => setDetailTask(null)} />

      <Modal
        open={clearOpen}
        title={t('history.clearConfirmTitle')}
        onCancel={() => {
          setClearOpen(false)
          setClearInput('')
        }}
        onOk={() => void handleClearAll()}
        confirmLoading={clearSubmitting}
        okText={t('common.confirm')}
        cancelText={t('common.cancel')}
        okButtonProps={{ danger: true, disabled: clearInput !== CLEAR_CONFIRM_WORD, autoInsertSpace: false }}
        cancelButtonProps={{ autoInsertSpace: false }}
        destroyOnHidden
      >
        <Paragraph>{t('history.clearConfirmContent', { count: total })}</Paragraph>
        <Paragraph type="secondary">{t('history.clearConfirmHint', { word: CLEAR_CONFIRM_WORD })}</Paragraph>
        <Input
          value={clearInput}
          onChange={(e) => setClearInput(e.target.value)}
          placeholder={CLEAR_CONFIRM_WORD}
          data-testid="clear-confirm-input"
        />
      </Modal>
    </div>
  )
}
