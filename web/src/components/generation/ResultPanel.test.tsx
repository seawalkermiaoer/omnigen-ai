import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import ResultPanel from './ResultPanel'
import type { GenerationTask } from '@/types/generation'

vi.mock('@/api/generation', () => ({
  generationApi: {
    downloadResult: vi.fn(),
  },
}))

const writeText = vi.fn<(text: string) => Promise<void>>()

beforeEach(() => {
  writeText.mockReset()
  writeText.mockResolvedValue(undefined)
})

/**
 * 必须在 userEvent.setup() 之后再装桩：setup() 自己会往 navigator 上挂一份
 * clipboard 存根，先装会被它覆盖掉，断言永远看到 0 次调用。
 */
function setupUserWithClipboard() {
  const user = userEvent.setup()
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  return user
}

function renderPanel(task?: GenerationTask | null) {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <ResultPanel task={task} />
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

const baseTask: GenerationTask = {
  id: 1,
  mode: 'imggen',
  model: 'qwen-image',
  status: 'SUCCEEDED',
  prompt: '一只猫',
  params: {},
  inputUrls: [],
  resultUrls: ['https://example.com/1.png', 'https://example.com/2.png'],
  createdAt: '2026-07-19T00:00:00Z',
  updatedAt: '2026-07-19T00:00:00Z',
}

describe('ResultPanel', () => {
  it('没有任务时展示空态', () => {
    renderPanel(null)
    expect(screen.getByTestId('result-panel-empty')).toBeInTheDocument()
  })

  it('图片任务渲染图片网格，每张图都有下载与复制链接按钮', () => {
    renderPanel(baseTask)

    expect(screen.getByTestId('result-image-grid')).toBeInTheDocument()
    expect(screen.queryByTestId('result-video')).not.toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: i18n.t('generation.resultDownload') })).toHaveLength(2)
    expect(screen.getAllByRole('button', { name: i18n.t('generation.resultCopyLink') })).toHaveLength(2)
  })

  // 「复制链接」必须复制出一个粘到别处就能打开的地址，也就是 resultUrls 本身
  // （新任务是我方 OSS 的永久公开地址）；内部下载接口那条路径需要 Bearer 头，
  // 复制出去必然打不开，不该出现在剪贴板里。
  it('复制链接复制的是该项的 resultUrls 原始地址', async () => {
    const user = setupUserWithClipboard()
    renderPanel(baseTask)

    const copyButtons = screen.getAllByRole('button', { name: i18n.t('generation.resultCopyLink') })
    await user.click(copyButtons[1])

    await waitFor(() => expect(writeText).toHaveBeenCalledWith('https://example.com/2.png'))
    expect(writeText).not.toHaveBeenCalledWith(expect.stringContaining('/api/download/'))
  })

  it('视频任务复制链接复制的是视频的 resultUrls[0]', async () => {
    const user = setupUserWithClipboard()
    renderPanel({ ...baseTask, mode: 'i2v', resultUrls: ['https://example.com/video.mp4'] })

    await user.click(screen.getByRole('button', { name: i18n.t('generation.resultCopyLink') }))

    await waitFor(() => expect(writeText).toHaveBeenCalledWith('https://example.com/video.mp4'))
  })

  // resultUrls 为空时结果区整块不渲染，所以复制按钮压根不存在——钉住这一点，
  // 避免以后有人放宽渲染条件后复制出 undefined。
  it('resultUrls 为空时不渲染复制链接按钮', () => {
    renderPanel({ ...baseTask, resultUrls: [] })

    expect(
      screen.queryByRole('button', { name: i18n.t('generation.resultCopyLink') }),
    ).not.toBeInTheDocument()
  })

  it('视频任务渲染 video 播放器而不是图片网格', () => {
    const videoTask: GenerationTask = {
      ...baseTask,
      mode: 'i2v',
      resultUrls: ['https://example.com/video.mp4'],
    }
    renderPanel(videoTask)

    expect(screen.getByTestId('result-video')).toBeInTheDocument()
    expect(screen.queryByTestId('result-image-grid')).not.toBeInTheDocument()
    expect(document.querySelector('video')).toHaveAttribute('src', 'https://example.com/video.mp4')
  })

  it('存在 note 时展示提示文案', () => {
    const withNote: GenerationTask = { ...baseTask, note: '本次生成使用了备用模型' }
    renderPanel(withNote)

    expect(screen.getByTestId('result-note')).toHaveTextContent('本次生成使用了备用模型')
  })

  it('不存在 note 时不渲染提示区块', () => {
    renderPanel(baseTask)
    expect(screen.queryByTestId('result-note')).not.toBeInTheDocument()
  })

  it('失败任务展示错误信息，不渲染结果区', () => {
    const failed: GenerationTask = {
      ...baseTask,
      status: 'FAILED',
      resultUrls: [],
      errorMessage: '上游服务不可用',
    }
    renderPanel(failed)

    expect(screen.getByTestId('result-error')).toHaveTextContent('上游服务不可用')
    expect(screen.queryByTestId('result-image-grid')).not.toBeInTheDocument()
  })
})
