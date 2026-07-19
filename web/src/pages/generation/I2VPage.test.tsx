import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, type InitialEntry } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import I2VPage from './I2VPage'
import { catalogApi, generationApi, uploadApi } from '@/api/generation'
import { useAuthStore } from '@/stores/auth'
import type { CatalogModel, GenerationTask, ReuseGenerationState } from '@/types/generation'

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

// MediaUploader 用真实 <img> 解码本地文件读取宽高——jsdom 没有实现，
// 用可控的假 Image 替代，宽高来自模块级变量，测试用例开始前设置好即可。
// 与 MediaUploader.test.tsx 采用完全相同的打桩方式。
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
}

const happyhorseI2V: CatalogModel = {
  ID: 'happyhorse-1.1-i2v',
  Capabilities: ['i2v'],
  Protocol: 'dashscope',
  Sizes: null,
  MaxN: 0,
  SequentialMaxN: 0,
  Supports: ['seed', 'watermark'],
  Regions: null,
  MaxImages: 1,
  MinImageEdge: 300,
  RatioMin: 0.4,
  RatioMax: 2.5,
}

const wanI2V: CatalogModel = {
  ID: 'wan2.7-i2v-2026-04-25',
  Capabilities: ['i2v'],
  Protocol: 'dashscope',
  Sizes: null,
  MaxN: 0,
  SequentialMaxN: 0,
  Supports: ['negative_prompt', 'prompt_extend', 'seed', 'watermark'],
  Regions: ['cn-beijing', 'ap-southeast-1'],
  MaxImages: 2,
  MinImageEdge: 240,
  RatioMin: 0.125,
  RatioMax: 8,
}

const pendingTask: GenerationTask = {
  id: 40,
  mode: 'i2v',
  model: 'happyhorse-1.1-i2v',
  status: 'PENDING',
  prompt: '',
  params: {},
  inputUrls: [],
  resultUrls: [],
  createdAt: '2026-07-19T00:00:00Z',
  updatedAt: '2026-07-19T00:00:00Z',
}

