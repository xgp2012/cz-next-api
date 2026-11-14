<script setup lang="ts">
/**
 * 控制台：展示最近 7 天的请求数 / 额度 / Token 趋势。
 *
 * 数据源：GET /api/user/dashboard → model.SearchLogsByDayAndModel 的结果。
 * 注意该结构体（model.LogStatistic）没有 json tag，所以字段名是 Go 的
 * 导出名（Day / ModelName / RequestCount / Quota / PromptTokens /
 * CompletionTokens），不要写成 snake_case。
 *
 * 图表用内联 SVG 手绘，不引入图表库：只有 7 个点的折线，体积与维护成本
 * 都远低于 recharts/echarts，也避免了打包体积增长。
 */
import type { LogStatistic } from '~/utils/types'

const { t } = useI18n()
const { quotaConfig, isAdmin } = useUserStore()
const { showError } = useAppToast()

const loading = ref(false)
const statistics = ref<LogStatistic[]>([])

onMounted(async () => {
  loading.value = true
  try {
    const res = await api.get<LogStatistic[]>('/api/user/dashboard')
    statistics.value = res.data || []
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
})

/** 按日期聚合（同一天可能有多个模型） */
const dailyData = computed(() => {
  const byDay = new Map<
    string,
    { day: string, requests: number, quota: number, tokens: number }
  >()
  for (const row of statistics.value) {
    const day = row.Day || 'unknown'
    const entry = byDay.get(day) || { day, requests: 0, quota: 0, tokens: 0 }
    entry.requests += row.RequestCount || 0
    entry.quota += row.Quota || 0
    entry.tokens += (row.PromptTokens || 0) + (row.CompletionTokens || 0)
    byDay.set(day, entry)
  }
  return [...byDay.values()].sort((a, b) => a.day.localeCompare(b.day))
})

const totals = computed(() => ({
  requests: dailyData.value.reduce((sum, item) => sum + item.requests, 0),
  quota: dailyData.value.reduce((sum, item) => sum + item.quota, 0),
  tokens: dailyData.value.reduce((sum, item) => sum + item.tokens, 0),
}))

/** 每个模型各占多少额度，用于「模型消费占比」列表 */
const byModel = computed(() => {
  const map = new Map<string, { model: string, requests: number, quota: number, tokens: number }>()
  for (const row of statistics.value) {
    const model = row.ModelName || 'unknown'
    const entry = map.get(model) || { model, requests: 0, quota: 0, tokens: 0 }
    entry.requests += row.RequestCount || 0
    entry.quota += row.Quota || 0
    entry.tokens += (row.PromptTokens || 0) + (row.CompletionTokens || 0)
    map.set(model, entry)
  }
  return [...map.values()].sort((a, b) => b.quota - a.quota)
})

/**
 * 由数据点生成 SVG 折线路径。
 * viewBox 固定为 100x30，配合 width=100% 实现自适应，无需监听 resize。
 */
function buildPath(values: number[]): string {
  if (values.length === 0) return ''
  const max = Math.max(...values, 1)
  const stepX = values.length > 1 ? 100 / (values.length - 1) : 0
  return values
    .map((value, index) => {
      const x = values.length > 1 ? index * stepX : 50
      const y = 28 - (value / max) * 26
      return `${index === 0 ? 'M' : 'L'}${x.toFixed(2)},${y.toFixed(2)}`
    })
    .join(' ')
}

function buildArea(values: number[]): string {
  const line = buildPath(values)
  if (!line) return ''
  return `${line} L100,30 L0,30 Z`
}

interface ChartDef {
  key: string
  title: string
  tooltip: string
  color: string
  values: number[]
  format: (value: number) => string
}

const charts = computed<ChartDef[]>(() => [
  {
    key: 'requests',
    title: t('dashboard.charts.requests.title'),
    tooltip: t('dashboard.charts.requests.tooltip'),
    color: '#2185d0',
    values: dailyData.value.map((item) => item.requests),
    format: (value) => String(value),
  },
  {
    key: 'quota',
    title: t('dashboard.charts.quota.title'),
    tooltip: t('dashboard.charts.quota.tooltip'),
    color: '#21ba45',
    values: dailyData.value.map((item) => item.quota),
    format: (value) => renderQuota(value, quotaConfig.value),
  },
  {
    key: 'tokens',
    title: t('dashboard.charts.tokens.title'),
    tooltip: t('dashboard.charts.tokens.tooltip'),
    color: '#f2711c',
    values: dailyData.value.map((item) => item.tokens),
    format: (value) => String(renderNumber(value)),
  },
])
</script>

<template>
  <div class="space-y-4">
    <!-- 汇总卡片 -->
    <div class="grid gap-4 md:grid-cols-3">
      <UCard>
        <div class="text-sm text-gray-500">{{ t('dashboard.charts.requests.tooltip') }}（近 7 天）</div>
        <div class="mt-1 text-2xl font-semibold">{{ renderNumber(totals.requests) }}</div>
      </UCard>
      <UCard>
        <div class="text-sm text-gray-500">{{ t('dashboard.charts.quota.tooltip') }}（近 7 天）</div>
        <div class="mt-1 text-2xl font-semibold">{{ renderQuota(totals.quota, quotaConfig) }}</div>
      </UCard>
      <UCard>
        <div class="text-sm text-gray-500">{{ t('dashboard.charts.tokens.tooltip') }}（近 7 天）</div>
        <div class="mt-1 text-2xl font-semibold">{{ renderNumber(totals.tokens) }}</div>
      </UCard>
    </div>

    <!-- 趋势图 -->
    <div class="grid gap-4 lg:grid-cols-3">
      <UCard v-for="chart in charts" :key="chart.key">
        <template #header>
          <div class="text-sm font-semibold text-gray-700">{{ chart.title }}</div>
        </template>
        <div v-if="chart.values.length > 0">
          <svg viewBox="0 0 100 30" preserveAspectRatio="none" class="h-24 w-full">
            <path :d="buildArea(chart.values)" :fill="chart.color" opacity="0.12" />
            <path
              :d="buildPath(chart.values)"
              fill="none"
              :stroke="chart.color"
              stroke-width="1.2"
              vector-effect="non-scaling-stroke"
              stroke-linejoin="round"
              stroke-linecap="round"
            />
          </svg>
          <div class="mt-2 flex justify-between text-[0.7rem] text-gray-400">
            <span>{{ dailyData[0]?.day }}</span>
            <span>{{ dailyData[dailyData.length - 1]?.day }}</span>
          </div>
          <div class="mt-1 text-xs text-gray-500">
            峰值：{{ chart.format(Math.max(...chart.values)) }}
          </div>
        </div>
        <div v-else class="py-8 text-center text-sm text-gray-400">
          {{ loading ? t('common.loading') : t('common.none') }}
        </div>
      </UCard>
    </div>

    <!-- 按模型明细 -->
    <PageCard :title="t('dashboard.statistics.title')">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b border-gray-200 text-left text-gray-500">
            <tr>
              <th class="p-2">{{ t('log.table.model_name') }}</th>
              <th class="p-2">{{ t('dashboard.charts.requests.tooltip') }}</th>
              <th class="oa-hide-on-mobile p-2">Token</th>
              <th class="p-2">{{ t('dashboard.charts.quota.tooltip') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in byModel" :key="row.model" class="border-b border-gray-100 hover:bg-gray-50">
              <td class="p-2 font-medium">{{ row.model }}</td>
              <td class="p-2">{{ renderNumber(row.requests) }}</td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">{{ renderNumber(row.tokens) }}</td>
              <td class="p-2">{{ renderQuota(row.quota, quotaConfig) }}</td>
            </tr>
            <tr v-if="byModel.length === 0">
              <td colspan="4" class="p-8 text-center text-gray-400">
                {{ loading ? t('common.loading') : t('common.none') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </PageCard>
  </div>
</template>
