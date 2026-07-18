export type Role = 'admin' | 'user'
export type UserStatus = 'active' | 'disabled'

/** 对应后端 usermodel.UserResponse。刻意不含 passwordHash。 */
export interface User {
  id: number
  username: string
  displayName: string
  role: Role
  status: UserStatus
  createdAt: string
  updatedAt: string
}

export interface UserListResponse {
  total: number
  items: User[]
}

export interface CreateUserRequest {
  username: string
  password: string
  displayName?: string
  role: Role
}

export interface UpdateUserRequest {
  displayName?: string
  role?: Role
  status?: UserStatus
}

export interface ResetPasswordRequest {
  password: string
}
