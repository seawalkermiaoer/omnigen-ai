import { useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { App } from 'antd'
import { ApiError } from '@/api/client'

/**
 * 把后端错误码翻成用户可读文案。
 * 未知 code 兜底为通用文案，绝不把 code 裸露给用户。
 */
export function useApiError() {
  const { t } = useTranslation()
  const { message } = App.useApp()

  const toMessage = useCallback(
    (error: unknown): string => {
      const code = error instanceof ApiError ? error.code : 'UNKNOWN'
      const key = `errors.${code}`
      const text = t(key)
      return text === key ? t('errors.UNKNOWN') : text
    },
    [t],
  )

  const notify = useCallback(
    (error: unknown) => {
      void message.error(toMessage(error))
    },
    [message, toMessage],
  )

  return { toMessage, notify }
}
