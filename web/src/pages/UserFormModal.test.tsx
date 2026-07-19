import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App as AntdApp, ConfigProvider } from 'antd'
import { I18nextProvider } from 'react-i18next'

import i18n from '@/i18n'
import { omnigenTheme } from '@/theme'
import UserFormModal from './UserFormModal'
import type { User } from '@/types/user'

const editingUser: User = {
  id: 2, username: 'bob', displayName: 'Bob', role: 'user', status: 'active',
  createdAt: '2026-07-18T00:00:00Z', updatedAt: '2026-07-18T00:00:00Z',
  quotaTotal: 50, quotaUsed: 12,
}

function renderModal(props: Partial<React.ComponentProps<typeof UserFormModal>> = {}) {
  const onCreate = vi.fn().mockResolvedValue(undefined)
  const onUpdate = vi.fn().mockResolvedValue(undefined)
  const onCancel = vi.fn()
  const utils = render(
    <I18nextProvider i18n={i18n}>
      <ConfigProvider theme={omnigenTheme}>
        <AntdApp>
          <UserFormModal
            open
            editing={null}
            submitting={false}
            onCancel={onCancel}
            onCreate={onCreate}
            onUpdate={onUpdate}
            {...props}
          />
        </AntdApp>
      </ConfigProvider>
    </I18nextProvider>,
  )
  return { ...utils, onCreate, onUpdate, onCancel }
}

describe('UserFormModal 额度字段', () => {
  it('新建表单默认预填 100 并随请求一起提交', async () => {
    const user = userEvent.setup()
    const { onCreate } = renderModal()

    await user.type(await screen.findByLabelText(i18n.t('users.username')), 'carol')
    await user.type(screen.getByLabelText(i18n.t('login.password')), 'password123')
    await user.click(screen.getByRole('button', { name: i18n.t('common.confirm') }))

    await waitFor(() => expect(onCreate).toHaveBeenCalled())
    expect(onCreate.mock.calls[0][0]).toMatchObject({ username: 'carol', quotaTotal: 100 })
  })

  it('新建表单可以改写额度值', async () => {
    const user = userEvent.setup()
    const { onCreate } = renderModal()

    await user.type(await screen.findByLabelText(i18n.t('users.username')), 'dave')
    await user.type(screen.getByLabelText(i18n.t('login.password')), 'password123')

    const quotaInput = screen.getByLabelText(i18n.t('users.quotaTotalLabel'))
    await user.clear(quotaInput)
    await user.type(quotaInput, '300')

    await user.click(screen.getByRole('button', { name: i18n.t('common.confirm') }))

    await waitFor(() => expect(onCreate).toHaveBeenCalled())
    expect(onCreate.mock.calls[0][0]).toMatchObject({ quotaTotal: 300 })
  })

  it('编辑表单提交带上当前额度', async () => {
    const user = userEvent.setup()
    const { onUpdate } = renderModal({ editing: editingUser })

    await screen.findByDisplayValue('bob')
    await user.click(screen.getByRole('button', { name: i18n.t('common.confirm') }))

    await waitFor(() => expect(onUpdate).toHaveBeenCalled())
    expect(onUpdate.mock.calls[0][1]).toMatchObject({ quotaTotal: 50 })
    expect(onUpdate.mock.calls[0][1]).not.toHaveProperty('quotaUnlimited')
  })

  // 编辑表单打开「不限量」开关后，必须发送 quotaUnlimited: true，
  // 且不再把 quotaTotal 一并发出去误导后端（见 handleOk 注释）。
  it('打开不限量开关后提交 QuotaUnlimited，不再带 quotaTotal', async () => {
    const user = userEvent.setup()
    const { onUpdate } = renderModal({ editing: editingUser })

    await screen.findByDisplayValue('bob')
    await user.click(screen.getByLabelText(i18n.t('users.quotaUnlimitedSwitch')))
    await user.click(screen.getByRole('button', { name: i18n.t('common.confirm') }))

    await waitFor(() => expect(onUpdate).toHaveBeenCalled())
    expect(onUpdate.mock.calls[0][1]).toMatchObject({ quotaUnlimited: true })
    expect(onUpdate.mock.calls[0][1]).not.toHaveProperty('quotaTotal')
  })

  it('打开不限量开关后额度输入框被禁用', async () => {
    const user = userEvent.setup()
    renderModal({ editing: editingUser })

    await screen.findByDisplayValue('bob')
    const quotaInput = screen.getByLabelText(i18n.t('users.quotaTotalLabel'))
    expect(quotaInput).toBeEnabled()

    await user.click(screen.getByLabelText(i18n.t('users.quotaUnlimitedSwitch')))
    expect(quotaInput).toBeDisabled()
  })
})
