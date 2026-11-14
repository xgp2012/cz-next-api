<script setup lang="ts">
/**
 * 定价规则管理（仅管理员）。
 *
 * 对应后端 model/pricing.go 的「有序规则链」：
 *   匹配（model / group / channel_type / time_windows）→ 计价（per_token |
 *   per_call | tiered_by_tokens）。规则按 Priority 降序、Order 升序匹配，
 *   第一条命中即生效；全部不命中时回落到旧的 ModelRatio/GroupRatio。
 *
 * 本页的两处关键决策：
 *
 * 1. **不分页**。后端 GetAllPricingRules 一次返回全部规则（规则量通常 < 100
 *    且顺序本身就是语义的一部分），用游标分页反而会把「优先级链」切碎，
 *    也拿不到用来判断遮挡关系的完整上下文。
 *
 * 2. **模态框里做客户端实时预览**。定价最反直觉的地方是「ratio 到底是什么钱」，
 *    如果只让运营填倍率就保存，改错一位数要等到用户账单出来才会发现。
 *    因此这里用与后端完全相同的公式（见下文 preview 计算）即时算出
 *    「$X / 1M tokens」与「这次示例用量要扣多少额度」，让改动可见。
 *    预览只做展示，**不作为校验依据**——真正的校验在 model.ValidatePricingRule。
 */
import type { BillingMode, PriceTier, PricingRule, SelectOption } from '~/utils/types'

const { t } = useI18n()
const { isAdmin, quotaConfig } = useUserStore()
const { success, showError, warning } = useAppToast()

const loading = ref(false)
const saving = ref(false)
const rules = ref<PricingRule[]>([])
const searchKeyword = ref('')

// ---------------------------------------------------------------------------
// 列表
// ---------------------------------------------------------------------------
async function loadRules() {
  loading.value = true
  try {
    const res = await api.get<PricingRule[]>('/api/pricing_rules/')
    rules.value = res.data || []
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (!isAdmin.value) return
  loadRules()
})

const filteredRules = computed(() => {
  const keyword = searchKeyword.value.trim().toLowerCase()
  if (!keyword) return rules.value
  return rules.value.filter(
    (rule) =>
      rule.name?.toLowerCase().includes(keyword)
      || rule.model?.toLowerCase().includes(keyword)
      || rule.note?.toLowerCase().includes(keyword)
      || billingModeLabel(rule.billing_mode).includes(keyword),
  )
})

// ---------------------------------------------------------------------------
// 表单
// ---------------------------------------------------------------------------

/** 表单模型：字段名与后端 JSON tag 一一对应，避免提交前再做映射 */
interface RuleForm {
  id: number
  name: string
  note: string
  status: number
  model: string
  group: string
  channel_type: number
  time_windows: string
  billing_mode: BillingMode
  input_ratio: number
  output_ratio: number
  cache_hit_ratio: number
  cache_write_ratio: number
  per_call_price: number
  tiers: string
  priority: number
  order: number
}

function emptyForm(): RuleForm {
  return {
    id: 0,
    name: '',
    note: '',
    status: PRICING_RULE_STATUS_ENABLED,
    model: '',
    group: '',
    channel_type: CHANNEL_TYPE_ANY,
    time_windows: '',
    billing_mode: BILLING_MODE_PER_TOKEN,
    input_ratio: 1,
    output_ratio: 1,
    cache_hit_ratio: 0,
    cache_write_ratio: 0,
    per_call_price: 0.01,
    tiers: JSON.stringify(
      [
        { tier: 1, up_to_tokens: 1000000, input_ratio: 2.5, output_ratio: 2.5 },
        { tier: 2, up_to_tokens: 0, input_ratio: 2, output_ratio: 2 },
      ],
      null,
      2,
    ),
    priority: 0,
    order: 0,
  }
}

const form = reactive<RuleForm>(emptyForm())

