import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { useAuthStore } from './auth'
import { authApi } from '@/api/auth'
import { TOKEN_STORAGE_KEY, ApiError } from '@/api/client'
import type { User } from '@/types/user'

vi.mock('@/api/auth', () => ({
  authApi: { login: vi.fn(), me: vi.fn(), logout: vi.fn(), changePassword: vi.fn() },
}))

const fakeUser: User = {
  id: 1, username: 'alice', displayName: 'Alice', role: 'admin',
  status: 'active', createdAt: '2026-07-18T00:00:00Z', updatedAt: '2026-07-18T00:00:00Z',
  quotaTotal: null, quotaUsed: 0,
}

describe('auth store', () => {
  beforeEach(() => {
    localStorage.clear()
    useAuthStore.setState({ token: null, user: null, initializing: true })
    vi.clearAllMocks()
  })
  afterEach(() => vi.resetAllMocks())

  it('登录成功后写入 token 与用户，并持久化 token', async () => {
    vi.mocked(authApi.login).mockResolvedValue({ token: 'tok-123', user: fakeUser })

    await useAuthStore.getState().login({ username: 'alice', password: 'password123' })

    const state = useAuthStore.getState()
    expect(state.token).toBe('tok-123')
    expect(state.user).toEqual(fakeUser)
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBe('tok-123')
  })

  it('登录失败时不写入任何凭据', async () => {
    vi.mocked(authApi.login).mockRejectedValue(new ApiError('AUTH_INVALID_CREDENTIALS', 401))

    await expect(
      useAuthStore.getState().login({ username: 'alice', password: 'wrong' }),
    ).rejects.toThrow()

    expect(useAuthStore.getState().token).toBeNull()
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
  })

  it('登出清空状态与 localStorage', async () => {
    localStorage.setItem(TOKEN_STORAGE_KEY, 'tok-123')
    useAuthStore.setState({ token: 'tok-123', user: fakeUser, initializing: false })
    vi.mocked(authApi.logout).mockResolvedValue({} as never)

    await useAuthStore.getState().logout()

    expect(useAuthStore.getState().token).toBeNull()
    expect(useAuthStore.getState().user).toBeNull()
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
  })

  // 刷新页面时若 token 已过期，必须先验活再渲染，
  // 否则会拿着废 token 闪一下主界面再跳走。
  it('initialize 在 token 有效时恢复用户', async () => {
    localStorage.setItem(TOKEN_STORAGE_KEY, 'tok-123')
    vi.mocked(authApi.me).mockResolvedValue(fakeUser)

    await useAuthStore.getState().initialize()

    const state = useAuthStore.getState()
    expect(state.user).toEqual(fakeUser)
    expect(state.initializing).toBe(false)
  })

  it('initialize 在 token 失效时清空凭据', async () => {
    localStorage.setItem(TOKEN_STORAGE_KEY, 'stale-token')
    vi.mocked(authApi.me).mockRejectedValue(new ApiError('AUTH_UNAUTHORIZED', 401))

    await useAuthStore.getState().initialize()

    const state = useAuthStore.getState()
    expect(state.user).toBeNull()
    expect(state.token).toBeNull()
    expect(state.initializing).toBe(false)
    expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
  })

  it('initialize 在无 token 时直接结束', async () => {
    await useAuthStore.getState().initialize()

    expect(useAuthStore.getState().initializing).toBe(false)
    expect(authApi.me).not.toHaveBeenCalled()
  })

  it('isAdmin 反映当前用户角色', () => {
    useAuthStore.setState({ user: fakeUser })
    expect(useAuthStore.getState().isAdmin()).toBe(true)

    useAuthStore.setState({ user: { ...fakeUser, role: 'user' } })
    expect(useAuthStore.getState().isAdmin()).toBe(false)

    useAuthStore.setState({ user: null })
    expect(useAuthStore.getState().isAdmin()).toBe(false)
  })
})
