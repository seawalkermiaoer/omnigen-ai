import { useState } from 'react'
import { App, Form, Input, Modal } from 'antd'
import { useTranslation } from 'react-i18next'

import { authApi } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/hooks/useApiError'

interface Values {
  oldPassword: string
  newPassword: string
  confirmPassword: string
}

export default function ChangePasswordModal({
  open,
  onClose,
}: {
  open: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [form] = Form.useForm<Values>()
  const [submitting, setSubmitting] = useState(false)
  const { notify } = useApiError()
  const { message } = App.useApp()
  const clear = useAuthStore((s) => s.clear)

  const handleOk = async () => {
    const values = await form.validateFields()
    setSubmitting(true)
    try {
      await authApi.changePassword({
        oldPassword: values.oldPassword,
        newPassword: values.newPassword,
      })
      void message.success(t('password.success'))
      form.resetFields()
      onClose()
      // 改密后旧 token 对应的凭据已不可信，强制重新登录。
      clear()
    } catch (err) {
      notify(err)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={open}
      title={t('password.title')}
      onCancel={onClose}
      onOk={handleOk}
      confirmLoading={submitting}
      okText={t('common.confirm')}
      cancelText={t('common.cancel')}
      destroyOnHidden
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item
          name="oldPassword"
          label={t('password.oldPassword')}
          rules={[{ required: true, message: t('login.passwordRequired') }]}
        >
          <Input.Password autoComplete="current-password" />
        </Form.Item>

        <Form.Item
          name="newPassword"
          label={t('password.newPassword')}
          rules={[{ required: true, min: 8, max: 72, message: t('users.passwordRule') }]}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>

        <Form.Item
          name="confirmPassword"
          label={t('password.confirmPassword')}
          dependencies={['newPassword']}
          rules={[
            { required: true, message: t('users.passwordRule') },
            ({ getFieldValue }) => ({
              validator: (_, value) =>
                !value || getFieldValue('newPassword') === value
                  ? Promise.resolve()
                  : Promise.reject(new Error(t('password.mismatch'))),
            }),
          ]}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>
      </Form>
    </Modal>
  )
}
