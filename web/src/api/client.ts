import axios, { AxiosError } from 'axios'
import type { ApiResponse } from '@/types/common'

export const TOKEN_STORAGE_KEY = 'omnigen_token'

/** 携带后端错误码的异常，供 UI 查 i18n 表得到文案。 */
export class ApiError extends Error {
  readonly code: string
  readonly status: number

  constructor(code: string, status: number) {
    super(code)
    this.code = code
    this.status = status
    this.name = 'ApiError'
  }
}

export const client = axios.create({
  baseURL: '/api',
  timeout: 15000,
  headers: { 'Content-Type': 'application/json' },
})

client.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_STORAGE_KEY)
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

/** 401 时清空本地凭据并跳登录页。由 stores/auth 注册具体动作，避免循环依赖。 */
let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn
}

/**
 * 只有我们自己签发的"会话失效"错误码才应该触发登出。
 *
 * 后端 middleware.Auth（server/internal/middleware/auth.go）是唯一会产出
 * 401 的地方，且只用一个码：AUTH_UNAUTHORIZED——账号被禁用是 403
 * AUTH_USER_DISABLED，不在这里处理。
 *
 * 反面教材：/api/generate/image 等生成类接口在归一化之前会把上游（如
 * DashScope 对失效 API key 的拒绝）的真实 HTTP 401 原样转发成我们自己的
 * 401（见 server/internal/provider/dashscope/errors.go 的
 * newUpstreamHTTPError）。服务端已经在 service 层归一化掉了这种情况
 * （UPSTREAM_AUTH_FAILED @ 502，不再是裸 401），但这里额外按 code 收窄，
 * 是纵深防御：哪天有新接口没接上那层归一化、又意外把上游 401 转发出来，
 * 也不会仅仅因为 status===401 就把用户登出、丢掉正在填写的表单。
 */
const SESSION_EXPIRED_CODES = new Set(['AUTH_UNAUTHORIZED'])

client.interceptors.response.use(
  (response) => response,
  (error: AxiosError<ApiResponse>) => {
    if (!error.response) {
      return Promise.reject(new ApiError('NETWORK_ERROR', 0))
    }
    const { status, data } = error.response
    const code = data?.code ?? 'UNKNOWN'

    if (status === 401 && SESSION_EXPIRED_CODES.has(code)) {
      onUnauthorized?.()
    }
    return Promise.reject(new ApiError(code, status))
  },
)

/** 拆掉统一响应外壳，直接返回 data。 */
export async function unwrap<T>(promise: Promise<{ data: ApiResponse<T> }>): Promise<T> {
  const res = await promise
  return res.data.data as T
}
