<script setup lang="ts">
/**
 * 操作日志（管理员）。
 *
 * 后端支持按类型 / 时间范围 / 渠道 / 用户名 / 令牌名 / 模型名 过滤，
 * 参数为空字符串表示不过滤。分页同样是游标式（?p=N）。
 */
import type { Log, SelectOption } from '~/utils/types'
import { ITEMS_PER_PAGE } from '~/constants/channel'

const { t } = useI18n()
const { quotaConfig, isAdmin } = useUserStore()
const { showError, success } = useAppToast()

const typeOptions = computed<SelectOption[]>(() => [
  { text: t('log.type.all'), value: 0, key: 0 },
  { text: t('log.type.topup'), value: 1, key: 1 },
  { text: t('log.type.usage'), value: 2, key: 2 },
  { text: t('log.type.admin'), value: 3, key: 3 },
  { text: t('log.type.system'), value: 4, key: 4 },
  { text: t('log.type.test'), value: 5, key: 5 },
])

const filters = reactive({
  type: 0,
  startTime: '',
  endTime: '',
  channelId: '',
  username: '',
  tokenName: '',
  modelName: '',
  group: '',
})

const logs = ref<Log[]>([])
const loading = ref(false)
const activePage = ref(1)
const totalPages = ref(1)
const expandedId = ref<number | null>(null)


function buildQuery(startIdx: number) {
  const query: Record<string, any> = { p: startIdx }
  // 日志接口用「空字符串 = 不过滤」的约定
  query.type = filters.type || ''
  query.username = filters.username
  query.token_name = filters.tokenName
  query.model_name = filters.modelName
  query.channel = filters.channelId
  query.start_timestamp = filters.startTime ? string2timestamp(filters.startTime) : ''
  query.end_timestamp = filters.endTime ? string2timestamp(filters.endTime) : ''
  query.group = filters.group
  return query
}

