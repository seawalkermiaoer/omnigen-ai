import { useCallback, useEffect, useState } from 'react'
import { Alert, App, Button, Card, Form, Input, Popconfirm, Radio, Select, Space, Spin, Typography } from 'antd'
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
//
// dashscope_endpoint 不在这里——它不是用户直接编辑的一个文本框，而是由
// "接入方式"（官方/蓝星纪元/自定义）三选一推导出来的，见 buildItems 里的
// 专门处理。t8star_endpoint 仍然是普通文本框，走这里的通用 diff 逻辑。
const PLAIN_KEYS: SettingKey[] = [
  'region',
  't8star_endpoint',
  'workspace_id',
  'oss_bucket',
  'oss_region',
  'oss_role_arn',
]

const REGION_OPTIONS: { value: string; labelKey: string }[] = [
  { value: 'cn-beijing', labelKey: 'settings.regionCnBeijing' },
  { value: 'ap-southeast-1', labelKey: 'settings.regionApSoutheast1' },
  { value: 'us-east-1', labelKey: 'settings.regionUsEast1' },
  { value: 'eu-central-1', labelKey: 'settings.regionEuCentral1' },
]

// 蓝星纪元中转站的固定地址——选中这个接入方式时，dashscope_endpoint 恒等于
// 这个值，用户不能编辑它（也没有编辑的必要，见 public/index.html 旧版
// #endpoint 下拉框里同一个 <option value>）。
const BSUTOPIA_ENDPOINT = 'https://openapi.bsutopia.com'

type AccessMode = 'official' | 'bsutopia' | 'custom'

// 接入方式是从 dashscope_endpoint 的当前值反推出来的派生状态，不是数据库里
// 独立存在的一个字段：空值只可能来自"官方"，等于蓝星纪元固定地址只可能来自
// 那个选项，其余任何非空值都只可能是用户填过的自定义地址。三种情况互斥、
// 穷尽，因此可以在加载时无损地反推、保存时无损地还原。
function deriveAccessMode(dashscopeEndpoint: string): AccessMode {
  if (!dashscopeEndpoint) return 'official'
  if (dashscopeEndpoint === BSUTOPIA_ENDPOINT) return 'bsutopia'
  return 'custom'
}

interface FormValues {
  dashscope_api_key?: string
  accessMode?: AccessMode
  region?: string
  // 只在 accessMode === 'custom' 时被当作 dashscope_endpoint 的来源；官方/
  // 蓝星纪元两种模式下这个字段的值会被 buildItems 忽略，改用推导出的固定值。
  dashscope_endpoint?: string
  workspace_id?: string
  t8star_api_key?: string
  t8star_endpoint?: string
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
 * 非 secret 字段（region/t8star_endpoint/workspace_id/oss_*）不是 secret，
 * GET 直接给明文，因此表单里天然带着原值：真把它删空提交时，对比原值
 * 就能判断出这是"用户主动清空"而非"没碰过"，自动补上 clear:true，
 * 不需要额外的清除按钮。dashscope_endpoint 是例外，见下面 buildItems 的注释。
 *
 * 三张卡片对应三套互不相干的凭证体系（DashScope / t8star / OSS）——按
 * server/internal/model/catalog/catalog.go 的 Protocol 字段，生成页面上
 * 选的模型决定实际使用哪张卡片的凭证，不是"只能配一个"的全局开关。
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
  // t8star 的测试连接状态与百炼卡片的完全分开——两个按钮各打各的 provider，
  // 一个的结果绝不能覆盖或清空另一个的展示（见 handleTest/handleT8starTest）。
  const [t8starTesting, setT8starTesting] = useState(false)
  const [t8starTestResult, setT8starTestResult] = useState<TestResult | null>(null)
  const [settingsByKey, setSettingsByKey] = useState<Record<string, SettingItem>>({})
  const [clearedKeys, setClearedKeys] = useState<Set<SettingKey>>(new Set())

  const accessMode: AccessMode = Form.useWatch('accessMode', form) ?? 'official'
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

      const dashscopeEndpoint = byKey.dashscope_endpoint?.value || ''
      const mode = deriveAccessMode(dashscopeEndpoint)

      form.setFieldsValue({
        accessMode: mode,
        region: byKey.region?.value || undefined,
        // 只有自定义模式下这个字段才代表真实值；官方/蓝星纪元模式下它不会
        // 被渲染也不会被提交，留空即可。
        dashscope_endpoint: mode === 'custom' ? dashscopeEndpoint : '',
        t8star_endpoint: byKey.t8star_endpoint?.value || '',
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
      items.push({ key, value: (typeof raw === 'string' ? raw : '').trim(), clear: false })
    })

