import { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, App, Button, Card, Col, Flex, Input, Row, Typography } from 'antd'
import { DeleteOutlined, PlusOutlined, VideoCameraOutlined } from '@ant-design/icons'
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
  type VideoMediaImage,
  type VideoMediaVideo,
} from '@/types/generation'
import ConfigIncompleteAlert from './ConfigIncompleteAlert'
import VideoParamsFields from './VideoParamsFields'
import { DEFAULT_DURATION, DEFAULT_RATIO, DEFAULT_RESOLUTION } from './videoParams'
import { useVideoTaskPolling } from './useVideoTaskPolling'

const { Title, Paragraph, Text } = Typography
const CAPABILITY = 'r2v'
const NEGATIVE_PROMPT_MAX = 500

interface ReferenceImage extends MediaUploaderItem {
  referenceVoice?: string
}

interface ReferenceVideoRow {
  key: string
  url: string
  referenceVoice: string
}

function defaultParamValues(model: CatalogModel | undefined): ParamPanelValues {
  // 旧 UI 的 prompt-extend 复选框默认是勾选的（index.html:129 `checked`）。
  if (model && modelSupportsParam(model, ParamName.PromptExtend)) {
    return { promptExtend: true }
  }
  return {}
}

/**
 * 参考生视频。happyhorse-1.1-r2v 只接受 1~9 张参考图；
 * wan2.7-r2v 额外接受参考视频 URL，但图 + 视频合计不能超过 5
 * （目录里 wan2.7-r2v.MaxImages=5 记录的就是这个合计上限，见 catalog.go 的
 * 注释），每张图/每个视频都可以带一个可选的 reference_voice URL——照抄
 * public/js/r2v.js 的 renderImages/renderVideos/提交时的分支。
 *
 * MediaUploader（Task 15 的共用组件）只管理 {uid,url,name}，不认识
 * reference_voice 这个字段，所以这里用 uid 把 MediaUploader 报告的新数组
 * 和本地维护的 referenceVoice 重新拼接起来，而不是修改共用组件本身。
 */