const modalOpen = ref(false)
const isEdit = computed(() => form.id > 0)

/** 示例用量：实时预览据此算出「这一次请求大概扣多少」 */
const sample = reactive({
  prompt_tokens: 1000,
  completion_tokens: 1000,
  cache_hit_tokens: 0,
})
const groupRatio = ref(1)

function openCreate() {
  Object.assign(form, emptyForm())
  modalOpen.value = true
}

function openEdit(rule: PricingRule) {
  Object.assign(form, emptyForm(), {
    id: rule.id,
    name: rule.name || '',
    note: rule.note || '',
    status: rule.status || PRICING_RULE_STATUS_ENABLED,
    model: rule.model || '',
    group: rule.group || '',
    channel_type: Number.isFinite(rule.channel_type) ? rule.channel_type : CHANNEL_TYPE_ANY,
    time_windows: rule.time_windows || '',
    billing_mode: rule.billing_mode || BILLING_MODE_PER_TOKEN,
    input_ratio: Number(rule.input_ratio ?? 0),
    output_ratio: Number(rule.output_ratio ?? 0),
    cache_hit_ratio: Number(rule.cache_hit_ratio ?? 0),
    cache_write_ratio: Number(rule.cache_write_ratio ?? 0),
    per_call_price: Number(rule.per_call_price ?? 0),
    tiers: rule.tiers || '',
    priority: Number(rule.priority ?? 0),
    order: Number(rule.order ?? 0),
  })
  modalOpen.value = true
}

const statusOptions: SelectOption[] = [
  { key: PRICING_RULE_STATUS_ENABLED, value: PRICING_RULE_STATUS_ENABLED, text: '启用', label: '启用' },
  { key: PRICING_RULE_STATUS_DISABLED, value: PRICING_RULE_STATUS_DISABLED, text: '禁用', label: '禁用' },
]

function ruleStatus(rule: PricingRule) {
  return PRICING_RULE_STATUS_MAP[rule.status] || PRICING_RULE_STATUS_MAP[PRICING_RULE_STATUS_ENABLED]
}

// ---------------------------------------------------------------------------
// 实时预览：与后端 PriceUsage 完全相同的公式
// ---------------------------------------------------------------------------

/** 解析表单里的阶梯 JSON，非法时返回空数组（预览随之退化为「无法计算」） */
const parsedTiers = computed<PriceTier[]>(() => {
  if (form.billing_mode !== BILLING_MODE_TIERED) return []
  if (!verifyJSON(form.tiers)) return []
  try {
    const list = JSON.parse(form.tiers) as PriceTier[]
    if (!Array.isArray(list)) return []
    // 与后端一致：按 tier 升序，且只有最后一档允许无上界
    return [...list].sort((a, b) => Number(a.tier) - Number(b.tier))
  } catch {
    return []
  }
})

/** 命中的阶梯：UpToTokens == 0 表示无上界 */
const chosenTier = computed<PriceTier | null>(() => {
  const tiers = parsedTiers.value
  if (tiers.length === 0) return null
  const total = sample.prompt_tokens + sample.completion_tokens
  for (const tier of tiers) {
    if (!tier.up_to_tokens || total <= Number(tier.up_to_tokens)) return tier
  }
  return tiers[tiers.length - 1] || null
})

interface PreviewResult {
  ok: boolean
  /** 人类可读的单价摘要 */
  headline: string
  /** 示例用量对应的美元金额 */
  usd: number
  /** 换算成额度（乘分组倍率之后） */
  quota: number
  detail: string
}

/**
 * 计算预览。
 *
 * 公式与 model/pricing.go 严格对齐：
 *   usdPerRatioUnit = 0.002 / 1000      （ratio = 1 ⇒ $0.002 / 1K tokens）
 *   tokenQuota      = (prompt × in + completion × out) × usdPerRatioUnit
 *                      + cacheHit × cacheHitRatio × usdPerRatioUnit
 *                      + cacheWrite × cacheWriteRatio × usdPerRatioUnit
 *   最终 quota      = ceil(usd × groupRatio × QuotaPerUnit)，保底 1
 */
