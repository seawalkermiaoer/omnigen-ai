import { useEffect, useMemo, useState } from 'react'
import { Button, Card, Flex, Spin, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

import { catalogApi, generationApi } from '@/api/generation'
import { ApiError } from '@/api/client'
import { useApiError } from '@/hooks/useApiError'
import { useAuthStore } from '@/stores/auth'
import { ModelSelect, ParamPanel, PromptInput, ResultPanel } from '@/components/generation'
import { modelHasCapability, type CatalogModel, type GenerationTask, type ParamPanelValues } from '@/types/generation'
import ConfigIncompleteAlert from './ConfigIncompleteAlert'

const { Title, Text } = Typography

/**
 * 图片生成（文生图）。capability 固定为 't2i'，从不携带任何输入图片——
 * 与旧 public/js/imggen.js 的行为一致（提交体里 images 恒为空数组）。
 * 图片生成是同步接口，没有轮询：提交后要么直接拿到结果，要么直接报错。
 */
export default function ImageGenPage() {
  const { t } = useTranslation()
  const { notify } = useApiError()
  const isAdmin = useAuthStore((s) => s.isAdmin)

  const [models, setModels] = useState<CatalogModel[]>([])
  const [catalogLoading, setCatalogLoading] = useState(true)

  const [modelId, setModelId] = useState<string | undefined>(undefined)
  const [prompt, setPrompt] = useState('')
  const [params, setParams] = useState<ParamPanelValues>({})

  const [submitting, setSubmitting] = useState(false)
  const [task, setTask] = useState<GenerationTask | null>(null)
  const [notConfigured, setNotConfigured] = useState(false)

  const t2iModels = useMemo(() => models.filter((m) => modelHasCapability(m, 't2i')), [models])
  const selectedModel = t2iModels.find((m) => m.ID === modelId)

  useEffect(() => {
    let alive = true
    setCatalogLoading(true)
    catalogApi
      .list()
      .then((res) => {
        if (alive) setModels(res.models)
      })
      .catch((err) => {
        if (alive) notify(err)
      })
      .finally(() => {
        if (alive) setCatalogLoading(false)
      })
    return () => {
      alive = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 目录加载完成后默认选中第一个可用模型——旧版原生 <select> 天然带有一个
  // 默认选中项，这里保持同样的开箱可用体验，而不是让用户先手动选一次。
  useEffect(() => {
    if (!modelId && t2iModels.length > 0) {
      setModelId(t2iModels[0].ID)
    }
  }, [modelId, t2iModels])

  const handleModelChange = (id: string) => {
    setModelId(id)
    // 参数约束按模型而定，换模型后旧值可能已不合法（比如上一个模型开着
    // thinking_mode，新模型根本不认这个参数）——不能把旧参数带过去。
    setParams({})
  }

  const trimmedPrompt = prompt.trim()
  const canSubmit = !!selectedModel && trimmedPrompt.length > 0 && !submitting

  const handleSubmit = async () => {
    if (!selectedModel || !trimmedPrompt) return
    setSubmitting(true)
    setNotConfigured(false)
    setTask(null)
    try {
      const result = await generationApi.generateImage({
        model: selectedModel.ID,
        prompt: trimmedPrompt,
        size: params.size,
        n: params.n,
        watermark: params.watermark,
        seed: params.seed,
        thinkingMode: params.thinkingMode,
        enableSequential: params.enableSequential,
        promptExtend: params.promptExtend,
        negativePrompt: params.negativePrompt?.trim() || undefined,
      })
      setTask(result)
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
    <Flex vertical gap={20}>
      <Title level={3} style={{ margin: 0 }}>
        {t('nav.imggen')}
      </Title>

      <Flex gap={20} wrap align="flex-start">
        <Card title={t('generation.settingsTitle')} style={{ flex: '1 1 420px', minWidth: 340 }}>
          <Flex vertical gap={16}>
            <ModelSelect
              models={t2iModels}
              capability="t2i"
              value={modelId}
              onChange={handleModelChange}
              loading={catalogLoading}
              disabled={submitting}
            />

            <ParamPanel model={selectedModel} value={params} onChange={setParams} disabled={submitting} />

            <PromptInput
              value={prompt}
              onChange={setPrompt}
              mode="t2i"
              disabled={submitting}
              placeholder={t('generation.promptPlaceholder')}
            />

            <Flex vertical gap={4}>
              <Button
                type="primary"
                block
                loading={submitting}
                disabled={!canSubmit}
                autoInsertSpace={false}
                onClick={() => void handleSubmit()}
              >
                {submitting ? t('generation.submitting') : t('generation.submit')}
              </Button>
              {!submitting && !trimmedPrompt && (
                <Text type="secondary" style={{ fontSize: 12 }}>
                  {t('generation.promptRequiredHint')}
                </Text>
              )}
            </Flex>
          </Flex>
        </Card>

        <Card title={t('generation.resultTitle')} style={{ flex: '1 1 420px', minWidth: 340 }}>
          {notConfigured ? (
            <ConfigIncompleteAlert admin={isAdmin()} />
          ) : submitting ? (
            <Flex vertical align="center" gap={8} style={{ padding: '32px 0' }} data-testid="generation-pending">
              <Spin />
              <Text type="secondary">{t('generation.submitting')}</Text>
            </Flex>
          ) : (
            <ResultPanel task={task} />
          )}
        </Card>
      </Flex>
    </Flex>
  )
}
