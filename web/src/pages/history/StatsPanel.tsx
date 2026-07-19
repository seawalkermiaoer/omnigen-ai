import { useCallback, useEffect, useState } from 'react'
import {
  Card,
  Collapse,
  DatePicker,
  Empty,
  Flex,
  Row,
  Col,
  Segmented,
  Select,
  Space,
  Spin,
  Statistic,
  Table,
  Tooltip,
  Typography,
} from 'antd'
import { InfoCircleOutlined } from '@ant-design/icons'
import dayjs, { type Dayjs } from 'dayjs'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import type { ColumnsType } from 'antd/es/table'

import { statsApi } from '@/api/stats'
import { userApi } from '@/api/user'
import { useApiError } from '@/hooks/useApiError'
import { useAuthStore } from '@/stores/auth'
import { colors } from '@/theme'
import type { StatsByDay, StatsByModel, StatsQuery, StatsReport } from '@/types/stats'
import type { TaskMode } from '@/types/generation'
import type { User } from '@/types/user'
import { modeLabel, VIDEO_MODES } from './historyFormat'

const { Text } = Typography
const { RangePicker } = DatePicker

type Preset = 'last7' | 'last30' | 'all' | 'custom'

const EMPTY_REPORT: StatsReport = {
  overview: {
    totalCalls: 0,
    succeededCalls: 0,
    failedCalls: 0,
    totalTokens: 0,
    tokensAvailable: false,
    videoSeconds: 0,
  },
  byModel: [],
  byDay: [],
}

/**
 * 把筛选预设换算成 GET /api/stats 的 from/to 查询参数。
 *
 * 与后端 stats.Query 的注释保持一致的语义：左闭右开区间 [from, to)。
 * "全部"与尚未选定的"自定义"都不设边界，等价于不筛选。
 */
function presetRange(preset: Preset, custom: [Dayjs, Dayjs] | null): { from?: string; to?: string } {
  if (preset === 'all') return {}
  if (preset === 'custom') {
    if (!custom) return {}
    const [start, end] = custom
    return {
      from: start.startOf('day').toISOString(),
      // 结束日期要整天纳入，就要把上界推到"结束日期的次日零点"——
      // 半开区间下这是唯一能把结束当天完整算进去、又不会多算进下一天的写法。
      to: end.startOf('day').add(1, 'day').toISOString(),
    }
  }
  const days = preset === 'last7' ? 7 : 30
  return { from: dayjs().subtract(days - 1, 'day').startOf('day').toISOString() }
}

/** 向上取整到一个"整齐"的刻度上限（1/2/5 × 10^n），供 Y 轴使用。 */
function niceCeil(value: number): number {
  if (value <= 0) return 1
  const exponent = Math.floor(Math.log10(value))
  const magnitude = 10 ** exponent
  const residual = value / magnitude
  let niceResidual: number
  if (residual <= 1) niceResidual = 1
  else if (residual <= 2) niceResidual = 2
  else if (residual <= 5) niceResidual = 5
  else niceResidual = 10
  return niceResidual * magnitude
}

/**
 * 画一根柱子（或堆叠柱的一段）的 path：只有背离基线的"数据端"是圆角
 * （4px），贴基线或贴相邻分段的一端始终是直角——这是 dataviz skill 的
 * mark spec："4px rounded data-end, square at the baseline"，堆叠内部的
 * 分隔靠 2px 表面间隙而不是圆角或描边。
 */
function barPath(x: number, y: number, w: number, h: number, roundTop: boolean): string {
  if (h <= 0 || w <= 0) return ''
  const r = roundTop ? Math.min(4, w / 2, h) : 0
  if (r <= 0) {
    return `M${x},${y} H${x + w} V${y + h} H${x} Z`
  }
  return [
    `M${x},${y + h}`,
    `V${y + r}`,
    `Q${x},${y} ${x + r},${y}`,
    `H${x + w - r}`,
    `Q${x + w},${y} ${x + w},${y + r}`,
    `V${y + h}`,
    'Z',
  ].join(' ')
}

interface LegendSwatchProps {
  color: string
  label: string
}

function LegendSwatch({ color, label }: LegendSwatchProps) {
  return (
    <Space size={6}>
      <span
        aria-hidden
        style={{ width: 10, height: 10, borderRadius: 2, background: color, display: 'inline-block' }}
      />
      <Text type="secondary" style={{ fontSize: 12 }}>
        {label}
      </Text>
    </Space>
  )
}

interface ByDayChartProps {
  data: StatsByDay[]
  t: TFunction
}

