import { useEffect, useMemo, useState } from 'react'
import { Alert, App, Button, Card, Col, Flex, Row, Typography } from 'antd'
import { PlayCircleOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'

import { catalogApi, generationApi } from '@/api/generation'
import { ApiError } from '@/api/client'
import { useApiError } from '@/hooks/useApiError'
import { useAuthStore } from '@/stores/auth'
import { ModelSelect, ParamPanel, PromptInput, ResultPanel } from '@/components/generation'
import { modelHasCapability, type CatalogModel, type GenerateVideoRequest, type ParamPanelValues } from '@/types/generation'
import ConfigIncompleteAlert from './ConfigIncompleteAlert'
import VideoParamsFields from './VideoParamsFields'
import { DEFAULT_DURATION, DEFAULT_RATIO, DEFAULT_RESOLUTION } from './videoParams'
import { useVideoTaskPolling } from './useVideoTaskPolling'

const { Title, Paragraph } = Typography
const CAPABILITY = 't2v'

/**
 * 文生视频：三个视频页面里最简单的一个——只有 prompt，没有任何素材上传。
 * model 当前目录里只有 happyhorse-1.1-t2v 一个，但选择逻辑仍然完全按
 * capability 从目录派生，不写死这个 id（server.js:169-184 移植过来的目录
 * 未来加新 t2v 模型时，这里不需要跟着改）。
 */
export default function T2VPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const { notify } = useApiError()
  const isAdmin = useAuthStore((s) => s.isAdmin)

  const [models, setModels] = useState<CatalogModel[]>([])
  const [loadingCatalog, setLoadingCatalog] = useState(true)
  const [modelId, setModelId] = useState<string>()
  const [prompt, setPrompt] = useState('')
  const [resolution, setResolution] = useState(DEFAULT_RESOLUTION)
  const [duration, setDuration] = useState(DEFAULT_DURATION)
  const [ratio, setRatio] = useState(DEFAULT_RATIO)
  const [paramValues, setParamValues] = useState<ParamPanelValues>({})
  const [submitting, setSubmitting] = useState(false)
  const [notConfigured, setNotConfigured] = useState(false)

  const { task, polling, start } = useVideoTaskPolling()

  useEffect(() => {
    let cancelled = false
    setLoadingCatalog(true)
    catalogApi
      .list()
      .then((res) => {
        if (cancelled) return
        setModels(res.models)
        const first = res.models.find((m) => modelHasCapability(m, CAPABILITY))
        if (first) setModelId(first.ID)
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
  }, [notify])

  const model = useMemo(() => models.find((m) => m.ID === modelId), [models, modelId])
  const isRestrictedRegion = (model?.Regions?.length ?? 0) > 0

  const handleSubmit = async () => {
    if (!model) return
    if (!prompt.trim()) {
      void message.error(t('generation.promptRequiredHint'))
      return
    }

    const req: GenerateVideoRequest = {
      model: model.ID,
      prompt: prompt.trim(),
      resolution,
      duration,
      ratio,
      watermark: !!paramValues.watermark,
      seed: paramValues.seed,
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
          {t('nav.t2v')}
        </Title>
        <Paragraph type="secondary" style={{ marginBottom: 0 }}>
          {t('generation.t2vSubtitle')}
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

              <PromptInput
                value={prompt}
                onChange={setPrompt}
                mode="t2v"
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
                icon={<PlayCircleOutlined aria-hidden />}
                loading={submitting}
                disabled={!model}
                autoInsertSpace={false}
                data-testid="submit-t2v"
                onClick={() => void handleSubmit()}
              >
                {submitting ? t('generation.submitting') : t('generation.submit')}
              </Button>
            </Flex>
          </Card>
        </Col>

        <Col xs={24} xl={10}>
          <Card title={t('generation.resultTitle')} data-testid="t2v-result-card">
            {notConfigured ? <ConfigIncompleteAlert admin={isAdmin()} /> : <ResultPanel task={task} polling={polling} />}
          </Card>
        </Col>
      </Row>
    </Flex>
  )
}
