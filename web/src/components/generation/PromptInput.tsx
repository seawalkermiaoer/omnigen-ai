import { useState } from 'react'
import { Button, Flex, Input, Typography } from 'antd'
import { ThunderboltOutlined, UndoOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'

import { generationApi } from '@/api/generation'
import { useApiError } from '@/hooks/useApiError'
import type { OptimizeMode } from '@/types/generation'

const { TextArea } = Input
const { Text } = Typography

export interface PromptInputProps {
  value: string
  onChange: (value: string) => void
  /** 优化走哪套系统提示词——由调用页面根据自己的 tab/素材状态决定，见 task.js:bindOptimize 的 mode 选择逻辑。 */
  mode: OptimizeMode
  /** 视觉优化用的参考图（base64/URL），纯文本模式下留空即可。 */
  images?: string[]
  videoCount?: number
  disabled?: boolean
  placeholder?: string
  rows?: number
}

/**
 * Prompt 输入框：文本域 + 自动优化按钮 + 撤销。
 *
 * 优化成功后展示"由哪个模型产出"——旧 UI 的降级链可能让实际生效模型与
 * 主模型不同（AccessDenied 时回退），用户需要知道这一点，见
 * server/internal/handler/optimize.go 对 optimizePromptResponse.Model 的
 * 注释。
 */
export default function PromptInput({
  value,
  onChange,
  mode,
  images,
  videoCount,
  disabled,
  placeholder,
  rows = 4,
}: PromptInputProps) {
  const { t } = useTranslation()
  const { toMessage } = useApiError()

  const [optimizing, setOptimizing] = useState(false)
  const [undoSnapshot, setUndoSnapshot] = useState<string | null>(null)
  const [hint, setHint] = useState<{ type: 'ok' | 'err'; text: string } | null>(null)

  const handleOptimize = async () => {
    setOptimizing(true)
    setHint(null)
    try {
      const result = await generationApi.optimizePrompt({
        draft: value,
        images,
        mode,
        videoCount,
      })
      setUndoSnapshot(value)
      onChange(result.prompt)
      setHint({
        type: 'ok',
        text: t('generation.promptOptimizedBy', { model: result.model, count: result.prompt.length }),
      })
    } catch (err) {
      setHint({ type: 'err', text: toMessage(err) })
    } finally {
      setOptimizing(false)
    }
  }

  const handleUndo = () => {
    if (undoSnapshot === null) return
    onChange(undoSnapshot)
    setUndoSnapshot(null)
    setHint(null)
  }

  return (
    <Flex vertical gap={8} data-testid="prompt-input">
      <TextArea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder ?? t('generation.promptPlaceholder')}
        autoSize={{ minRows: rows, maxRows: rows + 6 }}
        disabled={disabled || optimizing}
      />

      <Flex align="center" gap={12} wrap>
        <Button
          icon={<ThunderboltOutlined aria-hidden />}
          loading={optimizing}
          disabled={disabled}
          autoInsertSpace={false}
          onClick={() => void handleOptimize()}
        >
          {optimizing ? t('generation.promptOptimizing') : t('generation.promptOptimize')}
        </Button>

        {undoSnapshot !== null && (
          <Button
            type="link"
            icon={<UndoOutlined aria-hidden />}
            autoInsertSpace={false}
            disabled={disabled}
            onClick={handleUndo}
          >
            {t('generation.promptUndo')}
          </Button>
        )}

        {hint && (
          <Text type={hint.type === 'err' ? 'danger' : 'secondary'} data-testid="prompt-hint">
            {hint.text}
          </Text>
        )}
      </Flex>
    </Flex>
  )
}