/**
 * 按天调用趋势——堆叠柱状图（成功/失败），dataviz skill 的选型：
 * "part-to-whole per category" 用 stacked bar，成功/失败是状态语义
 * （good/critical），颜色直接取主题里已有的 success/error token，不
 * 新引入色值（全系统颜色唯一定义处是 theme/index.ts）。
 *
 * 没有图表库依赖，手写一个轻量 SVG：viewBox 固定坐标系配合
 * width:100% 做响应式缩放；hover/focus 都能触发同一个 tooltip
 * （键盘可达，不是纯 hover-only）；图表下方另有可展开的数据表格，
 * 给不方便看图的场景一个等价的文本入口。
 */
function ByDayChart({ data, t }: ByDayChartProps) {
  const [hoverIndex, setHoverIndex] = useState<number | null>(null)

  if (data.length === 0) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('stats.byDayEmpty')} style={{ padding: '24px 0' }} />
  }

  const width = 800
  const height = 220
  const paddingLeft = 40
  const paddingRight = 12
  const paddingTop = 12
  const paddingBottom = 24
  const plotWidth = width - paddingLeft - paddingRight
  const plotHeight = height - paddingTop - paddingBottom

  const maxCalls = Math.max(1, ...data.map((d) => d.calls))
  const niceMax = niceCeil(maxCalls)
  const barSlot = plotWidth / data.length
  const barWidth = Math.min(24, barSlot * 0.6)
  const gap = 2 // 表面间隙：把同一天的成功/失败两段隔开，而不是描边分隔

  const yFor = (v: number) => paddingTop + plotHeight * (1 - v / niceMax)
  const baselineY = yFor(0)
  const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => Math.round(niceMax * f))
  // 数据点多于 8 个时按步长抽样显示 X 轴标签，避免日期挤在一起不可读。
  const labelStep = Math.max(1, Math.ceil(data.length / 8))

  return (
    <div>
      <Space size={16} style={{ marginBottom: 8 }}>
        <LegendSwatch color={colors.success} label={t('stats.legendSucceeded')} />
        <LegendSwatch color={colors.error} label={t('stats.legendFailed')} />
      </Space>

      <svg
        viewBox={`0 0 ${width} ${height}`}
        style={{ width: '100%', height: 'auto', display: 'block' }}
        role="img"
        aria-label={t('stats.chartAriaLabel')}
      >
        {ticks.map((tick) => (
          <g key={tick}>
            <line x1={paddingLeft} x2={width - paddingRight} y1={yFor(tick)} y2={yFor(tick)} stroke={colors.border} strokeWidth={1} />
            <text x={paddingLeft - 8} y={yFor(tick)} textAnchor="end" dominantBaseline="middle" fontSize={11} fill={colors.textMuted}>
              {tick}
            </text>
          </g>
        ))}

        {data.map((d, i) => {
          const cx = paddingLeft + barSlot * i + barSlot / 2
          const x = cx - barWidth / 2
          const succeededHeight = baselineY - yFor(d.succeeded)
          const succeededY = baselineY - succeededHeight
          const failedHeight = baselineY - yFor(d.failed)
          const failedY = succeededHeight > 0 ? succeededY - gap - failedHeight : baselineY - failedHeight
          const isHovered = hoverIndex === i
          const succeededPath = barPath(x, succeededY, barWidth, succeededHeight, d.failed === 0)
          const failedPath = barPath(x, failedY, barWidth, failedHeight, true)

          return (
            <g
              key={d.day}
              tabIndex={0}
              role="button"
              aria-label={`${dayjs(d.day).format('YYYY-MM-DD')}: ${t('stats.legendSucceeded')} ${d.succeeded}, ${t('stats.legendFailed')} ${d.failed}`}
              onMouseEnter={() => setHoverIndex(i)}
              onMouseLeave={() => setHoverIndex(null)}
              onFocus={() => setHoverIndex(i)}
              onBlur={() => setHoverIndex(null)}
              style={{ cursor: 'pointer', outline: 'none' }}
            >
              {/* 透明命中区域，比可视柱宽——保证窄柱在密集图表里也有可靠的悬浮/点击范围。 */}
              <rect x={paddingLeft + barSlot * i} y={paddingTop} width={barSlot} height={plotHeight} fill="transparent" />
              {succeededPath && <path d={succeededPath} fill={colors.success} opacity={isHovered ? 1 : 0.9} />}
              {failedPath && <path d={failedPath} fill={colors.error} opacity={isHovered ? 1 : 0.9} />}
            </g>
          )
        })}

        <line x1={paddingLeft} x2={width - paddingRight} y1={baselineY} y2={baselineY} stroke={colors.borderStrong} strokeWidth={1} />

        {data.map((d, i) =>
          i % labelStep === 0 ? (
            <text
              key={d.day}
              x={paddingLeft + barSlot * i + barSlot / 2}
              y={height - 6}
              textAnchor="middle"
              fontSize={11}
              fill={colors.textMuted}
            >
              {dayjs(d.day).format('MM-DD')}
            </text>
          ) : null,
        )}
      </svg>

      {hoverIndex !== null && (
        <div
          role="status"
          data-testid="stats-day-tooltip"
          style={{
            marginTop: 8,
            padding: '6px 10px',
            borderRadius: 6,
            background: colors.bgElevated,
            border: `1px solid ${colors.border}`,
            fontSize: 12,
            display: 'inline-block',
          }}
        >
          <Text strong>{dayjs(data[hoverIndex].day).format('YYYY-MM-DD')}</Text>
          <div>
            <span style={{ color: colors.success }} aria-hidden>●</span> {t('stats.legendSucceeded')}:{' '}
            <Text strong>{data[hoverIndex].succeeded}</Text>
          </div>
          <div>
            <span style={{ color: colors.error }} aria-hidden>●</span> {t('stats.legendFailed')}:{' '}
            <Text strong>{data[hoverIndex].failed}</Text>
          </div>
        </div>
      )}
    </div>
  )
}

