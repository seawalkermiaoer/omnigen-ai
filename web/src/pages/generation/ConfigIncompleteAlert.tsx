import { Alert } from 'antd'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

/**
 * 未配置生成凭证时的可操作提示：管理员看到"前往设置"的入口，普通用户
 * 只被告知联系管理员——旧系统没有这个状态（密钥就存在浏览器 localStorage
 * 里），新系统凭证只在服务端，SETTING_INCOMPLETE 是这里唯一需要特殊处理
 * 的错误码，不能用 useApiError 的通用 toast 一带而过。
 *
 * 与 ImageGenPage.tsx 里同名组件保持完全一致的文案 key 与 data-testid——
 * 五个生成页面（图片生成/编辑、文生/图生/参考生视频）都会命中同一个
 * SETTING_INCOMPLETE 错误码，用户看到的提示不该因为走的是哪个页面而不同。
 */
export default function ConfigIncompleteAlert({ admin }: { admin: boolean }) {
  const { t } = useTranslation()
  return (
    <Alert
      type="warning"
      showIcon
      data-testid="config-incomplete-alert"
      message={t('generation.notConfiguredTitle')}
      description={
        admin ? (
          <span>
            {t('generation.notConfiguredAdmin')} <Link to="/settings">{t('generation.notConfiguredGoSettings')}</Link>
          </span>
        ) : (
          t('generation.notConfiguredUser')
        )
      }
    />
  )
}
