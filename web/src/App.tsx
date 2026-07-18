import { useEffect } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'

import AppShell from '@/layouts/AppShell'
import ProtectedRoute from '@/components/ProtectedRoute'
import AdminRoute from '@/components/AdminRoute'
import LoginPage from '@/pages/LoginPage'
import UsersPage from '@/pages/UsersPage'
import PlaceholderPage from '@/pages/PlaceholderPage'
import ImageGenPage from '@/pages/generation/ImageGenPage'
import ImageEditPage from '@/pages/generation/ImageEditPage'
import T2VPage from '@/pages/generation/T2VPage'
import I2VPage from '@/pages/generation/I2VPage'
import R2VPage from '@/pages/generation/R2VPage'
import { useAuthStore } from '@/stores/auth'

export default function App() {
  const initialize = useAuthStore((s) => s.initialize)

  // 启动时用 /auth/me 验活，避免持过期 token 闪现主界面。
  useEffect(() => {
    void initialize()
  }, [initialize])

  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />

      <Route element={<ProtectedRoute />}>
        <Route element={<AppShell />}>
          <Route index element={<Navigate to="/imggen" replace />} />
          <Route path="/imggen" element={<ImageGenPage />} />
          <Route path="/imgedit" element={<ImageEditPage />} />
          <Route path="/t2v" element={<T2VPage />} />
          <Route path="/i2v" element={<I2VPage />} />
          <Route path="/r2v" element={<R2VPage />} />
          <Route path="/history" element={<PlaceholderPage nameKey="nav.history" />} />

          <Route element={<AdminRoute />}>
            <Route path="/users" element={<UsersPage />} />
          </Route>
        </Route>
      </Route>

      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
