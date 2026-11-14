<script setup lang="ts">
/**
 * 渠道管理页。
 *
 * 关键交互（与旧版行为对齐）：
 * - 列表分页与搜索（/api/channel/、/api/channel/search）
 * - 单测：GET /api/channel/test/:id?model=xxx
 * - 批量测试：GET /api/channel/test?scope=all|disabled（异步，需刷新看结果）
 * - 余额刷新：/api/channel/update_balance 与 /update_balance/:id
 * - 优先级可直接在表格里改（PUT /api/channel/），这是运维最高频的操作
 * - 删除禁用渠道：DELETE /api/channel/disabled
 */
import type { Channel, SelectOption } from '~/utils/types'
import { CHANNEL_OPTIONS, getChannelOption, ITEMS_PER_PAGE } from '~/constants/channel'

const { t } = useI18n()
const { isAdmin } = useUserStore()
const { success, showError, info } = useAppToast()

const searchKeyword = ref('')
const searching = ref(false)
const searchMode = ref(false)
const showDetail = ref(false)
const updatingBalance = ref(false)
const balanceUpdatingId = ref<number | null>(null)
const testingId = ref<number | null>(null)

// ---------------------------------------------------------------------------
// 数据加载
// ---------------------------------------------------------------------------
function processChannel(channel: Channel): Channel {
  if (!channel.models) {
    channel.models = ''
    channel.test_model = ''
    channel.model_options = []
    return channel
  }
  const models = String(channel.models).split(',').filter(Boolean)
  channel.test_model = models[0] || ''
  channel.model_options = models.map<SelectOption>((m) => ({ key: m, text: m, value: m }))
  return channel
}

const {
  rows: channels,
  loading,
  activePage,
  totalPages,
  loadPage,
  refresh,
  onPageChange,
} = usePagedTable({
  fetcher: async (startIdx) => {
    const res = await api.get<Channel[]>('/api/channel/', { query: { p: startIdx } })
    return (res.data || []).map(processChannel)
  },
})

async function loadChannelModels() {
  // 渠道模型列表（type -> models），用于「新建渠道」时预填
  try {
    const res = await api.get<Record<string, string[]>>('/api/models')
    writeChannelModelsCache(res.data || {})
  } catch {
    // 非管理员或后端未就绪时忽略
  }
}

onMounted(async () => {
  await loadChannelModels()
  await loadPage(0)
})

async function searchChannels() {
  if (!searchKeyword.value) {
    searchMode.value = false
    await refresh()
    return
  }
  searching.value = true
  try {
    const res = await api.get<Channel[]>('/api/channel/search', { query: { keyword: searchKeyword.value } })
    channels.value = (res.data || []).map(processChannel)
    activePage.value = 1
    searchMode.value = true
  } catch (error) {
    showError(error)
  } finally {
    searching.value = false
  }
}

// ---------------------------------------------------------------------------
// 单条操作
// ---------------------------------------------------------------------------
const confirmDeleteId = ref<number | null>(null)

async function deleteChannel(channel: Channel) {
  try {
    await api.delete(`/api/channel/${channel.id}/`)
    channel.deleted = true
    success(t('channel.messages.operation_success'))
  } catch (error) {
    showError(error)
  } finally {
    confirmDeleteId.value = null
  }
}

async function updateChannel(channel: Channel) {
  try {
    await api.put('/api/channel/', channel)
    success(t('channel.messages.operation_success'))
  } catch (error) {
    showError(error)
  }
}

async function testChannel(channel: Channel) {
  const model = channel.test_model
  if (!model) {
    showError(t('channel.table.select_test_model'))
    return
  }
  testingId.value = channel.id
  const start = Date.now()
  try {
    const res = await api.get<string>(`/api/channel/test/${channel.id}`, { query: { model } })
    const elapsed = ((Date.now() - start) / 1000).toFixed(2)
    channel.response_time = Number(elapsed) * 1000
    channel.test_time = Math.floor(Date.now() / 1000)
    success(
      t('channel.messages.test_success', {
        name: channel.name,
        model,
        time: elapsed,
        message: res.data || '',
      }),
    )
  } catch (error) {
    showError(error)
  } finally {
    testingId.value = null
  }
}

