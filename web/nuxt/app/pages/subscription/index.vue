<script setup lang="ts">
/**
 * 套餐订阅。
 *
 * 数据与端点（router/commercial.go 的 subRoute）：
 *   GET  /api/subscription/plans          → 上架套餐（公开可读，未登录也能看价）
 *   GET  /api/subscription/self           → 我的订阅 + 计费偏好
 *   POST /api/subscription/balance/pay    → 用钱包余额购买 { plan_id }
 *   PUT  /api/subscription/self/preference→ 切换计费偏好 { preference }
 *   PUT  /api/subscription/self/auto_renew→ 开关某个订阅的自动续费 { id, auto_renew }
 *
 * 为什么要把「计费偏好」讲得这么啰嗦：订阅与钱包是两套并存的扣费来源，
 * 用户最怕的是「我明明买了套餐，为什么还在扣余额」。所以四种偏好的
 * 『先扣哪个、用完了怎么办』必须在选项旁边直接写清楚（见 SUBSCRIPTION_PREFERENCES）。
 *
 * 自动续费的开关只在「套餐允许」（plan.auto_renew_enabled）时才可用：
 * 后端 SetAutoRenew 会校验，前端提前禁用避免用户白点一次。
 */
import type {
  BillingPreference,
  SelfSubscriptionResponse,
  SubscriptionPlan,
  SubscriptionPlansResponse,
  UserSubscription,
} from '~/utils/types'

const { t } = useI18n()
const { user, quotaConfig, restoreUser } = useUserStore()
const { success, showError } = useAppToast()

const loading = ref(false)
const purchasing = ref(false)
const savingPreference = ref(false)
const autoRenewBusyId = ref<number | null>(null)

const plans = ref<SubscriptionPlan[]>([])
const subscriptions = ref<UserSubscription[]>([])
const preference = ref<BillingPreference>(SUBSCRIPTION_PREFERENCE_SUBSCRIPTION_FIRST)
/** plan_id -> 套餐详情：订阅卡片要显示套餐名，而订阅本身只存了 plan_id */
const planMap = ref<Record<string, SubscriptionPlan>>({})

const confirmBuyPlanId = ref<number | null>(null)

// ---------------------------------------------------------------------------
// 数据加载
// ---------------------------------------------------------------------------

/** 把后端可能给出的 plans（对象 map 或数组）统一成 Record<id, plan> */
function normalizePlanMap(input: SelfSubscriptionResponse['plans']): Record<string, SubscriptionPlan> {
  if (!input) return {}
  if (Array.isArray(input)) {
    return input.reduce((acc, plan) => {
      acc[String(plan.id)] = plan
      return acc
    }, {} as Record<string, SubscriptionPlan>)
  }
  return input as Record<string, SubscriptionPlan>
}

async function loadPlans() {
  try {
    // 后端 data 形态是 { plans, quota_per_unit, pay_enabled }，但也兼容直接给数组
    const res = await api.get<SubscriptionPlansResponse | SubscriptionPlan[]>('/api/subscription/plans')
    const data = res.data
    plans.value = Array.isArray(data) ? data : (data?.plans || [])
  } catch (error) {
    showError(error)
  }
}

async function loadSelf() {
  try {
    const res = await api.get<SelfSubscriptionResponse>('/api/subscription/self')
    const data = res.data
    if (!data) return
    subscriptions.value = data.subscriptions || []
    preference.value = data.preference || SUBSCRIPTION_PREFERENCE_SUBSCRIPTION_FIRST
    const fromSelf = normalizePlanMap(data.plans)
    planMap.value = { ...planMap.value, ...fromSelf }
  } catch (error) {
    showError(error)
  }
}

async function loadAll() {
  loading.value = true
  try {
    await Promise.all([loadPlans(), loadSelf()])
  } finally {
    loading.value = false
  }
}

onMounted(() => loadAll())

// ---------------------------------------------------------------------------
// 展示
// ---------------------------------------------------------------------------
function planOf(sub: UserSubscription): SubscriptionPlan | undefined {
  return planMap.value[String(sub.plan_id)]
}

