<script setup lang="ts">
/**
 * 模型价格墙（公开页，未登录也能看）。
 *
 * 这一页承担转化漏斗的第一步：让访客在注册前就能确认「要花多少钱」。
 * 因此数据源必须与真实计费一致——后端 GetPricingWall 返回的就是
 * 规则链 + 旧倍率，前端只做展示与换算，不做任何价格推断。
 *
 * 关于两种响应形态的兼容：
 * 后端当前返回 { rules, legacy_ratios, group_ratios, ratio_explanation }，
 * 而产品约定的目标形态是 { models: [...], group_ratio }。这里两种都消费，
 * 后端收敛到任一种都不会把页面打挂。
 *
 * 价格换算规则（与 model/pricing.go 的 usdPerRatioUnit 一致）：
 *   ratio = 1 ⇔ $0.002 / 1K tokens ⇔ $2 / 1M tokens
 * 换算函数统一放在 constants/commercial.ts 的 renderPricePerMillion 里。
 */
import type { PricingRule, PricingWallItem, PricingWallResponse } from '~/utils/types'

const { t } = useI18n()
const { showError } = useAppToast()

const loading = ref(false)
const searchKeyword = ref('')
const groupRatio = ref(1)
const ratioExplanation = ref('')
/** 规则未命中时回落到旧倍率，这部分单独提示，避免用户以为是定价规则配错了 */
const legacyCount = ref(0)

/** 页面内部统一的行结构：把规则 / 旧倍率 / 目标形态的 models 三种来源归一化 */
interface PriceRow {
  key: string
  model: string
  billing_mode: string
  input_ratio: number
  output_ratio: number
  cache_hit_ratio: number
  per_call_price: number
  tier: number
  note: string
  fromLegacy: boolean
}

const rows = ref<PriceRow[]>([])

function rowFromRule(rule: PricingRule, index: number): PriceRow {
  const noteParts: string[] = []
  if (rule.name) noteParts.push(rule.name)
  if (rule.note) noteParts.push(rule.note)
  if (rule.group) noteParts.push(`分组：${rule.group}`)
  if (rule.channel_type >= 0) noteParts.push(`渠道类型：${rule.channel_type}`)
  if (rule.priority) noteParts.push(`优先级：${rule.priority}`)
  return {
    key: `rule-${rule.id ?? index}`,
    model: rule.model || '*',
    billing_mode: rule.billing_mode || 'per_token',
    input_ratio: Number(rule.input_ratio ?? 0),
    output_ratio: Number(rule.output_ratio ?? 0),
    cache_hit_ratio: Number(rule.cache_hit_ratio ?? 0),
    per_call_price: Number(rule.per_call_price ?? 0),
    tier: 0,
    note: noteParts.join(' · '),
    fromLegacy: false,
  }
}

function rowFromLegacy(model: string, ratio: number): PriceRow {
  return {
    key: `legacy-${model}`,
    model,
    billing_mode: 'per_token',
    input_ratio: Number(ratio ?? 0),
    output_ratio: 0,
    cache_hit_ratio: 0,
    per_call_price: 0,
    tier: 0,
    note: '未命中定价规则，按系统默认倍率计价',
    fromLegacy: true,
  }
}

function rowFromWallItem(item: PricingWallItem, index: number): PriceRow {
  return {
    key: `item-${item.model}-${index}`,
    model: item.model || '*',
    billing_mode: item.billing_mode || 'per_token',
    input_ratio: Number(item.input_ratio ?? 0),
    output_ratio: Number(item.output_ratio ?? 0),
    cache_hit_ratio: Number(item.cache_hit_ratio ?? 0),
    per_call_price: Number(item.per_call_price ?? 0),
    tier: Number(item.tier ?? 0),
    note: item.description || '',
    fromLegacy: false,
  }
}

onMounted(() => loadPricing())