/**
 * 用量报表——历史页「报表」Tab 的内容。
 *
 * 三块与后端 stats.Report 一一对应：总览（数字卡片）、按天（时间序列图，
 * 见 ByDayChart）、按模型（表格）。三块共享同一份筛选条件，一次请求
 * 拿回，不在浏览器里拼聚合——后端已经在 SQL 里做完了。
 *
 * 最容易做错的地方：`tokensAvailable === false` 时 Token 列必须显示
 * 「—」而不是 0——视频三种模式的上游根本不返回 token 数据，显示 0 会
 * 让人误以为视频生成免费。这个区分来自后端（JSONB 键是否存在），前端
 * 只负责原样保留，不能因为 `tokens` 恰好也是 0 就把两者混为一谈。
 */
export default function StatsPanel() {
  const { t } = useTranslation()
  const { notify } = useApiError()
  const { isAdmin } = useAuthStore()
  const admin = isAdmin()

  const [preset, setPreset] = useState<Preset>('last7')
  const [customRange, setCustomRange] = useState<[Dayjs, Dayjs] | null>(null)
  const [selectedUserId, setSelectedUserId] = useState<number | 'all'>('all')
  const [users, setUsers] = useState<User[]>([])

  const [report, setReport] = useState<StatsReport>(EMPTY_REPORT)
  const [loading, setLoading] = useState(false)

  // 用户下拉框只有管理员用得上，非管理员完全不渲染这个选择器，也就不需要
  // 拉全部用户列表——避免普通用户的报表页多打一个只有管理员用得上的接口。
  useEffect(() => {
    if (!admin) return
    userApi
      .list({ page: 1, pageSize: 200 })
      .then((res) => setUsers(res.items))
      .catch((err) => notify(err))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [admin])

  const load = useCallback(async () => {
    // 切到"自定义"后、用户还没选定区间之前不发请求——避免用一个"全部"
    // 的隐含语义悄悄覆盖用户刚看到的上一段筛选结果。
    if (preset === 'custom' && !customRange) return

    setLoading(true)
    try {
      const { from, to } = presetRange(preset, customRange)
      const query: StatsQuery = { from, to }
      if (admin && selectedUserId !== 'all') query.userId = selectedUserId
      const resp = await statsApi.getReport(query)
      setReport(resp)
    } catch (err) {
      notify(err)
    } finally {
      setLoading(false)
    }
  }, [preset, customRange, admin, selectedUserId, notify])

  useEffect(() => {
    void load()
  }, [load])

  const byDayTableColumns: ColumnsType<StatsByDay> = [
    { title: t('stats.columnDay'), dataIndex: 'day', key: 'day', render: (v: string) => dayjs(v).format('YYYY-MM-DD') },
    { title: t('stats.columnCalls'), dataIndex: 'calls', key: 'calls' },
    { title: t('stats.legendSucceeded'), dataIndex: 'succeeded', key: 'succeeded' },
    { title: t('stats.legendFailed'), dataIndex: 'failed', key: 'failed' },
  ]

  const byModelColumns: ColumnsType<StatsByModel> = [
    { title: t('stats.columnModel'), dataIndex: 'model', key: 'model' },
    {
      title: t('stats.columnMode'),
      dataIndex: 'mode',
      key: 'mode',
      render: (mode: TaskMode) => modeLabel(mode, t),
    },
    { title: t('stats.columnCalls'), dataIndex: 'calls', key: 'calls' },
    { title: t('stats.columnSucceeded'), dataIndex: 'succeeded', key: 'succeeded' },
    { title: t('stats.columnFailed'), dataIndex: 'failed', key: 'failed' },
    {
      // 表头就注明视频没有 Token 数据，不是只藏在 tooltip 里——见下方
      // Card 里的常驻说明文字，这里的图标 tooltip 是给愿意多看一眼的人。
      title: (
        <Space size={4}>
          {t('stats.columnTokens')}
          <Tooltip title={t('stats.tokensNote')}>
            <InfoCircleOutlined aria-hidden style={{ color: colors.textMuted }} />
          </Tooltip>
        </Space>
      ),
      key: 'tokens',
      render: (_, record) =>
        record.tokensAvailable ? (
          <span
            style={{ fontVariantNumeric: 'tabular-nums' }}
            data-testid={`stats-tokens-cell-${record.model}-${record.mode}`}
          >
            {record.tokens}
          </span>
        ) : (
          <Tooltip title={t('stats.tokensUnavailableTooltip')}>
            <Text type="secondary" data-testid={`stats-tokens-cell-${record.model}-${record.mode}`}>
              —
            </Text>
          </Tooltip>
        ),
    },
    {
      title: t('stats.columnVideoSeconds'),
      key: 'videoSeconds',
      render: (_, record) =>
        VIDEO_MODES.has(record.mode) ? (
          <span style={{ fontVariantNumeric: 'tabular-nums' }}>{record.videoSeconds}s</span>
        ) : (
          <Text type="secondary">—</Text>
        ),
    },
  ]

  return (
    <div>
      <Flex wrap="wrap" gap={12} align="center" style={{ marginBottom: 16 }}>
        <Segmented
          value={preset}
          onChange={(v) => setPreset(v as Preset)}
          options={[
            { label: t('stats.presetLast7'), value: 'last7' },
            { label: t('stats.presetLast30'), value: 'last30' },
            { label: t('stats.presetAll'), value: 'all' },
            { label: t('stats.presetCustom'), value: 'custom' },
          ]}
        />
        {preset === 'custom' && (
          <RangePicker
            value={customRange}
            onChange={(v) => setCustomRange(v && v[0] && v[1] ? [v[0], v[1]] : null)}
            allowClear={false}
          />
        )}
        {admin && (
          <Select<number | 'all'>
            value={selectedUserId}
            onChange={(v) => setSelectedUserId(v)}
            style={{ minWidth: 200 }}
            data-testid="stats-user-select"
            options={[{ label: t('stats.allUsers'), value: 'all' }, ...users.map((u) => ({ label: u.displayName || u.username, value: u.id }))]}
          />
        )}
      </Flex>

      <Spin spinning={loading}>
        <Row gutter={[16, 16]} style={{ marginBottom: 24 }}>
          <Col xs={12} sm={8} md={4}>
            <Card size="small">
              <Statistic title={t('stats.totalCalls')} value={report.overview.totalCalls} />
            </Card>
          </Col>
          <Col xs={12} sm={8} md={4}>
            <Card size="small">
              <Statistic title={t('stats.succeededCalls')} value={report.overview.succeededCalls} valueStyle={{ color: colors.success }} />
            </Card>
          </Col>
          <Col xs={12} sm={8} md={4}>
            <Card size="small">
              <Statistic title={t('stats.failedCalls')} value={report.overview.failedCalls} valueStyle={{ color: colors.error }} />
            </Card>
          </Col>
          <Col xs={12} sm={8} md={4}>
            <Card size="small">
              <Statistic
                title={t('stats.totalTokens')}
                value={report.overview.tokensAvailable ? report.overview.totalTokens : '—'}
                data-testid="stats-overview-tokens"
              />
            </Card>
          </Col>
          <Col xs={12} sm={8} md={4}>
            <Card size="small">
              <Statistic title={t('stats.videoSeconds')} value={report.overview.videoSeconds} suffix="s" />
            </Card>
          </Col>
        </Row>

        <Card title={t('stats.byDayTitle')} size="small" style={{ marginBottom: 24 }}>
          <ByDayChart data={report.byDay} t={t} />
          {report.byDay.length > 0 && (
            <Collapse
              ghost
              style={{ marginTop: 12 }}
              items={[
                {
                  key: 'table',
                  label: t('stats.byDayTableToggle'),
                  children: (
                    <Table<StatsByDay>
                      rowKey="day"
                      size="small"
                      pagination={false}
                      columns={byDayTableColumns}
                      dataSource={report.byDay}
                    />
                  ),
                },
              ]}
            />
          )}
        </Card>

        <Card title={t('stats.byModelTitle')} size="small">
          <Text type="secondary" style={{ display: 'block', marginBottom: 12, fontSize: 12 }}>
            {t('stats.tokensNote')}
          </Text>
          <Table<StatsByModel>
            rowKey={(r) => `${r.model}-${r.mode}`}
            size="small"
            pagination={false}
            columns={byModelColumns}
            dataSource={report.byModel}
            locale={{
              emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('common.empty')} style={{ padding: '24px 0' }} />,
            }}
          />
        </Card>
      </Spin>
    </div>
  )
}
