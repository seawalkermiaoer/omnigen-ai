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
  refreshUser: () => Promise<void>
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

  // 补一次 GET /api/auth/me，把 topbar 剩余额度这类"服务端状态一直是对的，
  // 只是本地缓存的 user 没跟着变"的字段刷新回来——配额的权威数据永远在
  // 服务端（ConsumeQuota/RefundQuota 都是数据库事务），user 只是登录/
  // initialize 时缓存的一份快照。调用方是"生成成功后""删除任务后"这类
  // 会改变配额的动作，不是每次渲染都调，也不是搭一层通用缓存失效机制。
  //
  // 没有 token 时是空操作：调用方（生成/删除后）本来就要求已登录，这里
  // 只是防御一下时序上的极端情况。请求失败（例如网络抖动）时静默忽略、
  // 保留旧的 user 不动——这只是一次"顺带刷新"，不应该因为这一次请求
  // 失败就把用户登出；真正的 401 会经由 setUnauthorizedHandler 统一处理。
  refreshUser: async () => {
    if (!get().token) return
    try {
      const user = await authApi.me()
      set({ user })
    } catch {
      // 忽略：保留当前缓存的 user，不影响调用方（生成/删除）本身已经成功。
    }
  },

  isAdmin: () => get().user?.role === 'admin',
}))

// 任何请求收到 401 都直接清空本地凭据，路由守卫随之跳回登录页。
setUnauthorizedHandler(() => useAuthStore.getState().clear())
