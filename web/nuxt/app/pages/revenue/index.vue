<script setup lang="ts">
/**
 * 营收看板（仅管理员）。
 *
 * 口径（来自 model/revenue.go，这是最容易看错的地方）：
 *   - **收入**来自 top_ups 表中 status=success 的订单，按 complete_time 归日；
 *     订阅订单的 Quota 为 0（它是「买来的额度」而非「入账额度」），
 *     所以收入统一按实付金额折算，而不是按 quota 累加。
 *   - **消耗**来自 logs 表中 type=consume 的行，按 created_at 归日。
 *   - **毛利** =（收入 − 消耗）/ 收入，是账面口径：用户充了钱没消费的部分
 *     仍算已确认收入，新站点毛利会偏高，必须结合「未消耗余额」一起看。
 *
 * 为什么又手绘 SVG：控制台已经用内联 SVG 画了 7 点折线（零图表库依赖），
 * 这里延续同一套做法——两条序列、hover 显示 title，需求就这么多，
 * 引一个 echarts 会让静态产物多出几百 KB。
 */
import type { RevenueDaily, RevenueModelRow, RevenueStats, RevenueUserRow } from '~/utils/types'

const { t } = useI18n()
const { isAdmin, quotaConfig } = useUserStore()
const { showError } = useAppToast()

const loading = ref(false)
const days = ref(7)
const stats = ref<RevenueStats | null>(null)

const dayOptions = [
  { label: '近 7 天', value: 7 },
  { label: '近 14 天', value: 14 },
  { label: '近 30 天', value: 30 },
  { label: '近 90 天', value: 90 },
]

async function loadStats() {
  loading.value = true
  try {
    const res = await api.get<RevenueStats>('/api/admin/revenue', { query: { days: days.value } })
    stats.value = res.data || null
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (!isAdmin.value) return
  loadStats()
})

watch(days, () => {
  if (!isAdmin.value) return
  loadStats()
})

// ---------------------------------------------------------------------------
// KPI
// ---------------------------------------------------------------------------

/**
 * 收入换算成额度。
 *
 * 后端统一返回 `total_income_cents`（单位：分），这里按 QuotaPerUnit 折算成额度，
 * 以便和 `total_consumed_quota`（消耗，单位：额度）同口径比较与绘制折线图。
 */
const totalIncomeQuota = computed(() => {
  const data = stats.value
  if (!data) return 0
  const cents = Number(data.total_income_cents ?? 0)
  if (cents <= 0) return 0
  const perUnit = Number(quotaConfig.value.quotaPerUnit || 500000) || 500000
  return (cents / 100) * perUnit
})

const totalConsumedQuota = computed(() => Number(stats.value?.total_consumed_quota ?? 0))

/** 毛利率：后端已算好 gross_margin，缺失时按同一定义兜底 */
const grossMargin = computed(() => {
  if (typeof stats.value?.gross_margin === 'number') return stats.value.gross_margin
  if (totalIncomeQuota.value <= 0) return 0
  return (totalIncomeQuota.value - totalConsumedQuota.value) / totalIncomeQuota.value
})

/** 人均收入（额度）：后端的 arpu_cents 是分，这里折算 */
const arpu = computed(() => {
  const cents = Number(stats.value?.arpu_cents ?? 0)
  if (cents <= 0) return 0
  const perUnit = Number(quotaConfig.value.quotaPerUnit || 500000) || 500000
  return (cents / 100) * perUnit
})

const renewRate = computed(() => {
  if (typeof stats.value?.renew_rate === 'number') return stats.value.renew_rate
  const active = Number(stats.value?.active_subscriptions ?? 0)
  const auto = Number(stats.value?.auto_renew_subscriptions ?? 0)
  if (active <= 0) return 0
  return auto / active
})

const daily = computed<RevenueDaily[]>(() => stats.value?.daily || [])
const topModels = computed<RevenueModelRow[]>(() => stats.value?.top_models || [])
const topUsers = computed<RevenueUserRow[]>(() => stats.value?.top_users || [])

/** 面板上的「未消耗余额」：收入减消耗，是理解毛利虚高的关键数字 */
const unconsumedQuota = computed(() => totalIncomeQuota.value - totalConsumedQuota.value)

/**
 * 把「分」换算成「额度」。
 *
 * 为什么需要：后端 daily 行的收入字段是 `income_cents`（分），
 * 而消耗字段 `consumed_quota` 是额度。折线图要让两条线可比高低，
 * 必须先统一到同一口径，否则收入会被画成一条贴地的线。
 */
