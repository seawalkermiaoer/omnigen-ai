import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import ParamPanel from './ParamPanel'
import { gptImage2, qwenImage, wanImage } from './__fixtures__/catalog'
import type { CatalogModel, ParamPanelValues } from '@/types/generation'

function renderPanel(model: CatalogModel | undefined, value: ParamPanelValues, onChange = vi.fn()) {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <ParamPanel model={model} value={value} onChange={onChange} />
      </ConfigProvider>
    </I18nextProvider>,
  )
}

/** 受控包装：驱动真实的 value/onChange 循环，用于需要交互后再断言的用例。 */
function ControlledParamPanel({ model }: { model: CatalogModel }) {
  const [value, setValue] = useState<ParamPanelValues>({})
  return <ParamPanel model={model} value={value} onChange={setValue} />
}

describe('ParamPanel — 由目录约束驱动渲染', () => {
  it('qwen-image：有尺寸/数量(上限1)/反向提示词/随机种子/水印，没有深度思考、顺序生成', () => {
    renderPanel(qwenImage, {})

    expect(screen.getByTestId('param-size')).toBeInTheDocument()
    expect(screen.getByTestId('param-count')).toBeInTheDocument()
    expect(screen.getByTestId('param-negative-prompt')).toBeInTheDocument()
    expect(screen.getByTestId('param-seed')).toBeInTheDocument()
    expect(screen.getByTestId('param-watermark')).toBeInTheDocument()

    expect(screen.queryByTestId('param-thinking-mode')).not.toBeInTheDocument()
    expect(screen.queryByTestId('param-enable-sequential')).not.toBeInTheDocument()
    expect(screen.queryByTestId('param-prompt-extend')).not.toBeInTheDocument()
  })

  it('wan2.7-image：有深度思考与顺序生成开关，数量选择器同时存在', () => {
    renderPanel(wanImage, {})

    expect(screen.getByTestId('param-thinking-mode')).toBeInTheDocument()
    expect(screen.getByTestId('param-enable-sequential')).toBeInTheDocument()
    expect(screen.getByTestId('param-count')).toBeInTheDocument()
    expect(screen.getByTestId('param-size')).toBeInTheDocument()
  })

  it('gpt-image-2：不渲染任何 DashScope 参数控件（openai 协议，Sizes/Supports 均为空）', () => {
    renderPanel(gptImage2, {})

    expect(screen.queryByTestId('param-size')).not.toBeInTheDocument()
    expect(screen.queryByTestId('param-count')).not.toBeInTheDocument()
    expect(screen.queryByTestId('param-thinking-mode')).not.toBeInTheDocument()
    expect(screen.queryByTestId('param-enable-sequential')).not.toBeInTheDocument()
    expect(screen.queryByTestId('param-negative-prompt')).not.toBeInTheDocument()
    expect(screen.queryByTestId('param-seed')).not.toBeInTheDocument()
    expect(screen.queryByTestId('param-watermark')).not.toBeInTheDocument()
    expect(screen.queryByTestId('param-prompt-extend')).not.toBeInTheDocument()
  })

  it('未选择模型时展示提示文案，不渲染任何控件', () => {
    renderPanel(undefined, {})
    expect(screen.getByTestId('param-panel-empty')).toBeInTheDocument()
    expect(screen.queryByTestId('param-panel')).not.toBeInTheDocument()
  })

  it('数量超出 MaxN 时自动回落（qwen-image 上限为 1）', () => {
    const onChange = vi.fn()
    renderPanel(qwenImage, { n: 4 }, onChange)
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ n: 1 }))
  })

  it('数量上限随顺序生成开关切换：未开启用 MaxN=4 钳制，开启后用 SequentialMaxN=12 放行', () => {
    // 未开启顺序生成：上限是 MaxN=4，超出的 12 会被回落到 4。
    const onChangeOff = vi.fn()
    renderPanel(wanImage, { n: 12, enableSequential: false }, onChangeOff)
    expect(onChangeOff).toHaveBeenCalledWith(expect.objectContaining({ n: 4 }))

    // 开启顺序生成：上限变成 SequentialMaxN=12，同样的 12 不再被回落。
    const onChangeOn = vi.fn()
    renderPanel(wanImage, { n: 12, enableSequential: true }, onChangeOn)
    expect(onChangeOn).not.toHaveBeenCalled()
  })

  it('点击顺序生成开关会通过 onChange 通知调用方开启', async () => {
    const user = userEvent.setup()
    render(
      <I18nextProvider i18n={i18n}>
        <ConfigProvider theme={omnigenTheme}>
          <ControlledParamPanel model={wanImage} />
        </ConfigProvider>
      </I18nextProvider>,
    )

    const sequentialSwitch = screen.getByTestId('param-enable-sequential').querySelector('button')!
    expect(sequentialSwitch).toHaveAttribute('aria-checked', 'false')

    await user.click(sequentialSwitch)
    expect(sequentialSwitch).toHaveAttribute('aria-checked', 'true')
  })
})