async function updateChannelBalance(channel: Channel) {
  balanceUpdatingId.value = channel.id
  try {
    const res = await api.get<number>(`/api/channel/update_balance/${channel.id}/`)
    channel.balance = res.data ?? 0
    success(t('channel.messages.balance_update_success', { name: channel.name }))
  } catch (error) {
    showError(error)
  } finally {
    balanceUpdatingId.value = null
  }
}

// ---------------------------------------------------------------------------
// 批量操作
// ---------------------------------------------------------------------------
async function testAllChannels(scope: 'all' | 'disabled' = 'all') {
  try {
    await api.get('/api/channel/test', { query: { scope } })
    info(t('channel.messages.test_all_started'))
  } catch (error) {
    showError(error)
  }
}

async function updateAllBalances() {
  updatingBalance.value = true
  try {
    await api.get('/api/channel/update_balance')
    info(t('channel.messages.all_balance_updated'))
    await refresh()
  } catch (error) {
    showError(error)
  } finally {
    updatingBalance.value = false
  }
}

const confirmDeleteDisabled = ref(false)

async function deleteDisabledChannels() {
  try {
    const res = await api.delete<number>('/api/channel/disabled')
    success(t('channel.messages.delete_disabled_success', { count: res.data ?? 0 }))
    confirmDeleteDisabled.value = false
    await refresh()
  } catch (error) {
    showError(error)
  }
}

// ---------------------------------------------------------------------------
// 展示辅助
// ---------------------------------------------------------------------------
const typeOptions = computed<SelectOption[]>(() =>
  CHANNEL_OPTIONS.map((option) => ({
    key: option.value as number,
    value: option.value as number,
    text: String(option.text),
    label: option.description ? `${option.text} (${option.description})` : String(option.text),
  })),
)

function channelTypeLabel(type: number): string {
  const option = getChannelOption(type)
  return option ? String(option.text) : String(type)
}

function channelTypeColor(type: number) {
  const option = getChannelOption(type)
  return (option?.color as any) || 'neutral'
}

function channelStatus(channel: Channel) {
  switch (channel.status) {
    case 1:
      return { color: 'success' as const, label: t('channel.table.status_enabled'), tip: '' }
    case 2:
      return { color: 'error' as const, label: t('channel.table.status_disabled'), tip: t('channel.table.status_disabled_tip') }
    case 3:
      return {
        color: 'warning' as const,
        label: t('channel.table.status_auto_disabled'),
        tip: t('channel.table.status_auto_disabled_tip'),
      }
    default:
      return { color: 'neutral' as const, label: t('channel.table.status_unknown'), tip: '' }
  }
}

/** 余额展示：不同上游币种/量纲不同，保持与后端 controller/channel-billing.go 一致 */
function renderBalance(channel: Channel): string {
  const { type, balance } = channel
  const notSupported = t('channel.table.balance_not_supported')
  const currency = (symbol: string, value: number) => `${symbol}${value.toFixed(2)}`
  switch (type) {
    case 1: // OpenAI：官方已不支持通过 key 查余额
      return balance === 0 ? t('channel.table.balance_not_supported') : currency('$', balance)
    case 4:
    case 12:
    case 36:
    case 44:
      return currency('¥', balance)
    case 8:
    case 20:
      return currency('$', balance)
    case 5:
      return currency('¥', balance / 10000)
    case 10:
    case 13:
      return String(renderNumber(balance))
    default:
      return notSupported
  }
}

function renderGroups(channel: Channel) {
  const groups = (channel.group || '').split(',').filter(Boolean)
  if (groups.length === 0) return ['default']
  return [...groups].sort()
}

function groupColor(group: string) {
  if (group === 'vip' || group === 'pro') return 'warning'
  if (group === 'svip' || group === 'premium') return 'error'
  return 'neutral'
}

/** 表格当前页切片 */
const pageChannels = computed(() => {
  const start = (activePage.value - 1) * ITEMS_PER_PAGE
  return channels.value.slice(start, start + ITEMS_PER_PAGE).filter((channel) => !channel.deleted)
})
</script>

