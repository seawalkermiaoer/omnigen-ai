import { useEffect, useMemo, useState } from 'react'
import { Alert, App, Button, Card, Col, Flex, Input, Row, Typography } from 'antd'
import { VideoCameraOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'

import { catalogApi, generationApi } from '@/api/generation'
import { ApiError } from '@/api/client'
import { useApiError } from '@/hooks/useApiError'
import { useAuthStore } from '@/stores/auth'
import { ModelSelect, ParamPanel, PromptInput, ResultPanel } from '@/components/generation'
import {
  ParamName,
  modelHasCapability,
  modelSupportsParam,
  type CatalogModel,
  type Capability,
  type GenerateVideoRequest,
  type ParamPanelValues,
  type ReuseLocationState,
  type TaskMode,
} from '@/types/generation'
import ConfigIncompleteAlert from './ConfigIncompleteAlert'
import VideoParamsFields from './VideoParamsFields'
import { useVideoParamsState } from './useVideoParamsState'
import { useVideoTaskPolling } from './useVideoTaskPolling'

const { Title, Paragraph, Text } = Typography

export interface MediaUrlVideoPageProps {
  /** 'f2v' | 'l2v'——同时用作目录筛选的 capability 与请求体里的 mode。 */
  mode: Extract<Capability & TaskMode, 'f2v' | 'l2v'>
  /** 请求体里承载这条素材 URL 的字段名。 */
  urlField: 'fileUrl' | 'linkUrl'
  titleKey: string
  subtitleKey: string
  urlLabelKey: string
  urlPlaceholderKey: string
  urlRequiredKey: string
  /** data-testid 前缀，取值为 'f2v' / 'l2v'。 */
  testId: string
}

function defaultParamValues(model: CatalogModel | undefined): ParamPanelValues {
  if (model && modelSupportsParam(model, ParamName.PromptExtend)) {
    return { promptExtend: true }
  }
  return {}
}

/**
 * 文件生视频（f2v）与网页生视频（l2v）共用的页面实现。
 *
 * 这两个页面写成一个参数化组件而不是两份几乎相同的文件，是因为它们的
 * 差别只有三处纯数据：目录筛选用的 capability、请求体里承载 URL 的字段名
 * （fileUrl / linkUrl）、以及几条文案。表单结构、素材数量（恰好一条）、
 * 校验规则、参数面板、提交与轮询流程完全一致——上游对它们的处理也是
 * 一样的，都是 input.media 里的单条记录，只是 type 不同。
 *
 * 与其它视频页面不同的是这里没有上传控件：素材是一份文档（docx/pdf/pptx…，
 * 上限 100MB / 50 页）或一个网页地址，而本系统的 POST /api/upload 只接受
 * 图片（见 service/upload.go 的 uploadMimeExt 白名单）。所以和参考视频、
 * 续接片段一样走"贴 URL"，而不是先扩上传链路再说——上游只要一个可访问的
 * 地址，这条路是完整可用的。
 *
 * prompt 是可选的：上游要求 prompt 与 media 至少有一个，这里 media 必填，
 * 所以 prompt 可以为空（此时由模型自己决定怎么把文档/网页讲成视频）。
 */
export default function MediaUrlVideoPage({
  mode,
  urlField,
  titleKey,
  subtitleKey,
  urlLabelKey,
  urlPlaceholderKey,
  urlRequiredKey,
  testId,
}: MediaUrlVideoPageProps) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const { notify } = useApiError()
  const isAdmin = useAuthStore((s) => s.isAdmin)
  const location = useLocation()
  const reuse = (location.state as ReuseLocationState | null)?.reuse

  const [models, setModels] = useState<CatalogModel[]>([])
  const [loadingCatalog, setLoadingCatalog] = useState(true)
  const [modelId, setModelId] = useState<string>()
  const [mediaUrl, setMediaUrl] = useState('')
  const [prompt, setPrompt] = useState('')
  const [paramValues, setParamValues] = useState<ParamPanelValues>({})
  const [submitting, setSubmitting] = useState(false)
  const [notConfigured, setNotConfigured] = useState(false)

  const model = useMemo(() => models.find((m) => m.ID === modelId), [models, modelId])
  const { resolution, duration, ratio, setResolution, setDuration, setRatio } = useVideoParamsState(model, mode)

  const { task, polling, start } = useVideoTaskPolling()

  // 与 T2VPage 同构：挂载时拉目录，要么应用复用数据，要么默认选中第一个
  // 可用模型。本页面没有"切模型清空表单"的第二个 effect（素材只有一条
  // URL，换模型不会让它变得不合法），所以不需要 reuseAppliedModelRef。
  useEffect(() => {
    let cancelled = false
    setLoadingCatalog(true)
    catalogApi
      .list()
      .then((res) => {
        if (cancelled) return
        setModels(res.models)
        if (reuse && reuse.mode === mode && res.models.some((m) => m.ID === reuse.model)) {
          setModelId(reuse.model)
          setPrompt(reuse.prompt)
          if (typeof reuse.params.resolution === 'string') setResolution(reuse.params.resolution)
          if (typeof reuse.params.duration === 'number') setDuration(reuse.params.duration)
          if (typeof reuse.params.ratio === 'string') setRatio(reuse.params.ratio)
          setParamValues({
            watermark: typeof reuse.params.watermark === 'boolean' ? reuse.params.watermark : undefined,
            seed: typeof reuse.params.seed === 'number' ? reuse.params.seed : undefined,
            promptExtend: typeof reuse.params.promptExtend === 'boolean' ? reuse.params.promptExtend : undefined,
            audio: typeof reuse.params.audio === 'boolean' ? reuse.params.audio : undefined,
          })
          void message.info(t('generation.reuseImageNote'))
        } else {
          const first = res.models.find((m) => modelHasCapability(m, mode))
          if (first) {
            setModelId(first.ID)
            setParamValues(defaultParamValues(first))
          }
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

  const handleSubmit = async () => {
    if (!model) return
    if (!mediaUrl.trim()) {
      void message.error(t(urlRequiredKey))
      return
    }

    const req: GenerateVideoRequest = {
      model: model.ID,
      mode,
      [urlField]: mediaUrl.trim(),
      resolution,
      duration,
      ratio,
      watermark: !!paramValues.watermark,
      seed: paramValues.seed,
      promptExtend: paramValues.promptExtend,
      audio: paramValues.audio,
    }
    if (prompt.trim()) req.prompt = prompt.trim()

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
          {t(titleKey)}
        </Title>
        <Paragraph type="secondary" style={{ marginBottom: 0 }}>
          {t(subtitleKey)}
        </Paragraph>
      </div>

      <Row gutter={[24, 24]}>
        <Col xs={24} xl={14}>
          <Card>
            <Flex vertical gap={20}>
              <ModelSelect
                models={models}
                capability={mode}
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

              <Flex vertical gap={4} data-testid={`${testId}-url`}>
                <Text type="secondary">{t(urlLabelKey)}</Text>
                <Input
                  value={mediaUrl}
                  onChange={(e) => setMediaUrl(e.target.value)}
                  placeholder={t(urlPlaceholderKey)}
                  disabled={submitting}
                />
              </Flex>

              <PromptInput
                value={prompt}
                onChange={setPrompt}
                mode="t2v"
                placeholder={t('generation.mediaUrlPromptPlaceholder')}
                disabled={submitting}
              />

              <VideoParamsFields
                model={model}
                mode={mode}
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
                data-testid={`submit-${testId}`}
                onClick={() => void handleSubmit()}
              >
                {submitting ? t('generation.submitting') : t('generation.submit')}
              </Button>
            </Flex>
          </Card>
        </Col>

        <Col xs={24} xl={10}>
          <Card title={t('generation.resultTitle')} data-testid={`${testId}-result-card`}>
            {notConfigured ? <ConfigIncompleteAlert admin={isAdmin()} /> : <ResultPanel task={task} polling={polling} />}
          </Card>
        </Col>
      </Row>
    </Flex>
  )
}