const preview = computed<PreviewResult>(() => {
  const ratio = groupRatio.value > 0 ? groupRatio.value : 1
  const perUnit = Number(quotaConfig.value.quotaPerUnit || 500000) || 500000
  const toQuota = (usd: number) => {
    const q = Math.ceil(usd * ratio * perUnit)
    return q < 1 ? 1 : q
  }

  if (form.billing_mode === BILLING_MODE_PER_CALL) {
    const usd = form.per_call_price * ratio
    if (form.per_call_price <= 0) {
      return { ok: false, headline: '未配置单次价格', usd: 0, quota: 0, detail: '按次计费需要填写大于 0 的单次价格。' }
    }
    return {
      ok: true,
      headline: `${renderPerCallPrice(form.per_call_price)}（与 token 数无关）`,
      usd,
      quota: toQuota(form.per_call_price),
      detail: `单次 $${form.per_call_price.toFixed(4)} × 分组倍率 ${ratio.toFixed(2)} = $${usd.toFixed(4)}`,
    }
  }

  if (form.billing_mode === BILLING_MODE_TIERED) {
    const tiers = parsedTiers.value
    if (tiers.length === 0) {
      return { ok: false, headline: '阶梯 JSON 无法解析', usd: 0, quota: 0, detail: '请填写形如 [{"tier":1,"up_to_tokens":1000000,"input_ratio":2.5},{"tier":2,"up_to_tokens":0,"input_ratio":2}] 的数组。' }
    }
    const tier = chosenTier.value
    if (!tier) {
      return { ok: false, headline: '没有可用的阶梯', usd: 0, quota: 0, detail: '至少需要一个阶梯。' }
    }
    const total = sample.prompt_tokens + sample.completion_tokens
    const usd
      = (sample.prompt_tokens * Number(tier.input_ratio ?? 0)
        + sample.completion_tokens * Number(tier.output_ratio ?? 0))
      * USD_PER_RATIO_UNIT
    return {
      ok: true,
      headline: `Tier ${tier.tier}（≤ ${tier.up_to_tokens || '∞'} tokens）输入 ${renderPricePerMillion(tier.input_ratio)} 输出 ${renderPricePerMillion(tier.output_ratio)} / 1M`,
      usd: usd * ratio,
      quota: toQuota(usd),
      detail: `示例总量 ${total} tokens 落在 Tier ${tier.tier}，${
        (sample.prompt_tokens * Number(tier.input_ratio ?? 0) + sample.completion_tokens * Number(tier.output_ratio ?? 0)).toFixed(0)
      } × ${USD_PER_RATIO_UNIT} = $${(usd * ratio).toFixed(6)}`,
    }
  }

  // per_token
  if (form.input_ratio <= 0 && form.output_ratio <= 0) {
    return { ok: false, headline: '未配置倍率', usd: 0, quota: 0, detail: '按量计费至少需要填写输入或输出倍率中的一个。' }
  }
  // 缓存命中的部分从普通输入里扣除，避免重复计费（与后端 tokenQuota 一致）
  let prompt = sample.prompt_tokens
  const cacheHit = sample.cache_hit_tokens
  if (cacheHit > 0 && form.cache_hit_ratio > 0 && cacheHit <= prompt) prompt -= cacheHit
  let usd = (prompt * form.input_ratio + sample.completion_tokens * form.output_ratio) * USD_PER_RATIO_UNIT
  if (cacheHit > 0 && form.cache_hit_ratio > 0) usd += cacheHit * form.cache_hit_ratio * USD_PER_RATIO_UNIT
  if (cacheHit > 0 && form.cache_write_ratio > 0) usd += cacheHit * form.cache_write_ratio * USD_PER_RATIO_UNIT

  return {
    ok: true,
    headline: `输入 ${renderPricePerMillion(form.input_ratio)} / 输出 ${renderPricePerMillion(form.output_ratio)} per 1M tokens`,
    usd: usd * ratio,
    quota: toQuota(usd),
    detail: `示例（输入 ${sample.prompt_tokens} / 输出 ${sample.completion_tokens}）裸价 $${usd.toFixed(6)} × 分组倍率 ${ratio.toFixed(2)} = $${(usd * ratio).toFixed(6)}`,
  }
})

