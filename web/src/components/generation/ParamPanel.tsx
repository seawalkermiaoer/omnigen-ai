import { useEffect, useMemo } from 'react'
import { Flex, Input, InputNumber, Select, Switch, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

import {
  ParamName,
  modelSupportsParam,
  type CatalogModel,
  type ParamPanelValues,
} from '@/types/generation'

const { Text } = Typography
const { TextArea } = Input

export interface ParamPanelProps {
  /** 当前选中的模型；未选择模型时面板不渲染任何控件。 */
  model?: CatalogModel
  value: ParamPanelValues
  onChange: (value: ParamPanelValues) => void
  disabled?: boolean
}

/** 把 "1664*928" 这样的尺寸字符串转成更易读的 "1664×928"，纯展示格式化，不解释含义。 */
function formatSizeLabel(size: string): string {
  return size.includes('*') ? size.replace('*', ' × ') : size
}

/**
 * 参数面板：不为每个生成页面手写表单，而是完全依据所选模型的目录约束
 * （Sizes / MaxN / SequentialMaxN / Supports / Protocol）渲染控件。
 *
 * 渲染规则（全部从 CatalogModel 派生，没有任何按模型 id 的分支）：
 *   - 尺寸选择器：model.Sizes 非空时渲染，选项即 Sizes 本身。
 *   - 数量（n）选择器：仅 Protocol === 'dashscope' 且 MaxN > 0 时渲染。
 *     gpt-image-2 虽然 MaxN=1，但 Protocol 是 openai——它是聊天补全接口，
 *     从不接受 n 参数（见 catalog.go 对 gpt-image-2 的注释），因此不能仅凭
 *     MaxN 判断是否渲染，必须结合协议。
 *   - 数量上限：开启 enable_sequential 时用 SequentialMaxN，否则用 MaxN；
 *     当前值超出新上限时自动回落，不依赖调用方自己钳制。
 *   - thinking_mode / enable_sequential / negative_prompt / seed / watermark /
 *     prompt_extend：分别只在 model.Supports 里出现同名参数时渲染。
 *
 * 新增一个 Sizes/MaxN/Supports 组合不同的模型，本组件不需要任何改动。
 */
export default function ParamPanel({ model, value, onChange, disabled }: ParamPanelProps) {
  const { t } = useTranslation()

  const hasSizes = !!model?.Sizes?.length
  const supportsCount = !!model && model.Protocol === 'dashscope' && model.MaxN > 0
  const supportsSequential =
    !!model && modelSupportsParam(model, ParamName.EnableSequential) && model.SequentialMaxN > 0
  const supportsThinking = !!model && modelSupportsParam(model, ParamName.ThinkingMode)
  const supportsNegativePrompt = !!model && modelSupportsParam(model, ParamName.NegativePrompt)
  const supportsSeed = !!model && modelSupportsParam(model, ParamName.Seed)
  const supportsWatermark = !!model && modelSupportsParam(model, ParamName.Watermark)
  const supportsPromptExtend = !!model && modelSupportsParam(model, ParamName.PromptExtend)

  const effectiveMaxN = useMemo(() => {
    if (!model) return 1
    if (value.enableSequential && model.SequentialMaxN > 0) return model.SequentialMaxN
    return model.MaxN
  }, [model, value.enableSequential])

  const countOptions = useMemo(
    () => Array.from({ length: Math.max(effectiveMaxN, 1) }, (_, i) => i + 1),
    [effectiveMaxN],
  )

  // 数量上限变化（模型切换、开关顺序生成）时，若当前值越界则自动回落，
  // 不依赖每个调用方各自钳制——这正是"由目录强制约束"而不是约定俗成。
  useEffect(() => {
    if (supportsCount && typeof value.n === 'number' && value.n > effectiveMaxN) {
      onChange({ ...value, n: effectiveMaxN })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [effectiveMaxN, supportsCount])

  if (!model) {
    return (
      <Text type="secondary" data-testid="param-panel-empty">
        {t('generation.paramPanelSelectModelFirst')}
      </Text>
    )
  }

  return (
    <Flex wrap gap={16} data-testid="param-panel">
      {hasSizes && (
        <Flex vertical gap={4} style={{ minWidth: 180 }} data-testid="param-size">
          <Text type="secondary">{t('generation.paramSize')}</Text>
          <Select
            disabled={disabled}
            style={{ width: '100%' }}
            value={value.size}
            onChange={(size) => onChange({ ...value, size })}
            options={(model.Sizes ?? []).map((s) => ({ value: s, label: formatSizeLabel(s) }))}
          />
        </Flex>
      )}

      {supportsCount && (
        <Flex vertical gap={4} style={{ minWidth: 120 }} data-testid="param-count">
          <Text type="secondary">{t('generation.paramCount')}</Text>
          <Select
            disabled={disabled}
            style={{ width: '100%' }}
            value={value.n ?? 1}
            onChange={(n) => onChange({ ...value, n })}
            options={countOptions.map((n) => ({ value: n, label: n }))}
          />
        </Flex>
      )}

      {supportsThinking && (
        <Flex vertical gap={4} data-testid="param-thinking-mode">
          <Text type="secondary">{t('generation.paramThinkingMode')}</Text>
          <Switch
            disabled={disabled}
            checked={!!value.thinkingMode}
            onChange={(thinkingMode) => onChange({ ...value, thinkingMode })}
          />
        </Flex>
      )}

      {supportsSequential && (
        <Flex vertical gap={4} data-testid="param-enable-sequential">
          <Text type="secondary">{t('generation.paramEnableSequential')}</Text>
          <Switch
            disabled={disabled}
            checked={!!value.enableSequential}
            onChange={(enableSequential) => onChange({ ...value, enableSequential })}
          />
        </Flex>
      )}

      {supportsWatermark && (
        <Flex vertical gap={4} data-testid="param-watermark">
          <Text type="secondary">{t('generation.paramWatermark')}</Text>
          <Switch
            disabled={disabled}
            checked={!!value.watermark}
            onChange={(watermark) => onChange({ ...value, watermark })}
          />
        </Flex>
      )}

      {supportsPromptExtend && (
        <Flex vertical gap={4} data-testid="param-prompt-extend">
          <Text type="secondary">{t('generation.paramPromptExtend')}</Text>
          <Switch
            disabled={disabled}
            checked={!!value.promptExtend}
            onChange={(promptExtend) => onChange({ ...value, promptExtend })}
          />
        </Flex>
      )}

      {supportsSeed && (
        <Flex vertical gap={4} style={{ minWidth: 140 }} data-testid="param-seed">
          <Text type="secondary">{t('generation.paramSeed')}</Text>
          <InputNumber
            disabled={disabled}
            style={{ width: '100%' }}
            placeholder={t('generation.paramSeedPlaceholder')}
            value={value.seed}
            onChange={(seed) => onChange({ ...value, seed: seed ?? undefined })}
          />
        </Flex>
      )}

      {supportsNegativePrompt && (
        <Flex vertical gap={4} style={{ minWidth: 240, flex: 1 }} data-testid="param-negative-prompt">
          <Text type="secondary">{t('generation.paramNegativePrompt')}</Text>
          <TextArea
            disabled={disabled}
            autoSize={{ minRows: 1, maxRows: 3 }}
            placeholder={t('generation.paramNegativePromptPlaceholder')}
            value={value.negativePrompt}
            onChange={(e) => onChange({ ...value, negativePrompt: e.target.value })}
          />
        </Flex>
      )}
    </Flex>
  )
}
