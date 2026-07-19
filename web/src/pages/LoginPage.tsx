import { useState, type CSSProperties } from 'react'
import { Alert, Button, Form, Input, Segmented, Typography } from 'antd'
import {
  HistoryOutlined,
  LockOutlined,
  PictureOutlined,
  UserOutlined,
  VideoCameraOutlined,
} from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

import BrandMark from '@/components/BrandMark'

import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/hooks/useApiError'
import { colors } from '@/theme'
import { setLocale, getStoredLocale, type Locale } from '@/i18n'
import type { LoginRequest } from '@/types/auth'
import './LoginPage.css'

const { Text } = Typography

// 左半屏的能力展示：装饰性渐变色块，非 UI 色彩系统的一部分。
// 子项目 3（图片生成）完成后可替换为系统真实生成的作品缩略图，届时本数组整体移除。
/**
 * 左侧展示的三项能力。原先这里是六个纯 CSS 渐变方块，摆成 3×2 的网格假装
 * 是作品示例——它们不是任何真实产出，只是装饰，而真正有信息量的三行能力
 * 说明被压在最底下当灰色小字。现在把能力提到主位，装饰降级为背景光。
 *
 * 图标沿用 AppShell 侧栏导航同一套，让登录页承诺的能力与登录后看到的入口
 * 在视觉上对得上。
 */
const BRAND_FEATURES = [
  { key: 'featureImage', icon: <PictureOutlined /> },
  { key: 'featureVideo', icon: <VideoCameraOutlined /> },
  { key: 'featureHistory', icon: <HistoryOutlined /> },
] as const

// 注入给 LoginPage.css 使用的主题色变量，全部来自 @/theme 的 colors。
const shellStyle: CSSProperties = {
  '--omnigen-bg-base': colors.bgBase,
  '--omnigen-bg-container': colors.bgContainer,
  '--omnigen-border': colors.border,
  '--omnigen-text-base': colors.textBase,
  '--omnigen-text-muted': colors.textMuted,
} as CSSProperties

export default function LoginPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const login = useAuthStore((s) => s.login)
  const { toMessage } = useApiError()

  const [submitting, setSubmitting] = useState(false)
  const [errorText, setErrorText] = useState<string | null>(null)
  const [locale, setLocaleState] = useState<Locale>(getStoredLocale())

  const handleLocaleChange = (value: string | number) => {
    const next = value as Locale
    setLocale(next)
    setLocaleState(next)
  }

  const handleSubmit = async (values: LoginRequest) => {
    setSubmitting(true)
    setErrorText(null)
    try {
      await login(values)
      navigate('/', { replace: true })
    } catch (err) {
      setErrorText(toMessage(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="login-shell" style={shellStyle}>
      <aside className="login-brand">
        <div className="login-brand__logo">
          <BrandMark size={32} />
          {/* 字标的排法必须和侧栏 (.shell-logo__text) 一致：「AI」压暗一档。
              两处 lockup 长得不一样，用户登录前后会觉得是两个产品。 */}
          <span>
            OmniGen<span className="login-brand__logo-dim">AI</span>
          </span>
        </div>

        <div>
          <h1 className="login-brand__tagline">{t('login.brandTagline')}</h1>
          <ul className="login-brand__features">
            {BRAND_FEATURES.map(({ key, icon }) => (
              <li key={key} className="login-brand__feature">
                <span className="login-brand__feature-icon" aria-hidden>
                  {icon}
                </span>
                <span>
                  <span className="login-brand__feature-title">{t(`login.${key}`)}</span>
                  <span className="login-brand__feature-desc">{t(`login.${key}Desc`)}</span>
                </span>
              </li>
            ))}
          </ul>
        </div>

        <p className="login-brand__footnote">{t('login.brandFootnote')}</p>
      </aside>

      <main className="login-form-pane">
        <div className="login-locale" data-testid="locale-switch">
          <Segmented
            size="small"
            value={locale}
            onChange={handleLocaleChange}
            options={[
              { label: '中文', value: 'zh-CN' },
              { label: 'EN', value: 'en' },
            ]}
          />
        </div>

        <div className="login-form">
          <div className="login-form__title">{t('login.title')}</div>
          <Text className="login-form__subtitle" type="secondary">
            {t('login.subtitle')}
          </Text>

          {errorText && (
            <Alert
              className="login-form__alert"
              type="error"
              showIcon
              message={errorText}
              role="alert"
            />
          )}

          <Form layout="vertical" onFinish={handleSubmit} requiredMark={false} size="large">
            <Form.Item
              name="username"
              label={t('login.username')}
              rules={[{ required: true, message: t('login.usernameRequired') }]}
            >
              <Input
                prefix={<UserOutlined />}
                placeholder={t('login.usernamePlaceholder')}
                autoComplete="username"
              />
            </Form.Item>

            <Form.Item
              name="password"
              label={t('login.password')}
              rules={[{ required: true, message: t('login.passwordRequired') }]}
            >
              <Input.Password
                prefix={<LockOutlined />}
                placeholder={t('login.passwordPlaceholder')}
                autoComplete="current-password"
              />
            </Form.Item>

            <Button
              type="primary"
              htmlType="submit"
              block
              loading={submitting}
              autoInsertSpace={false}
            >
              {submitting ? t('login.submitting') : t('login.submit')}
            </Button>
          </Form>
        </div>
      </main>
    </div>
  )
}