<template>
  <div>
    <PageCard :title="t('channel.title')">
      <template #actions>
        <UButton to="/channel/add" icon="i-heroicons-plus" color="primary">
          {{ t('channel.buttons.add') }}
        </UButton>
      </template>

      <!-- 工具栏 -->
      <div class="mb-4 flex flex-wrap items-center gap-2">
        <UInput
          v-model="searchKeyword"
          icon="i-heroicons-magnifying-glass"
          :placeholder="t('channel.search')"
          class="min-w-64 flex-1"
          @keyup.enter="searchChannels"
        />
        <UButton color="neutral" variant="outline" :loading="searching" @click="searchChannels">
          {{ t('common.search') }}
        </UButton>
        <UButton color="neutral" variant="outline" icon="i-heroicons-map" @click="testAllChannels('all')">
          {{ t('channel.buttons.test_all') }}
        </UButton>
        <UButton color="neutral" variant="outline" @click="testAllChannels('disabled')">
          {{ t('channel.buttons.test_disabled') }}
        </UButton>
        <UButton
          color="neutral"
          variant="outline"
          icon="i-heroicons-banknotes"
          :loading="updatingBalance"
          @click="updateAllBalances"
        >
          更新余额
        </UButton>
        <UButton color="error" variant="soft" @click="confirmDeleteDisabled = true">
          {{ t('channel.buttons.delete_disabled') }}
        </UButton>
        <UButton color="neutral" variant="ghost" @click="showDetail = !showDetail">
          {{ showDetail ? t('channel.buttons.hide_detail') : t('channel.buttons.show_detail') }}
        </UButton>
      </div>

      <UAlert
        class="mb-3"
        color="info"
        variant="subtle"
        icon="i-heroicons-information-circle"
        :description="t('channel.test_notice')"
      />
      <UAlert
        v-if="showDetail"
        class="mb-3"
        color="neutral"
        variant="subtle"
        icon="i-heroicons-information-circle"
        :description="t('channel.detail_notice')"
      />

      <!-- 表格 -->
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b border-gray-200 text-left text-gray-500">
            <tr>
              <th class="p-2">ID</th>
              <th class="p-2">{{ t('channel.table.name') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('channel.table.group') }}</th>
              <th class="p-2">{{ t('channel.table.type') }}</th>
              <th class="p-2">{{ t('channel.table.status') }}</th>
              <th v-if="showDetail" class="p-2">{{ t('channel.table.balance') }}</th>
              <th v-if="showDetail" class="oa-hide-on-mobile p-2">{{ t('channel.table.response_time') }}</th>
              <th class="p-2" :title="t('channel.table.priority_tip')">
                {{ t('channel.table.priority') }}
              </th>
              <th v-if="showDetail" class="oa-hide-on-mobile p-2">
                {{ t('channel.table.test_model') }}
              </th>
              <th class="p-2 text-right">{{ t('channel.table.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="channel in pageChannels"
              :key="channel.id"
              class="border-b border-gray-100 align-middle hover:bg-gray-50"
            >
              <td class="p-2 text-gray-500">{{ channel.id }}</td>
              <td class="p-2 font-medium">
                {{ channel.name || t('channel.table.no_name') }}
              </td>
              <td class="oa-hide-on-mobile p-2">
                <div class="flex flex-wrap gap-1">
                  <UBadge
                    v-for="group in renderGroups(channel)"
                    :key="group"
                    :color="groupColor(group) as any"
                    variant="subtle"
                    size="sm"
                  >
                    {{ group }}
                  </UBadge>
                </div>
              </td>
              <td class="p-2">
                <UBadge :color="channelTypeColor(channel.type)" variant="subtle" size="sm">
                  {{ channelTypeLabel(channel.type) }}
                </UBadge>
              </td>
              <td class="p-2">
                <UTooltip :text="channelStatus(channel).tip || channelStatus(channel).label">
                  <StatusBadge :color="channelStatus(channel).color" :label="channelStatus(channel).label" />
                </UTooltip>
              </td>
              <td v-if="showDetail" class="p-2">
                <UButton
                  size="xs"
                  color="neutral"
                  variant="ghost"
                  :loading="balanceUpdatingId === channel.id"
                  :title="t('channel.table.click_to_update')"
                  @click="updateChannelBalance(channel)"
                >
                  {{ renderBalance(channel) }}
                </UButton>
              </td>
              <td v-if="showDetail" class="oa-hide-on-mobile p-2 text-gray-500">
                {{ channel.response_time > 0 ? `${channel.response_time} ms` : t('channel.table.not_tested') }}
              </td>
              <td class="p-2">
                <UInputNumber
                  v-model="channel.priority"
                  :min="-1000"
                  :max="1000"
                  size="sm"
                  class="w-20"
                  @blur="updateChannel(channel)"
                />
              </td>
              <td v-if="showDetail" class="oa-hide-on-mobile p-2">
                <USelect
                  v-model="channel.test_model"
                  :items="channel.model_options || []"
                  size="sm"
                  class="w-40"
                  :placeholder="t('channel.table.select_test_model')"
                />
              </td>
              <td class="p-2">
                <div class="flex flex-wrap justify-end gap-1">
                  <UButton
                    size="xs"
                    color="primary"
                    variant="soft"
                    :loading="testingId === channel.id"
                    @click="testChannel(channel)"
                  >
                    {{ t('channel.buttons.test') }}
                  </UButton>
                  <UButton
                    size="xs"
                    color="neutral"
                    variant="soft"
                    :to="`/channel/edit/${channel.id}`"
                  >
                    {{ t('channel.buttons.edit') }}
                  </UButton>
                  <UButton
                    size="xs"
                    :color="channel.status === 1 ? 'error' : 'success'"
                    variant="soft"
                    @click="() => { channel.status = channel.status === 1 ? 2 : 1; updateChannel(channel) }"
                  >
                    {{ channel.status === 1 ? t('channel.buttons.disable') : t('channel.buttons.enable') }}
                  </UButton>
                  <UButton size="xs" color="error" variant="ghost" @click="confirmDeleteId = channel.id">
                    {{ t('channel.buttons.delete') }}
                  </UButton>
                </div>

                <UModal
                  :open="confirmDeleteId === channel.id"
                  :title="t('channel.buttons.confirm_delete')"
                  @update:open="(v) => (confirmDeleteId = v ? channel.id : null)"
                >
                  <template #body>
                    <p class="text-sm text-gray-600">
                      确定要删除渠道「{{ channel.name || channel.id }}」吗？该操作不可恢复。
                    </p>
                  </template>
                  <template #footer>
                    <div class="flex w-full justify-end gap-2">
                      <UButton color="neutral" variant="ghost" @click="confirmDeleteId = null">
                        {{ t('common.cancel') }}
                      </UButton>
                      <UButton color="error" @click="deleteChannel(channel)">
                        {{ t('common.confirm') }}
                      </UButton>
                    </div>
                  </template>
                </UModal>
              </td>
            </tr>
            <tr v-if="pageChannels.length === 0">
              <td :colspan="showDetail ? 10 : 7" class="p-8 text-center text-gray-400">
                {{ loading ? t('common.loading') : t('common.none') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="mt-4 flex flex-wrap items-center justify-between gap-2">
        <UButton color="neutral" variant="outline" size="sm" :loading="loading" @click="refresh">
          {{ t('channel.buttons.refresh') }}
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

    <!-- 删除禁用渠道确认 -->
    <UModal v-model:open="confirmDeleteDisabled" :title="t('channel.buttons.delete_disabled')">
      <template #body>
        <p class="text-sm text-gray-600">
          将删除所有处于禁用状态（手动禁用或自动禁用）的渠道，该操作不可恢复。
        </p>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="confirmDeleteDisabled = false">
            {{ t('common.cancel') }}
          </UButton>
          <UButton color="error" @click="deleteDisabledChannels">
            {{ t('channel.buttons.confirm_delete_disabled') }}
          </UButton>
        </div>
      </template>
    </UModal>
  </div>
</template>