async function loadPricing() {
  loading.value = true
  try {
    const res = await api.get<PricingWallResponse | PricingWallResponse['models']>('/api/pricing')
    const data = res.data

    // 形态 A：直接就是模型数组
    if (Array.isArray(data)) {
      rows.value = data.map((item, index) => rowFromWallItem(item, index))
      return
    }
    if (!data) return

    ratioExplanation.value = data.ratio_explanation || ''
    groupRatio.value = Number(
      (data as PricingWallResponse).group_ratio
      ?? Object.values((data as PricingWallResponse).group_ratios || {})[0]
      ?? 1,
    )

    // 形态 B：目标形态 { models: [...] }
    if (Array.isArray(data.models) && data.models.length > 0) {
      rows.value = data.models.map((item, index) => rowFromWallItem(item, index))
      return
    }

    // 形态 C：现状形态 { rules, legacy_ratios }
    const ruleRows = (data.rules || []).map((rule, index) => rowFromRule(rule, index))
    const legacy = data.legacy_ratios || {}
    const legacyRows = Object.keys(legacy)
      .sort((a, b) => a.localeCompare(b))
      .map((model) => rowFromLegacy(model, legacy[model] as number))
    legacyCount.value = legacyRows.length
    rows.value = [...ruleRows, ...legacyRows]
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

/** 关键词过滤：模型名 / 说明 / 计费方式都参与匹配，方便按「按次」这类词筛 */
const filteredRows = computed(() => {
  const keyword = searchKeyword.value.trim().toLowerCase()
  if (!keyword) return rows.value
  return rows.value.filter(
    (row) =>
      row.model.toLowerCase().includes(keyword)
      || row.note.toLowerCase().includes(keyword)
      || row.billing_mode.toLowerCase().includes(keyword)
      || billingModeLabel(row.billing_mode).includes(keyword),
  )
})

function billingColor(mode: string) {
  const option = BILLING_MODE_MAP[mode]
  return (option?.color as 'primary' | 'warning' | 'info' | undefined) || 'neutral'
}

/** 缓存价：ratio 为 0 表示「不单独计价」，此时显示占位符而不是 $0.00 */
function cachePrice(row: PriceRow): string {
  if (row.cache_hit_ratio <= 0) return '—'
  return renderRatioAsPricePerMillion(row.cache_hit_ratio)
}

/** 输出价：旧倍率链路没有补全倍率信息，显示占位符避免误导 */
function outputPrice(row: PriceRow): string {
  if (row.fromLegacy) return '—'
  if (row.billing_mode === 'per_call') return '—'
  return renderRatioAsPricePerMillion(row.output_ratio)
}

function inputPrice(row: PriceRow): string {
  if (row.billing_mode === 'per_call') return '—'
  return renderRatioAsPricePerMillion(row.input_ratio)
}
</script>

<template>
  <div>
    <PageCard :title="t('pricing.title', '模型价格墙')" :subtitle="t('pricing.subtitle', '价格为实时计费口径，实际扣费以调用时的用量为准')">
      <!-- 口径说明 -->
      <UAlert
        class="mb-4"
        color="info"
        variant="subtle"
        icon="i-heroicons-information-circle"
        title="计价口径"
        :description="ratioExplanation || '倍率 1 = $0.002 / 1K tokens，即 $2.00 / 1M tokens；最终扣费 = 单价 × 分组倍率。'"
      />

      <div class="mb-4 flex flex-wrap items-center gap-2">
        <UInput
          v-model="searchKeyword"
          icon="i-heroicons-magnifying-glass"
          :placeholder="t('pricing.search', '搜索模型名、计费方式或说明 ...')"
          class="min-w-56 flex-1"
        />
        <UButton
          color="neutral"
          variant="outline"
          icon="i-heroicons-arrow-path"
          :loading="loading"
          @click="loadPricing"
        >
          {{ t('common.refresh', '刷新') }}
        </UButton>
        <UBadge v-if="groupRatio && groupRatio !== 1" color="warning" variant="subtle" size="sm">
          分组倍率 ×{{ groupRatio.toFixed(2) }}
        </UBadge>
        <UBadge v-if="legacyCount > 0" color="neutral" variant="subtle" size="sm">
          {{ legacyCount }} 个模型按默认倍率计价
        </UBadge>
      </div>

      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b border-gray-200 text-left text-gray-500">
            <tr>
              <th class="p-2">{{ t('pricing.table.model', '模型名') }}</th>
              <th class="p-2">{{ t('pricing.table.billing_mode', '计费方式') }}</th>
              <th class="p-2">{{ t('pricing.table.input', '输入价') }}</th>
              <th class="p-2">{{ t('pricing.table.output', '输出价') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('pricing.table.cached', '缓存价') }}</th>
              <th class="oa-hide-on-mobile p-2">{{ t('pricing.table.note', '说明') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in filteredRows" :key="row.key" class="border-b border-gray-100 hover:bg-gray-50">
              <td class="p-2">
                <code class="font-medium text-gray-800">{{ row.model }}</code>
                <UBadge v-if="row.tier > 0" class="ml-1" color="info" variant="subtle" size="sm">
                  Tier {{ row.tier }}
                </UBadge>
              </td>
              <td class="p-2">
                <UBadge :color="billingColor(row.billing_mode)" variant="subtle" size="sm">
                  {{ billingModeLabel(row.billing_mode) }}
                </UBadge>
              </td>
              <td class="p-2 whitespace-nowrap">
                <span v-if="row.billing_mode === 'per_call'" class="text-gray-500">
                  {{ renderPerCallPrice(row.per_call_price) }}
                </span>
                <span v-else>{{ inputPrice(row) }}</span>
              </td>
              <td class="p-2 whitespace-nowrap">{{ outputPrice(row) }}</td>
              <td class="oa-hide-on-mobile p-2 whitespace-nowrap">{{ cachePrice(row) }}</td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">{{ row.note || '—' }}</td>
            </tr>
            <tr v-if="filteredRows.length === 0">
              <td colspan="6" class="p-8 text-center text-gray-400">
                {{ loading ? t('common.loading', '加载中...') : t('common.none', '无') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <p class="mt-4 text-xs text-gray-400">
        按次计费的模型直接展示单次价格；缓存价为空表示该模型未单独区分缓存命中价。
        表中价格未乘分组倍率，登录后可在「订阅」页查看自己的实际分组。
      </p>
    </PageCard>
  </div>
</template>