// ---------------------------------------------------------------------------
// 提交
// ---------------------------------------------------------------------------

/** 时间窗每行的合法性：小时必须在 0-23（跨零点由 start > end 表达，是允许的） */
const timeWindowError = computed(() => {
  if (!form.time_windows.trim()) return ''
  if (!verifyJSON(form.time_windows)) return '时间窗必须是合法的 JSON 数组'
  try {
    const list = JSON.parse(form.time_windows)
    if (!Array.isArray(list)) return '时间窗必须是 JSON 数组'
    for (const item of list) {
      const start = Number(item?.start_hour)
      const end = Number(item?.end_hour)
      if (!Number.isInteger(start) || !Number.isInteger(end)) return 'start_hour / end_hour 必须是整数'
      if (start < 0 || start > 23 || end < 0 || end > 23) return 'start_hour / end_hour 必须在 0-23 之间'
    }
  } catch {
    return '时间窗必须是合法的 JSON 数组'
  }
  return ''
})

const tiersError = computed(() => {
  if (form.billing_mode !== BILLING_MODE_TIERED) return ''
  if (!form.tiers.trim()) return '阶梯计费需要配置至少一个阶梯'
  if (!verifyJSON(form.tiers)) return '阶梯必须是合法的 JSON 数组'
  const tiers = parsedTiers.value
  if (tiers.length === 0) return '阶梯必须是 JSON 数组且至少包含一项'
  for (let i = 0; i < tiers.length; i++) {
    if (!tiers[i].up_to_tokens && i !== tiers.length - 1) {
      return '只有最后一个阶梯可以设置为无上界（up_to_tokens = 0）'
    }
  }
  return ''
})

const formError = computed(() => {
  if (!form.name.trim()) return '规则名称不能为空'
  if (timeWindowError.value) return timeWindowError.value
  if (tiersError.value) return tiersError.value
  if (form.billing_mode === BILLING_MODE_PER_CALL && form.per_call_price <= 0) return '按次计费需要填写大于 0 的单次价格'
  if (form.billing_mode === BILLING_MODE_PER_TOKEN && form.input_ratio <= 0 && form.output_ratio <= 0) {
    return '按量计费至少需要配置输入倍率或输出倍率'
  }
  return ''
})

async function submit() {
  if (formError.value) {
    warning(formError.value)
    return
  }
  saving.value = true
  try {
    // 只提交后端 Select 列表里出现过的字段，避免把前端状态写进数据库
    const payload: Record<string, unknown> = {
      name: form.name.trim(),
      note: form.note,
      status: form.status,
      model: form.model.trim(),
      group: form.group.trim(),
      channel_type: form.channel_type,
      time_windows: form.time_windows.trim(),
      billing_mode: form.billing_mode,
      input_ratio: form.input_ratio,
      output_ratio: form.output_ratio,
      cache_hit_ratio: form.cache_hit_ratio,
      cache_write_ratio: form.cache_write_ratio,
      per_call_price: form.per_call_price,
      tiers: form.tiers.trim(),
      priority: form.priority,
      order: form.order,
    }
    if (isEdit.value) {
      payload.id = form.id
      await api.put('/api/pricing_rules/', payload)
      success(t('pricing.manage.update_success', '定价规则已更新'))
    } else {
      await api.post('/api/pricing_rules/', payload)
      success(t('pricing.manage.create_success', '定价规则已创建'))
    }
    modalOpen.value = false
    await loadRules()
  } catch (error) {
    showError(error)
  } finally {
    saving.value = false
  }
}

