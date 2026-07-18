import { client, unwrap } from './client'
import type { ApiResponse, PageQuery } from '@/types/common'
import type {
  CreateUserRequest,
  ResetPasswordRequest,
  UpdateUserRequest,
  User,
  UserListResponse,
} from '@/types/user'

export const userApi = {
  list: (query: PageQuery) =>
    unwrap<UserListResponse>(
      client.get<ApiResponse<UserListResponse>>('/users', { params: query }),
    ),

  create: (req: CreateUserRequest) =>
    unwrap<User>(client.post<ApiResponse<User>>('/users', req)),

  update: (id: number, req: UpdateUserRequest) =>
    unwrap<User>(client.put<ApiResponse<User>>(`/users/${id}`, req)),

  resetPassword: (id: number, req: ResetPasswordRequest) =>
    client.put(`/users/${id}/password`, req),

  remove: (id: number) => client.delete(`/users/${id}`),
}
