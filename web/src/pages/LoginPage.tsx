import { useState, type CSSProperties } from 'react'
import { Alert, Button, Form, Input, Segmented, Typography } from 'antd'
import { LockOutlined, UserOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/hooks/useApiError'
import { colors } from '@/theme'
import { setLocale, getStoredLocale, type Locale } from '@/i18n'
import type { LoginRequest } from '@/types/auth'
import './LoginPage.css'

const { Text } = Typography

// 左半屏的能力展示：装饰性渐变色块，非 UI 色彩系统的一部分。
// 子项目 3（图片生成）完成后可替换为系统真实生成的作品缩略图，届时本数组整体移除。
const TILE_GRADIENTS = [
  'linear-gradient(135deg, #7c3aed, #2563eb)',
  'linear-gradient(135deg, #db2777, #f59e0b)',
  'linear-gradient(135deg, #059669, #14b8a6)',
  'linear-gradient(135deg, #f43f5e, #7c3aed)',
  'linear-gradient(135deg, #0ea5e9, #6366f1)',
  'linear-gradient(135deg, #f59e0b, #db2777)',
]

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
          <span className="login-brand__mark" />
          <span>{t('app.title')}</span>
        </div>

        <div>
          <div className="login-brand__tagline">{t('login.brandTagline')}</div>
          <div className="login-brand__grid">
            {TILE_GRADIENTS.map((bg) => (
              <div key={bg} className="login-brand__tile" style={{ background: bg }} />
            ))}
          </div>
        </div>

        <div className="login-brand__features">
          <span>{t('login.featureImage')}</span>
          <span>{t('login.featureVideo')}</span>
          <span>{t('login.featureHistory')}</span>
        </div>
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
