<script setup lang="ts">
/**
 * 兑换码管理（仅管理员）。
 *
 * 支持：
 * - 列表 / 搜索 / 分页
 * - 删除、启用、禁用（PUT /api/redemption/?status_only=true）
 * - 批量生成（POST /api/redemption/ 携带 count）
 * - 一键复制 / 导出为 txt
 */
import type { Redemption } from '~/utils/types'
import { ITEMS_PER_PAGE } from '~/constants/channel'

const { t } = useI18n()
const { quotaConfig } = useUserStore()
const { success, showError } = useAppToast()

const searchKeyword = ref('')
const searching = ref(false)
const searchMode = ref(false)
const showDeleted = ref(false)

const {
  rows: redemptions,
  loading,
  activePage,
  totalPages,
  loadPage,
  refresh,
  onPageChange,
} = usePagedTable({
  fetcher: async (startIdx) => {
    const res = await api.get<Redemption[]>('/api/redemption/', { query: { p: startIdx } })
    return res.data || []
  },
})

onMounted(() => loadPage(0))

async function searchRedemptions() {
  if (!searchKeyword.value) {
    searchMode.value = false
    await refresh()
    return
  }
  searching.value = true
  try {
    const res = await api.get<Redemption[]>('/api/redemption/search', {
      query: { keyword: searchKeyword.value },
    })
    redemptions.value = res.data || []
    activePage.value = 1
    searchMode.value = true
  } catch (error) {
    showError(error)
  } finally {
    searching.value = false
  }
}

const confirmDeleteId = ref<number | null>(null)

async function deleteRedemption(item: Redemption) {
  try {
    await api.delete(`/api/redemption/${item.id}/`)
    item.deleted = true
    success(t('redemption.messages.operation_success', '操作成功'))
  } catch (error) {
    showError(error)
  } finally {
    confirmDeleteId.value = null
  }
}

async function setStatus(item: Redemption, status: number) {
  try {
    const res = await api.put<Redemption>('/api/redemption/?status_only=true', { id: item.id, status })
    item.status = res.data?.status ?? status
    success(t('redemption.messages.operation_success', '操作成功'))
  } catch (error) {
    showError(error)
  }
}

function redemptionStatus(status: number) {
  switch (status) {
    case 1:
      return { color: 'success' as const, label: t('redemption.status.unused') }
    case 2:
      return { color: 'error' as const, label: t('redemption.status.disabled') }
    case 3:
      return { color: 'neutral' as const, label: t('redemption.status.used') }
    default:
      return { color: 'neutral' as const, label: t('redemption.status.unknown') }
  }
}

const visibleRows = computed(() => {
  const start = (activePage.value - 1) * ITEMS_PER_PAGE
  return redemptions.value
    .slice(start, start + ITEMS_PER_PAGE)
    .filter((item) => showDeleted.value || !item.deleted)
})

async function copyAll() {
  const codes = visibleRows.value.map((item) => item.key).join('\n')
  if (!codes) return
  const ok = await copyText(codes)
  if (ok) success('兑换码已复制到剪贴板')
  else showError('复制失败，请检查浏览器剪贴板权限')
}

function exportTxt() {
  const codes = visibleRows.value.map((item) => item.key).join('\n')
  downloadTextAsFile(codes, 'redemptions.txt')
}
</script>