function renderPage(initialEntries: InitialEntry[] = ['/i2v']) {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <MemoryRouter initialEntries={initialEntries}>
            <I2VPage />
          </MemoryRouter>
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

function fileInputs(): HTMLInputElement[] {
  return Array.from(document.querySelectorAll('input[type="file"]'))
}

async function uploadFirstFrame(user: ReturnType<typeof userEvent.setup>) {
  const input = fileInputs()[0]
  const file = new File(['x'], 'first.jpg', { type: 'image/jpeg' })
  await user.upload(input, file)
  await waitFor(() => expect(uploadApi.upload).toHaveBeenCalled())
}

describe('I2VPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    nextDimensions = { width: 1000, height: 1000 }
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    vi.mocked(catalogApi.list).mockResolvedValue({ models: [happyhorseI2V, wanI2V] })
    vi.mocked(uploadApi.upload).mockResolvedValue({ url: 'https://example.com/frame.jpg', size: 100 })
  })

  it('happyhorse 模型不展示任务类型控件', async () => {
    renderPage()
    await screen.findByTestId('submit-i2v')
    expect(screen.queryByTestId('i2v-task-type')).not.toBeInTheDocument()
    expect(screen.getByTestId('i2v-first-frame')).toBeInTheDocument()
    expect(screen.queryByTestId('i2v-last-frame')).not.toBeInTheDocument()
    expect(screen.queryByTestId('i2v-first-clip')).not.toBeInTheDocument()
  })

  it('切到 wan 模型后展示三种任务类型，并按类型显示正确的素材槽位', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('submit-i2v')

    await user.click(screen.getByTestId('model-select'))
    await user.click(await screen.findByTitle(wanI2V.ID))

    expect(await screen.findByTestId('i2v-task-type')).toBeInTheDocument()
    // 默认 first_frame：只展示首帧 + 驱动音频。
    expect(screen.getByTestId('i2v-first-frame')).toBeInTheDocument()
    expect(screen.queryByTestId('i2v-last-frame')).not.toBeInTheDocument()
    expect(screen.queryByTestId('i2v-first-clip')).not.toBeInTheDocument()
    expect(screen.getByTestId('i2v-driving-audio')).toBeInTheDocument()

    // first_last：首帧 + 尾帧 + 驱动音频，没有续接片段。
    await user.click(screen.getByText(i18n.t('generation.i2vTaskFirstLast')))
    expect(screen.getByTestId('i2v-first-frame')).toBeInTheDocument()
    expect(screen.getByTestId('i2v-last-frame')).toBeInTheDocument()
    expect(screen.queryByTestId('i2v-first-clip')).not.toBeInTheDocument()
    expect(screen.getByTestId('i2v-driving-audio')).toBeInTheDocument()

    // continue：续接片段 + 可选尾帧，没有首帧、没有驱动音频。
    await user.click(screen.getByText(i18n.t('generation.i2vTaskContinue')))
    expect(screen.queryByTestId('i2v-first-frame')).not.toBeInTheDocument()
    expect(screen.getByTestId('i2v-last-frame')).toBeInTheDocument()
    expect(screen.getByTestId('i2v-first-clip')).toBeInTheDocument()
    expect(screen.queryByTestId('i2v-driving-audio')).not.toBeInTheDocument()
  })

  it('i2v 请求从不携带 ratio 字段', async () => {
    vi.mocked(generationApi.generateVideo).mockResolvedValue(pendingTask)
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('submit-i2v')

    await uploadFirstFrame(user)
    await user.click(screen.getByTestId('submit-i2v'))

    await waitFor(() => expect(generationApi.generateVideo).toHaveBeenCalled())
    const payload = vi.mocked(generationApi.generateVideo).mock.calls[0][0]
    expect(payload).not.toHaveProperty('ratio')
    expect('ratio' in payload).toBe(false)
  })

  it('展示"自动跟随首帧"文案，而不是比例选择器', async () => {
    renderPage()
    await screen.findByTestId('submit-i2v')
    expect(screen.getByTestId('video-ratio-auto')).toHaveTextContent(i18n.t('generation.videoRatioAuto'))
  })

  it('happyhorse 模型下未上传首帧时提交会被拦下', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('submit-i2v')

    await user.click(screen.getByTestId('submit-i2v'))
    expect(generationApi.generateVideo).not.toHaveBeenCalled()
  })

  it('同一张图片：happyhorse 阈值 300px 拒绝、wan 阈值 240px 接受', async () => {
    nextDimensions = { width: 260, height: 260 }
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('submit-i2v')

    // happyhorse：260px < 300px，应被拒绝，从不调用上传接口。
    const file1 = new File(['x'], 'a.jpg', { type: 'image/jpeg' })
    await user.upload(fileInputs()[0], file1)
    expect(await screen.findByText(/260×260/)).toBeInTheDocument()
    expect(uploadApi.upload).not.toHaveBeenCalled()

    // 切到 wan：260px >= 240px，应被接受。
    await user.click(screen.getByTestId('model-select'))
    await user.click(await screen.findByTitle(wanI2V.ID))
    const file2 = new File(['x'], 'b.jpg', { type: 'image/jpeg' })
    await user.upload(fileInputs()[0], file2)
    await waitFor(() => expect(uploadApi.upload).toHaveBeenCalledTimes(1))
  })

  it('wan 模型展示地区限制提示', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('submit-i2v')

    await user.click(screen.getByTestId('model-select'))
    await user.click(await screen.findByTitle(wanI2V.ID))

    expect(await screen.findByTestId('wan-region-notice')).toHaveTextContent(i18n.t('generation.videoRegionNotice'))
  })

  it('自动优化按钮对 happyhorse 用 mode=i2v，对 wan 用 mode=i2v_wan', async () => {
    vi.mocked(generationApi.optimizePrompt).mockResolvedValue({ prompt: '优化后的描述', model: 'qwen-vl-plus' })
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('submit-i2v')

    await user.click(screen.getByRole('button', { name: i18n.t('generation.promptOptimize') }))
    await waitFor(() =>
      expect(generationApi.optimizePrompt).toHaveBeenCalledWith(expect.objectContaining({ mode: 'i2v' })),
    )

    vi.mocked(generationApi.optimizePrompt).mockClear()
    await user.click(screen.getByTestId('model-select'))
    await user.click(await screen.findByTitle(wanI2V.ID))
    await user.click(screen.getByRole('button', { name: i18n.t('generation.promptOptimize') }))
    await waitFor(() =>
      expect(generationApi.optimizePrompt).toHaveBeenCalledWith(expect.objectContaining({ mode: 'i2v_wan' })),
    )
  })

  // I2VPage 是最容易踩坑的一个：目录加载完成后既要应用"复用"数据，又有一个
  // 单独的 [modelId] effect 会在模型变化时清空 paramValues（正常切模型需要
  // 这个行为）。这条测试确保复用触发的那次 modelId 变化不会被那个 effect
  // 冲掉——resolution/watermark/negativePrompt/promptExtend 都要保留下来。
  it('从历史记录复用：目录加载完成后回填 wan 模型的 prompt/参数，且不被"切模型清空"逻辑覆盖', async () => {
    const reuse: ReuseGenerationState = {
      taskId: 99,
      mode: 'i2v',
      model: wanI2V.ID,
      prompt: '复用的描述文字',
      params: { resolution: '1080P', duration: 8, watermark: true, seed: 42, negativePrompt: '低质量', promptExtend: false },
      hadInput: true,
    }
    renderPage([{ pathname: '/i2v', state: { reuse } }])

    await screen.findByTestId('submit-i2v')

    await waitFor(() => expect(screen.getByTestId('model-select')).toHaveTextContent(wanI2V.ID))
    expect(screen.getByTestId('prompt-input').querySelector('textarea')).toHaveValue('复用的描述文字')
    expect(screen.getByTestId('video-resolution')).toHaveTextContent('1080P')
    expect(screen.getByTestId('video-duration').querySelector('input')).toHaveValue('8')

    const watermarkSwitch = await screen.findByTestId('param-watermark')
    expect(watermarkSwitch.querySelector('button')).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByTestId('param-negative-prompt').querySelector('textarea')).toHaveValue('低质量')
    expect(screen.getByText(i18n.t('generation.reuseImageNote'))).toBeInTheDocument()
  })
})
