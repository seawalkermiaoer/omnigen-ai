import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, type InitialEntry } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import ImageGenPage from './ImageGenPage'
import { catalogApi, generationApi } from '@/api/generation'
import { ApiError } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { allModels, qwenImage, wanImage } from '@/components/generation/__fixtures__/catalog'
import type { GenerationTask, ReuseGenerationState } from '@/types/generation'

vi.mock('@/api/generation', () => ({
  catalogApi: { list: vi.fn() },
  generationApi: {
    generateImage: vi.fn(),
    optimizePrompt: vi.fn(),
    downloadResult: vi.fn(),
    downloadLinkPath: (taskId: number, index: number) => `/download/${taskId}/${index}`,
  },
  uploadApi: { upload: vi.fn() },
}))

const admin = {
  id: 1, username: 'admin', displayName: '管理员', role: 'admin' as const,
  status: 'active' as const, createdAt: '2026-07-19T00:00:00Z', updatedAt: '2026-07-19T00:00:00Z',
  quotaTotal: null, quotaUsed: 0,
}
const bob = { ...admin, id: 2, username: 'bob', displayName: 'Bob', role: 'user' as const }

const succeededTask: GenerationTask = {
  id: 10,
  mode: 'imggen',
  model: 'qwen-image',
  status: 'SUCCEEDED',
  prompt: '一只猫',
  params: {},
  inputUrls: [],
  resultUrls: ['https://example.com/1.png'],
  createdAt: '2026-07-19T00:00:00Z',
  updatedAt: '2026-07-19T00:00:00Z',
}

