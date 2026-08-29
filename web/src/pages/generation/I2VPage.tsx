import { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, App, Button, Card, Col, Flex, Input, Row, Segmented, Typography } from 'antd'
import { VideoCameraOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'

import { catalogApi, generationApi } from '@/api/generation'
import { ApiError } from '@/api/client'
import { useApiError } from '@/hooks/useApiError'
import { useAuthStore } from '@/stores/auth'
import {
  MediaUploader,
  ModelSelect,
  ParamPanel,
  PromptInput,
  ResultPanel,
  type MediaUploaderItem,
} from '@/components/generation'
import {
  ParamName,
  modelHasCapability,
  modelSupportsParam,
  type CatalogModel,
  type GenerateVideoRequest,
  type OptimizeMode,
  type ParamPanelValues,
  type ReuseLocationState,
} from '@/types/generation'
import ConfigIncompleteAlert from './ConfigIncompleteAlert'
import VideoParamsFields from './VideoParamsFields'
import { useVideoParamsState } from './useVideoParamsState'
import { useVideoTaskPolling } from './useVideoTaskPolling'

const { Title, Paragraph, Text } = Typography
const CAPABILITY = 'i2v'

type I2VTaskType = 'first_frame' | 'first_last' | 'continue'

/**
 * i2v 的三种任务类型（仅 wan 模型有）——服务端字面量与
 * server/internal/service/generation_video.go 的 I2VTaskType 常量一一对应,
 * 照抄 i2v.js:10 `i2v.state.task` 的取值。
 */
const TASK_TYPES: { value: I2VTaskType; labelKey: string }[] = [
  { value: 'first_frame', labelKey: 'generation.i2vTaskFirstFrame' },
  { value: 'first_last', labelKey: 'generation.i2vTaskFirstLast' },
  { value: 'continue', labelKey: 'generation.i2vTaskContinue' },
]

/**
 * wan3.0 只有前两种：它没有 first_clip 这种媒体类型，"续接"在 wan3.0 里
 * 是参考生视频的一种用法（参考视频 + 描述延长内容的 prompt），不属于本
 * 页面。切到 wan3.0 时 taskType 会被"换模型清空表单"的 effect 重置回
 * first_frame，所以不存在残留的 continue 值配上一个没有 continue 选项的
 * Segmented。
 */
const WAN30_TASK_TYPES = TASK_TYPES.filter((tt) => tt.value !== 'continue')

function defaultParamValues(model: CatalogModel | undefined): ParamPanelValues {
  // 旧 UI 的 prompt-extend 复选框默认是勾选的（index.html:270 `checked`），
  // 这里保持同样的默认值——不是"默认关闭，用户自己打开"。
  if (model && modelSupportsParam(model, ParamName.PromptExtend)) {
    return { promptExtend: true }
  }
  return {}
}

/**
 * 图生视频：三个视频页面里参数矩阵最复杂的一个。happyhorse-1.1-i2v 只有
 * 单一首帧输入；wan2.7-i2v-2026-04-25 额外有三种任务类型（首帧/首尾帧/
 * 续写），每种类型决定哪些素材槽位可见、哪些必填——完全照抄
 * public/js/i2v.js 的 applyI2vSlots 与提交时的分支。
 *
 * 两条不可违反的约束（都来自 server/internal/service/generation_video.go）：
 *   1. ratio 从不发给 i2v——宽高比由首帧决定，后端会拒绝携带 ratio 的 i2v
 *      请求，所以这里连控件都不渲染，只展示一段静态说明。
 *   2. 图片尺寸/比例校验阈值按模型不同（happyhorse 300px/0.4~2.5，wan
 *      240px/0.125~8，且宽高都要达标，不是取短边）——直接从目录字段
 *      MinImageEdge/RatioMin/RatioMax 读出来传给 MediaUploader，不在这里
 *      硬编码任何模型 id。
 */
export default function I2VPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const { notify } = useApiError()
  const isAdmin = useAuthStore((s) => s.isAdmin)
  const location = useLocation()
  const reuse = (location.state as ReuseLocationState | null)?.reuse
  // 复用触发的 setModelId 会顺带触发下面"modelId 变化清空表单"的 effect——
  // 那个 effect 会把我们刚填好的 paramValues 冲掉。这个 ref 记录"下一次
  // modelId 变成这个值时，是复用自己引发的，跳过一次清空"，消费后清零，
  // 之后用户手动切模型仍然正常触发清空。
  const reuseAppliedModelRef = useRef<string | null>(null)

  const [models, setModels] = useState<CatalogModel[]>([])
  const [loadingCatalog, setLoadingCatalog] = useState(true)
  const [modelId, setModelId] = useState<string>()
  const [taskType, setTaskType] = useState<I2VTaskType>('first_frame')

  const [firstFrame, setFirstFrame] = useState<MediaUploaderItem[]>([])
  const [lastFrame, setLastFrame] = useState<MediaUploaderItem[]>([])
  const [firstClipUrl, setFirstClipUrl] = useState('')
  const [drivingAudioUrl, setDrivingAudioUrl] = useState('')

  const [prompt, setPrompt] = useState('')
  const [paramValues, setParamValues] = useState<ParamPanelValues>({})
  const [submitting, setSubmitting] = useState(false)
  const [notConfigured, setNotConfigured] = useState(false)

  const model = useMemo(() => models.find((m) => m.ID === modelId), [models, modelId])
  const { resolution, duration, ratio, setResolution, setDuration, setRatio } = useVideoParamsState(
    model,
    CAPABILITY,
  )

  const { task, polling, start } = useVideoTaskPolling()

  // 目录加载完成后要么应用"复用"数据，要么按旧行为默认选中第一个可用
  // 模型。复用命中时顺带记一下 reuseAppliedModelRef，供下面"modelId 变化
  // 清空表单"的 effect 识别出这次变化不该清空刚填好的 paramValues。
  useEffect(() => {
    let cancelled = false
    setLoadingCatalog(true)
    catalogApi
      .list()
      .then((res) => {
        if (cancelled) return
        setModels(res.models)
        if (reuse && reuse.mode === CAPABILITY && res.models.some((m) => m.ID === reuse.model)) {
          reuseAppliedModelRef.current = reuse.model
          setModelId(reuse.model)
          setPrompt(reuse.prompt)
          if (typeof reuse.params.resolution === 'string') setResolution(reuse.params.resolution)
          if (typeof reuse.params.duration === 'number') setDuration(reuse.params.duration)
          if (typeof reuse.params.ratio === 'string') setRatio(reuse.params.ratio)
          setParamValues({
            watermark: typeof reuse.params.watermark === 'boolean' ? reuse.params.watermark : undefined,
            seed: typeof reuse.params.seed === 'number' ? reuse.params.seed : undefined,
            negativePrompt: typeof reuse.params.negativePrompt === 'string' ? reuse.params.negativePrompt : undefined,
            promptExtend: typeof reuse.params.promptExtend === 'boolean' ? reuse.params.promptExtend : undefined,
            audio: typeof reuse.params.audio === 'boolean' ? reuse.params.audio : undefined,
          })
          void message.info(t('generation.reuseImageNote'))
        } else {
          const first = res.models.find((m) => modelHasCapability(m, CAPABILITY))
          if (first) setModelId(first.ID)
        }
      })
      .catch((err) => {
        if (!cancelled) notify(err)
      })
      .finally(() => {
        if (!cancelled) setLoadingCatalog(false)
      })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [notify])

  const isRestrictedRegion = (model?.Regions?.length ?? 0) > 0

  // 切换模型时素材槽位的可见性、必填规则都可能整体改变（比如从 continue
  // 任务切到 happyhorse），残留的旧素材容易造出一个用户没看见、却仍会被
  // 提交的字段——直接清空，比"只清空看不见的那些"更不容易漏。
  //
  // 复用触发的 modelId 变化例外：那次不清空，见 reuseAppliedModelRef 声明
  // 处的注释——否则复用刚填进去的 paramValues 会被这里立刻覆盖掉。
  useEffect(() => {
    if (reuseAppliedModelRef.current !== null && reuseAppliedModelRef.current === modelId) {
      reuseAppliedModelRef.current = null
      return
    }
    setTaskType('first_frame')
    setFirstFrame([])
    setLastFrame([])
    setFirstClipUrl('')
    setDrivingAudioUrl('')
    setParamValues(defaultParamValues(model))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modelId])

  // 三代模型的任务类型矩阵：
  //   happyhorse — 只有首帧，没有任务类型标签页
  //   wan2.7     — 首帧 / 首尾帧 / 续接片段，前两种可带 driving_audio
  //   wan3.0     — 首帧 / 首尾帧；没有 first_clip 也没有 driving_audio
  //                （续接改为在"参考生视频"里用参考视频 + 延长意图 prompt，
  //                 后端 buildWan30I2VMedia 的报错文案也是这么指路的）
  // 判据同 R2VPage：目录里的 VideoProfile，不是地域白名单。
  const isWan27 = model?.VideoProfile === 'wan2.7'
  const isWan30 = model?.VideoProfile === 'wan3.0'
  const hasTaskTypes = isWan27 || isWan30
  const availableTaskTypes = isWan30 ? WAN30_TASK_TYPES : TASK_TYPES

  const effectiveTaskType: I2VTaskType = hasTaskTypes ? taskType : 'first_frame'
  const showFirstFrame = !hasTaskTypes || effectiveTaskType === 'first_frame' || effectiveTaskType === 'first_last'
  const showLastFrame = hasTaskTypes && (effectiveTaskType === 'first_last' || (isWan27 && effectiveTaskType === 'continue'))
  const showFirstClip = isWan27 && effectiveTaskType === 'continue'
  const showDrivingAudio = isWan27 && (effectiveTaskType === 'first_frame' || effectiveTaskType === 'first_last')

  const optimizeMode: OptimizeMode = hasTaskTypes ? 'i2v_wan' : 'i2v'
  const optimizeImages = firstFrame[0] ? [firstFrame[0].url] : undefined

  const validate = (): string | null => {
    if (!hasTaskTypes) {
      if (!firstFrame[0]) return t('generation.i2vNeedFirstFrame')
      return null
    }
    if (effectiveTaskType === 'first_frame') {
      if (!firstFrame[0]) return t('generation.i2vNeedFirstFrame')
    } else if (effectiveTaskType === 'first_last') {
      if (!firstFrame[0]) return t('generation.i2vNeedFirstFrame')
      if (!lastFrame[0]) return t('generation.i2vNeedLastFrame')
    } else if (effectiveTaskType === 'continue') {
      if (!firstClipUrl.trim()) return t('generation.i2vNeedFirstClip')
    }
    return null
  }

  const handleSubmit = async () => {
    if (!model) return
    const error = validate()
    if (error) {
      void message.error(error)
      return
    }

    const req: GenerateVideoRequest = {
      model: model.ID,
      mode: CAPABILITY,
      resolution,
      duration,
      watermark: !!paramValues.watermark,
      seed: paramValues.seed,
    }
    if (prompt.trim()) req.prompt = prompt.trim()
    // ratio 只有在模型接受时才有值——wan2.7/happyhorse 的 i2v 由首帧决定
    // 宽高比，useVideoParamsState 会把它收敛成空串，这里也就不会带上。
    if (ratio) req.ratio = ratio

    if (hasTaskTypes) {
      req.taskType = effectiveTaskType
      if (effectiveTaskType === 'first_frame' || effectiveTaskType === 'first_last') {
        req.firstFrame = firstFrame[0].url
      }
      if (effectiveTaskType === 'first_last') {
        req.lastFrame = lastFrame[0].url
      }
      if (effectiveTaskType === 'continue') {
        req.firstClip = firstClipUrl.trim()
        if (lastFrame[0]) req.lastFrame = lastFrame[0].url
      }
      if (showDrivingAudio && drivingAudioUrl.trim()) {
        req.drivingAudio = drivingAudioUrl.trim()
      }
      // negative_prompt 只有 wan2.7 有；wan3.0 的 input 里没有这个字段。
      if (isWan27 && paramValues.negativePrompt) req.negativePrompt = paramValues.negativePrompt
      req.promptExtend = !!paramValues.promptExtend
      if (isWan30) req.audio = paramValues.audio
    } else {
      req.firstFrame = firstFrame[0].url
    }

    setSubmitting(true)
    setNotConfigured(false)
    try {
      const created = await generationApi.generateVideo(req)
      start(created)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'SETTING_INCOMPLETE') {
        setNotConfigured(true)
      } else {
        notify(err)
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Flex vertical gap={24}>
      <div>
        <Title level={3} style={{ marginBottom: 4 }}>
          {t('nav.i2v')}
        </Title>
        <Paragraph type="secondary" style={{ marginBottom: 0 }}>
          {t('generation.i2vSubtitle')}
        </Paragraph>
      </div>

      <Row gutter={[24, 24]}>
        <Col xs={24} xl={14}>
          <Card>
            <Flex vertical gap={20}>
              <ModelSelect
                models={models}
                capability={CAPABILITY}
                value={modelId}
                onChange={setModelId}
                loading={loadingCatalog}
              />

              {isRestrictedRegion && (
                <Alert
                  type="warning"
                  showIcon
                  data-testid="wan-region-notice"
                  message={t('generation.videoRegionNotice')}
                />
              )}

              {hasTaskTypes && (
                <Flex vertical gap={4} data-testid="i2v-task-type">
                  <Text type="secondary">{t('generation.i2vTaskType')}</Text>
                  <Segmented
                    disabled={submitting}
                    value={taskType}
                    onChange={(value) => setTaskType(value as I2VTaskType)}
                    options={availableTaskTypes.map((tt) => ({ value: tt.value, label: t(tt.labelKey) }))}
                  />
                </Flex>
              )}

              {showFirstFrame && (
                <Flex vertical gap={4} data-testid="i2v-first-frame">
                  <Text type="secondary">{t('generation.i2vFirstFrameLabel')}</Text>
                  <MediaUploader
                    value={firstFrame}
                    onChange={setFirstFrame}
                    maxCount={1}
                    minEdge={model?.MinImageEdge}
                    ratioMin={model?.RatioMin}
                    ratioMax={model?.RatioMax}
                    disabled={submitting}
                  />
                </Flex>
              )}

              {showLastFrame && (
                <Flex vertical gap={4} data-testid="i2v-last-frame">
                  <Text type="secondary">{t('generation.i2vLastFrameLabel')}</Text>
                  <MediaUploader
                    value={lastFrame}
                    onChange={setLastFrame}
                    maxCount={1}
                    minEdge={model?.MinImageEdge}
                    ratioMin={model?.RatioMin}
                    ratioMax={model?.RatioMax}
                    disabled={submitting}
                  />
                </Flex>
              )}

              {showFirstClip && (
                <Flex vertical gap={4} data-testid="i2v-first-clip">
                  <Text type="secondary">{t('generation.i2vFirstClipLabel')}</Text>
                  <Input
                    value={firstClipUrl}
                    onChange={(e) => setFirstClipUrl(e.target.value)}
                    placeholder={t('generation.i2vFirstClipPlaceholder')}
                    disabled={submitting}
                  />
                </Flex>
              )}

              {showDrivingAudio && (
                <Flex vertical gap={4} data-testid="i2v-driving-audio">
                  <Text type="secondary">{t('generation.i2vDrivingAudioLabel')}</Text>
                  <Input
                    value={drivingAudioUrl}
                    onChange={(e) => setDrivingAudioUrl(e.target.value)}
                    placeholder={t('generation.i2vDrivingAudioPlaceholder')}
                    disabled={submitting}
                  />
                </Flex>
              )}

              <PromptInput
                value={prompt}
                onChange={setPrompt}
                mode={optimizeMode}
                images={optimizeImages}
                placeholder={t('generation.promptPlaceholder')}
                disabled={submitting}
              />

              <VideoParamsFields
                model={model}
                mode={CAPABILITY}
                resolution={resolution}
                duration={duration}
                ratio={ratio}
                onResolutionChange={setResolution}
                onDurationChange={setDuration}
                onRatioChange={setRatio}
                disabled={submitting}
              />

              <ParamPanel model={model} value={paramValues} onChange={setParamValues} disabled={submitting} />

              <Button
                type="primary"
                size="large"
                icon={<VideoCameraOutlined aria-hidden />}
                loading={submitting}
                disabled={!model}
                autoInsertSpace={false}
                data-testid="submit-i2v"
                onClick={() => void handleSubmit()}
              >
                {submitting ? t('generation.submitting') : t('generation.submit')}
              </Button>
            </Flex>
          </Card>
        </Col>

        <Col xs={24} xl={10}>
          <Card title={t('generation.resultTitle')} data-testid="i2v-result-card">
            {notConfigured ? <ConfigIncompleteAlert admin={isAdmin()} /> : <ResultPanel task={task} polling={polling} />}
          </Card>
        </Col>
      </Row>
    </Flex>
  )
}
