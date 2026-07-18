import { describe, it, expect } from 'vitest'
import { theme as antdTheme } from 'antd'
import { omnigenTheme, colors } from './index'

describe('主题配置', () => {
  it('固定使用暗色算法', () => {
    expect(omnigenTheme.algorithm).toBe(antdTheme.darkAlgorithm)
  })

  it('暴露品牌主色与背景色 token', () => {
    expect(omnigenTheme.token?.colorPrimary).toBe(colors.primary)
    expect(omnigenTheme.token?.colorBgBase).toBe(colors.bgBase)
  })

  it('所有色值为合法十六进制，避免拼写错误静默生效', () => {
    Object.entries(colors).forEach(([name, value]) => {
      expect(value, `${name} 不是合法色值`).toMatch(/^#[0-9a-fA-F]{6}$/)
    })
  })
})
