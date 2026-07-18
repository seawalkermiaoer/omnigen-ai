import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import LoginPage from './LoginPage'
import { useAuthStore } from '@/stores/auth'
import { ApiError } from '@/api/client'

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

function renderLogin() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <MemoryRouter>
            <LoginPage />
          </MemoryRouter>
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('LoginPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useAuthStore.setState({ token: null, user: null, initializing: false })
  })

  it('渲染用户名、密码与提交按钮', () => {
    renderLogin()
    expect(screen.getByLabelText(i18n.t('login.username'))).toBeInTheDocument()
    expect(screen.getByLabelText(i18n.t('login.password'))).toBeInTheDocument()
    expect(screen.getByRole('button', { name: i18n.t('login.submit') })).toBeInTheDocument()
  })

  it('空表单提交时展示校验错误且不调用登录', async () => {
    const login = vi.fn()
    useAuthStore.setState({ login } as never)
    const user = userEvent.setup()
    renderLogin()

    await user.click(screen.getByRole('button', { name: i18n.t('login.submit') }))

    expect(await screen.findByText(i18n.t('login.usernameRequired'))).toBeInTheDocument()
    expect(login).not.toHaveBeenCalled()
  })

  it('填写完整时调用登录并跳转首页', async () => {
    const login = vi.fn().mockResolvedValue(undefined)
    useAuthStore.setState({ login } as never)
    const user = userEvent.setup()
    renderLogin()

    await user.type(screen.getByLabelText(i18n.t('login.username')), 'alice')
    await user.type(screen.getByLabelText(i18n.t('login.password')), 'password123')
    await user.click(screen.getByRole('button', { name: i18n.t('login.submit') }))

    await waitFor(() => {
      expect(login).toHaveBeenCalledWith({ username: 'alice', password: 'password123' })
    })
    await waitFor(() => expect(mockNavigate).toHaveBeenCalled())
  })

  // 用户必须看到「用户名或密码错误」，而不是 AUTH_INVALID_CREDENTIALS。
  it('登录失败时展示翻译后的错误文案', async () => {
    const login = vi.fn().mockRejectedValue(new ApiError('AUTH_INVALID_CREDENTIALS', 401))
    useAuthStore.setState({ login } as never)
    const user = userEvent.setup()
    renderLogin()

    await user.type(screen.getByLabelText(i18n.t('login.username')), 'alice')
    await user.type(screen.getByLabelText(i18n.t('login.password')), 'wrongpass')
    await user.click(screen.getByRole('button', { name: i18n.t('login.submit') }))

    expect(await screen.findByText(i18n.t('errors.AUTH_INVALID_CREDENTIALS'))).toBeInTheDocument()
    expect(mockNavigate).not.toHaveBeenCalled()
  })

  it('提供语言切换', async () => {
    renderLogin()
    expect(screen.getByTestId('locale-switch')).toBeInTheDocument()
  })
})
