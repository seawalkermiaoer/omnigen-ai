import { useEffect, useState } from 'react'
import { Form, Input, InputNumber, Modal, Select, Switch } from 'antd'
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

// 新建用户在未显式改动时的默认额度，需与后端 UserService 的
// defaultQuotaTotal 保持一致，仅用于表单预填——真正的默认值判定发生在后端，
// 前端不传 quotaTotal 时后端仍会补上同样的 100。
const DEFAULT_QUOTA_TOTAL = 100

interface FormValues {
  username: string
  password: string
  displayName: string
  role: Role
  status: UserStatus
  quotaTotal: number
  quotaUnlimited: boolean
}

export default function UserFormModal({
  open, editing, submitting, onCancel, onCreate, onUpdate,
}: Props) {
  const { t } = useTranslation()
  const [form] = Form.useForm<FormValues>()
  const [internalSubmitting, setInternalSubmitting] = useState(false)
  // 编辑态才有这个字段（见下方 JSX），新建表单里 useWatch 拿到 undefined，
  // 数字输入框保持可用——与「不限量在创建时无法通过 API 表达」这条后端
  // 限制（usermodel.CreateRequest 没有 QuotaUnlimited）保持一致。
  const quotaUnlimited = Form.useWatch('quotaUnlimited', form)

  useEffect(() => {
    if (!open) return
    if (editing) {
      form.setFieldsValue({
        username: editing.username,
        displayName: editing.displayName,
        role: editing.role,
        status: editing.status,
        // quotaTotal 为 null 表示不限量：数字输入框此时会被禁用，仍给它填
        // 一个可见的占位值，避免开关切回"限量"时输入框空着无从下手。
        quotaTotal: editing.quotaTotal ?? DEFAULT_QUOTA_TOTAL,
        quotaUnlimited: editing.quotaTotal === null,
      })
    } else {
      form.resetFields()
      form.setFieldsValue({ role: 'user', status: 'active', quotaTotal: DEFAULT_QUOTA_TOTAL })
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
          // quotaUnlimited=true 时不必也传 quotaTotal——后端以
          // quotaUnlimited 为准，同传只会造成误导（见 UpdateUserRequest 注释）。
          ...(values.quotaUnlimited
            ? { quotaUnlimited: true }
            : { quotaTotal: values.quotaTotal }),
        })
      } else {
        await onCreate({
          username: values.username,
          password: values.password,
          displayName: values.displayName,
          role: values.role,
          quotaTotal: values.quotaTotal,
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

        <Form.Item
          name="quotaTotal"
          label={t('users.quotaTotalLabel')}
          // 不限量开启时不要求填写额度：此时输入框被禁用，其值不会被提交
          // （见 handleOk），required 校验会挡住一个用户根本看不到必要性的错误。
          rules={[{ required: !quotaUnlimited, type: 'number', min: 0, message: t('users.quotaTotalRule') }]}
        >
          <InputNumber min={0} style={{ width: '100%' }} disabled={!!quotaUnlimited} />
        </Form.Item>

        {/* 不限量开关只在编辑态出现：CreateRequest 没有 QuotaUnlimited，
            新建时无法通过 API 直接把用户创建为不限量（见后端 usermodel.CreateRequest
            注释）——需要的话，创建后立刻编辑一次即可打开。 */}
        {editing && (
          <Form.Item
            name="quotaUnlimited"
            label={t('users.quotaUnlimitedSwitch')}
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
        )}
      </Form>
    </Modal>
  )
}
