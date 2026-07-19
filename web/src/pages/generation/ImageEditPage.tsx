import { useEffect, useMemo, useRef, useState } from 'react'
import { App, Button, Card, Flex, Spin, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'

import { catalogApi, generationApi } from '@/api/generation'
import { ApiError } from '@/api/client'
import { useApiError } from '@/hooks/useApiError'
import { useAuthStore } from '@/stores/auth'
import { MediaUploader, ModelSelect, ParamPanel, PromptInput, ResultPanel } from '@/components/generation'
import type { MediaUploaderItem } from '@/components/generation'
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
 * 图片编辑。capability 固定为 'edit'，至少需要一张参考图才允许提交
 * （prompt 是可选的）——与旧 public/js/imgedit.js 的校验顺序一致
 * （`if (imgedit.state.images.length === 0) return alert(...)`，从不检查
 * prompt 是否为空）。上传张数上限完全来自目录的 MaxImages 字段
 * （qwen-edit 系列 3 张、wan 系列 9 张），不在这里写死。
 */
export default function ImageEditPage() {
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
  const [images, setImages] = useState<MediaUploaderItem[]>([])

  const [submitting, setSubmitting] = useState(false)
  const [task, setTask] = useState<GenerationTask | null>(null)
  const [notConfigured, setNotConfigured] = useState(false)

  const editModels = useMemo(() => models.filter((m) => modelHasCapability(m, 'edit')), [models])
  const selectedModel = editModels.find((m) => m.ID === modelId)

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

  // 目录加载完成后默认选中第一个可用模型，行为对齐 ImageGenPage。有待应用
  // 的"复用"数据时跳过，理由同 ImageGenPage 的同名 effect。
  useEffect(() => {
    if (!modelId && !reuse && editModels.length > 0) {
      setModelId(editModels[0].ID)
    }
  }, [modelId, editModels, reuse])

  // 从历史记录页"复用"过来：机制与 ImageGenPage 完全一致，见其注释。
  // 输入图片不恢复——旧系统的复用同样从不恢复输入素材（history.js 的
  // reuseOtherNote 文案），imgedit 提交前必须至少有一张图，所以复用后
  // 用户仍然需要重新上传才能提交,这里只提示一次而不是假装帮他恢复了。
  useEffect(() => {
    if (!reuse || reuse.mode !== 'imgedit' || reuseAppliedRef.current || models.length === 0) return
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
    void message.info(t('generation.reuseImageNote'))
  }, [reuse, models, message, t])

  const handleModelChange = (id: string) => {
    setModelId(id)
    setParams({})
  }

  const canSubmit = !!selectedModel && images.length > 0 && !submitting

  const handleSubmit = async () => {
    if (!selectedModel || images.length === 0) return
    setSubmitting(true)
    setNotConfigured(false)
    setTask(null)
    try {
      const result = await generationApi.generateImage({
        model: selectedModel.ID,
        prompt: prompt.trim(),
        images: images.map((img) => img.url),
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
        {t('nav.imgedit')}
      </Title>

      <Flex gap={20} wrap align="flex-start">
        <Card title={t('generation.settingsTitle')} style={{ flex: '1 1 420px', minWidth: 340 }}>
          <Flex vertical gap={16}>
            <ModelSelect
              models={editModels}
              capability="edit"
              value={modelId}
              onChange={handleModelChange}
              loading={catalogLoading}
              disabled={submitting}
            />

            <Flex vertical gap={4}>
              <Text type="secondary">{t('generation.mediaSectionTitle')}</Text>
              {selectedModel ? (
                <MediaUploader
                  value={images}
                  onChange={setImages}
                  maxCount={selectedModel.MaxImages}
                  minEdge={selectedModel.MinImageEdge || undefined}
                  ratioMin={selectedModel.RatioMin || undefined}
                  ratioMax={selectedModel.RatioMax || undefined}
                  disabled={submitting}
                />
              ) : (
                <Text type="secondary" data-testid="media-uploader-placeholder">
                  {t('generation.paramPanelSelectModelFirst')}
                </Text>
              )}
            </Flex>

            <ParamPanel model={selectedModel} value={params} onChange={setParams} disabled={submitting} />

            <PromptInput
              value={prompt}
              onChange={setPrompt}
              mode="imggen_edit"
              images={images.length > 0 ? images.map((img) => img.url) : undefined}
              disabled={submitting}
              placeholder={t('generation.imgeditPromptPlaceholder')}
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
              {!submitting && images.length === 0 && (
                <Text type="secondary" style={{ fontSize: 12 }}>
                  {t('generation.imageRequiredHint')}
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
