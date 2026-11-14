import { createTranslator, SUPPORTED_LOCALES, type SupportedLocale } from '~/utils/i18n'

/**
 * 轻量 i18n composable。
 *
 * 为什么不用 @nuxtjs/i18n：历史语言包是 i18next 语法（`{{name}}`），
 * vue-i18n 的预编译器会把 `{{...}}` 判为非法嵌套占位符使整个构建失败；
 * 即便关掉预编译，消息树仍要在 SSR 与客户端之间序列化，而我们的场景只有
 * 「按 key 取字符串 + 插值」这一种需求。因此直接用一个 50 行的实现替代模块，
 * 代价是失去复数/日期/数字本地化 —— 本项目界面文案里确实没有用到。
 *
 * 语言选择保存在 cookie（one_api_locale），SSR 与客户端读取同一来源，
 * 避免水合前后文案不一致。
 */
const LOCALE_COOKIE = 'one_api_locale'

export function useI18n() {
  const localeCookie = useCookie<SupportedLocale>(LOCALE_COOKIE, {
    default: () => 'zh',
    sameSite: 'lax',
    maxAge: 60 * 60 * 24 * 365,
  })

  const locale = useState<SupportedLocale>('locale', () => (localeCookie.value as SupportedLocale) || 'zh')

  // 让 SSR 输出的 HTML lang 跟随语言
  useHead({
    htmlAttrs: { lang: () => (locale.value === 'zh' ? 'zh-CN' : 'en-US') },
  })

  const t = createTranslator(() => locale.value)

  function setLocale(next: string) {
    const value = (SUPPORTED_LOCALES as readonly string[]).includes(next)
      ? (next as SupportedLocale)
      : 'zh'
    locale.value = value
    localeCookie.value = value
  }

  return { t, locale, setLocale }
}
