import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import ModelSelect from './ModelSelect'
import type { CatalogModel } from '@/types/generation'
import { legacyI2VFields, nonVideoFields } from '@/components/generation/__fixtures__/catalog'

// 模拟一份 GET /api/catalog 的响应形态：图片/编辑/图生视频模型混在一起，
// 用来验证组件按 capability 过滤，而不是把整份目录都塞进下拉框。
const mockedCatalogModels: CatalogModel[] = [
  {
    ID: 'qwen-image', Capabilities: ['t2i'], Protocol: 'dashscope',
    Sizes: ['1328*1328'], MaxN: 1, SequentialMaxN: 0, Supports: ['seed'],
    Regions: null, MaxImages: 0, MinImageEdge: 0, RatioMin: 0, RatioMax: 0,
    ...nonVideoFields,
  },
  {
    ID: 'qwen-image-edit', Capabilities: ['edit'], Protocol: 'dashscope',
    Sizes: ['1328*1328'], MaxN: 1, SequentialMaxN: 0, Supports: ['seed'],
    Regions: null, MaxImages: 3, MinImageEdge: 0, RatioMin: 0, RatioMax: 0,
    ...nonVideoFields,
  },
  {
    ID: 'wan2.7-image', Capabilities: ['t2i', 'edit'], Protocol: 'dashscope',
    Sizes: ['1K'], MaxN: 4, SequentialMaxN: 12, Supports: ['thinking_mode'],
    Regions: null, MaxImages: 9, MinImageEdge: 0, RatioMin: 0, RatioMax: 0,
    ...nonVideoFields,
  },
  {
    ID: 'happyhorse-1.1-i2v', Capabilities: ['i2v'], Protocol: 'dashscope',
    Sizes: null, MaxN: 0, SequentialMaxN: 0, Supports: ['seed'],
    Regions: null, MaxImages: 1, MinImageEdge: 300, RatioMin: 0.4, RatioMax: 2.5,
    ...legacyI2VFields,
  },
]

function renderSelect(capability: Parameters<typeof ModelSelect>[0]['capability'], onChange = vi.fn()) {
  return render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <ModelSelect models={mockedCatalogModels} capability={capability} onChange={onChange} />
      </ConfigProvider>
    </I18nextProvider>,
  )
}

describe('ModelSelect — 由目录驱动，按能力过滤', () => {
  it('t2i 能力只列出 qwen-image 与 wan2.7-image，不含 edit-only 或 i2v 模型', async () => {
    const user = userEvent.setup()
    renderSelect('t2i')

    await user.click(screen.getByTestId('model-select'))
    expect(await screen.findByTitle('qwen-image')).toBeInTheDocument()
    expect(screen.getByTitle('wan2.7-image')).toBeInTheDocument()
    expect(screen.queryByTitle('qwen-image-edit')).not.toBeInTheDocument()
    expect(screen.queryByTitle('happyhorse-1.1-i2v')).not.toBeInTheDocument()
  })

  it('edit 能力列出 qwen-image-edit 与 wan2.7-image（后者同时具备 t2i 与 edit）', async () => {
    const user = userEvent.setup()
    renderSelect('edit')

    await user.click(screen.getByTestId('model-select'))
    expect(await screen.findByTitle('qwen-image-edit')).toBeInTheDocument()
    expect(screen.getByTitle('wan2.7-image')).toBeInTheDocument()
    expect(screen.queryByTitle('qwen-image')).not.toBeInTheDocument()
  })

  it('i2v 能力只列出 happyhorse-1.1-i2v', async () => {
    const user = userEvent.setup()
    renderSelect('i2v')

    await user.click(screen.getByTestId('model-select'))
    expect(await screen.findByTitle('happyhorse-1.1-i2v')).toBeInTheDocument()
    expect(screen.queryByTitle('qwen-image')).not.toBeInTheDocument()
  })

  it('选中某个选项时回调携带模型 id', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    renderSelect('t2i', onChange)

    await user.click(screen.getByTestId('model-select'))
    await user.click(await screen.findByTitle('qwen-image'))
    expect(onChange).toHaveBeenCalledWith('qwen-image', expect.anything())
  })
})
