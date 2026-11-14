/**
 * 数字 / 额度 / 时间 的展示工具。
 * 全部是从旧 helpers/render.js + helpers/utils.js 迁移过来的纯函数，
 * 只把「读 localStorage 的副作用」抽成显式入参，方便 SSR 与单元测试。
 */

/**
 * 大数字缩写：1234567 -> 1.2M
 * 返回统一为 string：1234567 -> "1.2M"，1234 -> "1234"。
 * 原先返回 string | number 会让所有调用方都得多写一次 String(...) 兜底，
 * 而前端最终都是往 DOM 里塞文本，统一成 string 更省事也更安全。
 */
export function renderNumber(num: number): string {
  if (typeof num !== 'number' || Number.isNaN(num)) return String(num)
  if (num >= 1000000000) return `${(num / 1000000000).toFixed(1)}B`
  if (num >= 1000000) return `${(num / 1000000).toFixed(1)}M`
  if (num >= 10000) return `${(num / 1000).toFixed(1)}k`
  return String(num)
}

/** 截断文本 */
export function renderText(text: string, limit: number): string {
  if (!text) return ''
  if (text.length > limit) return `${text.slice(0, limit - 3)}...`
  return text
}

export interface QuotaDisplayConfig {
  displayInCurrency: boolean
  quotaPerUnit: number
}

/**
 * 额度 -> 展示文本。
 * 后端 QuotaPerUnit 默认 500000，即 1 美元 = 500000 quota。
 * 返回统一为 string，调用方不必再区分「有货币符号」与「纯数字」两种情况。
 */
export function renderQuota(
  quota: number,
  config: QuotaDisplayConfig,
  precision = 2,
): string {
  const { displayInCurrency, quotaPerUnit } = config
  if (displayInCurrency) {
    const unit = quotaPerUnit || 1
    return `$${(quota / unit).toFixed(precision)}`
  }
  return renderNumber(quota)
}

/** 额度 -> 带等价金额的提示后缀，用于表单 label */
export function renderQuotaWithPrompt(quota: number, config: QuotaDisplayConfig): string {
  if (!config.displayInCurrency) return ''
  const unit = config.quotaPerUnit || 1
  return ` (等价金额：$${(quota / unit).toFixed(2)})`
}

/** Unix 秒 -> 'YYYY-MM-DD HH:mm:ss' */
export function timestamp2string(timestamp: number): string {
  if (!timestamp && timestamp !== 0) return ''
  const date = new Date(timestamp * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} `
    + `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  )
}

/** 'YYYY-MM-DD HH:mm:ss' -> Unix 秒，非法返回 NaN */
export function string2timestamp(str: string): number {
  return Math.ceil(Date.parse(str) / 1000)
}

/** datetime-local 输入框需要的 'YYYY-MM-DDTHH:mm' */
export function timestamp2datetimeLocal(timestamp: number): string {
  const date = new Date(timestamp * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
    + `T${pad(date.getHours())}:${pad(date.getMinutes())}`
  )
}

/** 校验字符串是否为合法 JSON（倍率、模型映射等配置项用） */
export function verifyJSON(str: string): boolean {
  try {
    JSON.parse(str)
    return true
  } catch {
    return false
  }
}

/** 复制到剪贴板，返回是否成功 */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

/** 去掉末尾斜杠 */
export function removeTrailingSlash(url: string): string {
  if (!url) return url
  return url.endsWith('/') ? url.slice(0, -1) : url
}

/** 把文本作为文件下载（兑换码批量导出用） */
export function downloadTextAsFile(text: string, filename: string): void {
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}