function planTitle(sub: UserSubscription): string {
  const plan = planOf(sub)
  return plan?.title || `套餐 #${sub.plan_id}`
}

function priceLabel(plan: SubscriptionPlan): string {
  return renderCents(plan.price_cents)
}

function planPeriod(plan: SubscriptionPlan): string {
  return renderPlanPeriod(plan.duration_unit, plan.duration_value, plan.custom_seconds)
}

/** 套餐额度 -> 展示文本；0 表示不限量 */
function planQuotaLabel(plan: SubscriptionPlan): string {
  if (!plan.total_quota) return t('subscription.unlimited', '不限量')
  return renderQuota(plan.total_quota, quotaConfig.value)
}

function subscriptionQuotaLabel(sub: UserSubscription): string {
  if (!sub.amount_total) return t('subscription.unlimited', '不限量')
  return `${renderQuota(sub.amount_used, quotaConfig.value)} / ${renderQuota(sub.amount_total, quotaConfig.value)}`
}

/** 用量百分比，钳制到 0-100，避免后端数据异常时进度条溢出 */
function usagePercent(sub: UserSubscription): number {
  if (!sub.amount_total) return 0
  const percent = (Number(sub.amount_used || 0) / Number(sub.amount_total)) * 100
  if (!Number.isFinite(percent)) return 0
  return Math.min(100, Math.max(0, Math.round(percent * 10) / 10))
}

function usageColor(sub: UserSubscription): 'primary' | 'warning' | 'error' | 'success' {
  const percent = usagePercent(sub)
  if (percent >= 95) return 'error'
  if (percent >= 80) return 'warning'
  if (percent <= 0) return 'success'
  return 'primary'
}

/** 是否在宽限期内（已过 end_time 但 grace_end_time 还没到） */
function inGrace(sub: UserSubscription): boolean {
  const now = Math.floor(Date.now() / 1000)
  return Number(sub.grace_end_time ?? 0) > 0 && now > sub.end_time && now <= Number(sub.grace_end_time)
}

function statusOf(sub: UserSubscription) {
  if (inGrace(sub)) return { color: 'warning' as const, label: '宽限期' }
  return subscriptionStatus(sub.status)
}

/** 剩余天数（负数表示已过期），用于「还有几天到期」的提示 */
function daysLeft(sub: UserSubscription): number {
  const now = Math.floor(Date.now() / 1000)
  return Math.ceil((sub.end_time - now) / 86400)
}

function canUseBalance(plan: SubscriptionPlan): boolean {
  return plan.allow_balance_pay !== false
}

/** 余额是否够买（后端才是权威判断，这里只用于按钮态提示） */
function balanceEnough(plan: SubscriptionPlan): boolean {
  const perUnit = Number(quotaConfig.value.quotaPerUnit || 500000) || 500000
  const cost = plan.price_cents / 100 * perUnit
  return Number(user.value?.quota ?? 0) >= cost
}

const buyTarget = computed(() => plans.value.find((plan) => plan.id === confirmBuyPlanId.value) || null)

// ---------------------------------------------------------------------------
// 操作
// ---------------------------------------------------------------------------
async function buyWithBalance(plan: SubscriptionPlan) {
  purchasing.value = true
  try {
    await api.post<UserSubscription>('/api/subscription/balance/pay', { plan_id: plan.id })
    success(t('subscription.messages.buy_success', '套餐购买成功'))
    await restoreUser()
    await loadAll()
  } catch (error) {
    showError(error)
  } finally {
    purchasing.value = false
    confirmBuyPlanId.value = null
  }
}

async function savePreference(next: BillingPreference) {
  const previous = preference.value
  preference.value = next
  savingPreference.value = true
  try {
    await api.put('/api/subscription/self/preference', { preference: next })
    success(t('subscription.messages.preference_saved', '计费偏好已更新'))
  } catch (error) {
    preference.value = previous
    showError(error)
  } finally {
    savingPreference.value = false
  }
}

