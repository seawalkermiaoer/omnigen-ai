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

client.interceptors.response.use(
  (response) => response,
  (error: AxiosError<ApiResponse>) => {
    if (!error.response) {
      return Promise.reject(new ApiError('NETWORK_ERROR', 0))
    }
    const { status, data } = error.response
    const code = data?.code ?? 'UNKNOWN'

    if (status === 401) {
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
