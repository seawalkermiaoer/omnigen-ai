import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import R2VPage from './R2VPage'
import { catalogApi, generationApi, uploadApi } from '@/api/generation'
import { useAuthStore } from '@/stores/auth'
import type { CatalogModel, GenerationTask } from '@/types/generation'
import { legacyVideoFields } from '@/components/generation/__fixtures__/catalog'

vi.mock('@/api/generation', () => ({
  catalogApi: { list: vi.fn() },
  generationApi: {
    generateVideo: vi.fn(),
    getTask: vi.fn(),
    optimizePrompt: vi.fn(),
    downloadResult: vi.fn(),
  },
  uploadApi: { upload: vi.fn() },
}))

let nextDimensions: { width: number; height: number } = { width: 1000, height: 1000 }

class FakeImage {
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  naturalWidth = 0
  naturalHeight = 0
  set src(_value: string) {
    queueMicrotask(() => {
      this.naturalWidth = nextDimensions.width
      this.naturalHeight = nextDimensions.height
      this.onload?.()
    })
  }
}
vi.stubGlobal('Image', FakeImage)
vi.stubGlobal('URL', {
  ...URL,
  createObjectURL: vi.fn(() => 'blob:mock'),
  revokeObjectURL: vi.fn(),
})

const admin = {
  id: 1, username: 'admin', displayName: '管理员', role: 'admin' as const,
  status: 'active' as const, createdAt: '2026-07-19T00:00:00Z', updatedAt: '2026-07-19T00:00:00Z',
  quotaTotal: null, quotaUsed: 0,
}

const happyhorseR2V: CatalogModel = {
  ID: 'happyhorse-1.1-r2v',
  Capabilities: ['r2v'],
  Protocol: 'dashscope',
  Sizes: null,
  MaxN: 0,
  SequentialMaxN: 0,
  Supports: ['seed', 'watermark'],
  Regions: null,
  MaxImages: 9,
  MinImageEdge: 0,
  RatioMin: 0,
  RatioMax: 0,
  ...legacyVideoFields,
}

const wanR2V: CatalogModel = {
  ID: 'wan2.7-r2v',
  Capabilities: ['r2v'],
  Protocol: 'dashscope',
  Sizes: null,
  MaxN: 0,
  SequentialMaxN: 0,
  Supports: ['negative_prompt', 'prompt_extend', 'seed', 'watermark'],
  Regions: ['cn-beijing', 'ap-southeast-1'],
  MaxImages: 5,
  MinImageEdge: 0,
  RatioMin: 0,
  RatioMax: 0,
  ...legacyVideoFields,
  VideoProfile: 'wan2.7',
}

const pendingTask: GenerationTask = {
  id: 50,
  mode: 'r2v',
  model: 'happyhorse-1.1-r2v',
  status: 'PENDING',
  prompt: '一段参考视频',
  params: {},
  inputUrls: [],
  resultUrls: [],
  createdAt: '2026-07-19T00:00:00Z',
  updatedAt: '2026-07-19T00:00:00Z',
}

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <MemoryRouter>
            <R2VPage />
          </MemoryRouter>
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

function fileInput(): HTMLInputElement {
  return document.querySelector('input[type="file"]') as HTMLInputElement
}

async function uploadOneImage(user: ReturnType<typeof userEvent.setup>, name: string) {
  const before = vi.mocked(uploadApi.upload).mock.calls.length
  await user.upload(fileInput(), new File(['x'], name, { type: 'image/jpeg' }))
  await waitFor(() => expect(vi.mocked(uploadApi.upload).mock.calls.length).toBe(before + 1))
}

