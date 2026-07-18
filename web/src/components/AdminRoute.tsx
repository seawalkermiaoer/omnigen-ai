import { Navigate, Outlet } from 'react-router-dom'
import { Result } from 'antd'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth'

export default function AdminRoute() {
  const { t } = useTranslation()
  const user = useAuthStore((s) => s.user)

  if (!user) {
    return <Navigate to="/login" replace />
  }
  if (user.role !== 'admin') {
    return <Result status="403" title="403" subTitle={t('errors.AUTH_FORBIDDEN')} />
  }
  return <Outlet />
}
