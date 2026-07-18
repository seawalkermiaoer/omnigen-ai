import { create } from 'zustand'
import { authApi } from '@/api/auth'
import { TOKEN_STORAGE_KEY, setUnauthorizedHandler } from '@/api/client'
import type { LoginRequest } from '@/types/auth'
import type { User } from '@/types/user'

interface AuthState {
  token: string | null
  user: User | null
  /** 应用启动时的验活阶段。为 true 时不应渲染任何受保护路由。 */
  initializing: boolean
  login: (req: LoginRequest) => Promise<void>
  logout: () => Promise<void>
  initialize: () => Promise<void>
  clear: () => void
  isAdmin: () => boolean
}

export const useAuthStore = create<AuthState>((set, get) => ({
  token: localStorage.getItem(TOKEN_STORAGE_KEY),
  user: null,
  initializing: true,

  login: async (req) => {
    const resp = await authApi.login(req)
    localStorage.setItem(TOKEN_STORAGE_KEY, resp.token)
    set({ token: resp.token, user: resp.user, initializing: false })
  },

  logout: async () => {
    try {
      await authApi.logout()
    } catch {
      // 登出接口失败不应阻止本地清理——用户的意图是离开。
    }
    get().clear()
  },

  clear: () => {
    localStorage.removeItem(TOKEN_STORAGE_KEY)
    set({ token: null, user: null, initializing: false })
  },

  // 刷新页面后先验活再渲染，避免持过期 token 闪现主界面。
  initialize: async () => {
    const token = localStorage.getItem(TOKEN_STORAGE_KEY)
    if (!token) {
      set({ token: null, user: null, initializing: false })
      return
    }
    try {
      const user = await authApi.me()
      set({ token, user, initializing: false })
    } catch {
      get().clear()
    }
  },

  isAdmin: () => get().user?.role === 'admin',
}))

// 任何请求收到 401 都直接清空本地凭据，路由守卫随之跳回登录页。
setUnauthorizedHandler(() => useAuthStore.getState().clear())
