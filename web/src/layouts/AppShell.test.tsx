import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { colors, omnigenTheme } from '@/theme'
import AppShell from './AppShell'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types/user'

const admin: User = {
  id: 1, username: 'admin', displayName: '管理员', role: 'admin',
  status: 'active', createdAt: '2026-07-19T00:00:00Z', updatedAt: '2026-07-19T00:00:00Z',
  quotaTotal: null, quotaUsed: 0,
}
const bob: User = { ...admin, id: 2, username: 'bob', displayName: 'Bob', role: 'user' }

function renderShell() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <MemoryRouter initialEntries={['/imggen']}>
            <Routes>
              <Route element={<AppShell />}>
                <Route path="/imggen" element={<div>占位内容</div>} />
              </Route>
            </Routes>
          </MemoryRouter>
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('AppShell 导航', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('管理员能看到设置入口', () => {
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    renderShell()
    expect(screen.getByText(i18n.t('nav.settings'))).toBeInTheDocument()
  })

  it('普通用户看不到设置入口', () => {
    useAuthStore.setState({ token: 'tok', user: bob, initializing: false })
    renderShell()
    expect(screen.queryByText(i18n.t('nav.settings'))).not.toBeInTheDocument()
  })

  // 回归测试：导航顺序必须与旧系统 public/index.html + zh-CN.json 的侧栏
  // 顺序完全一致（r2v → i2v → t2v → imggen → imgedit → history → users →
  // settings），这是用户的肌肉记忆——断言的是顺序本身，不是"这些项都在"。
  it('导航顺序与旧系统完全一致', () => {
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    renderShell()

    const expectedOrder = [
      i18n.t('nav.r2v'),
      i18n.t('nav.i2v'),
      i18n.t('nav.t2v'),
      i18n.t('nav.imggen'),
      i18n.t('nav.imgedit'),
      i18n.t('nav.history'),
      i18n.t('nav.users'),
      i18n.t('nav.settings'),
    ]

    const menuItems = screen.getAllByRole('menuitem')
    expect(menuItems.map((el) => el.textContent)).toEqual(expectedOrder)
  })
})

describe('AppShell 剩余额度', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('不限量用户不显示剩余次数', () => {
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    renderShell()
    expect(screen.queryByTestId('quota-remaining')).not.toBeInTheDocument()
  })

  it('限量用户显示剩余次数', () => {
    const limited: User = { ...bob, quotaTotal: 100, quotaUsed: 30 }
    useAuthStore.setState({ token: 'tok', user: limited, initializing: false })
    renderShell()
    expect(screen.getByTestId('quota-remaining')).toHaveTextContent(
      i18n.t('users.quotaRemaining', { count: 70 }),
    )
  })

  // 剩余 <=10 时用警告色，颜色是唯一区分手段之外还有数字本身可读——
  // 不是纯用颜色传达信息（数字文案已经说明了剩余多少）。
  it('剩余额度 <=10 时使用警告色', () => {
    const nearlyExhausted: User = { ...bob, quotaTotal: 100, quotaUsed: 95 }
    useAuthStore.setState({ token: 'tok', user: nearlyExhausted, initializing: false })
    renderShell()
    expect(screen.getByTestId('quota-remaining')).toHaveStyle({ color: colors.warning })
  })

  it('剩余额度充足时不使用警告色', () => {
    const plenty: User = { ...bob, quotaTotal: 100, quotaUsed: 10 }
    useAuthStore.setState({ token: 'tok', user: plenty, initializing: false })
    renderShell()
    expect(screen.getByTestId('quota-remaining')).toHaveStyle({ color: colors.textMuted })
  })

  // 管理员可以把 quotaTotal 改到低于当前 quotaUsed（后端只校验 quotaTotal
  // >= 0，不要求 >= quotaUsed），此时 total - used 是负数——展示层必须夹到
  // 0，否则会显示"剩余 -5 次"，读起来像系统故障而不是一次正常的额度调整。
  it('quotaUsed 超过 quotaTotal 时剩余次数显示为 0 而非负数', () => {
    const overUsed: User = { ...bob, quotaTotal: 10, quotaUsed: 15 }
    useAuthStore.setState({ token: 'tok', user: overUsed, initializing: false })
    renderShell()
    expect(screen.getByTestId('quota-remaining')).toHaveTextContent(
      i18n.t('users.quotaRemaining', { count: 0 }),
    )
    expect(screen.getByTestId('quota-remaining')).toHaveStyle({ color: colors.warning })
  })
})

/**
 * 回归测试覆盖的 bug：账号菜单此前没传 trigger，用的是 antd Dropdown 默认的
 * ['hover']——鼠标只是路过顶栏右上角就会把菜单展开，而这个菜单里装着
 * 「登出」这种破坏性操作。窄视口下按钮离视口右边缘只有 20px，鼠标移向屏幕
 * 右侧的路径必然反复扫过它，浮层反复弹出收起，观感上就是「一 hover 就闪」。
 */
describe('AppShell 账号菜单的触发方式', () => {
  beforeEach(() => {
    localStorage.clear()
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
  })

  it('hover 不展开菜单', async () => {
    const user = userEvent.setup()
    renderShell()

    await user.hover(document.querySelector('.shell-user') as HTMLElement)
    // 必须等过 antd Dropdown 的 mouseEnterDelay（默认 0.15s）再断言。
    // 少了这一步，菜单在 hover 触发模式下也还没来得及打开，断言必然通过——
    // 这个测试就会变成一个恒绿的摆设，钉不住任何东西（写这条测试时就先踩了
    // 一次：移除 trigger={['click']} 后它照样全绿）。
    await new Promise((r) => setTimeout(r, 400))

    expect(screen.queryByText(i18n.t('common.logout'))).not.toBeInTheDocument()
  })

  it('click 才展开菜单', async () => {
    const user = userEvent.setup()
    renderShell()

    await user.click(document.querySelector('.shell-user') as HTMLElement)

    expect(await screen.findByText(i18n.t('common.logout'))).toBeInTheDocument()
  })
})
