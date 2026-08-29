import { Checkbox, Flex, InputNumber, Select, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

import type { CatalogModel, TaskMode } from '@/types/generation'
import {
  SMART_DURATION,
  acceptsRatio,
  defaultDuration,
  durationMax,
  durationMin,
  supportsSmartDuration,
  videoRatios,
  videoResolutions,
} from './videoParams'

const { Text } = Typography

export interface VideoParamsFieldsProps {
  /** 当前选中的模型；三个控件的取值范围全部从它派生。 */
  model?: CatalogModel
  /** 当前页面的模式——只用来判断 i2v 下要不要渲染 ratio 选择器。 */
  mode: TaskMode
  resolution: string
  duration: number
  onResolutionChange: (value: string) => void
  onDurationChange: (value: number) => void
  ratio: string
  onRatioChange: (value: string) => void
  disabled?: boolean
}

/**
 * 五个视频页面共用的 resolution/duration/ratio 控件组。
 *
 * 与 ParamPanel 的分工没变（那边管 Supports 驱动的可选参数，这边管这三个
 * 每次都要发的参数），但取值来源变了：过去是本地写死的常量，现在同样
 * 由目录驱动——见 videoParams.ts 顶部关于 wan3.0 打破"所有视频模型取值
 * 相同"这一前提的说明。
 *
 * ratio 不再由调用方传 `undefined` 来表达"这个模式没有宽高比"，而是本
 * 组件自己按 acceptsRatio(model, mode) 判断：是否有宽高比取决于模型 ×
 * 模式两个因素（wan2.7-i2v 没有、wan3.0-i2v 有），调用页面不该重复这份
 * 知识。不接受时渲染的仍是"自动跟随首帧"那段静态说明。
 */
export default function VideoParamsFields({
  model,
  mode,
  resolution,
  duration,
  onResolutionChange,
  onDurationChange,
  ratio,
  onRatioChange,
  disabled,
}: VideoParamsFieldsProps) {
  const { t } = useTranslation()

  const showRatio = acceptsRatio(model, mode)
  const smartAvailable = supportsSmartDuration(model)
  const smartOn = duration === SMART_DURATION
  const min = durationMin(model)
  const max = durationMax(model)

  return (
    <Flex wrap gap={16} data-testid="video-params-fields">
      <Flex vertical gap={4} style={{ minWidth: 140 }} data-testid="video-resolution">
        <Text type="secondary">{t('generation.videoResolution')}</Text>
        <Select
          disabled={disabled}
          style={{ width: '100%' }}
          value={resolution || undefined}
          onChange={onResolutionChange}
          options={videoResolutions(model).map((r) => ({ value: r, label: r }))}
        />
      </Flex>

      <Flex vertical gap={4} style={{ minWidth: 200 }} data-testid="video-duration">
        <Text type="secondary">
          {t('generation.videoDuration')}
          {model ? ` (${min}–${max}s)` : ''}
        </Text>
        <Flex gap={8} align="center">
          <InputNumber
            disabled={disabled || smartOn}
            style={{ flex: 1 }}
            min={min}
            max={max}
            // 智能时长期间 InputNumber 被禁用，但值仍是 -1（越界），直接
            // 显示会渲染成一个不合法的输入；此时展示模型默认时长作为占位，
            // 真正发出去的仍然是 -1。
            value={smartOn ? defaultDuration(model) : duration}
            onChange={(value) => onDurationChange(value ?? defaultDuration(model))}
          />
          {smartAvailable && (
            <Checkbox
              disabled={disabled}
              checked={smartOn}
              onChange={(e) => onDurationChange(e.target.checked ? SMART_DURATION : defaultDuration(model))}
              data-testid="video-duration-smart"
            >
              {t('generation.videoDurationSmart')}
            </Checkbox>
          )}
        </Flex>
      </Flex>

      <Flex vertical gap={4} style={{ minWidth: 160 }} data-testid="video-ratio">
        <Text type="secondary">{t('generation.videoRatio')}</Text>
        {showRatio ? (
          <Select
            disabled={disabled}
            style={{ width: '100%' }}
            value={ratio || undefined}
            onChange={onRatioChange}
            options={videoRatios(model).map((r) => ({
              value: r,
              // adaptive 是个语义值而不是一个比例，直接显示英文原词对
              // 中文用户没有信息量，翻译成"自适应（跟随输入素材）"。
              label: r === 'adaptive' ? t('generation.videoRatioAdaptive') : r,
            }))}
          />
        ) : (
          <Text type="secondary" data-testid="video-ratio-auto">
            {t('generation.videoRatioAuto')}
          </Text>
        )}
      </Flex>
    </Flex>
  )
}
