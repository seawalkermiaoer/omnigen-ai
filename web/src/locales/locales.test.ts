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
      'USER_PASSWORD_TOO_LONG',
      'VALIDATION_FAILED', 'INTERNAL_ERROR', 'UNKNOWN',
    ]
    required.forEach((code) => {
      expect(zhCN.errors, `zh-CN 缺少 ${code}`).toHaveProperty(code)
      expect(en.errors, `en 缺少 ${code}`).toHaveProperty(code)
    })
  })
})
