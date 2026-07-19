import { client, unwrap } from './client'
import type { ApiResponse } from '@/types/common'
import type { SettingsResponse, TestConnectionProvider, UpdateSettingsRequest } from '@/types/setting'

export const settingApi = {
  get: () =>
    unwrap<SettingsResponse>(client.get<ApiResponse<SettingsResponse>>('/settings')),

  update: (req: UpdateSettingsRequest) =>
    unwrap<SettingsResponse>(client.put<ApiResponse<SettingsResponse>>('/settings', req)),

  /**
   * 探测成功时后端返回 data: null；调用方只关心是否抛错。
   *
   * provider 缺省 'dashscope'，与后端 TestConnectionRequest.Provider 的
   * 缺省语义一致。两个 provider 各自打各自的上游，互不影响——见
   * SettingsPage 里两个按钮各自独立的 loading/result state。
   */
  test: (provider: TestConnectionProvider = 'dashscope') =>
    client.post<ApiResponse<null>>('/settings/test', { provider }),
}
