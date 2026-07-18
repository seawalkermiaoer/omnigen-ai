import { useMemo } from 'react'
import { Select } from 'antd'
import { useTranslation } from 'react-i18next'

import { modelHasCapability, type Capability, type CatalogModel } from '@/types/generation'

export interface ModelSelectProps {
  /** 完整目录（GET /api/catalog 的结果），本组件只按 capability 过滤，不裁剪字段。 */
  models: CatalogModel[]
  /** 只展示具备该能力的模型——目录驱动，永远不在这里写死某个模型 id。 */
  capability: Capability
  value?: string
  onChange: (modelId: string) => void
  loading?: boolean
  disabled?: boolean
}

/**
 * 模型下拉框：选项完全来自目录，按 capability 过滤。
 *
 * 展示名称查 `generation.models.<id>` 翻译；新模型上线还没来得及补翻译时，
 * 直接展示模型 id 兜底（不会因为缺翻译而"消失"）——这是允许的，因为它只
 * 影响展示文案，不影响目录里描述的能力/约束这条唯一数据源。
 */
export default function ModelSelect({
  models,
  capability,
  value,
  onChange,
  loading,
  disabled,
}: ModelSelectProps) {
  const { t } = useTranslation()

  const options = useMemo(
    () =>
      models
        .filter((m) => modelHasCapability(m, capability))
        .map((m) => {
          const key = `generation.models.${m.ID}`
          const label = t(key)
          return { value: m.ID, label: label === key ? m.ID : label }
        }),
    [models, capability, t],
  )

  return (
    <Select
      data-testid="model-select"
      style={{ width: '100%' }}
      placeholder={t('generation.modelSelectPlaceholder')}
      loading={loading}
      disabled={disabled}
      value={value}
      onChange={onChange}
      options={options}
      notFoundContent={loading ? t('common.loading') : t('common.empty')}
    />
  )
}
