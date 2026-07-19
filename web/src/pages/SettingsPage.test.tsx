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

const BSUTOPIA_ENDPOINT = 'https://openapi.bsutopia.com'

// 一份"部分已配置"的响应：dashscope_api_key 已配置（脱敏展示），其余密钥
// 未配置；region 已选 cn-beijing、dashscope_endpoint 为空（对应"官方"接入
// 方式）；其余非 secret 项均为空——覆盖两种起始状态（已配置 / 未配置），
// 不是所有字段都同一种状态。
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

function withDashscopeEndpoint(value: string): SettingsResponse {
  return {
    items: baseResponse.items.map((item) => (item.key === 'dashscope_endpoint' ? { ...item, value } : item)),
  }
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

  describe('接入方式：从 dashscope_endpoint 派生', () => {
    it('dashscope_endpoint 为空 → 派生为"官方"，展示地域选择', async () => {
      vi.mocked(settingApi.get).mockResolvedValue(withDashscopeEndpoint(''))
      renderPage()
      await screen.findByText(/sk-ab...wxyz/)

      expect(screen.getByRole('radio', { name: i18n.t('settings.accessModeOfficial') })).toBeChecked()
      expect(screen.getByTestId('settings-region-select')).toBeInTheDocument()
      expect(screen.queryByTestId('settings-custom-endpoint')).not.toBeInTheDocument()
      expect(screen.queryByTestId('settings-bsutopia-hint')).not.toBeInTheDocument()
    })

    it('dashscope_endpoint 等于蓝星纪元固定地址 → 派生为"蓝星纪元中转"，隐藏地域', async () => {
      vi.mocked(settingApi.get).mockResolvedValue(withDashscopeEndpoint(BSUTOPIA_ENDPOINT))
      renderPage()
      await screen.findByText(/sk-ab...wxyz/)

      expect(screen.getByRole('radio', { name: i18n.t('settings.accessModeBsutopia') })).toBeChecked()
      expect(screen.queryByTestId('settings-region-select')).not.toBeInTheDocument()
      expect(screen.getByTestId('settings-bsutopia-hint')).toBeInTheDocument()
    })

    it('dashscope_endpoint 为其他值 → 派生为"自定义"，回填到文本框，隐藏地域', async () => {
      vi.mocked(settingApi.get).mockResolvedValue(withDashscopeEndpoint('https://my-custom-relay.example.com'))
      renderPage()
      await screen.findByText(/sk-ab...wxyz/)

      expect(screen.getByRole('radio', { name: i18n.t('settings.accessModeCustom') })).toBeChecked()
      expect(screen.queryByTestId('settings-region-select')).not.toBeInTheDocument()
      const customInput = screen.getByTestId('settings-custom-endpoint') as HTMLInputElement
      expect(customInput.value).toBe('https://my-custom-relay.example.com')
    })
  })

  it('选择"蓝星纪元中转"后隐藏地域，提交时携带固定地址', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    expect(screen.getByTestId('settings-region-select')).toBeInTheDocument()

    await user.click(screen.getByRole('radio', { name: i18n.t('settings.accessModeBsutopia') }))

    expect(screen.queryByTestId('settings-region-select')).not.toBeInTheDocument()

    await user.click(screen.getByTestId('settings-save'))

    await waitFor(() => expect(settingApi.update).toHaveBeenCalledTimes(1))
    const payload = vi.mocked(settingApi.update).mock.calls[0][0]
    const endpointItem = payload.items.find((i) => i.key === 'dashscope_endpoint')
    expect(endpointItem).toEqual({ key: 'dashscope_endpoint', value: BSUTOPIA_ENDPOINT, clear: false })
  })

  it('切回"官方"时地域选择重新出现，此前保存的地域没有被清空', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    await user.click(screen.getByRole('radio', { name: i18n.t('settings.accessModeBsutopia') }))
    expect(screen.queryByTestId('settings-region-select')).not.toBeInTheDocument()

    await user.click(screen.getByRole('radio', { name: i18n.t('settings.accessModeOfficial') }))

    // 切回官方后地域选择器重新出现，且仍然显示切换前的地域（cn-beijing），
    // 没有因为中途隐藏而被清空。
    expect(screen.getByTestId('settings-region-select')).toHaveTextContent(i18n.t('settings.regionCnBeijing'))

    await user.click(screen.getByTestId('settings-save'))

    await waitFor(() => expect(settingApi.update).toHaveBeenCalledTimes(1))
    const payload = vi.mocked(settingApi.update).mock.calls[0][0]
    const regionItem = payload.items.find((i) => i.key === 'region')
    expect(regionItem).toEqual({ key: 'region', value: 'cn-beijing', clear: false })
    const endpointItem = payload.items.find((i) => i.key === 'dashscope_endpoint')
    expect(endpointItem).toEqual({ key: 'dashscope_endpoint', value: '', clear: false })
  })

  it('地域选择只在"官方"接入方式下可见', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    expect(screen.getByTestId('settings-region-select')).toBeInTheDocument()

    await user.click(screen.getByRole('radio', { name: i18n.t('settings.accessModeCustom') }))
    expect(screen.queryByTestId('settings-region-select')).not.toBeInTheDocument()
    expect(screen.getByTestId('settings-custom-endpoint')).toBeInTheDocument()
  })

  it('WorkspaceId 只在"官方 + eu-central-1"下出现，其余三个地域都不显示，且说明文字存在', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    // baseResponse 里 region 起始值就是 cn-beijing——先验证起始状态本身就
    // 不显示 workspace_id，不需要再点一次选择同一个值（antd Select 已选中
    // 的那一项与下拉框选项会共用同一个 title，重复点击会导致定位到两个
    // 元素）。
    expect(screen.queryByTestId('settings-workspace-id')).not.toBeInTheDocument()

    const otherNonFrankfurtRegions = [i18n.t('settings.regionApSoutheast1'), i18n.t('settings.regionUsEast1')]
    for (const label of otherNonFrankfurtRegions) {
      await user.click(screen.getByTestId('settings-region-select'))
      await user.click(await screen.findByTitle(label))
      expect(screen.queryByTestId('settings-workspace-id')).not.toBeInTheDocument()
    }

    await user.click(screen.getByTestId('settings-region-select'))
    await user.click(await screen.findByTitle(i18n.t('settings.regionEuCentral1')))

    expect(screen.getByTestId('settings-workspace-id')).toBeInTheDocument()
    expect(screen.getByText(i18n.t('settings.workspaceIdExplain'))).toBeInTheDocument()
  })

  it('t8star 测试连接成功时展示翻译后的成功文案，且带 provider 参数', async () => {
    const user = userEvent.setup()
    vi.mocked(settingApi.test).mockResolvedValue({ data: { code: 'OK' } } as never)
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    await user.click(screen.getByTestId('settings-test-connection-t8star'))

    expect(await screen.findByTestId('settings-test-result-t8star')).toHaveTextContent(i18n.t('settings.testSuccess'))
    expect(settingApi.test).toHaveBeenCalledWith('t8star')
  })

  it('t8star 测试连接失败时展示翻译文案，而不是裸露的错误码', async () => {
    const user = userEvent.setup()
    vi.mocked(settingApi.test).mockRejectedValue(new ApiError('UPSTREAM_AUTH_FAILED', 502))
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    await user.click(screen.getByTestId('settings-test-connection-t8star'))

    const result = await screen.findByTestId('settings-test-result-t8star')
    expect(result).toHaveTextContent(i18n.t('errors.UPSTREAM_AUTH_FAILED'))
    expect(result.textContent).not.toContain('UPSTREAM_AUTH_FAILED')
  })

  it('两个测试连接按钮各自触发正确的 provider，结果互不覆盖', async () => {
    const user = userEvent.setup()
    vi.mocked(settingApi.test).mockImplementation(((provider: string) =>
      provider === 't8star'
        ? Promise.reject(new ApiError('UPSTREAM_AUTH_FAILED', 502))
        : Promise.resolve({ data: { code: 'OK' } })) as never)
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    // 先点 DashScope，成功。
    await user.click(screen.getByTestId('settings-test-connection'))
    expect(await screen.findByTestId('settings-test-result')).toHaveTextContent(i18n.t('settings.testSuccess'))

    // 再点 t8star，失败——DashScope 那条成功结果不应该被清掉或覆盖。
    await user.click(screen.getByTestId('settings-test-connection-t8star'))
    expect(await screen.findByTestId('settings-test-result-t8star')).toHaveTextContent(
      i18n.t('errors.UPSTREAM_AUTH_FAILED'),
    )
    expect(screen.getByTestId('settings-test-result')).toHaveTextContent(i18n.t('settings.testSuccess'))

    expect(settingApi.test).toHaveBeenNthCalledWith(1, 'dashscope')
    expect(settingApi.test).toHaveBeenNthCalledWith(2, 't8star')
  })

  it('t8star 按钮带费用说明，百炼按钮不带', async () => {
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    const t8starCard = screen.getByText(i18n.t('settings.cardT8starTitle')).closest('.ant-card') as HTMLElement
    expect(t8starCard.querySelector('[data-testid="settings-t8star-test-cost-note"]')).toHaveTextContent(
      i18n.t('settings.t8starTestCostNote'),
    )

    const dashscopeCard = screen.getByText(i18n.t('settings.cardDashscopeTitle')).closest('.ant-card') as HTMLElement
    expect(dashscopeCard.textContent).not.toContain(i18n.t('settings.t8starTestCostNote'))
    expect(dashscopeCard.querySelector('[data-testid="settings-t8star-test-cost-note"]')).toBeNull()
  })

  it('t8star 卡片不渲染地域或 WorkspaceId 控件', async () => {
    renderPage()
    await screen.findByText(/sk-ab...wxyz/)

    const t8starCard = screen.getByText(i18n.t('settings.cardT8starTitle')).closest('.ant-card') as HTMLElement
    expect(t8starCard).toBeTruthy()
    expect(t8starCard.querySelector('[data-testid="settings-region-select"]')).toBeNull()
    expect(t8starCard.querySelector('[data-testid="settings-workspace-id"]')).toBeNull()
    expect(t8starCard.textContent).not.toContain(i18n.t('settings.accessMode'))
  })
})
