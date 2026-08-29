/**
 * wan3.0 在前端的行为：跨页面的那几条差异集中在这里测。
 *
 * 分散到各页面自己的测试文件里也可以，但那样每个文件都要再造一份
 * wan3.0 夹具，而这里真正要钉住的恰恰是"同一个 model id 在五个页面上
 * 表现不同"——放在一起才看得出这组断言互为对照。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import { catalogApi, generationApi } from '@/api/generation'
import { useAuthStore } from '@/stores/auth'
import type { CatalogModel, GenerationTask } from '@/types/generation'
import { wan30VideoFields } from '@/components/generation/__fixtures__/catalog'
import T2VPage from './T2VPage'
import R2VPage from './R2VPage'
import F2VPage from './F2VPage'
import L2VPage from './L2VPage'

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

const admin = {
  id: 1, username: 'admin', displayName: '管理员', role: 'admin' as const,
  status: 'active' as const, createdAt: '2026-07-19T00:00:00Z', updatedAt: '2026-07-19T00:00:00Z',
  quotaTotal: null, quotaUsed: 0,
}

/** 逐字对应 catalog.go 里的 wan3.0-video 条目。 */
const wan30: CatalogModel = {
  ID: 'wan3.0-video',
  Capabilities: ['t2v', 'i2v', 'r2v', 'f2v', 'l2v'],
  Protocol: 'dashscope',
  Sizes: null,
  MaxN: 0,
  SequentialMaxN: 0,
  Supports: ['prompt_extend', 'seed', 'watermark', 'audio'],
  Regions: null,
  MaxImages: 10,
  MinImageEdge: 240,
  RatioMin: 0.125,
  RatioMax: 8,
  ...wan30VideoFields,
}

const pendingTask: GenerationTask = {
  id: 90,
  mode: 't2v',
  model: 'wan3.0-video',
  status: 'PENDING',
  prompt: 'x',
  params: {},
  inputUrls: [],
  resultUrls: [],
  createdAt: '2026-07-19T00:00:00Z',
  updatedAt: '2026-07-19T00:00:00Z',
}

