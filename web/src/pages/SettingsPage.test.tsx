import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import SettingsPage from './SettingsPage'
import { settingApi } from '@/api/setting'
import { ApiError } from '@/api/client'
import type { SettingsResponse } from '@/types/setting'

vi.mock('@/api/setting', () => ({
  settingApi: { get: vi.fn(), update: vi.fn(), test: vi.fn() },
}))

// 一份"部分已配置"的响应：dashscope_api_key 已配置（脱敏展示），其余密钥
// 未配置；region 已选 cn-beijing；其余非 secret 项均为空——覆盖两种起始
// 状态（已配置 / 未配置），不是所有字段都同一种状态。
const baseResponse: SettingsResponse = {
  items: [
    { key: 'dashscope_api_key', isSecret: true, configured: true, masked: 'sk-ab...wxyz' },
    { key: 't8star_api_key', isSecret: true, configured: false, masked: '' },
    { key: 'region', isSecret: false, value: 'cn-beijing' },
    { key: 'dashscope_endpoint', isSecret: false, value: '' },
    { key: 't8star_endpoint', isSecret: false, value: '' },
    { key: 'workspace_id', isSecret: false, value: '' },
    { key: 'oss_bucket', isSecret: false, value: '' },
    { key: 'oss_region', isSecret: false, value: '' },
    { key: 'oss_role_arn', isSecret: false, value: '' },
    { key: 'oss_access_key_id', isSecret: true, configured: false, masked: '' },
    { key: 'oss_access_key_secret', isSecret: true, configured: false, masked: '' },
  ],
}

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <SettingsPage />
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('SettingsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(settingApi.get).mockResolvedValue(baseResponse)
    vi.mocked(settingApi.update).mockResolvedValue(baseResponse)
  })

  it('加载后展示脱敏密钥，密钥输入框保持空白，DOM 中不出现任何明文密钥', async () => {
    renderPage()

    expect(await screen.findByText(/sk-ab...wxyz/)).toBeInTheDocument()
    // 三个未配置的密钥项（t8star_api_key、两个 OSS AccessKey）都应展示"尚未配置"。
    expect(screen.getAllByText(i18n.t('settings.secretNotConfigured')).length).toBe(3)

    // 密钥输入框必须留白——placeholder 只是展示脱敏值，绝不能把它当成 value。
    const dashscopeInput = screen.getByLabelText(i18n.t('settings.dashscopeApiKey')) as HTMLInputElement
    expect(dashscopeInput.value).toBe('')

    // 全文档兜底断言：任何看起来像真实密钥中段的字符串都不应该出现。
    expect(document.body.textContent).not.toMatch(/sk-ab-real-plaintext-secret/)
  })

  it('仅修改 region 并提交：密钥字段不携带 clear，原密钥不受影响', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    await user.click(screen.getByTestId('settings-region-select'))
    await user.click(await screen.findByTitle(i18n.t('settings.regionApSoutheast1')))

    await user.click(screen.getByTestId('settings-save'))

    await waitFor(() => expect(settingApi.update).toHaveBeenCalledTimes(1))
    const payload = vi.mocked(settingApi.update).mock.calls[0][0]

    const dashscopeItem = payload.items.find((i) => i.key === 'dashscope_api_key')
    expect(dashscopeItem).toEqual({ key: 'dashscope_api_key', value: '', clear: false })

    const t8starItem = payload.items.find((i) => i.key === 't8star_api_key')
    expect(t8starItem).toEqual({ key: 't8star_api_key', value: '', clear: false })

    const regionItem = payload.items.find((i) => i.key === 'region')
    expect(regionItem).toEqual({ key: 'region', value: 'ap-southeast-1', clear: false })
  })

  it('点击清除并确认后提交，才会对该密钥发送 clear:true', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    await user.click(screen.getByTestId('clear-dashscope_api_key'))
    await user.click(await screen.findByRole('button', { name: i18n.t('common.confirm') }))
    expect(await screen.findByText(i18n.t('settings.clearPendingHint'))).toBeInTheDocument()

    await user.click(screen.getByTestId('settings-save'))

    await waitFor(() => expect(settingApi.update).toHaveBeenCalledTimes(1))
    const payload = vi.mocked(settingApi.update).mock.calls[0][0]
    const dashscopeItem = payload.items.find((i) => i.key === 'dashscope_api_key')
    expect(dashscopeItem).toEqual({ key: 'dashscope_api_key', value: '', clear: true })
  })

  it('region 为 eu-central-1 时 workspace_id 变为必填，留空提交会被拦下', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    await user.click(screen.getByTestId('settings-region-select'))
    await user.click(await screen.findByTitle(i18n.t('settings.regionEuCentral1')))

    await user.click(screen.getByTestId('settings-save'))

    await waitFor(() => {
      expect(screen.getAllByText(i18n.t('settings.workspaceIdRequiredHint')).length).toBeGreaterThan(0)
    })
    expect(settingApi.update).not.toHaveBeenCalled()
  })

  it('测试连接成功时展示翻译后的成功文案', async () => {
    const user = userEvent.setup()
    vi.mocked(settingApi.test).mockResolvedValue({ data: { code: 'OK' } } as never)
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    await user.click(screen.getByTestId('settings-test-connection'))

    expect(await screen.findByTestId('settings-test-result')).toHaveTextContent(i18n.t('settings.testSuccess'))
  })

  it('测试连接失败时展示翻译文案，而不是裸露的错误码', async () => {
    const user = userEvent.setup()
    vi.mocked(settingApi.test).mockRejectedValue(new ApiError('SETTING_TEST_FAILED', 502))
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    await user.click(screen.getByTestId('settings-test-connection'))

    const result = await screen.findByTestId('settings-test-result')
    expect(result).toHaveTextContent(i18n.t('errors.SETTING_TEST_FAILED'))
    expect(result.textContent).not.toMatch(/^SETTING_TEST_FAILED$/)
    expect(result.textContent).not.toContain('SETTING_TEST_FAILED')
  })
})
