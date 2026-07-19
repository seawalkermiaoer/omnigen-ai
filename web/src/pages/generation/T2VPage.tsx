import { useEffect, useMemo, useState } from 'react'
import { Alert, App, Button, Card, Col, Flex, Row, Typography } from 'antd'
import { PlayCircleOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'

import { catalogApi, generationApi } from '@/api/generation'
import { ApiError } from '@/api/client'
import { useApiError } from '@/hooks/useApiError'
import { useAuthStore } from '@/stores/auth'
import { ModelSelect, ParamPanel, PromptInput, ResultPanel } from '@/components/generation'
import {
  modelHasCapability,
  type CatalogModel,
  type GenerateVideoRequest,
  type ParamPanelValues,
  type ReuseLocationState,
} from '@/types/generation'
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
  const location = useLocation()
  const reuse = (location.state as ReuseLocationState | null)?.reuse

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

  // 目录加载完成后要么应用"复用"数据（历史记录页跳转过来时），要么按旧行为
  // 默认选中第一个可用模型——这个 effect 只在挂载时跑一次（deps 只有
  // notify），此刻读到的 reuse 就是这次挂载唯一会用到的那份，不需要额外
  // 用 ref 防重放；本页面没有像 I2V/R2V 那样"modelId 变化时清空表单"的
  // 第二个 effect，所以这里直接设 modelId 不会被谁再覆盖掉。
  useEffect(() => {
    let cancelled = false
    setLoadingCatalog(true)
    catalogApi
      .list()
      .then((res) => {
        if (cancelled) return
        setModels(res.models)
        if (reuse && reuse.mode === CAPABILITY && res.models.some((m) => m.ID === reuse.model)) {
          setModelId(reuse.model)
          setPrompt(reuse.prompt)
          if (typeof reuse.params.resolution === 'string') setResolution(reuse.params.resolution)
          if (typeof reuse.params.duration === 'number') setDuration(reuse.params.duration)
          if (typeof reuse.params.ratio === 'string') setRatio(reuse.params.ratio)
          setParamValues({
            watermark: typeof reuse.params.watermark === 'boolean' ? reuse.params.watermark : undefined,
            seed: typeof reuse.params.seed === 'number' ? reuse.params.seed : undefined,
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