function renderPage(ui: React.ReactElement) {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <MemoryRouter>{ui}</MemoryRouter>
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('wan3.0 视频页面', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useAuthStore.setState({ token: 'tok', user: admin, initializing: false })
    vi.mocked(catalogApi.list).mockResolvedValue({ models: [wan30] })
    vi.mocked(generationApi.generateVideo).mockResolvedValue(pendingTask)
  })

  // 最关键的一条：wan3.0 一个 model id 同时具备五种视频能力，后端推断
  // 不出模式，请求体里必须带 mode，否则会被 422 拒绝。
  it('提交时携带 mode，且默认值取自目录而不是写死的 720P/16:9', async () => {
    const user = userEvent.setup()
    renderPage(<T2VPage />)

    await screen.findByTestId('submit-t2v')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫')
    await user.click(screen.getByTestId('submit-t2v'))

    await waitFor(() =>
      expect(generationApi.generateVideo).toHaveBeenCalledWith(
        expect.objectContaining({
          model: 'wan3.0-video',
          mode: 't2v',
          resolution: '720P',
          duration: 5,
          // wan2.7 的默认值是 16:9；adaptive 是 wan3.0 目录里的默认值，
          // 说明取值确实来自模型而不是前端常量。
          ratio: 'adaptive',
          // 旧 UI 里 prompt-extend 复选框默认勾选；wan3.0 是第一个支持
          // 这个参数的 t2v 模型，界面显示为开就必须真的发出去。
          promptExtend: true,
        }),
      ),
    )
  })

  // 480P 是 wan3.0 独有的分辨率——它出现在下拉框里就证明选项来自目录。
  it('分辨率下拉框包含 wan3.0 独有的 480P', async () => {
    renderPage(<T2VPage />)

    // 先等目录到位：选项来自 model.Resolutions，目录没回来之前这个
    // Select 是空的，直接展开会什么都查不到。选中项渲染成
    // <span class="ant-select-selection-item" title="720P">，等它出现
    // 就等于等到了模型。
    await screen.findByTitle('720P')
    // antd 的 Select 靠 mousedown 展开，userEvent.click 在 jsdom 里打不开它。
    fireEvent.mouseDown(within(screen.getByTestId('video-resolution')).getByRole('combobox'))

    expect(await screen.findByTitle('480P')).toBeInTheDocument()
  })

  // 智能时长（duration=-1）只有 SmartDuration 的模型才渲染这个勾选框。
  it('勾选智能时长后提交 duration=-1', async () => {
    const user = userEvent.setup()
    renderPage(<T2VPage />)

    await screen.findByTestId('submit-t2v')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '一只猫')
    await user.click(screen.getByTestId('video-duration-smart'))
    await user.click(screen.getByTestId('submit-t2v'))

    await waitFor(() =>
      expect(generationApi.generateVideo).toHaveBeenCalledWith(expect.objectContaining({ duration: -1 })),
    )
  })

  // wan3.0 的 Supports 里没有 negative_prompt、有 audio——同一个 ParamPanel
  // 因此少一个输入框、多一个开关。这是 Supports 第一次用来"减"控件。
  it('参数面板出现 audio 开关，且没有 negative_prompt 输入框', async () => {
    renderPage(<T2VPage />)

    await screen.findByTestId('param-panel')
    expect(screen.getByTestId('param-audio')).toBeInTheDocument()
    expect(screen.queryByTestId('param-negative-prompt')).not.toBeInTheDocument()
  })

  // wan3.0 全地域可用，Regions 为空——不该弹"仅 cn-beijing / ap-southeast-1"
  // 的警告。这条同时守着一个回归点：旧代码用 Regions 非空来判断"是不是
  // wan 模型"，若沿用，wan3.0 会既不显示警告、又被当成 happyhorse。
  it('不显示地域限制警告', async () => {
    renderPage(<T2VPage />)

    await screen.findByTestId('submit-t2v')
    expect(screen.queryByTestId('wan-region-notice')).not.toBeInTheDocument()
  })

  // 参考生视频页面：wan3.0 有参考音频、没有 reference_voice。
  it('参考生视频页面渲染参考音频区块，且不渲染 reference_voice 输入框', async () => {
    renderPage(<R2VPage />)

    await screen.findByTestId('submit-r2v')
    expect(screen.getByTestId('r2v-audios')).toBeInTheDocument()
    // 参考视频区块在（wan3.0 支持参考视频），但每行只有 URL 一个输入框。
    expect(screen.getByTestId('r2v-videos')).toBeInTheDocument()
    expect(screen.queryByTestId('r2v-image-voices')).not.toBeInTheDocument()
  })

  it('参考生视频页面提交时把音频放进 audios 数组', async () => {
    const user = userEvent.setup()
    renderPage(<R2VPage />)

    await screen.findByTestId('submit-r2v')
    await user.type(screen.getByTestId('prompt-input').querySelector('textarea')!, '让画面动起来')
    await user.click(screen.getByTestId('r2v-add-audio'))
    await user.type(screen.getByTestId('r2v-audio-row-0').querySelector('input')!, 'https://e.com/a.mp3')
    await user.click(screen.getByTestId('submit-r2v'))

    await waitFor(() =>
      expect(generationApi.generateVideo).toHaveBeenCalledWith(
        expect.objectContaining({
          mode: 'r2v',
          audios: [{ url: 'https://e.com/a.mp3' }],
        }),
      ),
    )
  })

  it('文件生视频页面把 URL 放进 fileUrl 并带上 mode=f2v', async () => {
    const user = userEvent.setup()
    renderPage(<F2VPage />)

    await screen.findByTestId('submit-f2v')
    await user.type(screen.getByTestId('f2v-url').querySelector('input')!, 'https://e.com/deck.pptx')
    await user.click(screen.getByTestId('submit-f2v'))

    await waitFor(() =>
      expect(generationApi.generateVideo).toHaveBeenCalledWith(
        expect.objectContaining({ mode: 'f2v', fileUrl: 'https://e.com/deck.pptx' }),
      ),
    )
  })

  it('网页生视频页面把 URL 放进 linkUrl 并带上 mode=l2v', async () => {
    const user = userEvent.setup()
    renderPage(<L2VPage />)

    await screen.findByTestId('submit-l2v')
    await user.type(screen.getByTestId('l2v-url').querySelector('input')!, 'https://e.com/p')
    await user.click(screen.getByTestId('submit-l2v'))

    await waitFor(() =>
      expect(generationApi.generateVideo).toHaveBeenCalledWith(
        expect.objectContaining({ mode: 'l2v', linkUrl: 'https://e.com/p' }),
      ),
    )
  })

  // prompt 可选、素材必填——两者顺序反了会让用户在没填 URL 时白白发一次
  // 注定失败的请求。
  it('文件生视频未填 URL 时不发请求', async () => {
    const user = userEvent.setup()
    renderPage(<F2VPage />)

    await screen.findByTestId('submit-f2v')
    await user.click(screen.getByTestId('submit-f2v'))

    expect(generationApi.generateVideo).not.toHaveBeenCalled()
    expect(screen.getByText(i18n.t('generation.f2vNeedFile'))).toBeInTheDocument()
  })
})
