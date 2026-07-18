import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import enUS from 'antd/locale/en_US'
import { useTranslation } from 'react-i18next'

import App from './App'
import { omnigenTheme } from '@/theme'
import '@/i18n'
import './index.css'

/** antd 自带组件文案跟随应用语言切换。 */
function Root() {
  const { i18n } = useTranslation()
  return (
    <ConfigProvider theme={omnigenTheme} locale={i18n.language === 'en' ? enUS : zhCN}>
      <AntdApp>
        <BrowserRouter>
          <App />
        </BrowserRouter>
      </AntdApp>
    </ConfigProvider>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <Root />
  </React.StrictMode>,
)
