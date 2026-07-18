export interface ApiResponse<T = unknown> {
  code: string
  message?: string
  data?: T
}

export interface PageQuery {
  page: number
  pageSize: number
}
