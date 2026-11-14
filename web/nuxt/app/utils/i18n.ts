/**
 * i18n 消息层。
 *
 * 为什么要自己重写一遍插值：
 * 旧前端用的是 i18next，插值语法是 `{{name}}`（双花括号）。
 * 新前端换成 vue-i18n，其默认语法是 `{name}`（单花括号），并且把 `{{...}}`
 * 视为「嵌套占位符」而直接报错，导致 800 行历史语言包无法直接复用。
 *
 * 这里兼容了两种形态：
 * - `{{name}}`：i18next 风格，替换为参数值
 * - `{name}` ：vue-i18n 风格，同样替换（两者共用同一套参数）
 *
 * 之所以不用 vue-i18n 的 messageCompiler 自定义方案，是因为它需要运行期把整棵
 * 消息树重新编译，代价高且与 SSG 预渲染的序列化冲突；对后台这种「纯插值」场景，
 * 直接做字符串替换既简单又零开销。
 */

/** 历史语言包（i18next 语法），按语言拆成独立模块便于并行加载 */
import zhMessages from '~~/i18n/locales/zh.json'
import enMessages from '~~/i18n/locales/en.json'
import { zhExtra, enExtra } from '~~/i18n/extra'

export type MessageTree = Record<string, any>

/** 深合并且不覆盖已有键：历史语言包优先，extra 只做补充 */
function mergeDeep(target: MessageTree, source: MessageTree): MessageTree {
  for (const key of Object.keys(source)) {
    const value = source[key]
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      target[key] = mergeDeep(target[key] || {}, value)
    } else if (target[key] === undefined) {
      target[key] = value
    }
  }
  return target
}

export const MESSAGES: Record<string, MessageTree> = {
  zh: mergeDeep(structuredClone(zhMessages as MessageTree), zhExtra as MessageTree),
  en: mergeDeep(structuredClone(enMessages as MessageTree), enExtra as MessageTree),
}

/** 按 'a.b.c' 路径取消息，取不到返回 undefined */
function getByPath(tree: MessageTree, path: string): string | undefined {
  const segments = path.split('.')
  let current: any = tree
  for (const segment of segments) {
    if (current == null) return undefined
    current = current[segment]
  }
  return typeof current === 'string' ? current : undefined
}

/**
 * 插值渲染。
 * @param template 形如 '渠道 {{name}} 测试成功'
 * @param params   形如 { name: 'x' }
 */
export function renderTemplate(template: string, params?: Record<string, any>): string {
  if (!template) return ''
  if (!params) return template
  return template
    // 先匹配 {{name}}（i18next），再匹配 {name}（vue-i18n）
    .replace(/\{\{\s*(\w+)\s*\}\}/g, (match, key: string) =>
      params[key] !== undefined && params[key] !== null ? String(params[key]) : match,
    )
    .replace(/(?<!\{)\{\s*(\w+)\s*\}(?!\})/g, (match, key: string) =>
      params[key] !== undefined && params[key] !== null ? String(params[key]) : match,
    )
}

/**
 * 翻译函数工厂。
 * 第二个参数支持两种形态，覆盖两类真实调用场景：
 *   t('header.token')                                     —— 纯取文案
 *   t('channel.messages.test_success', { name, model })   —— 带插值
 *   t('redemption.messages.operation_success', '操作成功')  —— 语言包缺该 key 时的兜底文案
 * 最后一种很常用：历史语言包并未覆盖所有新文案，
 * 与其让界面直接渲染出原始 key，不如就地给一个可读的默认值。
 */
export function createTranslator(getLocale: () => string) {
  return function t(
    key: string,
    named?: Record<string, any> | string,
    fallbackLocale = 'zh',
  ): string {
    const locale = getLocale()
    const params = typeof named === 'string' ? undefined : named
    const inlineFallback = typeof named === 'string' ? named : undefined

    const found
      = getByPath(MESSAGES[locale] || {}, key)
        ?? getByPath(MESSAGES[fallbackLocale] || {}, key)

    // 语言包里没有该 key 时，优先用调用方给的默认文案，最后才退回 key 本身
    const message = found ?? inlineFallback ?? key
    return renderTemplate(message, params)
  }
}

export const SUPPORTED_LOCALES = ['zh', 'en'] as const
export type SupportedLocale = (typeof SUPPORTED_LOCALES)[number]

export const LOCALE_OPTIONS = [
  { label: '中文', value: 'zh' },
  { label: 'English', value: 'en' },
]
