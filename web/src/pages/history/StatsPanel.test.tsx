import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import StatsPanel from './StatsPanel'
import { statsApi } from '@/api/stats'
import { userApi } from '@/api/user'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types/user'
import type { StatsReport } from '@/types/stats'

vi.mock('@/api/stats', () => ({
  statsApi: { getReport: vi.fn() },
}))

vi.mock('@/api/user', () => ({
  userApi: { list: vi.fn() },
}))

const admin: User = {
  id: 1,
  username: 'admin',
  displayName: '管理员',
  role: 'admin',
  status: 'active',
  createdAt: '2026-07-18T00:00:00Z',
  updatedAt: '2026-07-18T00:00:00Z',
  quotaTotal: null,
  quotaUsed: 0,
}
const normalUser: User = { ...admin, id: 2, username: 'bob', displayName: 'Bob', role: 'user' }

// 三条 byModel 行分别覆盖三种组合：token 可得且非零（图片）、token 可得
// 且恰好为零（图片，另一个模式，与「不可得」区分开）、token 不可得（视频）。
const REPORT: StatsReport = {
  overview: {
    totalCalls: 10,
    succeededCalls: 8,
    failedCalls: 2,
    totalTokens: 500,
    tokensAvailable: true,
    videoSeconds: 120,
  },
  byModel: [
    {
      model: 'qwen-image-plus',
      mode: 'imggen',
      calls: 5,
      succeeded: 4,
      failed: 1,
      tokens: 300,
      tokensAvailable: true,
      videoSeconds: 0,
    },
    {
      model: 'qwen-image-lite',
      mode: 'imgedit',
      calls: 1,
      succeeded: 1,
      failed: 0,
      tokens: 0,
      tokensAvailable: true,
      videoSeconds: 0,
    },
    {
      model: 'happyhorse-t2v',
      mode: 't2v',
      calls: 4,
      succeeded: 3,
      failed: 1,
      tokens: 0,
      tokensAvailable: false,
      videoSeconds: 120,
    },
  ],
  byDay: [
    { day: '2026-07-17T00:00:00Z', calls: 6, succeeded: 5, failed: 1 },
    { day: '2026-07-18T00:00:00Z', calls: 4, succeeded: 3, failed: 1 },
  ],
}

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

function renderPanel() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <StatsPanel />
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('StatsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(statsApi.getReport).mockResolvedValue(REPORT)
    vi.mocked(userApi.list).mockResolvedValue({ total: 2, items: [admin, normalUser] })
  })

  it('渲染总览、按天、按模型三块', async () => {
    useAuthStore.setState({ token: 'tok', user: normalUser, initializing: false })
    renderPanel()

    await screen.findByText('qwen-image-plus')

    // 总览：数字卡片（10 同时也会出现在图表的 Y 轴刻度上，用 antd Statistic
    // 自己的 DOM 结构把断言限定在数字卡片里，避免撞上坐标轴文本）。
    const totalCallsTitle = screen.getByText(i18n.t('stats.totalCalls'))
    const totalCallsCard = totalCallsTitle.closest('.ant-statistic')
    expect(totalCallsCard).not.toBeNull()
    expect(within(totalCallsCard as HTMLElement).getByText('10')).toBeInTheDocument()

    // 按天：时间序列图（有图例与坐标轴文本，视为已渲染）
    expect(screen.getByText(i18n.t('stats.byDayTitle'))).toBeInTheDocument()
    expect(screen.getByRole('img', { name: i18n.t('stats.chartAriaLabel') })).toBeInTheDocument()

    // 按模型：表格
    expect(screen.getByText(i18n.t('stats.byModelTitle'))).toBeInTheDocument()
    expect(screen.getByText('happyhorse-t2v')).toBeInTheDocument()
  })

  // 回归测试：视频行（tokensAvailable: false）必须显示「—」，不是 0——
  // 否则会让人误以为视频生成不消耗任何算力。
  it('tokensAvailable 为 false 的行，Token 列显示「—」而不是 0', async () => {
    useAuthStore.setState({ token: 'tok', user: normalUser, initializing: false })
    renderPanel()

    await screen.findByText('happyhorse-t2v')

    const cell = screen.getByTestId('stats-tokens-cell-happyhorse-t2v-t2v')
    expect(cell).toHaveTextContent('—')
    expect(cell).not.toHaveTextContent('0')
  })

  // 反过来验证：tokensAvailable 为 true 且 tokens 恰好为 0 时，必须显示
  // 真实的 0，不能因为"不可得"分支的存在就连带把这行也隐藏成「—」。
  it('tokensAvailable 为 true 且 tokens 为 0 的行，Token 列显示 0', async () => {
    useAuthStore.setState({ token: 'tok', user: normalUser, initializing: false })
    renderPanel()

    await screen.findByText('qwen-image-lite')

    const cell = screen.getByTestId('stats-tokens-cell-qwen-image-lite-imgedit')
    expect(cell).toHaveTextContent('0')
    expect(cell).not.toHaveTextContent('—')
  })

  it('非管理员看不到用户选择器，也不会去拉用户列表', async () => {
    useAuthStore.setState({ token: 'tok', user: normalUser, initializing: false })
    renderPanel()

    await screen.findByText('qwen-image-plus')
    expect(screen.queryByTestId('stats-user-select')).not.toBeInTheDocument()
    expect(userApi.list).not.toHaveBeenCalled()
  })

  it('管理员看到用户选择器，默认选中「全部用户」', async () => {
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    renderPanel()

    await screen.findByText('qwen-image-plus')
    const select = await screen.findByTestId('stats-user-select')
    expect(select).toBeInTheDocument()
    expect(within(select).getByText(i18n.t('stats.allUsers'))).toBeInTheDocument()
  })

  // 回归测试：server/internal/model/user/request.go 的 ListQuery.PageSize
  // 上限是 100（binding:"max=100"），超过就整个请求 422。这里直接断言
  // 组件实际发出的 pageSize 落在后端能接受的范围内——之前发的是 200，
  // 422 被 catch 悄悄吞掉，用户选择器永远只有「全部用户」一个选项，
  // 页面上完全看不出请求失败了。
  it('拉取用户列表的 pageSize 必须在后端接受的范围内（<= 100）', async () => {
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    renderPanel()

    await screen.findByText('qwen-image-plus')
    await waitFor(() => expect(userApi.list).toHaveBeenCalled())
    const call = vi.mocked(userApi.list).mock.calls[0][0]
    expect(call.page).toBe(1)
    expect(call.pageSize).toBeLessThanOrEqual(100)
  })

  // 用户总数超过一次能拉到的上限时，选择器不能装作已经显示了全部用户——
  // 必须有可见提示告诉管理员"只看到前 N 个"，否则会误以为筛选不到的
  // 用户是因为该用户不存在，而不是因为分页上限。
  it('用户总数超过选择器上限时，显示「仅显示前 N 位」提示', async () => {
    vi.mocked(userApi.list).mockResolvedValue({ total: 150, items: [admin, normalUser] })
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    renderPanel()

    await screen.findByText('qwen-image-plus')
    const hint = await screen.findByTestId('stats-user-select-truncated-hint')
    expect(hint).toHaveTextContent('2')
    expect(hint).toHaveTextContent('150')
  })

  it('切换时间预设会重新请求', async () => {
    useAuthStore.setState({ token: 'tok', user: normalUser, initializing: false })
    const user = userEvent.setup()
    renderPanel()

    await screen.findByText('qwen-image-plus')
    expect(statsApi.getReport).toHaveBeenCalledTimes(1)

    await user.click(screen.getByText(i18n.t('stats.presetAll')))

    await waitFor(() => expect(statsApi.getReport).toHaveBeenCalledTimes(2))
    const lastCall = vi.mocked(statsApi.getReport).mock.calls[1][0]
    expect(lastCall.from).toBeUndefined()
    expect(lastCall.to).toBeUndefined()
  })

  it('空数据集渲染空状态而不崩溃', async () => {
    useAuthStore.setState({ token: 'tok', user: normalUser, initializing: false })
    vi.mocked(statsApi.getReport).mockResolvedValue(EMPTY_REPORT)
    renderPanel()

    await screen.findByText(i18n.t('stats.byDayEmpty'))
    expect(screen.getAllByText(i18n.t('common.empty')).length).toBeGreaterThan(0)
    // 总览卡片仍然渲染，只是全是 0。
    expect(screen.getByText(i18n.t('stats.totalCalls'))).toBeInTheDocument()
  })
})

