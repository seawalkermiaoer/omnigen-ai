import { client, unwrap } from './client'
import type { ApiResponse } from '@/types/common'
import type { StatsQuery, StatsReport } from '@/types/stats'

export const statsApi = {
  /** GET /api/stats?from=&to=&userId=——权限收敛（非管理员只看自己）完全在服务端完成。 */
  getReport: (query: StatsQuery) =>
    unwrap<StatsReport>(client.get<ApiResponse<StatsReport>>('/stats', { params: query })),
}