const quotaPerUnitValue = computed(() => Number(quotaConfig.value.quotaPerUnit || 500000) || 500000)
function centsToQuota(cents: number): number {
  if (!cents) return 0
  return (cents / 100) * quotaPerUnitValue.value
}
/** 单日收入（额度口径），供图表与表格复用 */
function dailyIncomeQuota(item: RevenueDaily): number {
  return centsToQuota(Number(item.income_cents || 0))
}

// ---------------------------------------------------------------------------
// 内联 SVG 折线图（两条序列）
// ---------------------------------------------------------------------------

/**
 * viewBox 固定 100x30，配合 preserveAspectRatio="none" 自适应宽度，
 * 无需监听 resize。两条序列**共用同一个 max**，否则两条线的相对高低
 * 会失去意义（收入与消耗必须能直接比高低）。
 */
const chartMax = computed(() => {
  const values = daily.value.flatMap((item) => [dailyIncomeQuota(item), Number(item.consumed_quota || 0)])
  return Math.max(...values, 1)
})

interface ChartSeries {
  key: 'income' | 'consumed'
  label: string
  color: string
  values: number[]
  points: string
  total: number
}

function buildPoints(values: number[]): string {
  if (values.length === 0) return ''
  const stepX = values.length > 1 ? 100 / (values.length - 1) : 0
  return values
    .map((value, index) => {
      const x = values.length > 1 ? index * stepX : 50
      // 图表上下各留 2 个单位的内边距，避免峰值贴边被裁掉
      const y = 28 - (value / chartMax.value) * 26
      return `${x.toFixed(2)},${y.toFixed(2)}`
    })
    .join(' ')
}

const series = computed<ChartSeries[]>(() => {
  const incomeValues = daily.value.map((item) => dailyIncomeQuota(item))
  const consumedValues = daily.value.map((item) => Number(item.consumed_quota || 0))
  return [
    {
      key: 'income',
      label: t('revenue.chart.income', '收入'),
      color: REVENUE_SERIES_COLORS.income,
      values: incomeValues,
      points: buildPoints(incomeValues),
      total: incomeValues.reduce((sum, value) => sum + value, 0),
    },
    {
      key: 'consumed',
      label: t('revenue.chart.consumed', '消耗'),
      color: REVENUE_SERIES_COLORS.consumed,
      values: consumedValues,
      points: buildPoints(consumedValues),
      total: consumedValues.reduce((sum, value) => sum + value, 0),
    },
  ]
})

/** 每个数据点的坐标与 hover 热区，原生 <title> 提供提示 */
const hoverPoints = computed(() =>
  daily.value.map((item, index) => {
    const count = daily.value.length
    const stepX = count > 1 ? 100 / (count - 1) : 0
    const x = count > 1 ? index * stepX : 50
    // hover 热区用「整条竖直带」而不是圆点：viewBox 被 preserveAspectRatio="none"
    // 非等比拉伸，圆会被压成扁椭圆，而矩形band 拉伸后正好是理想的宽热区。
    const bandWidth = count > 1 ? stepX : 100
    return {
      date: item.date,
      bandX: Math.max(0, x - bandWidth / 2),
      bandWidth: Math.min(100 - Math.max(0, x - bandWidth / 2), bandWidth),
      income: dailyIncomeQuota(item),
      consumed: Number(item.consumed_quota || 0),
      requests: Number(item.request_count || 0),
    }
  }),
)

/** 悬停提示文本：把三个指标放在一行，鼠标扫过即可读 */
function pointTitle(point: { date: string, income: number, consumed: number, requests: number }): string {
  return `${point.date}｜收入 ${renderQuota(point.income, quotaConfig.value)}｜消耗 ${renderQuota(point.consumed, quotaConfig.value)}｜请求 ${point.requests}`
}

function marginColor(): 'success' | 'warning' | 'error' {
  if (grossMargin.value >= 0.5) return 'success'
  if (grossMargin.value >= 0) return 'warning'
  return 'error'
}
</script>

