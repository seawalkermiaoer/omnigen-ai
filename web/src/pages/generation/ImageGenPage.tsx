import { useEffect, useMemo, useRef, useState } from 'react'
import { App, Button, Card, Flex, Spin, Typography } from 'antd'
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
  type GenerationTask,
  type ParamPanelValues,
  type ReuseLocationState,
} from '@/types/generation'
import ConfigIncompleteAlert from './ConfigIncompleteAlert'

const { Title, Text } = Typography

/**
 * 图片生成（文生图）。capability 固定为 't2i'，从不携带任何输入图片——
 * 与旧 public/js/imggen.js 的行为一致（提交体里 images 恒为空数组）。
 * 图片生成是同步接口，没有轮询：提交后要么直接拿到结果，要么直接报错。
 */
export default function ImageGenPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const { notify } = useApiError()
  const isAdmin = useAuthStore((s) => s.isAdmin)
  const location = useLocation()
  const reuse = (location.state as ReuseLocationState | null)?.reuse
  const reuseAppliedRef = useRef(false)

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
  // 有待应用的"复用"数据时跳过：不然这里会抢在下面的复用 effect 之前把
  // modelId 设成目录第一项，复用 effect 读到的 modelId 已经不是空值、
  // 但又还没提交，产生一次可见的"先选中默认模型、再跳成复用模型"的闪烁。
  useEffect(() => {
    if (!modelId && !reuse && t2iModels.length > 0) {
      setModelId(t2iModels[0].ID)
    }
  }, [modelId, t2iModels, reuse])

  // 从历史记录页"复用"过来：把 prompt/model/参数原样填回来。目录是异步
  // 加载的（见上面的 catalogApi.list() effect），这里用 models.length > 0
  // 作为"目录已就绪"的信号，不依赖任何特定的加载时序——无论复用点击发生
  // 在目录请求之前还是之后，effect 都会在目录到位后的下一次渲染补上这次
  // 应用，reuseAppliedRef 保证只应用一次（避免用户手动改了参数后被覆盖）。
  useEffect(() => {
    if (!reuse || reuse.mode !== 'imggen' || reuseAppliedRef.current || models.length === 0) return
    reuseAppliedRef.current = true
    setModelId(reuse.model)
    setPrompt(reuse.prompt)
    setParams({
      size: typeof reuse.params.size === 'string' ? reuse.params.size : undefined,
      n: typeof reuse.params.n === 'number' ? reuse.params.n : undefined,
      watermark: typeof reuse.params.watermark === 'boolean' ? reuse.params.watermark : undefined,
      seed: typeof reuse.params.seed === 'number' ? reuse.params.seed : undefined,
      thinkingMode: typeof reuse.params.thinkingMode === 'boolean' ? reuse.params.thinkingMode : undefined,
      enableSequential:
        typeof reuse.params.enableSequential === 'boolean' ? reuse.params.enableSequential : undefined,
      promptExtend: typeof reuse.params.promptExtend === 'boolean' ? reuse.params.promptExtend : undefined,
      negativePrompt: typeof reuse.params.negativePrompt === 'string' ? reuse.params.negativePrompt : undefined,
    })
    void message.info(reuse.hadInput ? t('generation.reuseImageNote') : t('generation.reuseApplied'))
  }, [reuse, models, message, t])

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
