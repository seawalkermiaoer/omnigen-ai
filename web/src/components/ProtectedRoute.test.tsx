import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

import ProtectedRoute from './ProtectedRoute'
import AdminRoute from './AdminRoute'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types/user'

const adminUser: User = {
  id: 1, username: 'admin', displayName: 'Admin', role: 'admin',
  status: 'active', createdAt: '', updatedAt: '',
}
const plainUser: User = { ...adminUser, id: 2, username: 'bob', role: 'user' }

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/login" element={<div>登录页</div>} />
        <Route element={<ProtectedRoute />}>
          <Route path="/home" element={<div>受保护内容</div>} />
          <Route element={<AdminRoute />}>
            <Route path="/users" element={<div>管理员内容</div>} />
          </Route>
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

describe('路由守卫', () => {
  beforeEach(() => {
    useAuthStore.setState({ token: null, user: null, initializing: false })
  })

  it('未登录访问受保护路由时跳转登录页', () => {
    renderAt('/home')
    expect(screen.getByText('登录页')).toBeInTheDocument()
    expect(screen.queryByText('受保护内容')).not.toBeInTheDocument()
  })

  it('已登录可访问受保护路由', () => {
    useAuthStore.setState({ token: 'tok', user: plainUser, initializing: false })
    renderAt('/home')
    expect(screen.getByText('受保护内容')).toBeInTheDocument()
  })

  // 验活未完成时不能渲染内容，也不能急着跳登录页——
  // 否则刷新页面会闪一下登录页再跳回来。
  it('验活进行中时既不渲染内容也不跳转', () => {
    useAuthStore.setState({ token: 'tok', user: null, initializing: true })
    renderAt('/home')
    expect(screen.queryByText('受保护内容')).not.toBeInTheDocument()
    expect(screen.queryByText('登录页')).not.toBeInTheDocument()
  })

  it('普通用户访问管理员路由被挡下', () => {
    useAuthStore.setState({ token: 'tok', user: plainUser, initializing: false })
    renderAt('/users')
    expect(screen.queryByText('管理员内容')).not.toBeInTheDocument()
  })

  it('管理员可访问管理员路由', () => {
    useAuthStore.setState({ token: 'tok', user: adminUser, initializing: false })
    renderAt('/users')
    expect(screen.getByText('管理员内容')).toBeInTheDocument()
  })
})
