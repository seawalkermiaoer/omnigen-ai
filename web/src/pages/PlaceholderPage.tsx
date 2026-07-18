import { Empty, Typography } from 'antd'
import { useTranslation } from 'react-i18next'

const { Title, Paragraph } = Typography

/** 生成类功能的占位页。子项目 3 会逐个替换掉。 */
export default function PlaceholderPage({ nameKey }: { nameKey: string }) {
  const { t } = useTranslation()
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', paddingTop: 80 }}>
      <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={false} />
      <Title level={4} style={{ marginTop: 24 }}>
        {t('placeholder.title')}
      </Title>
      <Paragraph type="secondary" style={{ maxWidth: 420, textAlign: 'center' }}>
        {t('placeholder.description', { name: t(nameKey) })}
      </Paragraph>
    </div>
  )
}