/**
 * 回归测试覆盖的 bug：按天图表的 tooltip 原本是 marginTop:8 的**普通文档流
 * 元素**，渲染在 SVG 下方。两个问题：
 *
 * 1. 位置完全脱离鼠标——用户 hover 右侧的柱子，提示却出现在图表左下角，
 *    要来回移动视线才能把「哪根柱子」和「什么数值」对上。
 * 2. 更糟的是它一出现就占据文档流高度，把下面的内容整体往下顶；鼠标进出
 *    柱子时反复插入/移除，页面就在那里一跳一跳。
 *
 * 改为绝对定位跟随光标。同时 pointerEvents 必须是 none——否则 tooltip 会
 * 盖在光标下方把 mouseleave 抢走：隐藏 → 光标重新落到柱子上 → 又显示，
 * 形成一个自激的闪烁循环，比原来的毛病更难查。
 */
describe('按天图表的 tooltip 跟随光标', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    vi.mocked(statsApi.getReport).mockResolvedValue(REPORT)
    vi.mocked(userApi.list).mockResolvedValue({ total: 2, items: [admin, normalUser] })
  })

  it('tooltip 绝对定位在光标处，且不吃鼠标事件', async () => {
    const user = userEvent.setup()
    renderPanel()
    await screen.findByRole('img', { name: i18n.t('stats.chartAriaLabel') })

    const bar = screen.getAllByRole('button', { name: /2026-07-17/ })[0]
    await user.hover(bar)

    const tip = await screen.findByTestId('stats-day-tooltip')
    expect(tip).toHaveStyle({ position: 'absolute' })
    // 关键：不吃鼠标事件，否则会形成显示/隐藏的自激循环
    expect(tip).toHaveStyle({ pointerEvents: 'none' })
    // 关键：不再占据文档流高度（原来是 marginTop:8 的 inline-block）
    expect(tip.style.marginTop).toBe('')
  })

  it('键盘聚焦同样能出 tooltip（不是纯 hover-only）', async () => {
    renderPanel()
    await screen.findByRole('img', { name: i18n.t('stats.chartAriaLabel') })

    const bar = screen.getAllByRole('button', { name: /2026-07-18/ })[0]
    bar.focus()

    const tip = await screen.findByTestId('stats-day-tooltip')
    expect(within(tip).getByText('2026-07-18')).toBeInTheDocument()
  })
})
