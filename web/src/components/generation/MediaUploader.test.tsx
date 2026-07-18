import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import MediaUploader, { type MediaUploaderItem } from './MediaUploader'
import { uploadApi } from '@/api/generation'
import { happyhorseI2V, wanI2V } from './__fixtures__/catalog'

vi.mock('@/api/generation', () => ({
  uploadApi: { upload: vi.fn() },
}))

// 用可控的假 Image 替代真实解码：src 赋值后异步触发 onload/onerror，
// 宽高读取自模块级变量，每个用例开始前设置好即可，不依赖真实图片文件。
let nextDimensions: { width: number; height: number } | 'error' = { width: 1000, height: 1000 }

class FakeImage {
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  naturalWidth = 0
  naturalHeight = 0
  set src(_value: string) {
    queueMicrotask(() => {
      if (nextDimensions === 'error') {
        this.onerror?.()
        return
      }
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

function renderUploader(
  props: Partial<React.ComponentProps<typeof MediaUploader>> & {
    value?: MediaUploaderItem[]
    onChange?: (items: MediaUploaderItem[]) => void
  } = {},
) {
  const onChange = props.onChange ?? vi.fn()
  const utils = render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <MediaUploader value={props.value ?? []} onChange={onChange} maxCount={props.maxCount ?? 3} {...props} />
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
  return { ...utils, onChange }
}

function fileInput(container: HTMLElement): HTMLInputElement {
  const el = container.querySelector('input[type="file"]')
  if (!el) throw new Error('file input not found')
  return el as HTMLInputElement
}

describe('MediaUploader — 上传前按目录阈值校验', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    nextDimensions = { width: 1000, height: 1000 }
  })

  it('拒绝小于最小边长的图片，且从不调用上传接口（happyhorse: minEdge 300）', async () => {
    nextDimensions = { width: 200, height: 200 }
    const user = userEvent.setup()
    const { container, onChange } = renderUploader({
      maxCount: happyhorseI2V.MaxImages,
      minEdge: happyhorseI2V.MinImageEdge,
      ratioMin: happyhorseI2V.RatioMin,
      ratioMax: happyhorseI2V.RatioMax,
    })

    const file = new File(['x'], 'small.jpg', { type: 'image/jpeg' })
    await user.upload(fileInput(container), file)

    expect(await screen.findByText(/200×200/)).toBeInTheDocument()
    expect(uploadApi.upload).not.toHaveBeenCalled()
    expect(onChange).not.toHaveBeenCalled()
  })

  it('拒绝超出比例范围的图片，且从不调用上传接口（happyhorse: ratio 0.4~2.5）', async () => {
    nextDimensions = { width: 1000, height: 300 } // 宽高都 >= minEdge(300)，但 ratio = 3.33，超出 2.5
    const user = userEvent.setup()
    const { container, onChange } = renderUploader({
      maxCount: happyhorseI2V.MaxImages,
      minEdge: happyhorseI2V.MinImageEdge,
      ratioMin: happyhorseI2V.RatioMin,
      ratioMax: happyhorseI2V.RatioMax,
    })

    const file = new File(['x'], 'wide.jpg', { type: 'image/jpeg' })
    await user.upload(fileInput(container), file)

    expect(await screen.findByText(/超出允许范围/)).toBeInTheDocument()
    expect(uploadApi.upload).not.toHaveBeenCalled()
    expect(onChange).not.toHaveBeenCalled()
  })

  it('阈值按模型 props 传入，不是组件内置：同一张图对 happyhorse 不合规、对 wan 合规', async () => {
    nextDimensions = { width: 250, height: 250 } // < happyhorse 的 300，>= wan 的 240
    const user = userEvent.setup()

    const rejected = renderUploader({
      maxCount: happyhorseI2V.MaxImages,
      minEdge: happyhorseI2V.MinImageEdge,
      ratioMin: happyhorseI2V.RatioMin,
      ratioMax: happyhorseI2V.RatioMax,
    })
    await user.upload(fileInput(rejected.container), new File(['x'], 'a.jpg', { type: 'image/jpeg' }))
    expect(await screen.findByText(/250×250/)).toBeInTheDocument()
    expect(rejected.onChange).not.toHaveBeenCalled()

    vi.mocked(uploadApi.upload).mockResolvedValue({ url: 'https://example.com/a.jpg', size: 123 })
    const accepted = renderUploader({
      maxCount: wanI2V.MaxImages,
      minEdge: wanI2V.MinImageEdge,
      ratioMin: wanI2V.RatioMin,
      ratioMax: wanI2V.RatioMax,
    })
    await user.upload(fileInput(accepted.container), new File(['x'], 'b.jpg', { type: 'image/jpeg' }))
    await vi.waitFor(() => expect(uploadApi.upload).toHaveBeenCalledTimes(1))
    await vi.waitFor(() => expect(accepted.onChange).toHaveBeenCalledWith([
      expect.objectContaining({ url: 'https://example.com/a.jpg', name: 'b.jpg' }),
    ]))
  })

  it('合规图片会调用上传接口并把结果加入列表', async () => {
    nextDimensions = { width: 800, height: 800 }
    vi.mocked(uploadApi.upload).mockResolvedValue({ url: 'https://example.com/ok.jpg', size: 456 })
    const user = userEvent.setup()
    const { container, onChange } = renderUploader({ maxCount: 3 })

    await user.upload(fileInput(container), new File(['x'], 'ok.jpg', { type: 'image/jpeg' }))

    await vi.waitFor(() => expect(uploadApi.upload).toHaveBeenCalledTimes(1))
    await vi.waitFor(() =>
      expect(onChange).toHaveBeenCalledWith([
        expect.objectContaining({ url: 'https://example.com/ok.jpg', name: 'ok.jpg' }),
      ]),
    )
  })

  it('达到 maxCount 后拒绝新上传', async () => {
    nextDimensions = { width: 800, height: 800 }
    const user = userEvent.setup()
    const existing: MediaUploaderItem[] = [{ uid: '1', url: 'https://example.com/1.jpg', name: '1.jpg' }]
    const { container, onChange } = renderUploader({ maxCount: 1, value: existing })

    await user.upload(fileInput(container), new File(['x'], 'extra.jpg', { type: 'image/jpeg' }))

    expect(uploadApi.upload).not.toHaveBeenCalled()
    expect(onChange).not.toHaveBeenCalled()
  })
})
