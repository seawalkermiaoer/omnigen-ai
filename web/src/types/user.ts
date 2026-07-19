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
  /** null 表示不限量。 */
  quotaTotal: number | null
  quotaUsed: number
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
  quotaTotal?: number
}

/**
 * quotaUnlimited 为 true 时，后端把 quotaTotal 置为 NULL（不限量），并忽略
 * 同一次请求里可能一并传来的 quotaTotal——与后端 usermodel.UpdateRequest
 * 的取舍一致（见该结构体注释）：quotaTotal 单独一个可选字段无法表达"改成
 * 不限量"，因为"不提供"和"改成不限量"都会长得像"没有这个字段"。
 */
export interface UpdateUserRequest {
  displayName?: string
  role?: Role
  status?: UserStatus
  quotaTotal?: number
  quotaUnlimited?: boolean
}

export interface ResetPasswordRequest {
  password: string
}
