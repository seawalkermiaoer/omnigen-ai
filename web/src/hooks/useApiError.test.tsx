import { describe, it, expect, beforeEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { App as AntdApp, ConfigProvider } from 'antd'
import type { ReactNode } from 'react'

import i18n, { setLocale } from '@/i18n'
import { omnigenTheme } from '@/theme'
import { useApiError } from './useApiError'
import { ApiError } from '@/api/client'

function wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>{children}</AntdApp>
      </ConfigProvider>
    </I18nextProvider>
  )
}

// 本次修的 bug 的第三层：即便后端已经归一化状态码、前端拦截器也不再误
// 登出，用户仍然需要看到"能看懂、能照做"的中文/英文文案，而不是裸的错误
// 码或兜底的"操作失败，请稍后重试"——那样用户根本不知道该去改 API Key
// 还是等一会儿重试。
describe('useApiError — 新增的上游错误码必须渲染成翻译文案，不是裸 code', () => {
  beforeEach(() => {
    setLocale('zh-CN')
  })

  const upstreamCodes = [
    'UPSTREAM_AUTH_FAILED',
    'UPSTREAM_RATE_LIMITED',
    'UPSTREAM_FAILED',
    'DASHSCOPE_REQUEST_FAILED',
    'DASHSCOPE_UPSTREAM_HTTP_ERROR',
    'DASHSCOPE_UPSTREAM_ERROR',
    'DASHSCOPE_NO_IMAGE_RESULT',
    'DASHSCOPE_NO_TASK_ID',
    'DASHSCOPE_NO_OPTIMIZE_RESULT',
    'T8STAR_UPSTREAM_HTTP_ERROR',
    'T8STAR_UPSTREAM_ERROR',
    'T8STAR_NO_IMAGE_RESULT',
  ]

  it.each(upstreamCodes)('%s 在中文下渲染为非空、非裸 code 的文案', (code) => {
    const { result } = renderHook(() => useApiError(), { wrapper })
    const message = result.current.toMessage(new ApiError(code, 502))

    expect(message).not.toBe(code)
    expect(message).not.toBe(i18n.t('errors.UNKNOWN'))
    expect(message.length).toBeGreaterThan(0)
  })

  it('UPSTREAM_AUTH_FAILED 的文案指向设置页，而不是泛泛的“稍后重试”', () => {
    const { result } = renderHook(() => useApiError(), { wrapper })
    const message = result.current.toMessage(new ApiError('UPSTREAM_AUTH_FAILED', 502))

    expect(message).toContain('设置')
  })

  it('UPSTREAM_RATE_LIMITED 的文案提示稍后重试', () => {
    const { result } = renderHook(() => useApiError(), { wrapper })
    const message = result.current.toMessage(new ApiError('UPSTREAM_RATE_LIMITED', 429))

    expect(message).toContain('稍后重试')
  })

  it('英文语言下同样渲染为翻译文案，且中英文案不同', () => {
    const { result: zh } = renderHook(() => useApiError(), { wrapper })
    const zhMessage = zh.current.toMessage(new ApiError('UPSTREAM_AUTH_FAILED', 502))

    setLocale('en')
    const { result: en } = renderHook(() => useApiError(), { wrapper })
    const enMessage = en.current.toMessage(new ApiError('UPSTREAM_AUTH_FAILED', 502))

    expect(enMessage).not.toBe('UPSTREAM_AUTH_FAILED')
    expect(enMessage).not.toBe(zhMessage)
  })

  it('未知 code 仍然兜底为 UNKNOWN 文案（回归防护，确保新增条目没有误改兜底逻辑）', () => {
    const { result } = renderHook(() => useApiError(), { wrapper })
    const message = result.current.toMessage(new ApiError('SOME_CODE_NOBODY_REGISTERED', 500))

    expect(message).toBe(i18n.t('errors.UNKNOWN'))
  })
})
