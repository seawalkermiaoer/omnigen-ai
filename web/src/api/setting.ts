import { client, unwrap } from './client'
import type { ApiResponse } from '@/types/common'
import type { SettingsResponse, UpdateSettingsRequest } from '@/types/setting'

export const settingApi = {
  get: () =>
    unwrap<SettingsResponse>(client.get<ApiResponse<SettingsResponse>>('/settings')),

  update: (req: UpdateSettingsRequest) =>
    unwrap<SettingsResponse>(client.put<ApiResponse<SettingsResponse>>('/settings', req)),

  /** 探测成功时后端返回 data: null；调用方只关心是否抛错。 */
  test: () => client.post<ApiResponse<null>>('/settings/test'),
}
