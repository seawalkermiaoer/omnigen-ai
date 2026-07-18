import { useEffect, useState } from 'react'
import { Form, Input, Modal, Select } from 'antd'
import { useTranslation } from 'react-i18next'

import type { CreateUserRequest, Role, UpdateUserRequest, User, UserStatus } from '@/types/user'

interface Props {
  open: boolean
  /** 传 null 表示新建，传用户表示编辑。 */
  editing: User | null
  submitting: boolean
  onCancel: () => void
  onCreate: (req: CreateUserRequest) => Promise<void>
  onUpdate: (id: number, req: UpdateUserRequest) => Promise<void>
}

interface FormValues {
  username: string
  password: string
  displayName: string
  role: Role
  status: UserStatus
}

export default function UserFormModal({
  open, editing, submitting, onCancel, onCreate, onUpdate,
}: Props) {
  const { t } = useTranslation()
  const [form] = Form.useForm<FormValues>()
  const [internalSubmitting, setInternalSubmitting] = useState(false)

  useEffect(() => {
    if (!open) return
    if (editing) {
      form.setFieldsValue({
        username: editing.username,
        displayName: editing.displayName,
        role: editing.role,
        status: editing.status,
      })
    } else {
      form.resetFields()
      form.setFieldsValue({ role: 'user', status: 'active' })
    }
  }, [open, editing, form])

  const handleOk = async () => {
    const values = await form.validateFields()
    setInternalSubmitting(true)
    try {
      if (editing) {
        await onUpdate(editing.id, {
          displayName: values.displayName,
          role: values.role,
          status: values.status,
        })
      } else {
        await onCreate({
          username: values.username,
          password: values.password,
          displayName: values.displayName,
          role: values.role,
        })
      }
    } finally {
      setInternalSubmitting(false)
    }
  }

  return (
    <Modal
      open={open}
      title={editing ? t('users.editTitle') : t('users.createTitle')}
      onCancel={onCancel}
      onOk={handleOk}
      confirmLoading={submitting || internalSubmitting}
      okText={t('common.confirm')}
      cancelText={t('common.cancel')}
      // antd 默认给两字中文按钮文案自动插入视觉空格，这里关掉以保持文案可被精确匹配/朗读。
      okButtonProps={{ autoInsertSpace: false }}
      cancelButtonProps={{ autoInsertSpace: false }}
      destroyOnHidden
      // 编辑态字段更多（含状态），窄弹窗会让 Select 显得拥挤。
      width={editing ? 480 : 420}
    >
      <Form form={form} layout="vertical" requiredMark={false} autoComplete="off">
        <Form.Item
          name="username"
          label={t('users.username')}
          // 用户名不可变时给出提示，而不是让用户猜为什么输入框是灰的。
          tooltip={editing ? t('users.usernameRule') : undefined}
          rules={[
            { required: true, message: t('users.usernameRule') },
            { min: 3, max: 64, pattern: /^[a-zA-Z0-9]+$/, message: t('users.usernameRule') },
          ]}
        >
          {/* 用户名是账号标识，创建后不允许修改 */}
          <Input disabled={!!editing} autoComplete="off" autoFocus={!editing} />
        </Form.Item>

        {!editing && (
          <Form.Item
            name="password"
            label={t('login.password')}
            extra={t('users.passwordRule')}
            rules={[{ required: true, min: 8, max: 72, message: t('users.passwordRule') }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        )}

        <Form.Item name="displayName" label={t('users.displayName')} rules={[{ max: 64 }]}>
          <Input autoComplete="off" />
        </Form.Item>

        <Form.Item name="role" label={t('users.role')} rules={[{ required: true }]}>
          <Select
            options={[
              { value: 'admin', label: t('users.roleAdmin') },
              { value: 'user', label: t('users.roleUser') },
            ]}
          />
        </Form.Item>

        {editing && (
          <Form.Item name="status" label={t('users.status')} rules={[{ required: true }]}>
            <Select
              options={[
                { value: 'active', label: t('users.statusActive') },
                { value: 'disabled', label: t('users.statusDisabled') },
              ]}
            />
          </Form.Item>
        )}
      </Form>
    </Modal>
  )
}
