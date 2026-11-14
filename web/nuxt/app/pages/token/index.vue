<script setup lang="ts">
/**
 * 令牌管理页（列表）。
 *
 * 交互与原版保持一致：
 * - 列表分页（后端游标式分页，见 usePagedTable）
 * - 排序（按剩余/已用额度，后端 ?order= 参数）
 * - 搜索（/api/token/search?keyword=）
 * - 启用/禁用/删除单条，复制 key（支持直接复制或生成第三方客户端链接）
 */
import type { Token } from '~/utils/types'
import { ITEMS_PER_PAGE } from '~/constants/channel'

const { t } = useI18n()
const { quotaConfig, status } = useUserStore()
const { success, showError, warning } = useAppToast()

const orderBy = ref('')
const searchKeyword = ref('')
const searching = ref(false)
const searchMode = ref(false)

const orderOptions = computed(() => [
  { label: t('token.sort.placeholder'), value: '' },
  { label: t('token.sort.by_remain'), value: 'remain_quota' },
  { label: t('token.sort.by_used'), value: 'used_quota' },
])

const {
  rows: tokens,
  loading,
  activePage,
  totalPages,
  loadPage,
  refresh,
  onPageChange,
} = usePagedTable({
  fetcher: async (startIdx) => {
    const res = await api.get<Token[]>('/api/token/', {
      query: { p: startIdx, order: orderBy.value },
    })
    return res.data || []
  },
})

onMounted(() => loadPage(0))

watch(orderBy, () => {
  if (!searchMode.value) refresh()
})

async function searchTokens() {
  if (!searchKeyword.value) {
    searchMode.value = false
    await refresh()
    return
  }
  searching.value = true
  try {
    const res = await api.get<Token[]>('/api/token/search', { query: { keyword: searchKeyword.value } })
    tokens.value = res.data || []
    activePage.value = 1
    searchMode.value = true
  } catch (error) {
    showError(error)
  } finally {
    searching.value = false
  }
}

const confirmDeleteId = ref<number | null>(null)
const confirmDisableId = ref<number | null>(null)

async function deleteToken(token: Token) {
  try {
    await api.delete(`/api/token/${token.id}/`)
    token.deleted = true
    success(t('token.messages.delete_success'))
  } catch (error) {
    showError(error)
  } finally {
    confirmDeleteId.value = null
  }
}

async function updateTokenStatus(token: Token) {
  const nextStatus = token.status === 1 ? 2 : 1
  try {
    const res = await api.put<Token>('/api/token/?status_only=true', {
      id: token.id,
      status: nextStatus,
    })
    token.status = res.data?.status ?? nextStatus
    success(t('token.messages.update_success'))
  } catch (error) {
    showError(error)
  } finally {
    confirmDisableId.value = null
  }
}

/** 复制 key 的多种形态（与原版 copy_options 一致） */
const copyModes = computed(() => [
  { label: t('token.copy_options.raw'), value: 'raw' },
  { label: t('token.copy_options.next'), value: 'next' },
  { label: t('token.copy_options.ama'), value: 'ama' },
  { label: t('token.copy_options.opencat'), value: 'opencat' },
  { label: t('token.copy_options.lobe'), value: 'lobechat' },
])

function buildServerAddress() {
  return (status.value?.server_address || window.location.origin).replace(/\/$/, '')
}

async function copyTokenKey(key: string, mode = 'raw') {
  const token = `sk-${key}`
  if (mode === 'raw') {
    const ok = await copyText(token)
    if (ok) success(t('token.messages.copy_success'))
    else showError(t('token.messages.copy_failed'))
    return
  }
  const server = encodeURIComponent(buildServerAddress())
  const chatLink = status.value?.chat_link || ''
  const settings = JSON.stringify({ key: token, url: buildServerAddress() })

  let url = ''
  switch (mode) {
    case 'next':
      url = chatLink
        ? `${chatLink}/#/?settings=${settings}`
        : `https://chat.nextweb.fun/#/?settings=${settings}`
      break
    case 'ama':
      url = `ama://set-api-key?server=${server}&key=${token}`
      break
    case 'opencat':
      url = `opencat://team/join?server=${server}&token=${token}`
      break
    case 'lobechat':
      url = `https://chat-preview.lobehub.com/?settings=${JSON.stringify({
        keyVaults: { openai: { apiKey: token, baseURL: buildServerAddress() } },
      })}`
      break
  }
  const ok = await copyText(url)
  if (ok) success(t('token.messages.copy_success'))
  else showError(t('token.messages.copy_failed'))
}

function renderTokenStatus(token: Token) {
  switch (token.status) {
    case 1:
      return { color: 'success' as const, label: t('token.table.status_enabled') }
    case 2:
      return { color: 'error' as const, label: t('token.table.status_disabled') }
    case 3:
      return { color: 'warning' as const, label: t('token.table.status_expired') }
    case 4:
      return { color: 'neutral' as const, label: t('token.table.status_depleted') }
    default:
      return { color: 'neutral' as const, label: t('token.table.status_unknown') }
  }
}
</script>

