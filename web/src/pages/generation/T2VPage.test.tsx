import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import T2VPage from './T2VPage'
import { catalogApi, generationApi } from '@/api/generation'
import { ApiError } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import type { CatalogModel, GenerationTask } from '@/types/generation'

vi.mock('@/api/generation', () => ({
  catalogApi: { list: vi.fn() },
  generationApi: {
    generateVideo: vi.fn(),
    getTask: vi.fn(),
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

const happyhorseT2V: CatalogModel = {
  ID: 'happyhorse-1.1-t2v',
  Capabilities: ['t2v'],
  Protocol: 'dashscope',
  Sizes: null,
  MaxN: 0,
  SequentialMaxN: 0,
  Supports: ['seed', 'watermark'],
  Regions: null,
  MaxImages: 0,
  MinImageEdge: 0,
  RatioMin: 0,
  RatioMax: 0,
}

const pendingTask: GenerationTask = {
  id: 30,
  mode: 't2v',
  model: 'happyhorse-1.1-t2v',
  status: 'PENDING',
  prompt: '一只猫在草地上奔跑',
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
            <T2VPage />
          </MemoryRouter>
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('T2VPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    vi.mocked(catalogApi.list).mockResolvedValue({ models: [happyhorseT2V] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('prompt 为空时点击提交不会调用生成接口', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('submit-t2v')
    await user.click(screen.getByTestId('submit-t2v'))

    expect(generationApi.generateVideo).not.toHaveBeenCalled()
    expect(screen.getByText(i18n.t('generation.promptRequiredHint'))).toBeInTheDocument()
  })

  it('填写 prompt 后提交，调用生成接口并携带 resolution/duration/watermark/ratio', async () => {
    vi.mocked(generationApi.generateVideo).mockResolvedValue(pendingTask)
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('submit-t2v')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫在草地上奔跑')
    await user.click(screen.getByTestId('submit-t2v'))

    await waitFor(() =>
      expect(generationApi.generateVideo).toHaveBeenCalledWith(
        expect.objectContaining({
          model: 'happyhorse-1.1-t2v',
          prompt: '一只猫在草地上奔跑',
          resolution: '720P',
          duration: 5,
          ratio: '16:9',
          watermark: false,
        }),
      ),
    )
  })

  it('提交成功后展示轮询中的结果面板', async () => {
    vi.mocked(generationApi.generateVideo).mockResolvedValue(pendingTask)
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('submit-t2v')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫')
    await user.click(screen.getByTestId('submit-t2v'))

    expect(await screen.findByTestId('result-status')).toHaveTextContent(i18n.t('generation.status.PENDING'))
  })

  it('生成失败时展示翻译后的错误文案，不出现裸错误码', async () => {
    vi.mocked(generationApi.generateVideo).mockRejectedValue(new ApiError('VALIDATION_FAILED', 422))
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('submit-t2v')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫')
    await user.click(screen.getByTestId('submit-t2v'))

    expect(await screen.findByText(i18n.t('errors.VALIDATION_FAILED'))).toBeInTheDocument()
    expect(screen.queryByText('VALIDATION_FAILED')).not.toBeInTheDocument()
  })

  it('未配置凭证（SETTING_INCOMPLETE）时展示可操作提示', async () => {
    vi.mocked(generationApi.generateVideo).mockRejectedValue(new ApiError('SETTING_INCOMPLETE', 422))
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('submit-t2v')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫')
    await user.click(screen.getByTestId('submit-t2v'))

    expect(await screen.findByTestId('config-incomplete-alert')).toBeInTheDocument()
  })

  it('任务从 PENDING 轮询到 SUCCEEDED 后渲染视频结果', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.mocked(generationApi.generateVideo).mockResolvedValue(pendingTask)
    vi.mocked(generationApi.getTask).mockResolvedValue({
      ...pendingTask,
      status: 'SUCCEEDED',
      resultUrls: ['https://example.com/video.mp4'],
    })

    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    renderPage()

    await screen.findByTestId('submit-t2v')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫')
    await user.click(screen.getByTestId('submit-t2v'))

    await screen.findByTestId('result-status')

    await vi.advanceTimersByTimeAsync(3000)

    await waitFor(() => expect(generationApi.getTask).toHaveBeenCalledWith(30))
    expect(await screen.findByTestId('result-video')).toBeInTheDocument()
  })
})
