import { useState, type CSSProperties, type ReactNode } from 'react'
import { Avatar, Dropdown, Layout, Menu, Segmented, Tooltip, Typography } from 'antd'
import {
  EditOutlined, HistoryOutlined, LogoutOutlined, MenuFoldOutlined,
  MenuUnfoldOutlined, PictureOutlined, PlayCircleOutlined, SettingOutlined, TeamOutlined,
  UserOutlined, VideoCameraOutlined,
} from '@ant-design/icons'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

import { useAuthStore } from '@/stores/auth'
import { colors } from '@/theme'
import { getStoredLocale, setLocale, type Locale } from '@/i18n'
import ChangePasswordModal from '@/components/ChangePasswordModal'
import './AppShell.css'

const { Header, Sider, Content } = Layout
const { Text } = Typography

const COLLAPSE_KEY = 'omnigen_sider_collapsed'

interface NavItem {
  key: string
  path: string
  icon: ReactNode
  labelKey: string
  adminOnly?: boolean
}

const NAV_ITEMS: NavItem[] = [
  { key: 'imggen', path: '/imggen', icon: <PictureOutlined />, labelKey: 'nav.imggen' },
  { key: 'imgedit', path: '/imgedit', icon: <EditOutlined />, labelKey: 'nav.imgedit' },
  { key: 't2v', path: '/t2v', icon: <PlayCircleOutlined />, labelKey: 'nav.t2v' },
  { key: 'i2v', path: '/i2v', icon: <VideoCameraOutlined />, labelKey: 'nav.i2v' },
  { key: 'r2v', path: '/r2v', icon: <VideoCameraOutlined />, labelKey: 'nav.r2v' },
  { key: 'history', path: '/history', icon: <HistoryOutlined />, labelKey: 'nav.history' },
  { key: 'users', path: '/users', icon: <TeamOutlined />, labelKey: 'nav.users', adminOnly: true },
  { key: 'settings', path: '/settings', icon: <SettingOutlined />, labelKey: 'nav.settings', adminOnly: true },
]

// 注入给 AppShell.css 使用的主题色变量，全部来自 @/theme 的 colors。
const shellVars: CSSProperties = {
  '--omnigen-border': colors.border,
  '--omnigen-bg-container': colors.bgContainer,
  '--omnigen-bg-elevated': colors.bgElevated,
  '--omnigen-text-base': colors.textBase,
  '--omnigen-text-muted': colors.textMuted,
  '--omnigen-primary': colors.primary,
} as CSSProperties

export default function AppShell() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const { user, logout, isAdmin } = useAuthStore()

  // 折叠状态持久化：这是用户的长期偏好，不该每次刷新都重置。
  // 未存过值时默认收起为窄图标栏。
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem(COLLAPSE_KEY) !== 'false',
  )
  const [locale, setLocaleState] = useState<Locale>(getStoredLocale())
  const [pwdOpen, setPwdOpen] = useState(false)

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      localStorage.setItem(COLLAPSE_KEY, String(!prev))
      return !prev
    })
  }

  const visibleItems = NAV_ITEMS.filter((item) => !item.adminOnly || isAdmin())
  const activeKey =
    visibleItems.find((item) => location.pathname.startsWith(item.path))?.key ?? 'imggen'
  const activeLabel = visibleItems.find((item) => item.key === activeKey)?.labelKey

  const handleLocaleChange = (value: string | number) => {
    const next = value as Locale
    setLocale(next)
    setLocaleState(next)
  }

  return (
    <Layout style={{ minHeight: '100vh', ...shellVars }}>
      <Sider
        collapsible
        collapsed={collapsed}
        trigger={null}
        collapsedWidth={64}
        width={220}
        theme="dark"
      >
        <div className="shell-logo">
          <span className="shell-logo__mark" />
          {!collapsed && <span className="shell-logo__text">{t('app.title')}</span>}
        </div>
        <Menu
          className="shell-nav"
          theme="dark"
          mode="inline"
          selectedKeys={[activeKey]}
          onClick={({ key }) => {
            const target = visibleItems.find((i) => i.key === key)
            if (target) navigate(target.path)
          }}
          items={visibleItems.map((item) => ({
            key: item.key,
            icon: item.icon,
            label: t(item.labelKey),
          }))}
        />
      </Sider>

      <Layout>
        <Header className="shell-header">
          <div className="shell-header__left">
            <Tooltip title={collapsed ? t('common.expand') : t('common.collapse')}>
              <button
                type="button"
                className="shell-toggle"
                aria-label={collapsed ? t('common.expand') : t('common.collapse')}
                onClick={toggleCollapsed}
              >
                {collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
              </button>
            </Tooltip>
            <span className="shell-header__title">{activeLabel ? t(activeLabel) : t('app.title')}</span>
          </div>

          <div className="shell-header__right">
            <Segmented
              size="small"
              value={locale}
              onChange={handleLocaleChange}
              options={[
                { label: '中文', value: 'zh-CN' },
                { label: 'EN', value: 'en' },
              ]}
            />
            <Dropdown
              menu={{
                items: [
                  { key: 'password', icon: <UserOutlined />, label: t('common.changePassword') },
                  { type: 'divider' },
                  { key: 'logout', icon: <LogoutOutlined />, label: t('common.logout'), danger: true },
                ],
                onClick: ({ key }) => {
                  if (key === 'logout') void logout()
                  if (key === 'password') setPwdOpen(true)
                },
              }}
            >
              <button type="button" className="shell-user" aria-haspopup="menu">
                <Avatar size="small" style={{ background: colors.primary }}>
                  {(user?.displayName || user?.username || '?').charAt(0).toUpperCase()}
                </Avatar>
                <Text className="shell-user__name">{user?.displayName || user?.username}</Text>
              </button>
            </Dropdown>
          </div>
        </Header>

        <Content className="shell-content">
          <Outlet />
        </Content>
      </Layout>

      <ChangePasswordModal open={pwdOpen} onClose={() => setPwdOpen(false)} />
    </Layout>
  )
}
