import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import ImageEditPage from './ImageEditPage'
import { catalogApi, generationApi, uploadApi } from '@/api/generation'
import { ApiError } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { allModels, qwenImageEdit } from '@/components/generation/__fixtures__/catalog'
import type { GenerationTask } from '@/types/generation'

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

// 用可控的假 Image 替代真实解码，做法与 MediaUploader.test.tsx 一致——
// qwen-image-edit 的 MinImageEdge/RatioMin/RatioMax 均为 0（不限制），
// 任意宽高都应通过校验直接进入上传。
class FakeImage {
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  naturalWidth = 0
  naturalHeight = 0
  set src(_value: string) {
    queueMicrotask(() => {
      this.naturalWidth = 800
      this.naturalHeight = 800
      this.onload?.()
    })
  }
}
vi.stubGlobal('Image', FakeImage)
vi.stubGlobal('URL', { ...URL, createObjectURL: vi.fn(() => 'blob:mock'), revokeObjectURL: vi.fn() })

const admin = {
  id: 1, username: 'admin', displayName: '管理员', role: 'admin' as const,
  status: 'active' as const, createdAt: '2026-07-19T00:00:00Z', updatedAt: '2026-07-19T00:00:00Z',
  quotaTotal: null, quotaUsed: 0,
}

const succeededTask: GenerationTask = {
  id: 11,
  mode: 'imgedit',
  model: 'qwen-image-edit',
  status: 'SUCCEEDED',
  prompt: '',
  params: {},
  inputUrls: ['https://example.com/in.jpg'],
  resultUrls: ['https://example.com/out.png'],
  createdAt: '2026-07-19T00:00:00Z',
  updatedAt: '2026-07-19T00:00:00Z',
}

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <MemoryRouter>
            <ImageEditPage />
          </MemoryRouter>
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

function fileInput(container: HTMLElement): HTMLInputElement {
  const el = container.querySelector('input[type="file"]')
  if (!el) throw new Error('file input not found')
  return el as HTMLInputElement
}

