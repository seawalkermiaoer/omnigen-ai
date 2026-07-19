import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import HistoryPage from './HistoryPage'
import { generationApi } from '@/api/generation'
import type { GenerationTask } from '@/types/generation'

const mockNavigate = vi.fn()

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

vi.mock('@/api/generation', () => ({
  generationApi: {
    listTasks: vi.fn(),
    getTask: vi.fn(),
    deleteTask: vi.fn(),
    deleteAllTasks: vi.fn(),
    downloadResult: vi.fn(),
    downloadLinkPath: (taskId: number, index: number) => `/download/${taskId}/${index}`,
  },
}))

const NOW = new Date('2026-07-19T12:00:00Z')

const imggenTask: GenerationTask = {
  id: 1,
  mode: 'imggen',
  model: 'qwen-image',
  status: 'SUCCEEDED',
  prompt: '一只猫在草地上奔跑',
  params: { size: '1328*1328', n: 2, watermark: true },
  inputUrls: [],
  resultUrls: ['https://example.com/1.png', 'https://example.com/2.png'],
  createdAt: '2026-07-19T11:55:00Z',
  updatedAt: '2026-07-19T11:55:30Z',
}

// 回归测试用：imgedit 记录，size/n 与 imggen 同源（都是图片家族），必须展示
// size/count 而不是 resolution/ratio/duration——旧 history.js 的
// `isImgMode = r.mode === 'imggen'` 会把这条记录错误地划进视频分支。
const imgeditTask: GenerationTask = {
  id: 2,
  mode: 'imgedit',
  model: 'qwen-image-edit',
  status: 'SUCCEEDED',
  prompt: '把背景换成海边',
  params: { size: '1140*1472', n: 1 },
  inputUrls: ['https://example.com/input.png'],
  resultUrls: ['https://example.com/edited.png'],
  createdAt: '2026-07-19T11:50:00Z',
  updatedAt: '2026-07-19T11:50:20Z',
}

const t2vDoneTask: GenerationTask = {
  id: 3,
  mode: 't2v',
  model: 'happyhorse-1.1-t2v',
  status: 'SUCCEEDED',
  prompt: '一只猫在草地上奔跑',
  params: { resolution: '720P', duration: 5, ratio: '16:9', watermark: false },
  inputUrls: [],
  resultUrls: ['https://example.com/video.mp4'],
  createdAt: '2026-07-19T11:00:00Z',
  updatedAt: '2026-07-19T11:00:40Z',
}

const failedTask: GenerationTask = {
  id: 4,
  mode: 't2v',
  model: 'happyhorse-1.1-t2v',
  status: 'FAILED',
  prompt: '失败的任务',
  params: { resolution: '720P', duration: 5, ratio: '16:9', watermark: false },
  inputUrls: [],
  resultUrls: [],
  errorCode: 'UPSTREAM_FAILED',
  errorMessage: 'raw upstream failure detail that should never reach the user',
  createdAt: '2026-07-19T11:40:00Z',
  updatedAt: '2026-07-19T11:41:00Z',
}

