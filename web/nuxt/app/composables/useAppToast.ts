import { marked } from 'marked'

const TOAST_TIMEOUT = {
  success: 5000,
  info: 8000,
  warning: 10000,
  error: 10000,
  notice: 20000,
}

/**
 * 全局提示。
 *
 * 旧实现直接依赖 react-toastify；这里改为委托给 @nuxt/ui 的 useToast，
 * 因此必须在 setup 上下文中调用（页面/组件的 script setup 内），
 * 不能在模块顶层执行。toast.d.ts 里做了类型声明避免重复 import。
 */
export function useAppToast() {
  const toast = useToast()

  function success(message: string) {
    toast.add({
      title: message,
      color: 'success',
      icon: 'i-heroicons-check-circle',
      duration: TOAST_TIMEOUT.success,
    })
  }

  function info(message: string) {
    toast.add({
      title: message,
      color: 'info',
      icon: 'i-heroicons-information-circle',
      duration: TOAST_TIMEOUT.info,
    })
  }

  function warning(message: string) {
    toast.add({
      title: message,
      color: 'warning',
      icon: 'i-heroicons-exclamation-triangle',
      duration: TOAST_TIMEOUT.warning,
    })
  }

  function error(message: string) {
    toast.add({
      title: message,
      color: 'error',
      icon: 'i-heroicons-x-circle',
      duration: TOAST_TIMEOUT.error,
    })
  }

  /** 公告：支持 Markdown 渲染后的 HTML，用 UAlert 承载 */
  function notice(message: string, isHtml = false) {
    toast.add({
      title: isHtml ? undefined : message,
      description: isHtml ? message : undefined,
      color: 'info',
      icon: 'i-heroicons-megaphone',
      duration: TOAST_TIMEOUT.notice,
    })
  }

  /** 统一错误出口：兼容 ApiError / Error / 字符串三种形态 */
  function showError(value: unknown) {
    if (!value) return
    const message
      = value instanceof Error ? value.message : typeof value === 'string' ? value : '未知错误'
    // 401 由 http 层跳转，这里不再弹提示
    if ((value as any)?.statusCode === 401) return
    error(message)
  }

  return { success, info, warning, error, notice, showError }
}

/** Markdown -> HTML，与旧前端 marked.parse 行为一致 */
export function renderMarkdown(text: string): string {
  if (!text) return ''
  return marked.parse(text) as string
}

/** 判断一段配置内容是不是外链（是则用 iframe 承载，否则按 Markdown 渲染） */
export function isEmbedUrl(text: string): boolean {
  return typeof text === 'string' && text.startsWith('https://')
}
