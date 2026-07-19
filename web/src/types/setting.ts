/** 对应后端 settingmodel.Key（server/internal/model/setting/types.go）。 */
export type SettingKey =
  | 'dashscope_api_key'
  | 't8star_api_key'
  | 'region'
  | 'dashscope_endpoint'
  | 't8star_endpoint'
  | 'workspace_id'
  | 'oss_bucket'
  | 'oss_region'
  | 'oss_role_arn'
  | 'oss_access_key_id'
  | 'oss_access_key_secret'

/**
 * 对应后端 settingmodel.SettingResponse。
 *
 * 非 secret 项：value 是明文，configured/masked 恒为 undefined。
 * secret 项：value 恒为空（后端用 omitempty 连字段都不下发），只有
 * configured（是否已配置）与 masked（首尾脱敏展示值）——明文永远不会
 * 出现在这个类型里的任何字段。
 */
export interface SettingItem {
  key: string
  isSecret: boolean
  value?: string
  configured?: boolean
  masked?: string
}

export interface SettingsResponse {
  items: SettingItem[]
}

/**
 * 对应后端 settingmodel.UpdateItem。
 *
 * value 留空表示"不修改"，而不是"清空"——这是这个类型存在的核心语义
 * 陷阱，前端表单必须按这个约定构造请求体，否则会把用户没碰过的密钥
 * 一并清空。clear 为 true 时才真正清空该项（忽略 value）。
 */
export interface UpdateSettingItem {
  key: SettingKey
  value: string
  clear: boolean
}

export interface UpdateSettingsRequest {
  items: UpdateSettingItem[]
}

/**
 * 对应后端 settingmodel.TestConnectionRequest.Provider（POST /api/settings/test）。
 *
 * 两个 provider 的测试成本天差地别：DashScope 探测的是文本模型，几厘钱
 * 级别；t8star 没有独立的鉴权探测接口，唯一的端点就是出图接口，测试即
 * 一次真实请求。UI 层必须为 t8star 单独展示费用说明，不能把两者当成
 * 对称的按钮处理。
 */
export type TestConnectionProvider = 'dashscope' | 't8star'
