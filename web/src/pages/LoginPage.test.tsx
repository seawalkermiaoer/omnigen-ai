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

/**
 * 回归测试覆盖的改动：左栏原先摆着六个纯 CSS 渐变方块（3×2 网格），
 * 视觉上像是作品示例，实际上不是任何真实产出——是假内容。而真正有信息量
 * 的三项能力被压在最底下当灰色小字。
 *
 * 现在能力提到主位（标题 + 一行说明 + 图标），装饰降级为背景环境光。
 * 这条测试钉住两件事：能力说明确实渲染出来了，以及假方块没有回来。
 */
describe('登录页左栏展示真实能力，而不是假作品格', () => {
  it('三项能力各有标题与说明', () => {
    renderLogin()

    for (const key of ['featureImage', 'featureVideo', 'featureHistory'] as const) {
      expect(screen.getByText(i18n.t(`login.${key}`))).toBeInTheDocument()
      expect(screen.getByText(i18n.t(`login.${key}Desc`))).toBeInTheDocument()
    }
  })

  it('不再渲染任何装饰性作品占位方块', () => {
    const { container } = renderLogin()

    expect(container.querySelector('.login-brand__grid')).toBeNull()
    expect(container.querySelectorAll('.login-brand__tile')).toHaveLength(0)
  })

  // 品牌标记从纯 CSS 渐变方块换成了内联 SVG（components/BrandMark.tsx）。
  // 用户反馈时截图里还是旧方块，怀疑过是没渲染出来——这条测试用来排除
  // 「组件根本没渲染」这种可能，把问题定位到浏览器缓存/热更新那一侧。
  it('渲染的是 BrandMark 内联 SVG，不是 CSS 色块', () => {
    const { container } = renderLogin()

    const mark = container.querySelector('.login-brand__logo svg')
    expect(mark).not.toBeNull()
    expect(container.querySelector('.login-brand__mark')).toBeNull()
  })
})
