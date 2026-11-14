/**
 * 系统设置（Option 表）的读写封装。
 *
 * 后端约定（controller/option.go）：
 * - GET  /api/option/  仅 root 可访问，返回 [{ key, value }] 列表，
 *   其中以 Token / Secret 结尾的敏感项会被后端过滤掉；
 * - PUT  /api/option/  { key, value } 逐项更新，成功即生效（无需重启），
 *   部分项有前置校验（如启用 GitHub OAuth 前必须先填 ClientId）。
 *
 * 所有 value 在传输时都是字符串，布尔项用 "true"/"false"，数字项用十进制字符串。
 * 这里统一做字符串 <-> 原生类型的转换，避免每个设置组件重复处理。
 */
import type { OptionItem } from '~/utils/types'

export function useOptions() {
  const { showError, success } = useAppToast()
  const { t } = useI18n()

  const optionMap = ref<Record<string, string>>({})
  const loading = ref(false)
  const saving = ref(false)

  async function loadOptions() {
    loading.value = true
    try {
      const res = await api.get<OptionItem[]>('/api/option/')
      const map: Record<string, string> = {}
      for (const item of res.data || []) {
        map[item.key] = item.value
      }
      optionMap.value = map
    } catch (error) {
      showError(error)
    } finally {
      loading.value = false
    }
  }

  /** 读取字符串项 */
  function getString(key: string, fallback = ''): string {
    return optionMap.value[key] ?? fallback
  }

  /** 读取布尔项，后端用 "true"/"false" 字符串表示 */
  function getBool(key: string, fallback = false): boolean {
    const raw = optionMap.value[key]
    if (raw === undefined) return fallback
    return raw === 'true'
  }

  /** 读取数字项 */
  function getNumber(key: string, fallback = 0): number {
    const raw = optionMap.value[key]
    if (raw === undefined || raw === '') return fallback
    const parsed = Number(raw)
    return Number.isFinite(parsed) ? parsed : fallback
  }

  /** 本地更新缓存，便于开关立即反映在界面上 */
  function setLocal(key: string, value: string | number | boolean) {
    optionMap.value = { ...optionMap.value, [key]: String(value) }
  }

  /**
   * 提交单个配置项。
   * 采用「先乐观更新 + 失败回滚」的方式：开关类控件需要即时反馈，
   * 但仍以后端返回为准（后端可能因校验失败而拒绝）。
   */
  async function updateOption(
    key: string,
    value: string | number | boolean,
    options: { successMessage?: string, silent?: boolean } = {},
  ): Promise<boolean> {
    const previous = optionMap.value[key]
    const serialized = typeof value === 'boolean' ? String(value) : String(value)
    setLocal(key, serialized)
    saving.value = true
    try {
      await api.put('/api/option/', { key, value: serialized })
      if (!options.silent) {
        success(options.successMessage || t('setting.messages.update_success', '设置已更新'))
      }
      return true
    } catch (error) {
      // 回滚，保持界面与后端一致
      if (previous === undefined) {
        const next = { ...optionMap.value }
        delete next[key]
        optionMap.value = next
      } else {
        setLocal(key, previous)
      }
      showError(error)
      return false
    } finally {
      saving.value = false
    }
  }

  /** 批量提交多个项（用于「保存 XXX 设置」按钮） */
  async function updateOptions(
    entries: Array<[string, string | number | boolean]>,
    successMessage?: string,
  ): Promise<boolean> {
    saving.value = true
    try {
      for (const [key, value] of entries) {
        await api.put('/api/option/', { key, value: String(value) })
        setLocal(key, value)
      }
      success(successMessage || t('setting.messages.update_success', '设置已更新'))
      return true
    } catch (error) {
      showError(error)
      return false
    } finally {
      saving.value = false
    }
  }

  return {
    optionMap,
    loading,
    saving,
    loadOptions,
    getString,
    getBool,
    getNumber,
    setLocal,
    updateOption,
    updateOptions,
  }
}
