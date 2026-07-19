import { useCallback, useEffect, useState } from 'react'
import { App, Button, Empty, Form, Input, Modal, Space, Table, Tag, Typography } from 'antd'
import { DeleteOutlined, EditOutlined, KeyOutlined, PlusOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import type { ColumnsType } from 'antd/es/table'

import { userApi } from '@/api/user'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/hooks/useApiError'
import type { CreateUserRequest, UpdateUserRequest, User } from '@/types/user'
import UserFormModal from './UserFormModal'

const { Title } = Typography

interface ResetPasswordFormValues {
  password: string
}

export default function UsersPage() {
  const { t } = useTranslation()
  const { message, modal } = App.useApp()
  const { notify } = useApiError()
  const currentUser = useAuthStore((s) => s.user)

  const [items, setItems] = useState<User[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<User | null>(null)

  // 重置密码是独立于编辑资料的动作：管理员无需旧密码即可直接设置新密码，
  // 用独立的表单弹窗承载，而不是塞进编辑表单或裸 Modal.confirm 里。
  const [resetTarget, setResetTarget] = useState<User | null>(null)
  const [resetSubmitting, setResetSubmitting] = useState(false)
  const [resetForm] = Form.useForm<ResetPasswordFormValues>()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const resp = await userApi.list({ page, pageSize })
      setItems(resp.items)
      setTotal(resp.total)
    } catch (err) {
      notify(err)
    } finally {
      setLoading(false)
    }
  }, [page, pageSize, notify])

  useEffect(() => {
    void load()
  }, [load])

  const handleCreate = async (req: CreateUserRequest) => {
    setSubmitting(true)
    try {
      await userApi.create(req)
      void message.success(t('users.createSuccess'))
      setFormOpen(false)
      await load()
    } catch (err) {
      notify(err)
    } finally {
      setSubmitting(false)
    }
  }

  const handleUpdate = async (id: number, req: UpdateUserRequest) => {
    setSubmitting(true)
    try {
      await userApi.update(id, req)
      void message.success(t('users.updateSuccess'))
      setFormOpen(false)
      await load()
    } catch (err) {
      notify(err)
    } finally {
      setSubmitting(false)
    }
  }

  const handleDelete = (target: User) => {
    modal.confirm({
      title: t('common.delete'),
      content: t('users.deleteConfirm', { name: target.username }),
      okText: t('common.confirm'),
      cancelText: t('common.cancel'),
      // autoInsertSpace: false 关掉两字中文按钮的自动视觉空格。
      okButtonProps: { danger: true, autoInsertSpace: false },
      cancelButtonProps: { autoInsertSpace: false },
      onOk: async () => {
        try {
          await userApi.remove(target.id)
          void message.success(t('users.deleteSuccess'))
          await load()
        } catch (err) {
          notify(err)
        }
      },
    })
  }

  const openResetPassword = (target: User) => {
    resetForm.resetFields()
    setResetTarget(target)
  }

  const handleResetPasswordSubmit = async () => {
    if (!resetTarget) return
    const values = await resetForm.validateFields()
    setResetSubmitting(true)
    try {
      await userApi.resetPassword(resetTarget.id, { password: values.password })
      void message.success(t('users.resetSuccess'))
      setResetTarget(null)
    } catch (err) {
      notify(err)
    } finally {
      setResetSubmitting(false)
    }
  }

  // 与历史记录页同样的道理：给除"显示名"外的每一列一个明确宽度，"显示名"
  // 不设宽度吃剩余空间，再配合 Table 的 scroll.x——窄屏下整表横向滚动，
  // 而不是把某一列挤没。
  const USERNAME_COL_WIDTH = 140
  const ROLE_COL_WIDTH = 100
  const STATUS_COL_WIDTH = 100
  const CREATED_COL_WIDTH = 170
  const USERS_ACTIONS_COL_WIDTH = 260
  const USERS_TABLE_SCROLL_X =
    USERNAME_COL_WIDTH + ROLE_COL_WIDTH + STATUS_COL_WIDTH + CREATED_COL_WIDTH + USERS_ACTIONS_COL_WIDTH + 160

  const columns: ColumnsType<User> = [
    { title: t('users.username'), dataIndex: 'username', key: 'username', width: USERNAME_COL_WIDTH },
    { title: t('users.displayName'), dataIndex: 'displayName', key: 'displayName' },
    {
      title: t('users.role'),
      dataIndex: 'role',
      key: 'role',
      width: ROLE_COL_WIDTH,
      render: (role: User['role']) => (
        <Tag color={role === 'admin' ? 'purple' : 'default'}>
          {role === 'admin' ? t('users.roleAdmin') : t('users.roleUser')}
        </Tag>
      ),
    },
    {
      title: t('users.status'),
      dataIndex: 'status',
      key: 'status',
      width: STATUS_COL_WIDTH,
      render: (status: User['status']) => (
        <Tag color={status === 'active' ? 'success' : 'error'}>
          {status === 'active' ? t('users.statusActive') : t('users.statusDisabled')}
        </Tag>
      ),
    },
    {
      title: t('users.createdAt'),
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: CREATED_COL_WIDTH,
      render: (v: string) => new Date(v).toLocaleString(),
    },
    {
      title: t('common.actions'),
      key: 'actions',
      width: USERS_ACTIONS_COL_WIDTH,
      render: (_, record) => {
        // 后端对自我删除已有护栏；前端一并隐藏，避免用户点了才被拒。
        const isSelf = record.id === currentUser?.id
        return (
          <Space size="small">
            <Button
              size="small"
              // 图标是纯装饰，按钮已有可见文案；隐藏图标避免读屏器重复播报。
              icon={<EditOutlined aria-hidden />}
              data-testid={`edit-user-${record.id}`}
              onClick={() => {
                setEditing(record)
                setFormOpen(true)
              }}
            >
              {t('common.edit')}
            </Button>
            <Button
              size="small"
              icon={<KeyOutlined aria-hidden />}
              data-testid={`reset-user-${record.id}`}
              onClick={() => openResetPassword(record)}
            >
              {t('users.resetPassword')}
            </Button>
            {!isSelf && (
              // 与其余操作留出间距，避免误触——删除是不可逆操作，视觉上应独立成组。
              <span style={{ marginLeft: 4 }}>
                <Button
                  size="small"
                  danger
                  icon={<DeleteOutlined aria-hidden />}
                  data-testid={`delete-user-${record.id}`}
                  onClick={() => handleDelete(record)}
                >
                  {t('common.delete')}
                </Button>
              </span>
            )}
          </Space>
        )
      },
    },
  ]

  return (
    <div>
      {/* flexWrap + gap：窄视口下标题与"新建用户"按钮挤在一行会把标题压缩到
          0 宽度，CJK 文本此时逐字换行——与历史记录页表格内容列是同一类
          bug。允许按钮在窄屏换到标题下方。 */}
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: 12,
          marginBottom: 16,
        }}
      >
        <Title level={3} style={{ margin: 0 }}>{t('users.title')}</Title>
        <Button
          type="primary"
          icon={<PlusOutlined aria-hidden />}
          onClick={() => {
            setEditing(null)
            setFormOpen(true)
          }}
        >
          {t('users.create')}
        </Button>
      </div>

      <Table<User>
        rowKey="id"
        size="middle"
        loading={loading}
        columns={columns}
        dataSource={items}
        tableLayout="fixed"
        scroll={{ x: USERS_TABLE_SCROLL_X }}
        locale={{
          emptyText: (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={t('common.empty')}
              style={{ padding: '32px 0' }}
            />
          ),
        }}
        pagination={{
          current: page,
          pageSize,
          total,
          showSizeChanger: true,
          onChange: (p, ps) => {
            setPage(p)
            setPageSize(ps)
          },
        }}
      />

      <UserFormModal
        open={formOpen}
        editing={editing}
        submitting={submitting}
        onCancel={() => setFormOpen(false)}
        onCreate={handleCreate}
        onUpdate={handleUpdate}
      />

      <Modal
        open={!!resetTarget}
        title={t('users.resetPasswordTitle')}
        onCancel={() => setResetTarget(null)}
        onOk={handleResetPasswordSubmit}
        confirmLoading={resetSubmitting}
        okText={t('common.confirm')}
        cancelText={t('common.cancel')}
        okButtonProps={{ autoInsertSpace: false }}
        cancelButtonProps={{ autoInsertSpace: false }}
        destroyOnHidden
      >
        <Form form={resetForm} layout="vertical" requiredMark={false}>
          <Form.Item
            name="password"
            label={t('users.newPassword')}
            extra={t('users.passwordRule')}
            rules={[{ required: true, min: 8, max: 72, message: t('users.passwordRule') }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
