import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import PromptInput from './PromptInput'
import { generationApi } from '@/api/generation'

vi.mock('@/api/generation', () => ({
  generationApi: { optimizePrompt: vi.fn() },
}))

function Harness() {
  const [value, setValue] = useState('一只猫')
  return <PromptInput value={value} onChange={setValue} mode="t2i" />
}

function renderHarness() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <Harness />
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('PromptInput — 优化与撤销', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('点击自动优化调用 /api/optimize-prompt，并把结果写回文本框、展示产出模型', async () => {
    vi.mocked(generationApi.optimizePrompt).mockResolvedValue({
      prompt: '一只橘色的猫坐在窗台上，午后阳光洒落',
      model: 'qwen3.7-plus',
    })
    const user = userEvent.setup()
    renderHarness()

    await user.click(screen.getByRole('button', { name: i18n.t('generation.promptOptimize') }))

    expect(generationApi.optimizePrompt).toHaveBeenCalledWith(
      expect.objectContaining({ draft: '一只猫', mode: 't2i' }),
    )
    expect(await screen.findByDisplayValue('一只橘色的猫坐在窗台上，午后阳光洒落')).toBeInTheDocument()
    expect(screen.getByTestId('prompt-hint')).toHaveTextContent('qwen3.7-plus')
  })

  it('优化后可以撤销，恢复优化前的文本', async () => {
    vi.mocked(generationApi.optimizePrompt).mockResolvedValue({
      prompt: '优化后的文本',
      model: 'qwen3.7-plus',
    })
    const user = userEvent.setup()
    renderHarness()

    await user.click(screen.getByRole('button', { name: i18n.t('generation.promptOptimize') }))
    expect(await screen.findByDisplayValue('优化后的文本')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: i18n.t('generation.promptUndo') }))
    expect(await screen.findByDisplayValue('一只猫')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: i18n.t('generation.promptUndo') })).not.toBeInTheDocument()
  })

  it('优化失败时展示错误提示，不清空原文本', async () => {
    vi.mocked(generationApi.optimizePrompt).mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    renderHarness()

    await user.click(screen.getByRole('button', { name: i18n.t('generation.promptOptimize') }))

    expect(await screen.findByTestId('prompt-hint')).toHaveTextContent(i18n.t('errors.UNKNOWN'))
    expect(screen.getByDisplayValue('一只猫')).toBeInTheDocument()
  })
})