async function toggleAutoRenew(sub: UserSubscription, value: boolean) {
  autoRenewBusyId.value = sub.id
  const previous = !!sub.auto_renew
  sub.auto_renew = value
  try {
    await api.put('/api/subscription/self/auto_renew', { id: sub.id, auto_renew: value })
    success(
      value
        ? t('subscription.messages.auto_renew_on', '已开启自动续费，到期前将从钱包余额扣费')
        : t('subscription.messages.auto_renew_off', '已关闭自动续费'),
    )
  } catch (error) {
    sub.auto_renew = previous
    showError(error)
  } finally {
    autoRenewBusyId.value = null
  }
}

/** 自动续费是否对这个订阅可用：套餐必须允许，且订阅本身要在有效期内 */
function autoRenewAvailable(sub: UserSubscription): boolean {
  const plan = planOf(sub)
  return !!plan?.auto_renew_enabled && sub.status === 'active'
}

const currentPreference = computed(() => subscriptionPreferenceOption(preference.value))
</script>

<template>
  <div class="space-y-4">
    <!-- ------------------------------------------------------------------ -->
    <!-- 套餐卡片                                                            -->
    <!-- ------------------------------------------------------------------ -->
    <PageCard :title="t('subscription.title', '套餐订阅')" :subtitle="t('subscription.subtitle', '订阅按周期提供额度，到期自动回落分组；可用钱包余额直接购买')">
      <template #actions>
        <UButton color="neutral" variant="outline" icon="i-heroicons-arrow-path" :loading="loading" @click="loadAll">
          {{ t('common.refresh', '刷新') }}
        </UButton>
      </template>

      <div v-if="plans.length > 0" class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <UCard v-for="plan in plans" :key="plan.id" class="flex flex-col">
          <template #header>
            <div class="flex items-start justify-between gap-2">
              <div>
                <div class="text-base font-semibold text-gray-800">{{ plan.title }}</div>
                <div v-if="plan.subtitle" class="mt-0.5 text-xs text-gray-500">{{ plan.subtitle }}</div>
              </div>
              <UBadge color="primary" variant="subtle" size="sm">{{ planPeriod(plan) }}</UBadge>
            </div>
          </template>

          <div class="space-y-3">
            <div class="flex items-baseline gap-1">
              <span class="text-2xl font-semibold">{{ priceLabel(plan) }}</span>
              <span class="text-xs text-gray-400">/ {{ planPeriod(plan) }}</span>
            </div>

            <ul class="space-y-1 text-xs text-gray-600">
              <li class="flex items-start gap-1">
                <UIcon name="i-heroicons-check-circle" class="mt-0.5 size-3.5 shrink-0 text-green-500" />
                <span>周期额度：{{ planQuotaLabel(plan) }}</span>
              </li>
              <li v-if="plan.upgrade_group" class="flex items-start gap-1">
                <UIcon name="i-heroicons-check-circle" class="mt-0.5 size-3.5 shrink-0 text-green-500" />
                <span>购买后分组升级为「{{ plan.upgrade_group }}」，到期回落</span>
              </li>
              <li v-if="plan.quota_reset_period && plan.quota_reset_period !== 'never'" class="flex items-start gap-1">
                <UIcon name="i-heroicons-check-circle" class="mt-0.5 size-3.5 shrink-0 text-green-500" />
                <span>
                  额度按{{
                    plan.quota_reset_period === 'daily' ? '天' : plan.quota_reset_period === 'weekly' ? '周' : '月'
                  }}重置
                </span>
              </li>
              <li v-if="plan.allow_wallet_overflow" class="flex items-start gap-1">
                <UIcon name="i-heroicons-check-circle" class="mt-0.5 size-3.5 shrink-0 text-green-500" />
                <span>额度用尽后可溢出到钱包余额扣费</span>
              </li>
              <li v-if="plan.auto_renew_enabled" class="flex items-start gap-1">
                <UIcon name="i-heroicons-check-circle" class="mt-0.5 size-3.5 shrink-0 text-green-500" />
                <span>支持自动续费（可在订阅卡片里开关）</span>
              </li>
              <li v-if="Number(plan.grace_period_days ?? 0) > 0" class="flex items-start gap-1">
                <UIcon name="i-heroicons-check-circle" class="mt-0.5 size-3.5 shrink-0 text-green-500" />
                <span>到期后 {{ plan.grace_period_days }} 天宽限期，期间服务不中断</span>
              </li>
              <li v-if="Number(plan.max_purchase_per_user ?? 0) > 0" class="flex items-start gap-1">
                <UIcon name="i-heroicons-information-circle" class="mt-0.5 size-3.5 shrink-0 text-gray-400" />
                <span>每人最多同时持有 {{ plan.max_purchase_per_user }} 份</span>
              </li>
            </ul>
          </div>

          <template #footer>
            <div class="space-y-2">
              <UButton
                color="primary"
                block
                :disabled="!canUseBalance(plan)"
                @click="confirmBuyPlanId = plan.id"
              >
                {{ canUseBalance(plan) ? t('subscription.buttons.buy', '用余额购买') : '该套餐不支持余额支付' }}
              </UButton>
              <p v-if="canUseBalance(plan) && !balanceEnough(plan)" class="text-center text-xs text-amber-600">
                当前余额可能不足，可在充值中心补足后再购买
              </p>
            </div>
          </template>
        </UCard>
      </div>

      <div v-else class="py-10 text-center text-sm text-gray-400">
        {{ loading ? t('common.loading', '加载中...') : t('subscription.no_plans', '暂时没有上架的套餐') }}
      </div>
    </PageCard>

    <!-- ------------------------------------------------------------------ -->
    <!-- 计费偏好                                                            -->
    <!-- ------------------------------------------------------------------ -->
    <PageCard :title="t('subscription.preference.title', '计费偏好')" :subtitle="t('subscription.preference.subtitle', '决定每次请求先从订阅额度还是钱包余额里扣费')">
      <div class="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
        <button
          v-for="option in SUBSCRIPTION_PREFERENCES"
          :key="String(option.value)"
          type="button"
          class="rounded-md border p-3 text-left transition-colors"
          :class="preference === option.value
            ? 'border-primary-500 bg-primary-50'
            : 'border-gray-200 hover:border-gray-300 hover:bg-gray-50'"
          :disabled="savingPreference"
          @click="savePreference(option.value as BillingPreference)"
        >
          <div class="flex items-center justify-between">
            <span class="text-sm font-medium text-gray-800">{{ option.text }}</span>
            <UIcon
              v-if="preference === option.value"
              name="i-heroicons-check-circle"
              class="size-4 text-primary-500"
            />
          </div>
          <p class="mt-1 text-xs text-gray-500">{{ option.description }}</p>
        </button>
      </div>
      <p class="mt-3 text-xs text-gray-400">
        当前生效：<strong>{{ currentPreference.text }}</strong> —— {{ currentPreference.description }}
      </p>
    </PageCard>

    <!-- ------------------------------------------------------------------ -->
    <!-- 我的订阅                                                            -->
    <!-- ------------------------------------------------------------------ -->
    <PageCard :title="t('subscription.mine.title', '我的订阅')" :subtitle="t('subscription.mine.subtitle', '额度按到期时间从近到远消耗，先过期先用完，避免浪费')">
      <div v-if="subscriptions.length > 0" class="space-y-3">
        <UCard v-for="sub in subscriptions" :key="sub.id">
          <div class="space-y-3">
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div>
                <div class="flex items-center gap-2">
                  <span class="font-medium text-gray-800">{{ planTitle(sub) }}</span>
                  <StatusBadge :color="statusOf(sub).color" :label="statusOf(sub).label" />
                  <UBadge v-if="sub.upgrade_group" color="neutral" variant="subtle" size="sm">
                    分组 {{ sub.upgrade_group }}
                  </UBadge>
                </div>
                <div class="mt-1 text-xs text-gray-500">
                  {{ timestamp2string(sub.start_time) }} 至 {{ timestamp2string(sub.end_time) }}
                  <span v-if="sub.status === 'active' && daysLeft(sub) > 0">（剩余 {{ daysLeft(sub) }} 天）</span>
                  <span v-else-if="sub.status === 'active'" class="text-amber-600">（已到期）</span>
                </div>
              </div>
              <div class="text-right">
                <div class="text-sm font-medium">{{ subscriptionQuotaLabel(sub) }}</div>
                <div class="text-xs text-gray-400">
                  {{ sub.amount_total ? `已用 ${usagePercent(sub)}%` : '不限量' }}
                </div>
              </div>
            </div>

            <!-- 用量进度条 -->
            <div v-if="sub.amount_total">
              <UProgress :model-value="usagePercent(sub)" :max="100" :color="usageColor(sub)" size="sm" />
            </div>

            <div class="flex flex-wrap items-center justify-between gap-2">
              <div class="flex flex-wrap items-center gap-3 text-xs text-gray-500">
                <span v-if="sub.source">来源：{{ sub.source === 'balance' ? '余额购买' : sub.source === 'admin' ? '管理员赠送' : '订单支付' }}</span>
                <span v-if="Number(sub.renew_attempts ?? 0) > 0" class="text-amber-600">
                  连续续费失败 {{ sub.renew_attempts }} 次
                </span>
                <span v-if="inGrace(sub)">宽限截止：{{ timestamp2string(Number(sub.grace_end_time)) }}</span>
              </div>

              <div class="flex items-center gap-2">
                <USwitch
                  :model-value="!!sub.auto_renew"
                  :disabled="!autoRenewAvailable(sub) || autoRenewBusyId === sub.id"
                  @update:model-value="(value: boolean) => toggleAutoRenew(sub, value)"
                />
                <span class="text-xs" :class="autoRenewAvailable(sub) ? 'text-gray-600' : 'text-gray-400'">
                  自动续费
                  <span v-if="!autoRenewAvailable(sub)">
                    （{{ sub.status !== 'active' ? '订阅已结束' : '该套餐不支持' }}）
                  </span>
                </span>
              </div>
            </div>
          </div>
        </UCard>
      </div>
      <div v-else class="py-10 text-center text-sm text-gray-400">
        {{ loading ? t('common.loading', '加载中...') : t('subscription.mine.empty', '你还没有订阅任何套餐') }}
      </div>
    </PageCard>

    <!-- 购买确认 -->
    <UModal
      :open="confirmBuyPlanId !== null"
      :title="t('subscription.buttons.buy', '用余额购买')"
      @update:open="(v: boolean) => (confirmBuyPlanId = v ? confirmBuyPlanId : null)"
    >
      <template #body>
        <div v-if="buyTarget" class="space-y-2 text-sm text-gray-600">
          <p>
            即将用钱包余额购买套餐
            <strong>{{ buyTarget.title }}</strong>（{{ planPeriod(buyTarget) }}）。
          </p>
          <div class="rounded border border-gray-100 bg-gray-50 p-3">
            <div class="flex justify-between">
              <span>应付金额</span>
              <span class="font-medium">{{ priceLabel(buyTarget) }}</span>
            </div>
            <div class="mt-1 flex justify-between">
              <span>当前余额</span>
              <span>{{ renderQuota(user?.quota ?? 0, quotaConfig) }}</span>
            </div>
            <div class="mt-1 flex justify-between">
              <span>周期额度</span>
              <span>{{ planQuotaLabel(buyTarget) }}</span>
            </div>
          </div>
          <p class="text-xs text-gray-400">
            购买后立即扣减余额并生效；套餐为虚拟服务，已生效的周期不支持退款。
          </p>
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="confirmBuyPlanId = null">
            {{ t('common.cancel', '取消') }}
          </UButton>
          <UButton
            color="primary"
            :loading="purchasing"
            @click="buyTarget && buyWithBalance(buyTarget)"
          >
            {{ t('common.confirm', '确认') }}
          </UButton>
        </div>
      </template>
    </UModal>
  </div>
</template>