function renderPage(initialEntries: InitialEntry[] = ['/imggen']) {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <MemoryRouter initialEntries={initialEntries}>
            <ImageGenPage />
          </MemoryRouter>
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('ImageGenPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    vi.mocked(catalogApi.list).mockResolvedValue({ models: allModels })
  })

  it('模型下拉只展示具备 t2i 能力的模型，不含 edit-only 或 i2v 模型', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('param-panel')
    await user.click(screen.getByTestId('model-select'))

    // rc-virtual-list 在 jsdom 下只挂载能填满一个（恒为 0 的）视口高度的选项，
    // 与目录过滤逻辑无关——只断言前两个匹配项存在、非 t2i 模型不存在，
    // 与 ModelSelect.test.tsx 的既有做法一致。
    const listbox = await screen.findByRole('listbox')
    expect(within(listbox).getByText('qwen-image')).toBeInTheDocument()
    expect(within(listbox).getByText('wan2.7-image')).toBeInTheDocument()
    expect(within(listbox).queryByText('qwen-image-edit')).not.toBeInTheDocument()
    expect(within(listbox).queryByText('happyhorse-1.1-i2v')).not.toBeInTheDocument()
  })

  it('prompt 为空时提交按钮禁用，不调用生成接口', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('param-panel')
    const submitBtn = screen.getByRole('button', { name: i18n.t('generation.submit') })
    expect(submitBtn).toBeDisabled()

    await user.click(submitBtn)
    expect(generationApi.generateImage).not.toHaveBeenCalled()
  })

  it('填写 prompt 后提交，调用生成接口并渲染结果（默认选中目录里第一个 t2i 模型）', async () => {
    vi.mocked(generationApi.generateImage).mockResolvedValue(succeededTask)
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('param-panel')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫')
    await user.click(screen.getByRole('button', { name: i18n.t('generation.submit') }))

    await waitFor(() =>
      expect(generationApi.generateImage).toHaveBeenCalledWith(
        expect.objectContaining({ model: qwenImage.ID, prompt: '一只猫' }),
      ),
    )
    expect(await screen.findByTestId('result-image-grid')).toBeInTheDocument()
  })

  it('提交时不携带任何图片字段（图片生成从不附带输入图）', async () => {
    vi.mocked(generationApi.generateImage).mockResolvedValue(succeededTask)
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('param-panel')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫')
    await user.click(screen.getByRole('button', { name: i18n.t('generation.submit') }))

    await waitFor(() => expect(generationApi.generateImage).toHaveBeenCalled())
    const payload = vi.mocked(generationApi.generateImage).mock.calls[0][0]
    expect(payload.images).toBeUndefined()
  })

  it('生成失败时展示翻译后的错误文案，不出现裸错误码', async () => {
    vi.mocked(generationApi.generateImage).mockRejectedValue(new ApiError('VALIDATION_FAILED', 422))
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('param-panel')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫')
    await user.click(screen.getByRole('button', { name: i18n.t('generation.submit') }))

    expect(await screen.findByText(i18n.t('errors.VALIDATION_FAILED'))).toBeInTheDocument()
    expect(screen.queryByText('VALIDATION_FAILED')).not.toBeInTheDocument()
  })

  it('未配置凭证时（SETTING_INCOMPLETE）管理员看到"前往设置"链接', async () => {
    vi.mocked(generationApi.generateImage).mockRejectedValue(new ApiError('SETTING_INCOMPLETE', 422))
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('param-panel')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫')
    await user.click(screen.getByRole('button', { name: i18n.t('generation.submit') }))

    expect(await screen.findByTestId('config-incomplete-alert')).toBeInTheDocument()
    const link = screen.getByRole('link', { name: i18n.t('generation.notConfiguredGoSettings') })
    expect(link).toHaveAttribute('href', '/settings')
  })

  it('未配置凭证时普通用户只被告知联系管理员，没有设置入口', async () => {
    useAuthStore.setState({ token: 'tok', user: bob, initializing: false })
    vi.mocked(generationApi.generateImage).mockRejectedValue(new ApiError('SETTING_INCOMPLETE', 422))
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('param-panel')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫')
    await user.click(screen.getByRole('button', { name: i18n.t('generation.submit') }))

    expect(await screen.findByTestId('config-incomplete-alert')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: i18n.t('generation.notConfiguredGoSettings') })).not.toBeInTheDocument()
    expect(screen.getByText(i18n.t('generation.notConfiguredUser'))).toBeInTheDocument()
  })

  it('自动优化按钮使用 mode=t2i', async () => {
    vi.mocked(generationApi.optimizePrompt).mockResolvedValue({ prompt: '优化后的猫', model: 'qwen3.7-plus' })
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('param-panel')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫')
    await user.click(screen.getByRole('button', { name: i18n.t('generation.promptOptimize') }))

    await waitFor(() =>
      expect(generationApi.optimizePrompt).toHaveBeenCalledWith(expect.objectContaining({ mode: 't2i' })),
    )
  })

  it('切换模型会清空参数面板的旧值（比如 wan 的 thinking_mode 不应带到 qwen 上）', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('param-panel')
    // 默认选中 qwen-image，切到 wan2.7-image 后应出现深度思考开关，且为初始未开启状态。
    await user.click(screen.getByTestId('model-select'))
    await user.click(await screen.findByTitle(wanImage.ID))

    const thinkingSwitch = await screen.findByTestId('param-thinking-mode')
    expect(thinkingSwitch.querySelector('button')).toHaveAttribute('aria-checked', 'false')
  })

  // 从历史记录页"复用"过来：目录默认选中第一项的 effect 必须让路给复用数据，
  // 否则会出现"先选中 qwen-image（目录第一项），再跳成 wan2.7-image（复用
  // 目标模型）"的可见闪烁，或者复用的参数被目录首选逻辑覆盖掉。
  it('从历史记录复用：目录加载完成后回填 prompt/model/参数，而不是目录默认的第一项', async () => {
    const reuse: ReuseGenerationState = {
      taskId: 77,
      mode: 'imggen',
      model: wanImage.ID,
      prompt: '复用的猫咪描述',
      params: { size: '2K', n: 3, watermark: true, thinkingMode: true },
      hadInput: false,
    }
    renderPage([{ pathname: '/imggen', state: { reuse } }])

    await screen.findByTestId('param-panel')

    await waitFor(() => expect(screen.getByTestId('model-select')).toHaveTextContent(wanImage.ID))
    expect(screen.getByTestId('prompt-input').querySelector('textarea')).toHaveValue('复用的猫咪描述')

    const thinkingSwitch = await screen.findByTestId('param-thinking-mode')
    expect(thinkingSwitch.querySelector('button')).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText(i18n.t('generation.reuseApplied'))).toBeInTheDocument()
  })
})