<template>
  <div>
    <!-- 非管理员 -->
    <PageCard v-if="!isAdmin" :title="t('revenue.title', '营收看板')">
      <UAlert
        color="warning"
        icon="i-heroicons-lock-closed"
        title="无权访问"
        description="营收数据包含全部用户的订单与消费明细，仅管理员可以查看。"
      />
    </PageCard>

    <div v-else class="space-y-4">
      <!-- KPI -->
      <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <UCard>
          <div class="text-sm text-gray-500">{{ t('revenue.kpi.income', '总收入') }}</div>
          <div class="mt-1 text-2xl font-semibold text-green-600">
            {{ renderQuota(totalIncomeQuota, quotaConfig) }}
          </div>
          <p class="mt-1 text-xs text-gray-400">充值 + 订阅订单的实付金额折算</p>
        </UCard>

        <UCard>
          <div class="text-sm text-gray-500">{{ t('revenue.kpi.consumed', '总消耗') }}</div>
          <div class="mt-1 text-2xl font-semibold text-orange-500">
            {{ renderQuota(totalConsumedQuota, quotaConfig) }}
          </div>
          <p class="mt-1 text-xs text-gray-400">全部用户的 API 调用消耗</p>
        </UCard>

        <UCard>
          <div class="text-sm text-gray-500">{{ t('revenue.kpi.margin', '毛利率') }}</div>
          <div class="mt-1 flex items-center gap-2">
            <span class="text-2xl font-semibold">{{ (grossMargin * 100).toFixed(1) }}%</span>
            <UBadge :color="marginColor()" variant="subtle" size="sm">
              {{ grossMargin >= 0.5 ? '健康' : grossMargin >= 0 ? '偏低' : '倒挂' }}
            </UBadge>
          </div>
          <p class="mt-1 text-xs text-gray-400">
            （收入 − 消耗）/ 收入；未消耗余额 {{ renderQuota(unconsumedQuota, quotaConfig) }}
          </p>
        </UCard>

        <UCard>
          <div class="text-sm text-gray-500">{{ t('revenue.kpi.arpu', 'ARPU') }}</div>
          <div class="mt-1 text-2xl font-semibold">{{ renderQuota(arpu, quotaConfig) }}</div>
          <p class="mt-1 text-xs text-gray-400">
            付费用户 {{ renderNumber(stats?.paying_users ?? 0) }} 人的人均收入
          </p>
        </UCard>

        <UCard>
          <div class="text-sm text-gray-500">{{ t('revenue.kpi.active_subscriptions', '生效订阅') }}</div>
          <div class="mt-1 text-2xl font-semibold">{{ renderNumber(stats?.active_subscriptions ?? 0) }}</div>
          <p class="mt-1 text-xs text-gray-400">
            其中自动续费 {{ renderNumber(stats?.auto_renew_subscriptions ?? 0) }} 份
          </p>
        </UCard>

        <UCard>
          <div class="text-sm text-gray-500">{{ t('revenue.kpi.renew_rate', '自动续费率') }}</div>
          <div class="mt-1 text-2xl font-semibold">{{ (renewRate * 100).toFixed(1) }}%</div>
          <p class="mt-1 text-xs text-gray-400">自动续费订阅 / 全部生效订阅</p>
        </UCard>
      </div>

      <!-- 趋势图 -->
      <UCard>
        <template #header>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="text-sm font-semibold text-gray-700">
              {{ t('revenue.chart.title', '收入与消耗趋势') }}
            </div>
            <div class="flex items-center gap-3">
              <div v-for="item in series" :key="item.key" class="flex items-center gap-1 text-xs text-gray-500">
                <span class="inline-block size-2.5 rounded-full" :style="{ backgroundColor: item.color }" />
                {{ item.label }}
                <span class="font-medium text-gray-700">{{ renderQuota(item.total, quotaConfig) }}</span>
              </div>
              <USelect v-model="days" :items="dayOptions" size="sm" class="w-28" />
              <UButton size="xs" color="neutral" variant="outline" :loading="loading" @click="loadStats">
                {{ t('common.refresh', '刷新') }}
              </UButton>
            </div>
          </div>
        </template>

        <div v-if="daily.length > 0">
          <svg viewBox="0 0 100 30" preserveAspectRatio="none" class="h-48 w-full">
            <!-- 参考线：0 / 50% / 100% -->
            <line
              v-for="ratio in [0, 0.5, 1]"
              :key="ratio"
              x1="0"
              :y1="(28 - ratio * 26).toFixed(2)"
              x2="100"
              :y2="(28 - ratio * 26).toFixed(2)"
              stroke="#e5e7eb"
              stroke-width="0.3"
              vector-effect="non-scaling-stroke"
            />
            <!-- 两条折线 -->
            <polyline
              v-for="item in series"
              :key="item.key"
              :points="item.points"
              fill="none"
              :stroke="item.color"
              stroke-width="1.4"
              vector-effect="non-scaling-stroke"
              stroke-linejoin="round"
              stroke-linecap="round"
            />
            <!-- hover 热区：每个数据点一条透明竖直带，鼠标扫过显示日期与三列数字 -->
            <rect
              v-for="point in hoverPoints"
              :key="point.date"
              :x="point.bandX"
              y="0"
              :width="point.bandWidth"
              height="30"
              fill="transparent"
            >
              <title>{{ pointTitle(point) }}</title>
            </rect>
          </svg>
          <div class="mt-2 flex justify-between text-[0.7rem] text-gray-400">
            <span>{{ daily[0]?.date }}</span>
            <span class="text-gray-500">纵轴峰值 {{ renderQuota(chartMax, quotaConfig) }}</span>
            <span>{{ daily[daily.length - 1]?.date }}</span>
          </div>
        </div>
        <div v-else class="py-12 text-center text-sm text-gray-400">
          {{ loading ? t('common.loading', '加载中...') : t('common.none', '无') }}
        </div>
      </UCard>

      <!-- 明细表 -->
      <div class="grid gap-4 lg:grid-cols-2">
        <PageCard :title="t('revenue.top_models.title', '模型消耗排行')">
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead class="border-b border-gray-200 text-left text-gray-500">
                <tr>
                  <th class="p-2">#</th>
                  <th class="p-2">模型</th>
                  <th class="p-2">请求数</th>
                  <th class="p-2">消耗</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(row, index) in topModels" :key="row.model" class="border-b border-gray-100 hover:bg-gray-50">
                  <td class="p-2 text-gray-400">{{ index + 1 }}</td>
                  <td class="p-2 font-medium">{{ row.model || '未知' }}</td>
                  <td class="p-2">{{ renderNumber(row.count ?? 0) }}</td>
                  <td class="p-2 whitespace-nowrap">{{ renderQuota(row.quota ?? 0, quotaConfig) }}</td>
                </tr>
                <tr v-if="topModels.length === 0">
                  <td colspan="4" class="p-8 text-center text-gray-400">
                    {{ loading ? t('common.loading', '加载中...') : t('common.none', '无') }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </PageCard>

        <PageCard :title="t('revenue.top_users.title', '用户消耗排行')">
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead class="border-b border-gray-200 text-left text-gray-500">
                <tr>
                  <th class="p-2">#</th>
                  <th class="p-2">用户</th>
                  <th class="p-2">消耗</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(row, index) in topUsers" :key="`${row.user_id ?? index}-${row.username}`" class="border-b border-gray-100 hover:bg-gray-50">
                  <td class="p-2 text-gray-400">{{ index + 1 }}</td>
                  <td class="p-2 font-medium">{{ row.username || `ID ${row.user_id ?? '-'}` }}</td>
                  <td class="p-2 whitespace-nowrap">{{ renderQuota(row.quota ?? 0, quotaConfig) }}</td>
                </tr>
                <tr v-if="topUsers.length === 0">
                  <td colspan="3" class="p-8 text-center text-gray-400">
                    {{ loading ? t('common.loading', '加载中...') : t('common.none', '无') }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </PageCard>
      </div>

      <!-- 每日明细：折线图不方便读具体数字，补一张表 -->
      <PageCard :title="t('revenue.daily.title', '每日明细')">
        <div class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead class="border-b border-gray-200 text-left text-gray-500">
              <tr>
                <th class="p-2">日期</th>
                <th class="p-2">收入</th>
                <th class="p-2">消耗</th>
                <th class="oa-hide-on-mobile p-2">请求数</th>
                <th class="oa-hide-on-mobile p-2">活跃用户</th>
                <th class="oa-hide-on-mobile p-2">当日毛利</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in daily" :key="row.date" class="border-b border-gray-100 hover:bg-gray-50">
                <td class="p-2 whitespace-nowrap">{{ row.date }}</td>
                <td class="p-2 whitespace-nowrap text-green-600">{{ renderQuota(dailyIncomeQuota(row), quotaConfig) }}</td>
                <td class="p-2 whitespace-nowrap text-orange-500">{{ renderQuota(row.consumed_quota ?? 0, quotaConfig) }}</td>
                <td class="oa-hide-on-mobile p-2">{{ renderNumber(row.request_count ?? 0) }}</td>
                <td class="oa-hide-on-mobile p-2">{{ renderNumber(row.active_users ?? 0) }}</td>
                <td class="oa-hide-on-mobile p-2 text-gray-500">
                  {{ renderQuota(dailyIncomeQuota(row) - (row.consumed_quota ?? 0), quotaConfig) }}
                </td>
              </tr>
              <tr v-if="daily.length === 0">
                <td colspan="6" class="p-8 text-center text-gray-400">
                  {{ loading ? t('common.loading', '加载中...') : t('common.none', '无') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </PageCard>
    </div>
  </div>
</template>
