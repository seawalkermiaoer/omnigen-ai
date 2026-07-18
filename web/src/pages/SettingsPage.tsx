import { useCallback, useEffect, useState } from 'react'
import { Alert, App, Button, Card, Form, Input, Popconfirm, Select, Space, Spin, Typography } from 'antd'
import { DeleteOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'

import { settingApi } from '@/api/setting'
import { useApiError } from '@/hooks/useApiError'
import type { SettingItem, SettingKey, UpdateSettingItem } from '@/types/setting'

const { Title, Text, Paragraph } = Typography

// 四个 secret 项——加密落库，GET 永远拿不到明文，只有 configured/masked。
const SECRET_KEYS: SettingKey[] = [
  'dashscope_api_key',
  't8star_api_key',
  'oss_access_key_id',
  'oss_access_key_secret',
]

// 其余非 secret 项：GET 直接拿明文，PUT 时"清空"与"未修改"要靠对比原值区分
// （见 buildItems），不能像 secret 项那样靠一个独立的清除开关。
const PLAIN_KEYS: SettingKey[] = ['region', 'endpoint', 'workspace_id', 'oss_bucket', 'oss_region', 'oss_role_arn']

const REGION_OPTIONS: { value: string; labelKey: string }[] = [
  { value: 'cn-beijing', labelKey: 'settings.regionCnBeijing' },
  { value: 'ap-southeast-1', labelKey: 'settings.regionApSoutheast1' },
  { value: 'us-east-1', labelKey: 'settings.regionUsEast1' },
  { value: 'eu-central-1', labelKey: 'settings.regionEuCentral1' },
]

interface FormValues {
  dashscope_api_key?: string
  t8star_api_key?: string
  region?: string
  endpoint?: string
  workspace_id?: string
  oss_bucket?: string
  oss_region?: string
  oss_role_arn?: string
  oss_access_key_id?: string
  oss_access_key_secret?: string
}

type TestResult = { ok: true } | { ok: false; message: string }

/**
 * 设置页（admin only，路由层由 AdminRoute 把关）。
 *
 * 核心语义："空值=不修改"：secret 输入框留空提交，服务端保留原密钥；
 * 真要清空必须走显式的"清除"操作（见 secretExtra），不能靠删空文本框
 * 这种容易误触的方式——这是 UpdateItem.Clear 存在的唯一理由。
 *
 * 非 secret 字段（region/endpoint/workspace_id/oss_*）不是 secret，
 * GET 直接给明文，因此表单里天然带着原值：真把它删空提交时，对比原值
 * 就能判断出这是"用户主动清空"而非"没碰过"，自动补上 clear:true，
 * 不需要额外的清除按钮。
 */
export default function SettingsPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const { notify, toMessage } = useApiError()
  const [form] = Form.useForm<FormValues>()

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<TestResult | null>(null)
  const [settingsByKey, setSettingsByKey] = useState<Record<string, SettingItem>>({})
  const [clearedKeys, setClearedKeys] = useState<Set<SettingKey>>(new Set())

  const region = Form.useWatch('region', form)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const resp = await settingApi.get()
      const byKey: Record<string, SettingItem> = {}
      resp.items.forEach((item) => {
        byKey[item.key] = item
      })
      setSettingsByKey(byKey)
      setClearedKeys(new Set())
      form.setFieldsValue({
        region: byKey.region?.value || undefined,
        endpoint: byKey.endpoint?.value || '',
        workspace_id: byKey.workspace_id?.value || '',
        oss_bucket: byKey.oss_bucket?.value || '',
        oss_region: byKey.oss_region?.value || '',
        oss_role_arn: byKey.oss_role_arn?.value || '',
        // secret 项永远从空白开始——GET 从不返回明文，留空即代表"不修改"。
        dashscope_api_key: '',
        t8star_api_key: '',
        oss_access_key_id: '',
        oss_access_key_secret: '',
      })
    } catch (err) {
      notify(err)
    } finally {
      setLoading(false)
    }
  }, [form, notify])

  useEffect(() => {
    void load()
  }, [load])

  const toggleClear = (key: SettingKey) => {
    setClearedKeys((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
        form.setFieldValue(key, '')
      }
      return next
    })
  }

  const buildItems = (values: FormValues): UpdateSettingItem[] => {
    const items: UpdateSettingItem[] = []

    SECRET_KEYS.forEach((key) => {
      if (clearedKeys.has(key)) {
        items.push({ key, value: '', clear: true })
        return
      }
      // 未点"清除"时，无论输入框是否为空都原样发送 value:'', clear:false——
      // 空值命中后端"空值=不修改"分支，原密钥不受影响。
      const raw = values[key as keyof FormValues]
      items.push({ key, value: (raw ?? '').trim(), clear: false })
    })

    PLAIN_KEYS.forEach((key) => {
      const raw = values[key as keyof FormValues]
      const newValue = (raw ?? '').trim()
      const original = settingsByKey[key]?.value ?? ''
      if (newValue === '' && original !== '') {
        // 之前有值、现在被删空并提交——这是明确的"清空"意图。
        items.push({ key, value: '', clear: true })
      } else {
        items.push({ key, value: newValue, clear: false })
      }
    })

    return items
  }

  const handleSave = async () => {
    let values: FormValues
    try {
      values = await form.validateFields()
    } catch {
      // 校验失败：antd 已经把错误信息渲染在对应字段下方，这里只需要吞掉
      // 这个 rejection（不吞会变成未处理的 promise rejection），不用再弹一次提示。
      return
    }
    setSaving(true)
    setTestResult(null)
    try {
      await settingApi.update({ items: buildItems(values) })
      void message.success(t('settings.saveSuccess'))
      await load()
    } catch (err) {
      notify(err)
    } finally {
      setSaving(false)
    }
  }

  const handleTest = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      await settingApi.test()
      setTestResult({ ok: true })
    } catch (err) {
      setTestResult({ ok: false, message: toMessage(err) })
    } finally {
      setTesting(false)
    }
  }

  const secretExtra = (key: SettingKey) => {
    const item = settingsByKey[key]
    if (clearedKeys.has(key)) {
      return (
        <Space size="small">
          <Text type="warning" data-testid={`clear-pending-${key}`}>
            {t('settings.clearPendingHint')}
          </Text>
          <Button size="small" type="link" onClick={() => toggleClear(key)}>
            {t('settings.undoClear')}
          </Button>
        </Space>
      )
    }
    return (
      <Space size="small">
        <Text type="secondary">
          {item?.configured
            ? t('settings.secretConfigured', { masked: item.masked })
            : t('settings.secretNotConfigured')}
        </Text>
        {item?.configured && (
          <Popconfirm
            title={t('settings.clearConfirmTitle')}
            description={t('settings.clearConfirmContent')}
            okText={t('common.confirm')}
            cancelText={t('common.cancel')}
            okButtonProps={{ danger: true, autoInsertSpace: false }}
            cancelButtonProps={{ autoInsertSpace: false }}
            onConfirm={() => toggleClear(key)}
          >
            <Button size="small" danger type="link" icon={<DeleteOutlined aria-hidden />} data-testid={`clear-${key}`}>
              {t('settings.clearAction')}
            </Button>
          </Popconfirm>
        )}
      </Space>
    )
  }

  const secretPlaceholder = (key: SettingKey) => {
    const item = settingsByKey[key]
    return item?.configured
      ? t('settings.secretPlaceholderConfigured', { masked: item.masked })
      : t('settings.secretPlaceholderEmpty')
  }

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', paddingTop: 96 }}>
        <Spin size="large" />
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 720 }}>
      <Title level={3} style={{ marginBottom: 16 }}>
        {t('settings.title')}
      </Title>

      <Form form={form} layout="vertical" requiredMark={false}>
        <Card title={t('settings.sectionCredentials')} style={{ marginBottom: 16 }}>
          <Paragraph type="secondary">{t('settings.sectionCredentialsDesc')}</Paragraph>

          <Form.Item
            name="dashscope_api_key"
            label={t('settings.dashscopeApiKey')}
            extra={secretExtra('dashscope_api_key')}
          >
            <Input.Password
              autoComplete="off"
              disabled={clearedKeys.has('dashscope_api_key')}
              placeholder={secretPlaceholder('dashscope_api_key')}
            />
          </Form.Item>

          <Form.Item name="t8star_api_key" label={t('settings.t8starApiKey')} extra={secretExtra('t8star_api_key')}>
            <Input.Password
              autoComplete="off"
              disabled={clearedKeys.has('t8star_api_key')}
              placeholder={secretPlaceholder('t8star_api_key')}
            />
          </Form.Item>
        </Card>

        <Card title={t('settings.sectionEndpoint')} style={{ marginBottom: 16 }}>
          <Paragraph type="secondary">{t('settings.sectionEndpointDesc')}</Paragraph>

          <Form.Item
            name="region"
            label={t('settings.region')}
            rules={[{ required: true, message: t('settings.regionRequired') }]}
          >
            <Select
              data-testid="settings-region-select"
              placeholder={t('settings.regionPlaceholder')}
              options={REGION_OPTIONS.map((opt) => ({ value: opt.value, label: t(opt.labelKey) }))}
            />
          </Form.Item>

          <Form.Item name="endpoint" label={t('settings.endpoint')} extra={t('settings.endpointHint')}>
            <Input placeholder={t('settings.endpointPlaceholder')} autoComplete="off" />
          </Form.Item>

          <Form.Item
            name="workspace_id"
            label={t('settings.workspaceId')}
            dependencies={['region']}
            extra={region === 'eu-central-1' ? t('settings.workspaceIdRequiredHint') : undefined}
            rules={[
              {
                validator: (_, value: string | undefined) => {
                  if (region === 'eu-central-1' && !value?.trim()) {
                    return Promise.reject(new Error(t('settings.workspaceIdRequiredHint')))
                  }
                  return Promise.resolve()
                },
              },
            ]}
          >
            <Input placeholder={t('settings.workspaceIdPlaceholder')} autoComplete="off" />
          </Form.Item>
        </Card>

        <Card title={t('settings.sectionOSS')} style={{ marginBottom: 16 }}>
          <Paragraph type="secondary">{t('settings.sectionOSSDesc')}</Paragraph>

          <Form.Item
            name="oss_access_key_id"
            label={t('settings.ossAccessKeyId')}
            extra={secretExtra('oss_access_key_id')}
          >
            <Input.Password
              autoComplete="off"
              disabled={clearedKeys.has('oss_access_key_id')}
              placeholder={secretPlaceholder('oss_access_key_id')}
            />
          </Form.Item>

          <Form.Item
            name="oss_access_key_secret"
            label={t('settings.ossAccessKeySecret')}
            extra={secretExtra('oss_access_key_secret')}
          >
            <Input.Password
              autoComplete="off"
              disabled={clearedKeys.has('oss_access_key_secret')}
              placeholder={secretPlaceholder('oss_access_key_secret')}
            />
          </Form.Item>

          <Form.Item name="oss_bucket" label={t('settings.ossBucket')}>
            <Input autoComplete="off" />
          </Form.Item>

          <Form.Item name="oss_region" label={t('settings.ossRegion')}>
            <Input autoComplete="off" />
          </Form.Item>

          <Form.Item name="oss_role_arn" label={t('settings.ossRoleArn')}>
            <Input autoComplete="off" />
          </Form.Item>
        </Card>

        <Space>
          <Button type="primary" onClick={handleSave} loading={saving} data-testid="settings-save">
            {t('settings.save')}
          </Button>
          <Button onClick={handleTest} loading={testing} data-testid="settings-test-connection">
            {t('settings.testConnection')}
          </Button>
        </Space>

        {testResult && (
          <Alert
            style={{ marginTop: 16 }}
            type={testResult.ok ? 'success' : 'error'}
            showIcon
            data-testid="settings-test-result"
            message={testResult.ok ? t('settings.testSuccess') : `${t('settings.testFailedPrefix')}${testResult.message}`}
          />
        )}
      </Form>
    </div>
  )
}