describe('ImageEditPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    vi.mocked(catalogApi.list).mockResolvedValue({ models: allModels })
  })

  it('模型下拉只展示具备 edit 能力的模型，不含纯 t2i 或 i2v 模型', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('media-uploader')
    await user.click(screen.getByTestId('model-select'))

    // rc-virtual-list 在 jsdom 下只挂载能填满一个（恒为 0 的）视口高度的选项，
    // 与目录过滤逻辑无关——只断言前两个匹配项存在、非 edit 模型不存在，
    // 与 ModelSelect.test.tsx 的既有做法一致。
    const listbox = await screen.findByRole('listbox')
    expect(within(listbox).getByText('qwen-image-edit')).toBeInTheDocument()
    expect(within(listbox).getByText('wan2.7-image')).toBeInTheDocument()
    expect(within(listbox).queryByText('happyhorse-1.1-i2v')).not.toBeInTheDocument()
  })

  it('未上传图片时提交按钮禁用，不调用生成接口', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('media-uploader')
    const submitBtn = screen.getByRole('button', { name: i18n.t('generation.submit') })
    expect(submitBtn).toBeDisabled()

    await user.click(submitBtn)
    expect(generationApi.generateImage).not.toHaveBeenCalled()
  })

  it('上传一张图片后可以提交，调用生成接口并渲染结果', async () => {
    vi.mocked(uploadApi.upload).mockResolvedValue({ url: 'https://example.com/in.jpg', size: 100 })
    vi.mocked(generationApi.generateImage).mockResolvedValue(succeededTask)
    const user = userEvent.setup()
    const { container } = renderPage()

    await screen.findByTestId('media-uploader')
    await user.upload(fileInput(container), new File(['x'], 'a.jpg', { type: 'image/jpeg' }))
    await waitFor(() => expect(uploadApi.upload).toHaveBeenCalledTimes(1))

    const submitBtn = await screen.findByRole('button', { name: i18n.t('generation.submit') })
    await waitFor(() => expect(submitBtn).not.toBeDisabled())
    await user.click(submitBtn)

    await waitFor(() =>
      expect(generationApi.generateImage).toHaveBeenCalledWith(
        expect.objectContaining({
          model: qwenImageEdit.ID,
          images: ['https://example.com/in.jpg'],
        }),
      ),
    )
    expect(await screen.findByTestId('result-image-grid')).toBeInTheDocument()
  })

  it('prompt 为空也允许提交（图片编辑的描述是可选的）', async () => {
    vi.mocked(uploadApi.upload).mockResolvedValue({ url: 'https://example.com/in.jpg', size: 100 })
    vi.mocked(generationApi.generateImage).mockResolvedValue(succeededTask)
    const user = userEvent.setup()
    const { container } = renderPage()

    await screen.findByTestId('media-uploader')
    await user.upload(fileInput(container), new File(['x'], 'a.jpg', { type: 'image/jpeg' }))
    await waitFor(() => expect(uploadApi.upload).toHaveBeenCalledTimes(1))

    const submitBtn = await screen.findByRole('button', { name: i18n.t('generation.submit') })
    await waitFor(() => expect(submitBtn).not.toBeDisabled())
    await user.click(submitBtn)

    await waitFor(() =>
      expect(generationApi.generateImage).toHaveBeenCalledWith(expect.objectContaining({ prompt: '' })),
    )
  })

  it('生成失败时展示翻译后的错误文案，不出现裸错误码', async () => {
    vi.mocked(uploadApi.upload).mockResolvedValue({ url: 'https://example.com/in.jpg', size: 100 })
    vi.mocked(generationApi.generateImage).mockRejectedValue(new ApiError('VALIDATION_FAILED', 422))
    const user = userEvent.setup()
    const { container } = renderPage()

    await screen.findByTestId('media-uploader')
    await user.upload(fileInput(container), new File(['x'], 'a.jpg', { type: 'image/jpeg' }))
    await waitFor(() => expect(uploadApi.upload).toHaveBeenCalledTimes(1))

    const submitBtn = await screen.findByRole('button', { name: i18n.t('generation.submit') })
    await waitFor(() => expect(submitBtn).not.toBeDisabled())
    await user.click(submitBtn)

    expect(await screen.findByText(i18n.t('errors.VALIDATION_FAILED'))).toBeInTheDocument()
    expect(screen.queryByText('VALIDATION_FAILED')).not.toBeInTheDocument()
  })

  it('自动优化按钮使用 mode=imggen_edit，即使还没上传图片', async () => {
    vi.mocked(generationApi.optimizePrompt).mockResolvedValue({ prompt: '优化后的描述', model: 'qwen3.7-plus' })
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('media-uploader')
    await user.click(screen.getByRole('button', { name: i18n.t('generation.promptOptimize') }))

    await waitFor(() =>
      expect(generationApi.optimizePrompt).toHaveBeenCalledWith(expect.objectContaining({ mode: 'imggen_edit' })),
    )
  })

  it('未配置凭证时（SETTING_INCOMPLETE）展示可操作提示而不是裸错误码', async () => {
    vi.mocked(uploadApi.upload).mockResolvedValue({ url: 'https://example.com/in.jpg', size: 100 })
    vi.mocked(generationApi.generateImage).mockRejectedValue(new ApiError('SETTING_INCOMPLETE', 422))
    const user = userEvent.setup()
    const { container } = renderPage()

    await screen.findByTestId('media-uploader')
    await user.upload(fileInput(container), new File(['x'], 'a.jpg', { type: 'image/jpeg' }))
    await waitFor(() => expect(uploadApi.upload).toHaveBeenCalledTimes(1))

    const submitBtn = await screen.findByRole('button', { name: i18n.t('generation.submit') })
    await waitFor(() => expect(submitBtn).not.toBeDisabled())
    await user.click(submitBtn)

    expect(await screen.findByTestId('config-incomplete-alert')).toBeInTheDocument()
    const link = screen.getByRole('link', { name: i18n.t('generation.notConfiguredGoSettings') })
    expect(link).toHaveAttribute('href', '/settings')
  })
})
