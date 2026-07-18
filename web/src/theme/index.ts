import { theme, type ThemeConfig } from 'antd'

/**
 * 全系统唯一允许定义颜色的地方。
 * 组件中不得出现硬编码色值——子项目 2-4 会引入大量新页面，
 * 这是唯一能守住视觉一致性的手段。
 */
export const colors = {
  primary: '#6366f1',
  primaryHover: '#818cf8',
  bgBase: '#0d0d0f',
  bgContainer: '#131317',
  bgElevated: '#17171b',
  border: '#26262c',
  borderStrong: '#2c2c33',
  textBase: '#f2f2f4',
  textMuted: '#6b6b76',
  success: '#059669',
  warning: '#f59e0b',
  error: '#f43f5e',
} as const

export const omnigenTheme: ThemeConfig = {
  algorithm: theme.darkAlgorithm,
  token: {
    colorPrimary: colors.primary,
    colorBgBase: colors.bgBase,
    colorBgContainer: colors.bgContainer,
    colorBgElevated: colors.bgElevated,
    colorBorder: colors.border,
    colorText: colors.textBase,
    colorTextSecondary: colors.textMuted,
    colorSuccess: colors.success,
    colorWarning: colors.warning,
    colorError: colors.error,
    borderRadius: 8,
    fontFamily:
      "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif",
  },
  components: {
    Layout: {
      siderBg: colors.bgContainer,
      headerBg: colors.bgContainer,
      bodyBg: colors.bgBase,
    },
    Menu: {
      darkItemBg: colors.bgContainer,
      darkItemSelectedBg: colors.primary,
    },
  },
}
