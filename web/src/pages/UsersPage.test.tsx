import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import UsersPage from './UsersPage'
import { userApi } from '@/api/user'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types/user'

vi.mock('@/api/user', () => ({
  userApi: {
    list: vi.fn(), create: vi.fn(), update: vi.fn(),
    resetPassword: vi.fn(), remove: vi.fn(),
  },
}))

const admin: User = {
  id: 1, username: 'admin', displayName: '管理员', role: 'admin',
  status: 'active', createdAt: '2026-07-18T00:00:00Z', updatedAt: '2026-07-18T00:00:00Z',
}
const bob: User = { ...admin, id: 2, username: 'bob', displayName: 'Bob', role: 'user' }

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <UsersPage />
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('UsersPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    vi.mocked(userApi.list).mockResolvedValue({ total: 2, items: [admin, bob] })
  })

  it('加载并展示用户列表', async () => {
    renderPage()
    expect(await screen.findByText('admin')).toBeInTheDocument()
    expect(screen.getByText('bob')).toBeInTheDocument()
  })

  it('展示角色与状态的翻译文案而非原始枚举值', async () => {
    renderPage()
    await screen.findByText('admin')
    expect(screen.getAllByText(i18n.t('users.roleAdmin')).length).toBeGreaterThan(0)
    expect(screen.getAllByText(i18n.t('users.statusActive')).length).toBeGreaterThan(0)
  })

  it('创建用户后刷新列表', async () => {
    vi.mocked(userApi.create).mockResolvedValue({ ...bob, id: 3, username: 'carol' })
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('admin')

    await user.click(screen.getByRole('button', { name: i18n.t('users.create') }))
    await user.type(await screen.findByLabelText(i18n.t('users.username')), 'carol')
    await user.type(screen.getByLabelText(i18n.t('login.password')), 'password123')
    await user.click(screen.getByRole('button', { name: i18n.t('common.confirm') }))

    await waitFor(() => expect(userApi.create).toHaveBeenCalled())
    await waitFor(() => expect(userApi.list).toHaveBeenCalledTimes(2))
  })

  // 后端已有护栏，前端也要藏起来，避免用户点了才被拒绝。
  it('不为当前登录用户自己渲染删除按钮', async () => {
    renderPage()
    await screen.findByText('admin')

    const rows = screen.getAllByRole('row')
    const adminRow = rows.find((r) => r.textContent?.includes('admin'))
    expect(adminRow?.querySelector('[data-testid="delete-user-1"]')).toBeNull()

    const bobRow = rows.find((r) => r.textContent?.includes('bob'))
    expect(bobRow?.querySelector('[data-testid="delete-user-2"]')).not.toBeNull()
  })
})