// ---------------------------------------------------------------------------
// 状态切换 / 删除
// ---------------------------------------------------------------------------
async function toggleStatus(rule: PricingRule) {
  const next = rule.status === PRICING_RULE_STATUS_ENABLED
    ? PRICING_RULE_STATUS_DISABLED
    : PRICING_RULE_STATUS_ENABLED
  try {
    // 后端 UpdatePricingRule 是全字段覆盖，禁用时也必须把计价字段原样带回，
    // 否则 Select 列表外的字段会被零值覆盖（倍率会被清零）。
    await api.put('/api/pricing_rules/', { ...rule, status: next })
    rule.status = next
    success(t('pricing.manage.operation_success', '操作成功'))
  } catch (error) {
    showError(error)
  }
}

const confirmDeleteId = ref<number | null>(null)

async function deleteRule(rule: PricingRule) {
  try {
    await api.delete(`/api/pricing_rules/${rule.id}/`)
    success(t('pricing.manage.delete_success', '定价规则已删除'))
    await loadRules()
  } catch (error) {
    showError(error)
  } finally {
    confirmDeleteId.value = null
  }
}

/** 规则摘要：列表里一眼能看出这条规则在卖什么 */
function ruleSummary(rule: PricingRule): string {
  if (rule.billing_mode === BILLING_MODE_PER_CALL) return renderPerCallPrice(rule.per_call_price)
  if (rule.billing_mode === BILLING_MODE_TIERED) {
    try {
      const tiers = JSON.parse(rule.tiers || '[]') as PriceTier[]
      if (Array.isArray(tiers) && tiers.length > 0) return `${tiers.length} 档阶梯（最低 ${renderPricePerMillion(Math.min(...tiers.map((x) => Number(x.input_ratio ?? 0))))} / 1M）`
    } catch {
      return '阶梯 JSON 无法解析'
    }
    return '未配置阶梯'
  }
  return `${renderPricePerMillion(rule.input_ratio)} / ${renderPricePerMillion(rule.output_ratio)} per 1M`
}
</script>