const pendingTask: GenerationTask = {
  id: 5,
  mode: 't2v',
  model: 'happyhorse-1.1-t2v',
  status: 'PENDING',
  prompt: '还在排队的任务',
  params: { resolution: '720P', duration: 5, ratio: '16:9', watermark: false },
  inputUrls: [],
  resultUrls: [],
  createdAt: '2026-07-19T11:59:00Z',
  updatedAt: '2026-07-19T11:59:00Z',
}

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <MemoryRouter initialEntries={['/history']}>
            <HistoryPage />
          </MemoryRouter>
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('HistoryPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.setSystemTime(NOW)
    vi.mocked(generationApi.listTasks).mockResolvedValue({
      total: 4,
      items: [imggenTask, imgeditTask, t2vDoneTask, failedTask],
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('渲染列表，展示正确的模式/状态/相对时间', async () => {
    renderPage()

    await screen.findByTestId('history-status-1')
    expect(screen.getByTestId('history-status-1')).toHaveTextContent(i18n.t('generation.status.SUCCEEDED'))
    expect(screen.getByText(i18n.t('history.modeLabels.imggen'))).toBeInTheDocument()
    expect(screen.getByText('qwen-image')).toBeInTheDocument()
    // 11:55 提交，系统时间 12:00 —— 5 分钟前。
    expect(screen.getByTestId('history-time-1')).toHaveTextContent(i18n.t('history.minutesAgo', { n: 5 }))
  })

  // 回归测试：修复 history.js 的 isImgMode = mode==='imggen' bug——imgedit
  // 必须落进图片家族，展示 size/count，不是 resolution/ratio/duration。
  it('imgedit 记录展示图片参数（size/count），不是视频参数', async () => {
    renderPage()

    const paramsEl = await screen.findByTestId('history-params-2')
    expect(paramsEl).toHaveTextContent('1140*1472')
    expect(paramsEl).toHaveTextContent(i18n.t('history.imageCount', { n: 1 }))
    // 视频参数字段不该出现——错误分组时 duration undefined 但不会拼出这些值。
    expect(paramsEl).not.toHaveTextContent('720P')
    expect(paramsEl).not.toHaveTextContent('16:9')
  })

  it('详情弹窗展示完整参数与内联结果', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('history-detail-1')
    await user.click(screen.getByTestId('history-detail-1'))

    const modal = await screen.findByTestId('history-detail-modal')
    expect(within(modal).getByText('一只猫在草地上奔跑')).toBeInTheDocument()
    expect(within(modal).getByText(/"size"/)).toBeInTheDocument()
    expect(await within(modal).findByTestId('result-image-grid')).toBeInTheDocument()
  })

  it('删除单条记录：确认后调用接口并刷新列表', async () => {
    vi.mocked(generationApi.deleteTask).mockResolvedValue(null)
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('history-delete-1')
    await user.click(screen.getByTestId('history-delete-1'))

    const okBtn = await screen.findByRole('button', { name: i18n.t('common.confirm') })
    await user.click(okBtn)

    await waitFor(() => expect(generationApi.deleteTask).toHaveBeenCalledWith(1))
    await waitFor(() => expect(generationApi.listTasks).toHaveBeenCalledTimes(2))
  })

  it('清空全部：需要输入确认词才能确定，确认后调用接口', async () => {
    vi.mocked(generationApi.deleteAllTasks).mockResolvedValue({ deleted: 4 })
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('history-delete-1')
    await user.click(screen.getByRole('button', { name: i18n.t('history.clearAll') }))

    const input = await screen.findByTestId('clear-confirm-input')
    const confirmButtons = screen.getAllByRole('button', { name: i18n.t('common.confirm') })
    const clearOkBtn = confirmButtons[confirmButtons.length - 1]
    expect(clearOkBtn).toBeDisabled()

    await user.type(input, 'DELETE')
    expect(clearOkBtn).toBeEnabled()
    await user.click(clearOkBtn)

    await waitFor(() => expect(generationApi.deleteAllTasks).toHaveBeenCalled())
  })

  it('导出 JSON：生成包含当前列表数据的 blob 并触发下载', async () => {
    const createObjectURL = vi.fn((_blob: Blob) => 'blob:mock-url')
    const revokeObjectURL = vi.fn()
    vi.stubGlobal('URL', { ...URL, createObjectURL, revokeObjectURL })
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('history-delete-1')
    await user.click(screen.getByRole('button', { name: i18n.t('history.exportJson') }))

    expect(createObjectURL).toHaveBeenCalledTimes(1)
    const blobArg = createObjectURL.mock.calls[0][0] as Blob
    const text = await blobArg.text()
    expect(text).toContain('"id": 1')
    expect(text).toContain(imggenTask.prompt)

    clickSpy.mockRestore()
    vi.unstubAllGlobals()
  })

  it('复用：导航到对应生成页面并携带该任务的参数', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByTestId('history-reuse-3')
    await user.click(screen.getByTestId('history-reuse-3'))

    expect(mockNavigate).toHaveBeenCalledWith('/t2v', {
      state: {
        reuse: {
          taskId: 3,
          mode: 't2v',
          model: 'happyhorse-1.1-t2v',
          prompt: t2vDoneTask.prompt,
          params: t2vDoneTask.params,
          hadInput: false,
        },
      },
    })
  })

  it('在飞任务无需用户操作即可自动轮询并更新', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.setSystemTime(NOW)
    vi.mocked(generationApi.listTasks).mockResolvedValue({ total: 1, items: [pendingTask] })
    vi.mocked(generationApi.getTask).mockResolvedValue({
      ...pendingTask,
      status: 'SUCCEEDED',
      resultUrls: ['https://example.com/video.mp4'],
    })

    renderPage()

    await screen.findByTestId('history-status-5')
    expect(screen.getByTestId('history-status-5')).toHaveTextContent(i18n.t('generation.status.PENDING'))

    await vi.advanceTimersByTimeAsync(3000)

    await waitFor(() => expect(generationApi.getTask).toHaveBeenCalledWith(5))
    await waitFor(() =>
      expect(screen.getByTestId('history-status-5')).toHaveTextContent(i18n.t('generation.status.SUCCEEDED')),
    )
  })

  it('失败任务展示翻译后的错误文案，不出现裸错误码', async () => {
    renderPage()

    const errorEl = await screen.findByTestId('history-error-4')
    expect(errorEl).toHaveTextContent(i18n.t('errors.UPSTREAM_FAILED'))
    expect(screen.queryByText('UPSTREAM_FAILED')).not.toBeInTheDocument()
    expect(errorEl).not.toHaveTextContent(failedTask.errorMessage!)
  })
})
