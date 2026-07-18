import { describe, it, expect } from 'vitest'
import zhCN from './zh-CN.json'
import en from './en.json'

/** 把嵌套对象展平成 'a.b.c' 形式的 key 列表。 */
function flatten(obj: Record<string, unknown>, prefix = ''): string[] {
  return Object.entries(obj).flatMap(([k, v]) => {
    const key = prefix ? `${prefix}.${k}` : k
    return v !== null && typeof v === 'object'
      ? flatten(v as Record<string, unknown>, key)
      : [key]
  })
}

describe('语言文件', () => {
  const zhKeys = flatten(zhCN).sort()
  const enKeys = flatten(en).sort()

  // 旧系统就是靠人工同步两份翻译，漏译到线上才发现。这条测试是闸门。
  it('中英 key 完全一致', () => {
    expect(enKeys.filter((k) => !zhKeys.includes(k))).toEqual([])
    expect(zhKeys.filter((k) => !enKeys.includes(k))).toEqual([])
  })

  it('没有空文案', () => {
    const findEmpty = (obj: Record<string, unknown>, prefix = ''): string[] =>
      Object.entries(obj).flatMap(([k, v]) => {
        const key = prefix ? `${prefix}.${k}` : k
        if (v !== null && typeof v === 'object') return findEmpty(v as Record<string, unknown>, key)
        return typeof v === 'string' && v.trim() === '' ? [key] : []
      })

    expect(findEmpty(zhCN)).toEqual([])
    expect(findEmpty(en)).toEqual([])
  })

  // 后端每个错误码都必须有对应文案，否则用户会看到裸露的 code。
  it('errors 命名空间覆盖后端全部错误码', () => {
    const required = [
      'AUTH_INVALID_CREDENTIALS', 'AUTH_UNAUTHORIZED', 'AUTH_USER_DISABLED',
      'AUTH_FORBIDDEN', 'AUTH_WRONG_OLD_PASSWORD', 'USER_NOT_FOUND',
      'USER_USERNAME_TAKEN', 'USER_CANNOT_MODIFY_SELF', 'USER_LAST_ADMIN',
      'USER_PASSWORD_TOO_LONG', 'SETTING_NOT_FOUND', 'SETTING_INCOMPLETE', 'SETTING_TEST_FAILED',
      'VALIDATION_FAILED', 'INTERNAL_ERROR', 'UNKNOWN',
      'TASK_NOT_FOUND', 'TASK_POLL_FAILED', 'TASK_TIMEOUT', 'DOWNLOAD_FAILED',
      // 生成类接口（图片/视频/prompt 优化）的上游错误归一化产物——见
      // server/internal/pkg/apperr/apperr.go 与
      // server/internal/service/upstream_error.go。一个失效/被吊销的
      // API key 曾经会让上游 401 冒充成我们自己的 401、把用户直接登出
      // （web/src/api/client.ts 的拦截器修复了这一半），这三个码是后端那
      // 一半：把上游状态收口成小集合，绝不再泄漏成裸 401。
      'UPSTREAM_AUTH_FAILED', 'UPSTREAM_RATE_LIMITED', 'UPSTREAM_FAILED',
      // provider 包自己的错误码——正常情况下已经被上面三个码在 service 层
      // 收口掉，不会再出现在新请求的响应里；保留翻译是为了这两种历史留存
      // 场景：(1) 归一化上线之前落库的旧 FAILED 任务行仍然带着这些码，
      // (2) 万一未来某条新路径忘了接 normalizeUpstreamError，也不会让用户
      // 看到裸 code 或英文报错。
      'DASHSCOPE_REQUEST_FAILED', 'DASHSCOPE_UPSTREAM_HTTP_ERROR', 'DASHSCOPE_UPSTREAM_ERROR',
      'DASHSCOPE_NO_IMAGE_RESULT', 'DASHSCOPE_NO_TASK_ID', 'DASHSCOPE_NO_OPTIMIZE_RESULT',
      'T8STAR_UPSTREAM_HTTP_ERROR', 'T8STAR_UPSTREAM_ERROR', 'T8STAR_NO_IMAGE_RESULT',
    ]
    required.forEach((code) => {
      expect(zhCN.errors, `zh-CN 缺少 ${code}`).toHaveProperty(code)
      expect(en.errors, `en 缺少 ${code}`).toHaveProperty(code)
    })
  })
})
