import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import zhCN from '@/locales/zh-CN.json'
import en from '@/locales/en.json'

export const SUPPORTED_LOCALES = ['zh-CN', 'en'] as const
export type Locale = (typeof SUPPORTED_LOCALES)[number]

const STORAGE_KEY = 'omnigen_locale'

export function getStoredLocale(): Locale {
  const stored = localStorage.getItem(STORAGE_KEY)
  return SUPPORTED_LOCALES.includes(stored as Locale) ? (stored as Locale) : 'zh-CN'
}

export function setLocale(locale: Locale): void {
  localStorage.setItem(STORAGE_KEY, locale)
  void i18n.changeLanguage(locale)
}

void i18n.use(initReactI18next).init({
  resources: {
    'zh-CN': { translation: zhCN },
    en: { translation: en },
  },
  lng: getStoredLocale(),
  fallbackLng: 'zh-CN',
  interpolation: { escapeValue: false },
})

export default i18n
