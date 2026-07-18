import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { client, setUnauthorizedHandler, ApiError } from './client'
import type { ApiResponse } from '@/types/common'

/**
 * 直接给 axios 请求装一个假 adapter（axios 的传输层钩子），跳过真实网络，
 * 但完整走 client 实例上真正注册的响应拦截器——这样测的是 client.ts 里
 * 那份判断逻辑本身，不是它的复制品。
 */
function respondWith(status: number, code: string) {
  return async (config: InternalAxiosRequestConfig) => {
    const body: ApiResponse = { code }
    throw new AxiosError(
      'Request failed',
      AxiosError.ERR_BAD_REQUEST,
      config,
      {},
      {
        status,
        statusText: 'Error',
        headers: new AxiosHeaders(),
        config,
        data: body,
      },
    )
  }
}

describe('api client 响应拦截器', () => {
  const onUnauthorized = vi.fn()

  beforeEach(() => {
    onUnauthorized.mockClear()
    setUnauthorizedHandler(onUnauthorized)
  })

  afterEach(() => {
    setUnauthorizedHandler(() => {})
  })

  // 这是本次要修的 bug 的正面用例：我们自己的会话失效必须仍然登出。
  it('401 + AUTH_UNAUTHORIZED 触发登出', async () => {
    await expect(
      client.get('/whatever', { adapter: respondWith(401, 'AUTH_UNAUTHORIZED') }),
    ).rejects.toBeInstanceOf(ApiError)

    expect(onUnauthorized).toHaveBeenCalledTimes(1)
  })

  // 这是本次要修的 bug 本身：上游（DashScope/t8star）拒绝一个失效 API key
  // 时会返回 401，若归一化失效、原样转发到我们自己的响应上，绝不能被当成
  // 会话过期处理——那样会把用户直接登出、丢掉正在填写的生成表单。
  // 后端已经在 service 层归一化掉了这种情况（见
  // server/internal/service/upstream_error.go），这里是前端这一侧的纵深
  // 防御：哪怕未来某个接口意外漏转发了一个裸 401，也不能仅凭 status===401
  // 就登出。
  it('401 + 非 AUTH_UNAUTHORIZED 不触发登出', async () => {
    await expect(
      client.get('/whatever', { adapter: respondWith(401, 'DASHSCOPE_UPSTREAM_HTTP_ERROR') }),
    ).rejects.toBeInstanceOf(ApiError)

    expect(onUnauthorized).not.toHaveBeenCalled()
  })

  // AUTH_USER_DISABLED 是 403，从不在这条 401 专用判断路径里；
  // 附带确认它确实不会误触发登出（该场景由别处的 403 处理逻辑负责，不是
  // 这个拦截器的职责）。
  it('403 + AUTH_USER_DISABLED 不触发（401 专用的）登出处理', async () => {
    await expect(
      client.get('/whatever', { adapter: respondWith(403, 'AUTH_USER_DISABLED') }),
    ).rejects.toBeInstanceOf(ApiError)

    expect(onUnauthorized).not.toHaveBeenCalled()
  })

  it('非 401 错误正常抛出 ApiError，带上原始 code/status', async () => {
    let caught: unknown
    try {
      await client.get('/whatever', { adapter: respondWith(502, 'UPSTREAM_AUTH_FAILED') })
    } catch (e) {
      caught = e
    }

    expect(caught).toBeInstanceOf(ApiError)
    expect((caught as ApiError).code).toBe('UPSTREAM_AUTH_FAILED')
    expect((caught as ApiError).status).toBe(502)
    expect(onUnauthorized).not.toHaveBeenCalled()
  })

  it('网络层失败（无 response）映射为 NETWORK_ERROR，不触发登出', async () => {
    const networkFailAdapter = async (config: InternalAxiosRequestConfig) => {
      throw new AxiosError('Network Error', AxiosError.ERR_NETWORK, config)
    }

    let caught: unknown
    try {
      await client.get('/whatever', { adapter: networkFailAdapter })
    } catch (e) {
      caught = e
    }

    expect(caught).toBeInstanceOf(ApiError)
    expect((caught as ApiError).code).toBe('NETWORK_ERROR')
    expect(onUnauthorized).not.toHaveBeenCalled()
  })
})
