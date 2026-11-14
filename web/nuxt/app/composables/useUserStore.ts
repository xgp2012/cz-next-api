import type { Status, User } from '~/utils/types'
import { ROLE_ADMIN, ROLE_ROOT } from '~/utils/types'

/**
 * 全局状态（用户 + 系统状态）。
 *
 * 设计取舍：
 * - 用 Nuxt 的 useState 承载，天然可用 SSR 传递，且跨组件共享。
 * - 用户身份在「客户端挂载后」从 localStorage 恢复，与服务端渲染解耦：
 *   原有实现也是纯 localStorage，后端不提供 /api/me，因此没有更好的选择。
 * - 系统状态（Status）额外做一次 /api/status 拉取，并缓存在 localStorage，
 *   使得离线/后端短暂不可用时的首屏仍能显示系统名与 Logo（与旧行为一致）。
 */
const STORAGE_KEYS = {
  user: 'user',
  status: 'status',
  systemName: 'system_name',
  logo: 'logo',
  footerHtml: 'footer_html',
  quotaPerUnit: 'quota_per_unit',
  displayInCurrency: 'display_in_currency',
  chatLink: 'chat_link',
  channelModels: 'channel_models',
  homePageContent: 'home_page_content',
  aff: 'aff',
  notice: 'notice',
}

export function useUserStore() {
  const user = useState<User | null>('user', () => null)
  const status = useState<Status | null>('status', () => null)
  const statusLoaded = useState<boolean>('status-loaded', () => false)
  /** 是否已完成「客户端身份恢复」，用来避免守卫在恢复前误判为未登录 */
  const hydrated = useState<boolean>('user-hydrated', () => false)

  const isLogin = computed(() => !!user.value)
  const isAdmin = computed(() => (user.value?.role ?? 0) >= ROLE_ADMIN)
  const isRoot = computed(() => (user.value?.role ?? 0) >= ROLE_ROOT)

  const systemName = computed(() => status.value?.system_name || 'One API')
  const logo = computed(() => status.value?.logo || '/logo.png')
  const footerHtml = computed(() => status.value?.footer_html || '')
  const chatLink = computed(() => status.value?.chat_link || '')
  const quotaPerUnit = computed(() => Number(status.value?.quota_per_unit ?? 1))
  const displayInCurrency = computed(() => !!status.value?.display_in_currency)

  /** renderQuota 系列函数需要的展示配置 */
  const quotaConfig = computed(() => ({
    displayInCurrency: displayInCurrency.value,
    quotaPerUnit: quotaPerUnit.value,
  }))

  function setUser(next: User | null) {
    user.value = next
    if (import.meta.client) {
      if (next) localStorage.setItem(STORAGE_KEYS.user, JSON.stringify(next))
      else localStorage.removeItem(STORAGE_KEYS.user)
    }
  }

  function login(next: User) {
    setUser(next)
  }

  function logout() {
    setUser(null)
  }

  /** 从 localStorage 恢复身份，客户端挂载后调用一次 */
  function restoreUser() {
    if (!import.meta.client) return
    const raw = localStorage.getItem(STORAGE_KEYS.user)
    if (raw) {
      try {
        user.value = JSON.parse(raw)
      } catch {
        localStorage.removeItem(STORAGE_KEYS.user)
      }
    }
    hydrated.value = true
  }

  /** 先读缓存把 UI 点亮，再拉最新状态覆盖（缓存优先，避免白屏） */
  async function loadStatus(force = false) {
    if (!import.meta.client) return
    if (statusLoaded.value && !force) return

    const cached = localStorage.getItem(STORAGE_KEYS.status)
    if (cached) {
      try {
        status.value = JSON.parse(cached)
      } catch {
        localStorage.removeItem(STORAGE_KEYS.status)
      }
    }

    try {
      const res = await api.get<Status>('/api/status')
      if (res.data) {
        applyStatus(res.data)
      }
    } catch {
      // 状态拉取失败不阻塞渲染，沿用缓存
    } finally {
      statusLoaded.value = true
    }
  }

  /**
   * 应用状态数据。
   *
   * 接受 Partial<Status>：既用于 /api/status 的完整响应，
   * 也用于「设置页改了系统名称/Logo 后立刻同步顶栏」这类单字段更新。
   * 传入部分字段时会与已有状态合并，不会把其余字段清空。
   */
  function applyStatus(data: Partial<Status>) {
    const merged = { ...(status.value || {}), ...data } as Status
    status.value = merged
    if (!import.meta.client) return
    localStorage.setItem(STORAGE_KEYS.status, JSON.stringify(merged))
    if (data.system_name !== undefined) localStorage.setItem(STORAGE_KEYS.systemName, data.system_name)
    if (data.logo !== undefined) localStorage.setItem(STORAGE_KEYS.logo, data.logo)
    if (data.footer_html !== undefined) localStorage.setItem(STORAGE_KEYS.footerHtml, data.footer_html)
    if (data.quota_per_unit !== undefined) {
      localStorage.setItem(STORAGE_KEYS.quotaPerUnit, String(data.quota_per_unit))
    }
    if (data.display_in_currency !== undefined) {
      localStorage.setItem(STORAGE_KEYS.displayInCurrency, String(!!data.display_in_currency))
    }
    if (data.chat_link !== undefined) {
      if (data.chat_link) localStorage.setItem(STORAGE_KEYS.chatLink, data.chat_link)
      else localStorage.removeItem(STORAGE_KEYS.chatLink)
    }
  }

  return {
    user,
    status,
    hydrated,
    statusLoaded,
    isLogin,
    isAdmin,
    isRoot,
    systemName,
    logo,
    footerHtml,
    chatLink,
    quotaPerUnit,
    displayInCurrency,
    quotaConfig,
    setUser,
    login,
    logout,
    restoreUser,
    loadStatus,
    applyStatus,
  }
}

/** 供非 composable 环境读取缓存渠道模型列表（对应旧 getChannelModels） */
export function readChannelModelsCache(): Record<string, string[]> {
  if (!import.meta.client) return {}
  const raw = localStorage.getItem(STORAGE_KEYS.channelModels)
  if (!raw) return {}
  try {
    return JSON.parse(raw)
  } catch {
    return {}
  }
}

export function writeChannelModelsCache(data: Record<string, string[]>) {
  if (!import.meta.client) return
  localStorage.setItem(STORAGE_KEYS.channelModels, JSON.stringify(data))
}

export { STORAGE_KEYS }
