import { client, unwrap } from './client'
import type { ApiResponse } from '@/types/common'
import type { ChangePasswordRequest, LoginRequest, LoginResponse } from '@/types/auth'
import type { User } from '@/types/user'

export const authApi = {
  login: (req: LoginRequest) =>
    unwrap<LoginResponse>(client.post<ApiResponse<LoginResponse>>('/auth/login', req)),

  me: () => unwrap<User>(client.get<ApiResponse<User>>('/auth/me')),

  logout: () => client.post('/auth/logout'),

  changePassword: (req: ChangePasswordRequest) => client.put('/auth/password', req),
}
