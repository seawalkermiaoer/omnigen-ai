import { Flex, InputNumber, Select, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

import { DEFAULT_DURATION, VIDEO_DURATION_MAX, VIDEO_DURATION_MIN, VIDEO_RATIOS, VIDEO_RESOLUTIONS } from './videoParams'

const { Text } = Typography

export interface VideoParamsFieldsProps {
  resolution: string
  duration: number
  onResolutionChange: (value: string) => void
  onDurationChange: (value: number) => void
  /**
   * i2v 的宽高比由首帧决定，从不接受这个参数——调用页面此时传 `undefined`
   * （不是空字符串），本组件据此渲染一段静态说明而不是选择器，与旧
   * i2v.js 里禁用态的 #autoRatio-i2v 输入框（"自动跟随首帧"）行为一致。
   * t2v/r2v 传真实字符串值 + onRatioChange 时渲染可选的下拉框。
   */
  ratio?: string
  onRatioChange?: (value: string) => void
  disabled?: boolean
}

/**
 * 三个视频页面共用的 resolution/duration/ratio 控件组——这三个参数不由
 * 目录（CatalogModel.Supports）驱动，因为它们对全部视频模型取值范围相同，
 * 与 ParamPanel 的"按目录渲染"职责是互补而非重叠的关系：ParamPanel 负责
 * thinking_mode/negative_prompt/seed/watermark/prompt_extend 这些真正因
 * 模型而异的参数，本组件负责剩下的、模式而非模型驱动的三个。
 */
export default function VideoParamsFields({
  resolution,
  duration,
  onResolutionChange,
  onDurationChange,
  ratio,
  onRatioChange,
  disabled,
}: VideoParamsFieldsProps) {
  const { t } = useTranslation()

  return (
    <Flex wrap gap={16} data-testid="video-params-fields">
      <Flex vertical gap={4} style={{ minWidth: 140 }} data-testid="video-resolution">
        <Text type="secondary">{t('generation.videoResolution')}</Text>
        <Select
          disabled={disabled}
          style={{ width: '100%' }}
          value={resolution}
          onChange={onResolutionChange}
          options={VIDEO_RESOLUTIONS.map((r) => ({ value: r, label: r }))}
        />
      </Flex>

      <Flex vertical gap={4} style={{ minWidth: 160 }} data-testid="video-duration">
        <Text type="secondary">{t('generation.videoDuration')}</Text>
        <InputNumber
          disabled={disabled}
          style={{ width: '100%' }}
          min={VIDEO_DURATION_MIN}
          max={VIDEO_DURATION_MAX}
          value={duration}
          onChange={(value) => onDurationChange(value ?? DEFAULT_DURATION)}
        />
      </Flex>

      <Flex vertical gap={4} style={{ minWidth: 160 }} data-testid="video-ratio">
        <Text type="secondary">{t('generation.videoRatio')}</Text>
        {ratio === undefined ? (
          <Text type="secondary" data-testid="video-ratio-auto">
            {t('generation.videoRatioAuto')}
          </Text>
        ) : (
          <Select
            disabled={disabled}
            style={{ width: '100%' }}
            value={ratio}
            onChange={onRatioChange}
            options={VIDEO_RATIOS.map((r) => ({ value: r, label: r }))}
          />
        )}
      </Flex>
    </Flex>
  )
}