<template>
  <div>
    <PageCard :title="t('redemption.title')">
      <template #actions>
        <UButton to="/redemption/add" icon="i-heroicons-plus" color="primary">
          {{ t('redemption.buttons.add') }}
        </UButton>
      </template>

      <div class="mb-4 flex flex-wrap items-center gap-2">
        <UInput
          v-model="searchKeyword"
          icon="i-heroicons-magnifying-glass"
          :placeholder="t('redemption.buttons.search')"
          class="min-w-56 flex-1"
          @keyup.enter="searchRedemptions"
        />
        <UButton color="neutral" variant="outline" :loading="searching" @click="searchRedemptions">
          {{ t('common.search') }}
        </UButton>
        <UButton color="neutral" variant="outline" icon="i-heroicons-document-duplicate" @click="copyAll">
          {{ t('redemption.buttons.copy') }}
        </UButton>
        <UButton color="neutral" variant="outline" icon="i-heroicons-arrow-down-tray" @click="exportTxt">
          {{ t('redemption.buttons.export') }}
        </UButton>
        <UCheckbox v-model="showDeleted" label="显示已删除" />
      </div>

      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b border-gray-200 text-left text-gray-500">
            <tr>
              <th class="p-2">ID</th>
              <th class="p-2">{{ t('redemption.table.name') }}</th>
              <th class="p-2">{{ t('redemption.table.status') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('redemption.table.quota') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('redemption.table.created_time') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('redemption.table.redeemed_time') }}</th>
              <th class="p-2 text-right">{{ t('redemption.table.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="item in visibleRows"
              :key="item.id"
              class="border-b border-gray-100 hover:bg-gray-50"
              :class="{ 'opacity-50': item.deleted }"
            >
              <td class="p-2 text-gray-500">{{ item.id }}</td>
              <td class="p-2">
                <div class="font-medium">{{ item.name || t('redemption.table.none') }}</div>
                <code class="text-xs text-gray-400">{{ item.key }}</code>
              </td>
              <td class="p-2">
                <StatusBadge :color="redemptionStatus(item.status).color" :label="redemptionStatus(item.status).label" />
              </td>
              <td class="oa-hide-on-mobile p-2">{{ renderQuota(item.quota, quotaConfig) }}</td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">{{ timestamp2string(item.created_time) }}</td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">{{ timestamp2string(item.redeemed_time) }}</td>
              <td class="p-2">
                <div class="flex flex-wrap justify-end gap-1">
                  <UButton size="xs" color="neutral" variant="soft" @click="copyText(item.key)">
                    {{ t('redemption.buttons.copy_key') }}
                  </UButton>
                  <UButton
                    size="xs"
                    color="neutral"
                    variant="soft"
                    :to="`/redemption/edit/${item.id}`"
                    :disabled="item.deleted"
                  >
                    {{ t('common.edit') }}
                  </UButton>
                  <UButton
                    size="xs"
                    :color="item.status === 2 ? 'success' : 'warning'"
                    variant="soft"
                    :disabled="item.deleted"
                    @click="setStatus(item, item.status === 2 ? 1 : 2)"
                  >
                    {{ item.status === 2 ? t('redemption.buttons.enable') : t('redemption.buttons.disable') }}
                  </UButton>
                  <UButton
                    size="xs"
                    color="error"
                    variant="ghost"
                    :disabled="item.deleted"
                    @click="confirmDeleteId = item.id"
                  >
                    {{ t('common.delete') }}
                  </UButton>
                </div>

                <UModal
                  :open="confirmDeleteId === item.id"
                  :title="t('redemption.buttons.delete')"
                  @update:open="(v) => (confirmDeleteId = v ? item.id : null)"
                >
                  <template #body>
                    <p class="text-sm text-gray-600">
                      确定要删除兑换码 <code>{{ item.key }}</code> 吗？该操作不可恢复。
                    </p>
                  </template>
                  <template #footer>
                    <div class="flex w-full justify-end gap-2">
                      <UButton color="neutral" variant="ghost" @click="confirmDeleteId = null">
                        {{ t('common.cancel') }}
                      </UButton>
                      <UButton color="error" @click="deleteRedemption(item)">{{ t('common.confirm') }}</UButton>
                    </div>
                  </template>
                </UModal>
              </td>
            </tr>
            <tr v-if="visibleRows.length === 0">
              <td colspan="7" class="p-8 text-center text-gray-400">
                {{ loading ? t('common.loading') : t('common.none') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="mt-4 flex flex-wrap items-center justify-between gap-2">
        <UButton color="neutral" variant="outline" size="sm" :loading="loading" @click="refresh">
          {{ t('common.refresh') }}
        </UButton>
        <UPagination
          :page="activePage"
          :total="totalPages * ITEMS_PER_PAGE"
          :items-per-page="ITEMS_PER_PAGE"
          size="sm"
          @update:page="onPageChange"
        />
      </div>
    </PageCard>
  </div>
</template>
