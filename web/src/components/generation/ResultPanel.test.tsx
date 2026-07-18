import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import ResultPanel from './ResultPanel'
import type { GenerationTask } from '@/types/generation'

vi.mock('@/api/generation', () => ({
  generationApi: {
    downloadResult: vi.fn(),
    downloadLinkPath: (taskId: number, index: number) => `/download/${taskId}/${index}`,
  },
}))

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