async function loadPage(startIdx: number) {
  loading.value = true
  try {
    const res = await api.get<Log[]>('/api/log/', { query: buildQuery(startIdx) })
    logs.value = res.data || []
    activePage.value = startIdx + 1
    totalPages.value = logs.value.length === ITEMS_PER_PAGE ? startIdx + 2 : startIdx + 1
    expandedId.value = null
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

onMounted(() => loadPage(0))

/** 有类型过滤时改用 /api/log/search，这是后端为「搜索」单独提供的入口 */
async function query() {
  loading.value = true
  try {
    const res = await api.get<Log[]>('/api/log/search', { query: buildQuery(0) })
    logs.value = res.data || []
    activePage.value = 1
    totalPages.value = 1
    expandedId.value = null
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

async function cleanLogs() {
  if (!filters.startTime || !filters.endTime) {
    showError('请先选择清理的起始与结束时间')
    return
  }
  try {
    const res = await api.delete<number>('/api/log/', {
      query: {
        start_timestamp: string2timestamp(filters.startTime),
        end_timestamp: string2timestamp(filters.endTime),
      },
    })
    success(t('setting.operation.log.cleaned', { count: res.data ?? 0 }))
    await loadPage(0)
  } catch (error) {
    showError(error)
  }
}

function logTypeLabel(type: number) {
  const found = typeOptions.value.find((option) => option.value === type)
  return found ? String(found.text) : t('log.type_name.unknown')
}

function logTypeColor(type: number) {
  switch (type) {
    case 1:
      return 'success' as const
    case 2:
      return 'primary' as const
    case 3:
      return 'warning' as const
    case 4:
      return 'neutral' as const
    case 5:
      return 'info' as const
    default:
      return 'neutral' as const
  }
}
</script>

<template>
  <div>
    <PageCard :title="t('log.title')">
      <!-- 过滤条件 -->
      <div class="mb-4 grid gap-3 md:grid-cols-3 lg:grid-cols-4">
        <UFormField :label="t('log.type.select')">
          <USelect v-model="filters.type" :items="typeOptions" value-key="value" class="w-full" />
        </UFormField>
        <UFormField :label="t('log.table.start_time')">
          <UInput v-model="filters.startTime" type="datetime-local" class="w-full" />
        </UFormField>
        <UFormField :label="t('log.table.end_time')">
          <UInput v-model="filters.endTime" type="datetime-local" class="w-full" />
        </UFormField>
        <UFormField :label="t('log.table.channel_id')">
          <UInput v-model="filters.channelId" :placeholder="t('log.table.channel_id_placeholder')" class="w-full" />
        </UFormField>
        <UFormField v-if="isAdmin" :label="t('log.table.username')">
          <UInput v-model="filters.username" :placeholder="t('log.table.username_placeholder')" class="w-full" />
        </UFormField>
        <UFormField :label="t('log.table.token_name')">
          <UInput v-model="filters.tokenName" :placeholder="t('log.table.token_name_placeholder')" class="w-full" />
        </UFormField>
        <UFormField :label="t('log.table.model_name')">
          <UInput v-model="filters.modelName" :placeholder="t('log.table.model_name_placeholder')" class="w-full" />
        </UFormField>
      </div>

      <div class="mb-4 flex flex-wrap items-center gap-2">
        <UButton color="primary" icon="i-heroicons-magnifying-glass" :loading="loading" @click="query">
          {{ t('log.buttons.submit') }}
        </UButton>
        <UButton color="neutral" variant="outline" :loading="loading" @click="loadPage(0)">
          {{ t('log.buttons.refresh') }}
        </UButton>
        <UButton v-if="isAdmin" color="error" variant="soft" @click="cleanLogs">
          清理所选时间范围内的日志
        </UButton>
      </div>

      <!-- 日志表 -->
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b border-gray-200 text-left text-gray-500">
            <tr>
              <th class="p-2">{{ t('log.table.time') }}</th>
              <th class="p-2">{{ t('log.table.type') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('log.table.channel') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('log.table.username') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('log.table.token_name') }}</th>
              <th class="p-2">{{ t('log.table.model') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('log.table.prompt_tokens') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('log.table.completion_tokens') }}</th>
              <th class="p-2">{{ t('log.table.quota') }}</th>
              <th class="p-2">{{ t('log.table.detail') }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="item in logs" :key="item.id">
              <tr class="border-b border-gray-100 hover:bg-gray-50">
                <td class="whitespace-nowrap p-2 text-gray-500">{{ timestamp2string(item.created_at) }}</td>
                <td class="p-2">
                  <UBadge :color="logTypeColor(item.type)" variant="subtle" size="sm">
                    {{ logTypeLabel(item.type) }}
                  </UBadge>
                </td>
                <td class="oa-hide-on-mobile p-2">
                  <UBadge v-if="item.channel > 0" color="neutral" variant="subtle" size="sm">
                    {{ item.channel }}
                  </UBadge>
                  <span v-else class="text-gray-400">-</span>
                </td>
                <td class="oa-hide-on-mobile p-2 text-gray-500">{{ item.username || '-' }}</td>
                <td class="oa-hide-on-mobile p-2 text-gray-500">{{ item.token_name || '-' }}</td>
                <td class="p-2">{{ item.model_name || '-' }}</td>
                <td class="oa-hide-on-mobile p-2 text-gray-500">{{ item.prompt_tokens || '-' }}</td>
                <td class="oa-hide-on-mobile p-2 text-gray-500">{{ item.completion_tokens || '-' }}</td>
                <td class="p-2 whitespace-nowrap">{{ renderQuota(item.quota, quotaConfig) }}</td>
                <td class="p-2">
                  <UButton
                    size="xs"
                    color="neutral"
                    variant="ghost"
                    :icon="expandedId === item.id ? 'i-heroicons-chevron-up' : 'i-heroicons-chevron-down'"
                    @click="expandedId = expandedId === item.id ? null : item.id"
                  >
                    {{ t('log.click_to_view') }}
                  </UButton>
                </td>
              </tr>
              <tr v-if="expandedId === item.id" class="bg-gray-50">
                <td colspan="10" class="p-3">
                  <div class="space-y-1 text-xs text-gray-600">
                    <div><strong>内容：</strong>{{ item.content || '-' }}</div>
                    <div v-if="item.other" class="break-all"><strong>其他：</strong>{{ item.other }}</div>
                    <div v-if="item.request_id" class="flex items-center gap-2">
                      <strong>请求 ID：</strong>
                      <code>{{ item.request_id }}</code>
                      <UButton
                        size="xs"
                        color="neutral"
                        variant="ghost"
                        icon="i-heroicons-clipboard"
                        @click="copyText(item.request_id!)"
                      />
                    </div>
                  </div>
                </td>
              </tr>
            </template>
            <tr v-if="logs.length === 0">
              <td colspan="10" class="p-8 text-center text-gray-400">
                {{ loading ? t('common.loading') : t('common.none') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="mt-4 flex justify-end">
        <UPagination
          :page="activePage"
          :total="totalPages * ITEMS_PER_PAGE"
          :items-per-page="ITEMS_PER_PAGE"
          size="sm"
          @update:page="(page) => loadPage(page - 1)"
        />
      </div>
    </PageCard>
  </div>
</template>