<template>
  <div>
    <!-- 非管理员：给出明确的拒绝提示，而不是渲染一个点不动的空表格 -->
    <PageCard v-if="!isAdmin" :title="t('pricing.manage.title', '定价规则管理')">
      <UAlert
        color="warning"
        icon="i-heroicons-lock-closed"
        title="无权访问"
        description="定价规则涉及计费口径，仅管理员可以查看和修改。如需权限请联系系统管理员。"
      />
    </PageCard>

    <PageCard v-else :title="t('pricing.manage.title', '定价规则管理')" :subtitle="t('pricing.manage.subtitle', '规则按优先级降序、同优先级按 Order 升序匹配，第一条命中即生效；全部不命中时回落默认倍率')">
      <template #actions>
        <UButton icon="i-heroicons-plus" color="primary" @click="openCreate">
          {{ t('pricing.manage.add', '新建规则') }}
        </UButton>
      </template>

      <div class="mb-4 flex flex-wrap items-center gap-2">
        <UInput
          v-model="searchKeyword"
          icon="i-heroicons-magnifying-glass"
          :placeholder="t('pricing.manage.search', '搜索规则名、模型或备注 ...')"
          class="min-w-56 flex-1"
        />
        <UFormField label="预览用分组倍率" class="w-40">
          <UInputNumber v-model="groupRatio" :min="0" :step="0.1" size="sm" class="w-full" />
        </UFormField>
        <UButton color="neutral" variant="outline" icon="i-heroicons-arrow-path" :loading="loading" @click="loadRules">
          {{ t('common.refresh', '刷新') }}
        </UButton>
        <UButton to="/pricing" color="neutral" variant="outline" icon="i-heroicons-eye">
          {{ t('pricing.manage.view_wall', '查看价格墙') }}
        </UButton>
      </div>

      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b border-gray-200 text-left text-gray-500">
            <tr>
              <th class="p-2">名称</th>
              <th class="p-2">匹配</th>
              <th class="p-2">计费方式</th>
              <th class="p-2">计价</th>
              <th class="oa-hide-on-mobile p-2">优先级 / 序号</th>
              <th class="p-2">状态</th>
              <th class="p-2 text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rule in filteredRules" :key="rule.id" class="border-b border-gray-100 hover:bg-gray-50">
              <td class="p-2">
                <div class="font-medium">{{ rule.name }}</div>
                <div class="text-xs text-gray-400">
                  ID {{ rule.id }}<span v-if="rule.note"> · {{ rule.note }}</span>
                </div>
              </td>
              <td class="p-2">
                <div>
                  <code class="text-xs">{{ rule.model || '*' }}</code>
                  <UBadge v-if="rule.model?.endsWith('*')" class="ml-1" color="info" variant="subtle" size="sm">
                    通配
                  </UBadge>
                </div>
                <div class="text-xs text-gray-400">
                  分组 {{ rule.group || '任意' }} · 渠道 {{ rule.channel_type >= 0 ? rule.channel_type : '任意' }}
                  <template v-if="rule.time_windows"> · 分时 {{ rule.time_windows }}</template>
                </div>
              </td>
              <td class="p-2">
                <UBadge
                  :color="(BILLING_MODE_MAP[rule.billing_mode]?.color as any) || 'neutral'"
                  variant="subtle"
                  size="sm"
                >
                  {{ billingModeLabel(rule.billing_mode) }}
                </UBadge>
              </td>
              <td class="p-2 whitespace-nowrap text-gray-600">{{ ruleSummary(rule) }}</td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">{{ rule.priority }} / {{ rule.order }}</td>
              <td class="p-2">
                <StatusBadge :color="ruleStatus(rule).color" :label="ruleStatus(rule).label" />
              </td>
              <td class="p-2">
                <div class="flex flex-wrap justify-end gap-1">
                  <UButton size="xs" color="neutral" variant="soft" @click="openEdit(rule)">
                    {{ t('common.edit', '编辑') }}
                  </UButton>
                  <UButton
                    size="xs"
                    :color="rule.status === PRICING_RULE_STATUS_ENABLED ? 'warning' : 'success'"
                    variant="soft"
                    @click="toggleStatus(rule)"
                  >
                    {{ rule.status === PRICING_RULE_STATUS_ENABLED ? '禁用' : '启用' }}
                  </UButton>
                  <UButton size="xs" color="error" variant="ghost" @click="confirmDeleteId = rule.id">
                    {{ t('common.delete', '删除') }}
                  </UButton>
                </div>

                <UModal
                  :open="confirmDeleteId === rule.id"
                  title="删除定价规则"
                  @update:open="(v: boolean) => (confirmDeleteId = v ? rule.id : null)"
                >
                  <template #body>
                    <p class="text-sm text-gray-600">
                      确定要删除规则「{{ rule.name }}」吗？删除后命中它的模型会立即回落到默认倍率计价。
                    </p>
                  </template>
                  <template #footer>
                    <div class="flex w-full justify-end gap-2">
                      <UButton color="neutral" variant="ghost" @click="confirmDeleteId = null">
                        {{ t('common.cancel', '取消') }}
                      </UButton>
                      <UButton color="error" @click="deleteRule(rule)">{{ t('common.confirm', '确认') }}</UButton>
                    </div>
                  </template>
                </UModal>
              </td>
            </tr>
            <tr v-if="filteredRules.length === 0">
              <td colspan="7" class="p-8 text-center text-gray-400">
                {{ loading ? t('common.loading', '加载中...') : t('common.none', '无') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </PageCard>

    <!-- ------------------------------------------------------------------ -->
    <!-- 新建 / 编辑：模态框表单 + 实时预览                                    -->
    <!-- ------------------------------------------------------------------ -->
    <UModal
      v-model:open="modalOpen"
      :title="isEdit ? t('pricing.manage.edit_title', '编辑定价规则') : t('pricing.manage.create_title', '新建定价规则')"
      :description="t('pricing.manage.form_hint', '匹配条件留空表示不限制；计价方式决定下面显示哪些字段')"
      :ui="{ content: 'max-w-3xl' }"
    >
      <template #body>
        <div class="space-y-4">
          <!-- 基础信息 -->
          <div class="grid gap-3 md:grid-cols-2">
            <UFormField label="规则名称" required>
              <UInput v-model="form.name" placeholder="例如：GPT-4o 标准价" class="w-full" />
            </UFormField>
            <UFormField label="状态">
              <USelect v-model="form.status" :items="statusOptions" value-key="value" class="w-full" />
            </UFormField>
          </div>

          <UFormField label="备注">
            <UInput v-model="form.note" placeholder="给运营看的说明，例如：面向 VIP 分组的夜间折扣" class="w-full" />
          </UFormField>

          <!-- 匹配条件 -->
          <USeparator label="匹配条件" />

          <div class="grid gap-3 md:grid-cols-2">
            <UFormField label="模型" help="支持 prefix:* 通配，例如 gpt-4*；留空表示任意模型">
              <UInput v-model="form.model" placeholder="gpt-4o 或 gpt-4*" class="w-full" />
            </UFormField>
            <UFormField label="用户分组" help="留空 = 任意；多个分组用英文逗号分隔">
              <UInput v-model="form.group" placeholder="default,vip" class="w-full" />
            </UFormField>
            <UFormField label="渠道类型" help="-1 表示任意渠道类型">
              <UInputNumber v-model="form.channel_type" :min="-1" class="w-full" />
            </UFormField>
            <UFormField label="时间窗（JSON）" :error="timeWindowError || undefined" help="留空 = 全天；start_hour > end_hour 表示跨零点">
              <UInput v-model="form.time_windows" placeholder='[{"start_hour":22,"end_hour":6}]' class="w-full" />
            </UFormField>
          </div>

          <!-- 计价 -->
          <USeparator label="计价设置" />

          <UFormField label="计费方式">
            <USelect v-model="form.billing_mode" :items="BILLING_MODES" value-key="value" class="w-full" />
          </UFormField>

          <!-- per_token -->
          <div v-if="form.billing_mode === BILLING_MODE_PER_TOKEN" class="grid gap-3 md:grid-cols-2">
            <UFormField label="输入倍率" :help="`${renderPricePerMillion(form.input_ratio)} / 1M tokens`">
              <UInputNumber v-model="form.input_ratio" :min="0" :step="0.1" class="w-full" />
            </UFormField>
            <UFormField label="输出倍率" :help="`${renderPricePerMillion(form.output_ratio)} / 1M tokens`">
              <UInputNumber v-model="form.output_ratio" :min="0" :step="0.1" class="w-full" />
            </UFormField>
            <UFormField label="缓存命中倍率" help="0 = 不单独计价，命中部分按输入价计">
              <UInputNumber v-model="form.cache_hit_ratio" :min="0" :step="0.05" class="w-full" />
            </UFormField>
            <UFormField label="缓存写入倍率" help="0 = 不单独计价">
              <UInputNumber v-model="form.cache_write_ratio" :min="0" :step="0.05" class="w-full" />
            </UFormField>
          </div>

          <!-- per_call -->
          <div v-else-if="form.billing_mode === BILLING_MODE_PER_CALL">
            <UFormField label="单次价格（美元）" :help="renderPerCallPrice(form.per_call_price)">
              <UInputNumber v-model="form.per_call_price" :min="0" :step="0.001" :step-snapping="false" class="w-full" />
            </UFormField>
          </div>

          <!-- tiered_by_tokens -->
          <div v-else class="space-y-3">
            <UFormField
              label="阶梯（JSON 数组）"
              :error="tiersError || undefined"
              help="up_to_tokens 为该档的累计 token 上界；只有最后一档可填 0 表示无上界"
            >
              <UTextarea v-model="form.tiers" :rows="7" class="w-full font-mono text-xs" />
            </UFormField>
            <div class="overflow-x-auto rounded border border-gray-100">
              <table class="w-full text-xs">
                <thead class="bg-gray-50 text-left text-gray-500">
                  <tr>
                    <th class="p-2">Tier</th>
                    <th class="p-2">累计上界</th>
                    <th class="p-2">输入价 / 1M</th>
                    <th class="p-2">输出价 / 1M</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="tier in parsedTiers" :key="tier.tier" class="border-t border-gray-100">
                    <td class="p-2">{{ tier.tier }}</td>
                    <td class="p-2">{{ tier.up_to_tokens || '无上界' }}</td>
                    <td class="p-2">{{ renderPricePerMillion(tier.input_ratio) }}</td>
                    <td class="p-2">{{ renderPricePerMillion(tier.output_ratio) }}</td>
                  </tr>
                  <tr v-if="parsedTiers.length === 0">
                    <td colspan="4" class="p-3 text-center text-gray-400">暂无可解析的阶梯</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <!-- 调度 -->
          <USeparator label="调度" />
          <div class="grid gap-3 md:grid-cols-2">
            <UFormField label="优先级" help="越大越优先，先匹配优先级最高的规则">
              <UInputNumber v-model="form.priority" class="w-full" />
            </UFormField>
            <UFormField label="同优先级序号" help="数字越小越先匹配">
              <UInputNumber v-model="form.order" class="w-full" />
            </UFormField>
          </div>

          <!-- 实时预览 -->
          <USeparator label="实时预览（仅本页计算，不代表最终账单）" />
          <div class="rounded-md border border-gray-100 bg-gray-50 p-3">
            <div class="grid gap-3 md:grid-cols-3">
              <UFormField label="示例输入 tokens">
                <UInputNumber v-model="sample.prompt_tokens" :min="0" size="sm" class="w-full" />
              </UFormField>
              <UFormField label="示例输出 tokens">
                <UInputNumber v-model="sample.completion_tokens" :min="0" size="sm" class="w-full" />
              </UFormField>
              <UFormField label="其中缓存命中 tokens">
                <UInputNumber v-model="sample.cache_hit_tokens" :min="0" size="sm" class="w-full" />
              </UFormField>
            </div>

            <UAlert
              class="mt-3"
              :color="preview.ok ? 'success' : 'error'"
              variant="subtle"
              :icon="preview.ok ? 'i-heroicons-calculator' : 'i-heroicons-exclamation-triangle'"
              :title="preview.headline"
            >
              <template #description>
                <div class="space-y-1 text-xs">
                  <div>{{ preview.detail }}</div>
                  <div v-if="preview.ok" class="font-medium">
                    预计扣费：{{ renderQuota(preview.quota, quotaConfig) }}（{{ renderNumber(preview.quota) }} 额度，${{ preview.usd.toFixed(6) }}）
                  </div>
                </div>
              </template>
            </UAlert>

            <p v-if="formError" class="mt-2 text-xs text-red-500">提交前请先修正：{{ formError }}</p>
          </div>
        </div>
      </template>

      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="modalOpen = false">
            {{ t('common.cancel', '取消') }}
          </UButton>
          <UButton color="primary" :loading="saving" :disabled="!!formError" @click="submit">
            {{ t('common.save', '保存') }}
          </UButton>
        </div>
      </template>
    </UModal>
  </div>
</template>
