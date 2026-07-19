import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import AppShell from './AppShell'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types/user'

const admin: User = {
  id: 1, username: 'admin', displayName: '管理员', role: 'admin',
  status: 'active', createdAt: '2026-07-19T00:00:00Z', updatedAt: '2026-07-19T00:00:00Z',
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