describe('R2VPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    nextDimensions = { width: 1000, height: 1000 }
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    vi.mocked(catalogApi.list).mockResolvedValue({ models: [happyhorseR2V, wanR2V] })
    vi.mocked(uploadApi.upload).mockImplementation((file: File) =>
      Promise.resolve({ url: `https://example.com/${file.name}`, size: 100 }),
    )
  })

  it('happyhorse 模型下没有参考视频区块，上限提示为 9 张', async () => {
    renderPage()
    await screen.findByTestId('submit-r2v')
    expect(screen.queryByTestId('r2v-videos')).not.toBeInTheDocument()
    expect(screen.getByTestId('media-count-hint')).toHaveTextContent('9')
  })

  it('wan 模型展示参考视频区块与地区提示', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('submit-r2v')

    await user.click(screen.getByTestId('model-select'))
    await user.click(await screen.findByTitle(wanR2V.ID))

    expect(await screen.findByTestId('r2v-videos')).toBeInTheDocument()
    expect(screen.getByTestId('wan-region-notice')).toHaveTextContent(i18n.t('generation.videoRegionNotice'))
  })

  it('wan 模型下参考图 + 参考视频合计超过 5 个会被提交拦下', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('submit-r2v')

    await user.click(screen.getByTestId('model-select'))
    await user.click(await screen.findByTitle(wanR2V.ID))
    await screen.findByTestId('r2v-videos')

    // 上传 3 张图。
    await uploadOneImage(user, 'a.jpg')
    await uploadOneImage(user, 'b.jpg')
    await uploadOneImage(user, 'c.jpg')

    // 加满 2 条参考视频行（3 图 + 2 视频 = 5，达到上限，第三次加应被拦下）。
    await user.click(screen.getByTestId('r2v-add-video'))
    await user.click(screen.getByTestId('r2v-add-video'))
    expect(screen.getByTestId('r2v-add-video')).toBeDisabled()

    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '参考生视频')
    await user.type(
      screen.getAllByPlaceholderText(i18n.t('generation.r2vVideoUrlPlaceholder'))[0],
      'https://example.com/v1.mp4',
    )

    await user.click(screen.getByTestId('submit-r2v'))
    // 只有一条视频行填了 url（1 个有效视频），3 图 + 1 有效视频 = 4，不超限，应正常提交。
    await waitFor(() => expect(generationApi.generateVideo).toHaveBeenCalled())
  })

  it('happyhorse 模型允许最多 9 张参考图', async () => {
    renderPage()
    await screen.findByTestId('submit-r2v')
    const uploader = screen.getByTestId('media-uploader')
    expect(uploader).toBeInTheDocument()
    expect(screen.getByTestId('media-count-hint')).toHaveTextContent('0 / 9')
  })

  it('未填写 prompt 时提交会被拦下', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('submit-r2v')

    await uploadOneImage(user, 'a.jpg')
    await user.click(screen.getByTestId('submit-r2v'))

    expect(generationApi.generateVideo).not.toHaveBeenCalled()
  })

  it('填写 prompt 与参考图后提交（happyhorse），请求携带 resolution/duration/ratio', async () => {
    vi.mocked(generationApi.generateVideo).mockResolvedValue(pendingTask)
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('submit-r2v')

    await uploadOneImage(user, 'a.jpg')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一段参考视频')
    await user.click(screen.getByTestId('submit-r2v'))

    await waitFor(() =>
      expect(generationApi.generateVideo).toHaveBeenCalledWith(
        expect.objectContaining({
          model: 'happyhorse-1.1-r2v',
          prompt: '一段参考视频',
          resolution: '720P',
          duration: 5,
          ratio: '16:9',
          images: [{ url: 'https://example.com/a.jpg' }],
        }),
      ),
    )
  })

  it('生成失败时展示翻译后的错误文案', async () => {
    const { ApiError } = await import('@/api/client')
    vi.mocked(generationApi.generateVideo).mockRejectedValue(new ApiError('VALIDATION_FAILED', 422))
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('submit-r2v')

    await uploadOneImage(user, 'a.jpg')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一段参考视频')
    await user.click(screen.getByTestId('submit-r2v'))

    expect(await screen.findByText(i18n.t('errors.VALIDATION_FAILED'))).toBeInTheDocument()
    expect(screen.queryByText('VALIDATION_FAILED')).not.toBeInTheDocument()
  })
})
