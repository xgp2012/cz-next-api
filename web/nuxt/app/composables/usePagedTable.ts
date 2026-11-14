/**
 * 客户端分页表格的公共逻辑。
 *
 * One API 后端是「游标式」分页：GET /?p=0 取前 10 条，p=1 取第 11-20 条，
 * 且返回条数少于 ITEMS_PER_PAGE 时表示到底了。旧实现用「本地累积 + splice」实现，
 * 这里把这个约定封装成一个 composable，避免在每个表格里重复。
 */
import { ITEMS_PER_PAGE } from '~/constants/channel'

export interface PagedTableOptions {
  /** 拉取指定页（0-based）的数据 */
  fetcher: (startIdx: number) => Promise<any[]>
  /** 获取方式（排序字段等）变化时自动重载 */
  watchSource?: () => unknown
}

export function usePagedTable(options: PagedTableOptions) {
  const rows = ref<any[]>([])
  const loading = ref(true)
  const activePage = ref(1)
  /** 是否还有下一页：后端返回满页即认为可能还有更多 */
  const hasMore = ref(true)

  async function loadPage(startIdx: number) {
    loading.value = true
    try {
      const data = await options.fetcher(startIdx)
      const list = Array.isArray(data) ? data : []
      if (startIdx === 0) {
        rows.value = list
      } else {
        // 覆盖式写入，避免重复追加
        const next = [...rows.value]
        next.splice(startIdx * ITEMS_PER_PAGE, list.length, ...list)
        rows.value = next
      }
      hasMore.value = list.length >= ITEMS_PER_PAGE
    } finally {
      loading.value = false
    }
  }

  /** 当前页要渲染的行（后端已分页，这里只是切窗口） */
  const pageRows = computed(() => {
    const start = (activePage.value - 1) * ITEMS_PER_PAGE
    return rows.value.slice(start, start + ITEMS_PER_PAGE)
  })

  /** 已加载数据对应的最大页码 */
  const maxLoadedPage = computed(() => Math.max(1, Math.ceil(rows.value.length / ITEMS_PER_PAGE)))

  /** 总页数：已知还有更多时多留一页，让用户能触发下一页加载 */
  const totalPages = computed(() => maxLoadedPage.value + (hasMore.value && rows.value.length % ITEMS_PER_PAGE === 0 ? 1 : 0))

  async function onPageChange(page: number) {
    // 请求的页尚未加载过 -> 先加载
    if (page > maxLoadedPage.value) {
      await loadPage(page - 1)
    }
    activePage.value = page
  }

  async function refresh() {
    activePage.value = 1
    await loadPage(0)
  }

  /** 本地排序（后端不支持任意列排序，保持与原实现一致的行为） */
  function sortBy(key: string) {
    if (rows.value.length === 0) return
    const sorted = [...rows.value]
    sorted.sort((a, b) => {
      const left = a[key]
      const right = b[key]
      if (!Number.isNaN(Number(left))) return Number(left) - Number(right)
      return String(left).localeCompare(String(right))
    })
    // 已经是升序则反转，实现「点击切换方向」
    if (sorted[0]?.id === rows.value[0]?.id) sorted.reverse()
    rows.value = sorted
  }

  if (options.watchSource) {
    watch(options.watchSource, () => refresh())
  }

  return {
    rows,
    pageRows,
    loading,
    activePage,
    totalPages,
    hasMore,
    loadPage,
    refresh,
    sortBy,
    onPageChange,
  }
}
