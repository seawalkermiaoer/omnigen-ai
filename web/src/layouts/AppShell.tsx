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
// 剩余额度不高于这个阈值时，topbar 的剩余次数改用警告色——纯粹的视觉提醒，
// 不阻断任何操作，真正的拦截发生在后端 ConsumeQuota。
const QUOTA_WARNING_THRESHOLD = 10

interface NavItem {
  key: string
  path: string
  icon: ReactNode
  labelKey: string
  adminOnly?: boolean
}

// 顺序与旧系统 public/index.html + public/locales/zh-CN.json 的侧栏顺序一一对应
// （r2v → i2v → t2v → imggen → imgedit → history → settings），这是用户的
// 肌肉记忆，不能按"新功能感觉上更重要"重排。用户管理是本次重写新增的
// 管理员入口，插在 history 之后、settings 之前，让原本七项的相对顺序保持不变。
const NAV_ITEMS: NavItem[] = [
  { key: 'r2v', path: '/r2v', icon: <VideoCameraOutlined />, labelKey: 'nav.r2v' },
  { key: 'i2v', path: '/i2v', icon: <VideoCameraOutlined />, labelKey: 'nav.i2v' },
  { key: 't2v', path: '/t2v', icon: <PlayCircleOutlined />, labelKey: 'nav.t2v' },
  { key: 'imggen', path: '/imggen', icon: <PictureOutlined />, labelKey: 'nav.imggen' },
  { key: 'imgedit', path: '/imgedit', icon: <EditOutlined />, labelKey: 'nav.imgedit' },
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
  // 窄视口（antd Sider 的 md 断点，<768px）下强制收起为图标栏，与用户手动
  // 偏好分开存储——否则在桌面端展开侧栏、切到手机宽度访问时，220px 的
  // 展开侧栏会把头部（标题/语言切换/用户菜单）挤出可视区甚至互相重叠。
  // 回到宽屏后自动恢复用户原本的展开/收起偏好。
  const [autoCollapsed, setAutoCollapsed] = useState(false)
  const [locale, setLocaleState] = useState<Locale>(getStoredLocale())
  const [pwdOpen, setPwdOpen] = useState(false)

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      localStorage.setItem(COLLAPSE_KEY, String(!prev))
      return !prev
    })
  }

  // quotaTotal 为 null 表示不限量，此时不展示剩余次数——见下方渲染处的说明。
  // 管理员可以把 quotaTotal 改到低于当前 quotaUsed（后端校验只保证
  // quotaTotal >= 0，不保证 >= quotaUsed），这里必须夹到 0，否则会显示
  // "剩余 -5 次"——后端 ConsumeQuota 本身不受影响（quota_used < quota_total
  // 一样会拦住），这纯粹是展示层的问题。
  const quotaRemaining =
    user?.quotaTotal == null ? null : Math.max(0, user.quotaTotal - user.quotaUsed)

  const effectiveCollapsed = collapsed || autoCollapsed
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
        collapsed={effectiveCollapsed}
        trigger={null}
        collapsedWidth={64}
        width={220}
        theme="dark"
        breakpoint="md"
        onBreakpoint={(broken) => setAutoCollapsed(broken)}
      >
        <div className="shell-logo">
          <span className="shell-logo__mark" />
          {!effectiveCollapsed && <span className="shell-logo__text">{t('app.title')}</span>}
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
            <Tooltip title={effectiveCollapsed ? t('common.expand') : t('common.collapse')}>
              <button
                type="button"
                className="shell-toggle"
                aria-label={effectiveCollapsed ? t('common.expand') : t('common.collapse')}
                onClick={toggleCollapsed}
              >
                {effectiveCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
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
            {/* quotaRemaining 为 null 时（不限量）整块不渲染——一个裸数字配不上
                分母会让人误以为额度快用完了，不如干脆不显示。
                剩余为 0 时文案不做区分（用完了 vs 被管理员把额度调低到已用量
                以下），两种情况用户能做的事完全一样——都是"当前不能再生成，
                找管理员"，分开措辞除了多一套 i18n key 不会带来任何可执行的
                额外信息。 */}
            {quotaRemaining !== null && (
              <Tooltip title={t('users.quotaRemainingTooltip')}>
                <span
                  className="shell-quota"
                  data-testid="quota-remaining"
                  style={{
                    color: quotaRemaining <= QUOTA_WARNING_THRESHOLD ? colors.warning : colors.textMuted,
                  }}
                >
                  {t('users.quotaRemaining', { count: quotaRemaining })}
                </span>
              </Tooltip>
            )}
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