export default function R2VPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const { notify } = useApiError()
  const isAdmin = useAuthStore((s) => s.isAdmin)
  const location = useLocation()
  const reuse = (location.state as ReuseLocationState | null)?.reuse
  // 同 I2VPage：记录复用触发的那次 modelId 变化，避免被下面"切模型清空
  // 表单"的 effect 立刻冲掉刚填好的 paramValues。
  const reuseAppliedModelRef = useRef<string | null>(null)

  const [models, setModels] = useState<CatalogModel[]>([])
  const [loadingCatalog, setLoadingCatalog] = useState(true)
  const [modelId, setModelId] = useState<string>()

  const [images, setImages] = useState<ReferenceImage[]>([])
  const [videos, setVideos] = useState<ReferenceVideoRow[]>([])

  const [prompt, setPrompt] = useState('')
  const [resolution, setResolution] = useState(DEFAULT_RESOLUTION)
  const [duration, setDuration] = useState(DEFAULT_DURATION)
  const [ratio, setRatio] = useState(DEFAULT_RATIO)
  const [paramValues, setParamValues] = useState<ParamPanelValues>({})
  const [submitting, setSubmitting] = useState(false)
  const [notConfigured, setNotConfigured] = useState(false)

  const { task, polling, start } = useVideoTaskPolling()

  // 目录加载完成后要么应用"复用"数据，要么按旧行为默认选中第一个可用
  // 模型，机制与 I2VPage 完全一致（见其同名 effect 的注释）。
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
          })
          void message.info(reuse.hadInput ? t('generation.reuseImageNote') : t('generation.reuseApplied'))
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

  const model = useMemo(() => models.find((m) => m.ID === modelId), [models, modelId])
  const isWan = (model?.Regions?.length ?? 0) > 0
  const mediaCap = model?.MaxImages || (isWan ? 5 : 9)

  // 切模型时，非 wan 模型完全不支持参考视频——残留的视频行会让 maxCount
  // 计算（下面的 imageMaxCount）变得对不上，直接清空更安全。
  //
  // 复用触发的 modelId 变化例外，跳过这次清空——理由同 I2VPage。
  useEffect(() => {
    if (reuseAppliedModelRef.current !== null && reuseAppliedModelRef.current === modelId) {
      reuseAppliedModelRef.current = null
      return
    }
    setVideos([])
    setParamValues(defaultParamValues(model))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modelId])

  const imageMaxCount = isWan ? Math.max(0, mediaCap - videos.length) : mediaCap

  const handleImagesChange = (next: MediaUploaderItem[]) => {
    setImages(
      next.map((item) => {
        const prev = images.find((i) => i.uid === item.uid)
        return { ...item, referenceVoice: prev?.referenceVoice ?? '' }
      }),
    )
  }

  const updateImageVoice = (uid: string, voice: string) => {
    setImages((prev) => prev.map((img) => (img.uid === uid ? { ...img, referenceVoice: voice } : img)))
  }

  const addVideoRow = () => {
    if (images.length + videos.length >= mediaCap) {
      void message.error(t('generation.r2vMaxMedia', { max: mediaCap }))
      return
    }
    setVideos((prev) => [...prev, { key: `${Date.now()}-${prev.length}`, url: '', referenceVoice: '' }])
  }

  const updateVideoRow = (key: string, patch: Partial<Omit<ReferenceVideoRow, 'key'>>) => {
    setVideos((prev) => prev.map((v) => (v.key === key ? { ...v, ...patch } : v)))
  }

  const removeVideoRow = (key: string) => {
    setVideos((prev) => prev.filter((v) => v.key !== key))
  }

  const optimizeMode: OptimizeMode = isWan ? 'r2v_wan' : 'r2v'
  const validVideos = videos.filter((v) => v.url.trim())

  const validate = (): string | null => {
    if (!prompt.trim()) return t('generation.promptRequiredHint')
    if (isWan) {
      if (images.length === 0 && validVideos.length === 0) return t('generation.r2vNeedMedia')
      if (images.length + validVideos.length > mediaCap) return t('generation.r2vMaxMedia', { max: mediaCap })
      if ((paramValues.negativePrompt?.length ?? 0) > NEGATIVE_PROMPT_MAX) {
        return t('generation.r2vNegativePromptTooLong', { max: NEGATIVE_PROMPT_MAX })
      }
    } else if (images.length === 0) {
      return t('generation.r2vNeedImage')
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

    const reqImages: VideoMediaImage[] = images.map((img) => ({
      url: img.url,
      ...(isWan && img.referenceVoice ? { referenceVoice: img.referenceVoice } : {}),
    }))

    const req: GenerateVideoRequest = {
      model: model.ID,
      prompt: prompt.trim(),
      images: reqImages,
      resolution,
      duration,
      ratio,
      watermark: !!paramValues.watermark,
      seed: paramValues.seed,
    }

    if (isWan) {
      if (validVideos.length > 0) {
        const reqVideos: VideoMediaVideo[] = validVideos.map((v) => ({
          url: v.url.trim(),
          ...(v.referenceVoice.trim() ? { referenceVoice: v.referenceVoice.trim() } : {}),
        }))
        req.videos = reqVideos
      }
      req.promptExtend = !!paramValues.promptExtend
      if (paramValues.negativePrompt) req.negativePrompt = paramValues.negativePrompt
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
          {t('nav.r2v')}
        </Title>
        <Paragraph type="secondary" style={{ marginBottom: 0 }}>
          {t('generation.r2vSubtitle')}
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

              {isWan && (
                <Alert
                  type="warning"
                  showIcon
                  data-testid="wan-region-notice"
                  message={t('generation.videoRegionNotice')}
                />
              )}

              <Flex vertical gap={4} data-testid="r2v-images">
                <Text type="secondary">{t('generation.mediaSectionTitle')}</Text>
                <MediaUploader
                  value={images}
                  onChange={handleImagesChange}
                  maxCount={imageMaxCount}
                  disabled={submitting}
                />
                {isWan && images.length > 0 && (
                  <Flex vertical gap={6} style={{ marginTop: 4 }} data-testid="r2v-image-voices">
                    {images.map((img, index) => (
                      <Flex key={img.uid} align="center" gap={8}>
                        <Text type="secondary" style={{ minWidth: 88 }}>
                          {t('generation.r2vVoiceLabel', { n: index + 1 })}
                        </Text>
                        <Input
                          value={img.referenceVoice}
                          onChange={(e) => updateImageVoice(img.uid, e.target.value)}
                          placeholder={t('generation.r2vVoiceUrlPlaceholder')}
                          disabled={submitting}
                        />
                      </Flex>
                    ))}
                  </Flex>
                )}
              </Flex>

              {isWan && (
                <Flex vertical gap={8} data-testid="r2v-videos">
                  <Flex align="center" justify="space-between">
                    <Text type="secondary">{t('generation.r2vVideosLabel')}</Text>
                    <Button
                      size="small"
                      icon={<PlusOutlined aria-hidden />}
                      autoInsertSpace={false}
                      disabled={submitting || images.length + videos.length >= mediaCap}
                      data-testid="r2v-add-video"
                      onClick={addVideoRow}
                    >
                      {t('generation.r2vAddVideo')}
                    </Button>
                  </Flex>
                  {videos.map((v, index) => (
                    <Flex key={v.key} align="center" gap={8} data-testid={`r2v-video-row-${index}`}>
                      <Input
                        value={v.url}
                        onChange={(e) => updateVideoRow(v.key, { url: e.target.value })}
                        placeholder={t('generation.r2vVideoUrlPlaceholder')}
                        disabled={submitting}
                      />
                      <Input
                        value={v.referenceVoice}
                        onChange={(e) => updateVideoRow(v.key, { referenceVoice: e.target.value })}
                        placeholder={t('generation.r2vVoiceUrlPlaceholder')}
                        disabled={submitting}
                      />
                      <Button
                        danger
                        type="text"
                        icon={<DeleteOutlined aria-hidden />}
                        aria-label={t('generation.r2vRemoveVideo')}
                        disabled={submitting}
                        onClick={() => removeVideoRow(v.key)}
                      />
                    </Flex>
                  ))}
                </Flex>
              )}

              <PromptInput
                value={prompt}
                onChange={setPrompt}
                mode={optimizeMode}
                images={images.map((img) => img.url)}
                videoCount={validVideos.length}
                placeholder={t('generation.promptPlaceholder')}
                disabled={submitting}
              />

              <VideoParamsFields
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
                data-testid="submit-r2v"
                onClick={() => void handleSubmit()}
              >
                {submitting ? t('generation.submitting') : t('generation.submit')}
              </Button>
            </Flex>
          </Card>
        </Col>

        <Col xs={24} xl={10}>
          <Card title={t('generation.resultTitle')} data-testid="r2v-result-card">
            {notConfigured ? <ConfigIncompleteAlert admin={isAdmin()} /> : <ResultPanel task={task} polling={polling} />}
          </Card>
        </Col>
      </Row>
    </Flex>
  )
}