<template>
  <div>
    <PageCard :title="t('token.title')">
      <template #actions>
        <UButton to="/token/add" icon="i-heroicons-plus" color="primary">
          {{ t('token.buttons.add') }}
        </UButton>
      </template>

      <div class="mb-4 flex flex-wrap items-center gap-2">
        <UDropdownMenu
          :items="copyModes.map(m => ({ label: m.label, onSelect: () => copyTokenKey(tokens[0]?.key || '', m.value) }))"
        >
          <UButton color="neutral" variant="outline" trailing-icon="i-heroicons-chevron-down">
            {{ t('token.buttons.copy_all') }}
          </UButton>
        </UDropdownMenu>

        <USelect v-model="orderBy" :items="orderOptions" class="w-40" />

        <UInput
          v-model="searchKeyword"
          icon="i-heroicons-magnifying-glass"
          :placeholder="t('token.buttons.search')"
          class="min-w-56 flex-1"
          @keyup.enter="searchTokens"
        />
        <UButton color="neutral" variant="outline" :loading="searching" @click="searchTokens">
          {{ t('common.search') }}
        </UButton>
      </div>

      <UAlert
        class="mb-4"
        color="info"
        variant="subtle"
        icon="i-heroicons-information-circle"
        :description="t('token.notice')"
      />

      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b border-gray-200 text-left text-gray-500">
            <tr>
              <th class="p-2">{{ t('token.table.name') }}</th>
              <th class="p-2">{{ t('token.table.status') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('token.table.quota') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('token.table.used_quota') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('token.table.created_time') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('token.table.accessed_time') }}</th>
              <th class="p-2 text-right">{{ t('token.table.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(token, idx) in tokens.slice((activePage - 1) * ITEMS_PER_PAGE, activePage * ITEMS_PER_PAGE)"
              :key="token.id"
              class="border-b border-gray-100 hover:bg-gray-50"
            >
              <td class="p-2 font-medium">{{ token.name }}</td>
              <td class="p-2">
                <StatusBadge :color="renderTokenStatus(token).color" :label="renderTokenStatus(token).label" />
              </td>
              <td class="oa-hide-on-mobile p-2">
                {{ token.unlimited_quota ? t('common.unlimited') : renderQuota(token.remain_quota, quotaConfig) }}
              </td>
              <td class="oa-hide-on-mobile p-2">
                {{ renderQuota(token.used_quota, quotaConfig) }}
              </td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">
                {{ timestamp2string(token.created_time) }}
              </td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">
                {{ timestamp2string(token.accessed_time) }}
              </td>
              <td class="p-2">
                <div class="flex flex-wrap justify-end gap-1">
                  <UButton
                    size="xs"
                    color="neutral"
                    variant="soft"
                    icon="i-heroicons-document-duplicate"
                    @click="copyTokenKey(token.key)"
                  >
                    {{ t('token.buttons.copy') }}
                  </UButton>
                  <UButton
                    size="xs"
                    color="neutral"
                    variant="soft"
                    :to="`/token/edit/${token.id}`"
                  >
                    {{ t('common.edit') }}
                  </UButton>
                  <UButton
                    size="xs"
                    color="error"
                    variant="soft"
                    @click="confirmDisableId = token.id"
                  >
                    {{ token.status === 1 ? t('token.buttons.disable') : t('token.buttons.enable') }}
                  </UButton>
                  <UButton
                    size="xs"
                    color="error"
                    variant="ghost"
                    @click="confirmDeleteId = token.id"
                  >
                    {{ t('token.buttons.delete') }}
                  </UButton>
                </div>

                <!-- 删除确认 -->
                <UModal
                  :open="confirmDeleteId === token.id"
                  :title="t('token.messages.confirm_delete')"
                  @update:open="(v) => (confirmDeleteId = v ? token.id : null)"
                >
                  <template #body>
                    <p class="text-sm text-gray-600">{{ t('token.messages.confirm_delete_content') }}</p>
                  </template>
                  <template #footer>
                    <div class="flex w-full justify-end gap-2">
                      <UButton color="neutral" variant="ghost" @click="confirmDeleteId = null">
                        {{ t('common.cancel') }}
                      </UButton>
                      <UButton color="error" @click="deleteToken(token)">
                        {{ t('common.confirm') }}
                      </UButton>
                    </div>
                  </template>
                </UModal>

                <!-- 启用/禁用确认 -->
                <UModal
                  :open="confirmDisableId === token.id"
                  :title="token.status === 1 ? t('token.buttons.disable') : t('token.buttons.enable')"
                  @update:open="(v) => (confirmDisableId = v ? token.id : null)"
                >
                  <template #body>
                    <p class="text-sm text-gray-600">
                      {{
                        token.status === 1
                          ? t('token.messages.confirm_disable_content')
                          : t('token.messages.confirm_enable_content')
                      }}
                    </p>
                  </template>
                  <template #footer>
                    <div class="flex w-full justify-end gap-2">
                      <UButton color="neutral" variant="ghost" @click="confirmDisableId = null">
                        {{ t('common.cancel') }}
                      </UButton>
                      <UButton color="error" @click="updateTokenStatus(token)">
                        {{ t('common.confirm') }}
                      </UButton>
                    </div>
                  </template>
                </UModal>
              </td>
            </tr>
            <tr v-if="tokens.length === 0">
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