    PLAIN_KEYS.forEach((key) => {
      const raw = values[key as keyof FormValues]
      const newValue = (typeof raw === 'string' ? raw : '').trim()
      const original = settingsByKey[key]?.value ?? ''
      if (newValue === '' && original !== '') {
        // 之前有值、现在被删空并提交——这是明确的"清空"意图。
        items.push({ key, value: '', clear: true })
      } else {
        items.push({ key, value: newValue, clear: false })
      }
    })

    // dashscope_endpoint 不是自由文本框，而是由 accessMode 推导出的三选一：
    // 官方 → 空（按地域自动推导，见后端 dashscopeConnectionTester）；
    // 蓝星纪元 → 固定地址；自定义 → 用户填的文本框。切到官方/蓝星纪元时，
    // 用户在自定义框里曾经填过的内容不会被清空——它仍然留在表单的
    // dashscope_endpoint 字段里（antd Form 默认 preserve，字段隐藏不等于
    // 清空），只是这次提交不会用到它；用户切回"自定义"时会看到原样内容。
    let effectiveDashscopeEndpoint = ''
    if (values.accessMode === 'bsutopia') {
      effectiveDashscopeEndpoint = BSUTOPIA_ENDPOINT
    } else if (values.accessMode === 'custom') {
      effectiveDashscopeEndpoint = (values.dashscope_endpoint ?? '').trim()
    }
    const originalDashscopeEndpoint = settingsByKey.dashscope_endpoint?.value ?? ''
    if (effectiveDashscopeEndpoint === '' && originalDashscopeEndpoint !== '') {
      items.push({ key: 'dashscope_endpoint', value: '', clear: true })
    } else {
      items.push({ key: 'dashscope_endpoint', value: effectiveDashscopeEndpoint, clear: false })
    }

    return items
  }

  const handleSave = async () => {
    try {
      await form.validateFields()
    } catch {
      // 校验失败：antd 已经把错误信息渲染在对应字段下方，这里只需要吞掉
      // 这个 rejection（不吞会变成未处理的 promise rejection），不用再弹一次提示。
      return
    }
    // 用 getFieldsValue(true) 而不是 validateFields() 的返回值：地域 select、
    // 自定义接入地址、workspace_id 这几个字段会随 accessMode/region 变化而
    // 隐藏/显示，隐藏时它们不会出现在当次校验范围里；但它们的值仍然留在
    // 表单内部状态中（antd Form 默认 preserve），getFieldsValue(true) 才能
    // 完整取到——否则被隐藏的地域字段会在这里读成 undefined，buildItems
    // 会误判成"用户把地域删空了"，把已保存的地域连带清掉。
    const values = form.getFieldsValue(true) as FormValues
    setSaving(true)
    setTestResult(null)
    setT8starTestResult(null)
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

  // 两个按钮共用同一段"调用 → 记录结果"逻辑，但各自的 loading/result
  // setter 完全独立传入——不共享一份 state，是两个结果互不覆盖的保证。
  const runTest = async (
    provider: 'dashscope' | 't8star',
    setLoadingState: (v: boolean) => void,
    setResultState: (v: TestResult | null) => void,
  ) => {
    setLoadingState(true)
    setResultState(null)
    try {
      await settingApi.test(provider)
      setResultState({ ok: true })
    } catch (err) {
      setResultState({ ok: false, message: toMessage(err) })
    } finally {
      setLoadingState(false)
    }
  }

  const handleTest = () => runTest('dashscope', setTesting, setTestResult)
  const handleT8starTest = () => runTest('t8star', setT8starTesting, setT8starTestResult)

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
    <div style={{ maxWidth: 760 }}>
      <Title level={3} style={{ marginBottom: 16 }}>
        {t('settings.title')}
      </Title>

      <Form form={form} layout="vertical" requiredMark={false}>
        {/* ── Card 1: 阿里云百炼（DashScope） ────────────────────────── */}
        <Card title={t('settings.cardDashscopeTitle')} style={{ marginBottom: 16 }}>
          <Paragraph type="secondary">{t('settings.cardDashscopeDesc')}</Paragraph>

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

          <Form.Item name="accessMode" label={t('settings.accessMode')} extra={t('settings.accessModeHint')}>
            <Radio.Group
              data-testid="settings-access-mode"
              options={[
                { value: 'official', label: t('settings.accessModeOfficial') },
                { value: 'bsutopia', label: t('settings.accessModeBsutopia') },
                { value: 'custom', label: t('settings.accessModeCustom') },
              ]}
            />
          </Form.Item>

          {/* 地域只在"官方"模式下有意义——蓝星纪元是固定地址、自定义地址是
              用户自己指定，两者都不走"按地域推导接入点"这条路径。 */}
          {accessMode === 'official' && (
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
          )}

          {accessMode === 'bsutopia' && (
            <Alert
              type="info"
              showIcon
              message={t('settings.bsutopiaFixedUrlHint')}
              style={{ marginBottom: 24 }}
              data-testid="settings-bsutopia-hint"
            />
          )}

          {accessMode === 'custom' && (
            <Form.Item
              name="dashscope_endpoint"
              label={t('settings.customEndpointLabel')}
              rules={[{ required: true, message: t('settings.customEndpointRequired') }]}
            >
              <Input
                data-testid="settings-custom-endpoint"
                placeholder={t('settings.customEndpointPlaceholder')}
                autoComplete="off"
              />
            </Form.Item>
          )}

          {/* WorkspaceId 只在"官方 + 法兰克福"这一种组合下才有意义：其余
              地域、以及蓝星纪元/自定义两种接入方式都不需要它——法兰克福没有
              固定的官方接入点，见下面 workspaceIdExplain 的解释。 */}
          {accessMode === 'official' && region === 'eu-central-1' && (
            <Form.Item
              name="workspace_id"
              label={t('settings.workspaceId')}
              extra={t('settings.workspaceIdExplain')}
              rules={[{ required: true, message: t('settings.workspaceIdRequiredHint') }]}
            >
              <Input
                data-testid="settings-workspace-id"
                placeholder={t('settings.workspaceIdPlaceholder')}
                autoComplete="off"
              />
            </Form.Item>
          )}

          <Paragraph type="secondary" style={{ marginTop: 8, marginBottom: 0 }} data-testid="settings-dashscope-models">
            {t('settings.dashscopeApplicableModels')}
          </Paragraph>

          <div style={{ marginTop: 16 }}>
            <Button onClick={handleTest} loading={testing} data-testid="settings-test-connection">
              {t('settings.testConnection')}
            </Button>
          </div>

          {testResult && (
            <Alert
              style={{ marginTop: 16 }}
              type={testResult.ok ? 'success' : 'error'}
              showIcon
              data-testid="settings-test-result"
              message={testResult.ok ? t('settings.testSuccess') : `${t('settings.testFailedPrefix')}${testResult.message}`}
            />
          )}
        </Card>

        {/* ── Card 2: t8star ─────────────────────────────────────────── */}
        <Card title={t('settings.cardT8starTitle')} style={{ marginBottom: 16 }}>
          <Paragraph type="secondary">{t('settings.cardT8starDesc')}</Paragraph>

          <Form.Item name="t8star_api_key" label={t('settings.t8starApiKey')} extra={secretExtra('t8star_api_key')}>
            <Input.Password
              autoComplete="off"
              disabled={clearedKeys.has('t8star_api_key')}
              placeholder={secretPlaceholder('t8star_api_key')}
            />
          </Form.Item>

          <Form.Item name="t8star_endpoint" label={t('settings.t8starEndpoint')} extra={t('settings.t8starEndpointHint')}>
            <Input placeholder={t('settings.t8starEndpointPlaceholder')} autoComplete="off" />
          </Form.Item>

          <Paragraph type="secondary" style={{ marginTop: 8, marginBottom: 0 }} data-testid="settings-t8star-models">
            {t('settings.t8starApplicableModels')}
          </Paragraph>

          <div style={{ marginTop: 16 }}>
            <Button onClick={handleT8starTest} loading={t8starTesting} data-testid="settings-test-connection-t8star">
              {t('settings.testConnection')}
            </Button>
          </div>

          {/*
            t8star 唯一的端点就是出图接口，但测试连接不会真的调用它去出图：
            探测用的是一个刻意不存在的哨兵模型名（见
            server/internal/service/t8star_tester.go 里 t8starProbeModel
            上的实测数据），上游在鉴权之后、路由到具体模型之前就会报错。
            所以这里是一句如实的说明，不是费用警告——不再用 warning 语气。
          */}
          <Paragraph type="secondary" style={{ marginTop: 8, marginBottom: 0 }} data-testid="settings-t8star-test-note">
            {t('settings.t8starTestNote')}
          </Paragraph>

          {t8starTestResult && (
            <Alert
              style={{ marginTop: 16 }}
              type={t8starTestResult.ok ? 'success' : 'error'}
              showIcon
              data-testid="settings-test-result-t8star"
              message={
                t8starTestResult.ok
                  ? t('settings.testSuccess')
                  : `${t('settings.testFailedPrefix')}${t8starTestResult.message}`
              }
            />
          )}
        </Card>

        {/* ── Card 3: 对象存储 OSS（可选） ───────────────────────────── */}
        <Card title={t('settings.cardOSSTitle')} style={{ marginBottom: 16 }}>
          <Paragraph type="secondary">{t('settings.cardOSSDesc')}</Paragraph>

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

        <Button type="primary" onClick={handleSave} loading={saving} data-testid="settings-save">
          {t('settings.save')}
        </Button>
      </Form>
    </div>
  )
}
