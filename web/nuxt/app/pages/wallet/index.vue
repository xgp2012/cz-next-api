<script setup lang="ts">
/**
 * 充值中心。
 *
 * 三条路径，与后端 router/payment.go 一一对应：
 *
 * 1. **在线支付**（pay_enabled = true）
 *    POST /api/user/pay { amount: 分, method } → { url, params }，
 *    然后由浏览器以表单方式 POST 到网关地址。为什么必须用真表单而不是
 *    fetch/axios：EPay 这类网关靠 302 把用户带到收银台，
 *    XHR 拿到 HTML 也不会跳转，还容易撞上跨域限制。
 *
 * 2. **外部充值链接**（pay_enabled = false 且配置了 top_up_link）
 *    站点没接在线支付时，改为把用户送去管理员配置的链接。
 *
 * 3. **兑换码**
 *    POST /api/user/topup { key }，逻辑与 topup/index.vue 完全一致
 *    （这里保留同一套交互，避免用户在两处看到不同的兑换体验）。
 *
 * 订单列表用 usePagedTable 走游标式分页 /api/user/topup/self?p=。
 */
import type { PayMethod, PayOrderResult, TopUp, TopUpInfo } from '~/utils/types'
import { ITEMS_PER_PAGE } from '~/constants/channel'

const { t } = useI18n()
const { user, quotaPerUnit, displayInCurrency, quotaConfig, restoreUser } = useUserStore()
const { success, showError, warning } = useAppToast()

// ---------------------------------------------------------------------------
// 充值配置
// ---------------------------------------------------------------------------
const loadingInfo = ref(false)
const info = ref<TopUpInfo | null>(null)
const selectedAmount = ref<number>(0)
const customAmount = ref<number | undefined>(undefined)
const selectedMethod = ref('')
const paying = ref(false)

/** 兜底：接口未返回档位时给一份保守的默认值，保证页面可用 */
const fallbackAmounts = [10, 20, 50, 100, 200, 500]

const amountOptions = computed(() => {
  const list = info.value?.amount_options
  if (Array.isArray(list) && list.length > 0) return list
  return fallbackAmounts
})

const minTopUp = computed(() => Number(info.value?.min_top_up ?? 1) || 1)

const payMethods = computed<PayMethod[]>(() => {
  const list = info.value?.pay_methods
  if (Array.isArray(list) && list.length > 0) return list
  // 接口没给出配置时用预设占位，但保持 pay_enabled 的判断不变
  return PAY_METHOD_PRESETS.map((preset) => ({
    name: String(preset.text),
    type: String(preset.value),
    color: String(preset.color || ''),
    min_topup: 0,
  }))
})

const payEnabled = computed(() => !!info.value?.pay_enabled)
const topUpLink = computed(() => info.value?.top_up_link || '')

/** 最终用于下单的金额（美元）：自定义值优先，否则用选中的档位 */
const finalAmount = computed(() => {
  const custom = Number(customAmount.value ?? 0)
  if (custom > 0) return custom
  return Number(selectedAmount.value ?? 0)
})

const amountError = computed(() => {
  if (!payEnabled.value) return ''
  if (finalAmount.value <= 0) return '请输入或选择充值金额'
  if (finalAmount.value < minTopUp.value) return `充值金额不能低于 $${minTopUp.value.toFixed(2)}`
  const method = payMethods.value.find((item) => item.type === selectedMethod.value)
  if (method && Number(method.min_topup ?? 0) > 0 && finalAmount.value < Number(method.min_topup)) {
    return `${method.name} 单笔最低充值 $${Number(method.min_topup).toFixed(2)}`
  }
  return ''
})

/** 到账额度预览：与后端 model.USDToQuota 一致（向上取整，保底 1） */
const estimatedQuota = computed(() => {
  if (finalAmount.value <= 0) return 0
  const perUnit = Number(quotaPerUnit.value || 500000) || 500000
  const quota = Math.ceil(finalAmount.value * perUnit)
  return quota < 1 ? 1 : quota
})

async function loadInfo() {
  loadingInfo.value = true
  try {
    const res = await api.get<TopUpInfo>('/api/user/topup/info')
    info.value = res.data || null
    // 默认选中第一个档位与第一种支付方式，减少一次点击
    if (info.value) {
      const first = Number(info.value.amount_options?.[0] ?? 0)
      selectedAmount.value = first > 0 ? first : minTopUp.value
      selectedMethod.value = info.value.pay_methods?.[0]?.type || ''
    }
  } catch (error) {
    showError(error)
  } finally {
    loadingInfo.value = false
  }
}

// ---------------------------------------------------------------------------
// 订单列表
// ---------------------------------------------------------------------------
const {
  rows: orders,
  loading: ordersLoading,
  activePage,
  totalPages,
  loadPage,
  refresh: refreshOrders,
  onPageChange,
} = usePagedTable({
  fetcher: async (startIdx) => {
    const res = await api.get<TopUp[]>('/api/user/topup/self', { query: { p: startIdx } })
    return res.data || []
  },
})

const visibleOrders = computed(() => {
  const start = (activePage.value - 1) * ITEMS_PER_PAGE
  return orders.value.slice(start, start + ITEMS_PER_PAGE)
})

// ---------------------------------------------------------------------------
// 兑换码
// ---------------------------------------------------------------------------
const redeemCode = ref('')
const redeeming = ref(false)

async function pasteCode() {
  try {
    const text = await navigator.clipboard.readText()
    redeemCode.value = text.trim()
  } catch {
    warning(t('topup.redeem_code.paste_error', '无法访问剪贴板，请手动粘贴'))
  }
}

async function redeem() {
  if (!redeemCode.value) {
    warning(t('topup.redeem_code.empty_code', '请输入兑换码！'))
    return
  }
  redeeming.value = true
  try {
    const res = await api.post<number>('/api/user/topup', { key: redeemCode.value })
    const added = res.data ?? 0
    success(`${t('topup.redeem_code.success', '充值成功！')} +${renderQuota(added, quotaConfig.value)}`)
    redeemCode.value = ''
    await restoreUser()
    await refreshOrders()
  } catch (error) {
    showError(error)
  } finally {
    redeeming.value = false
  }
}

// ---------------------------------------------------------------------------
// 支付
// ---------------------------------------------------------------------------

/**
 * 把 params 以表单 POST 提交到网关地址。
 *
 * 关键点：
 * - 表单必须 append 到 document.body 才会真正 submit（游离节点在部分浏览器上无效）；
 * - 提交后立刻移除，避免用户回退时看到一个残留的空表单；
 * - params 全部来自服务端签名结果，前端不做任何增删，否则签名会失效。
 */
function submitPayForm(url: string, params: Record<string, string>) {
  const form = document.createElement('form')
  form.method = 'POST'
  form.action = url
  form.style.display = 'none'
  for (const [key, value] of Object.entries(params)) {
    const input = document.createElement('input')
    input.type = 'hidden'
    input.name = key
    input.value = String(value)
    form.appendChild(input)
  }
  document.body.appendChild(form)
  form.submit()
  document.body.removeChild(form)
}

async function pay() {
  if (!payEnabled.value) {
    goExternalLink()
    return
  }
  if (amountError.value) {
    warning(amountError.value)
    return
  }
  if (!selectedMethod.value) {
    warning('请选择支付方式')
    return
  }
  paying.value = true
  try {
    // 后端金额单位是「分」，这里用整数分传输，避免浮点误差
    const res = await api.post<PayOrderResult>('/api/user/pay', {
      amount: usdToCents(finalAmount.value),
      method: selectedMethod.value,
    })
    const order = res.data
    if (!order?.url || !order?.params) {
      showError('支付网关返回的数据不完整，请联系管理员')
      return
    }
    submitPayForm(order.url, order.params)
    await refreshOrders()
  } catch (error) {
    showError(error)
  } finally {
    paying.value = false
  }
}

function goExternalLink() {
  if (!topUpLink.value) {
    warning(t('topup.redeem_code.no_link', '超级管理员未设置充值链接！'))
    return
  }
  window.open(topUpLink.value, '_blank', 'noopener')
}

function selectAmount(amount: number) {
  selectedAmount.value = amount
  customAmount.value = undefined
}

/** 支付方式按钮的品牌色：后端下发 hex，缺失时回退到主题主色 */
function methodStyle(method: PayMethod) {
  const color = method.color || '#2185d0'
  return { backgroundColor: color, borderColor: color }
}

function statusOf(order: TopUp) {
  return topUpStatus(order.status)
}

onMounted(async () => {
  await loadInfo()
  await loadPage(0)
})
</script>

<template>
  <div class="space-y-4">
    <!-- 钱包概览 -->
    <div class="grid gap-4 md:grid-cols-3">
      <UCard>
        <div class="text-sm text-gray-500">{{ t('topup.get_code.current_quota', '当前可用额度') }}</div>
        <div class="mt-1 text-2xl font-semibold">{{ renderQuota(user?.quota ?? 0, quotaConfig) }}</div>
        <p v-if="displayInCurrency" class="mt-1 text-xs text-gray-400">
          1 美元 = {{ renderNumber(quotaPerUnit) }} 额度
        </p>
      </UCard>
      <UCard>
        <div class="text-sm text-gray-500">已用额度</div>
        <div class="mt-1 text-2xl font-semibold">{{ renderQuota(user?.used_quota ?? 0, quotaConfig) }}</div>
      </UCard>
      <UCard>
        <div class="text-sm text-gray-500">累计请求</div>
        <div class="mt-1 text-2xl font-semibold">{{ renderNumber(user?.request_count ?? 0) }}</div>
      </UCard>
    </div>

    <PageCard :title="t('wallet.title', '充值中心')" :subtitle="t('wallet.subtitle', '在线支付由第三方支付网关处理，到账额度以服务端计算为准')">
      <div class="grid gap-6 lg:grid-cols-2">
        <!-- ------------------------------------------------------------ -->
        <!-- 在线充值                                                      -->
        <!-- ------------------------------------------------------------ -->
        <UCard>
          <template #header>
            <div class="flex items-center justify-between">
              <div class="font-semibold">在线充值</div>
              <UBadge :color="payEnabled ? 'success' : 'warning'" variant="subtle" size="sm">
                {{ payEnabled ? '已开启' : '未开启' }}
              </UBadge>
            </div>
          </template>

          <div v-if="loadingInfo" class="py-6 text-center text-sm text-gray-400">
            {{ t('common.loading', '加载中...') }}
          </div>

          <!-- 未开启在线支付：引导到外部链接 -->
          <div v-else-if="!payEnabled" class="space-y-3">
            <UAlert
              color="warning"
              variant="subtle"
              icon="i-heroicons-information-circle"
              title="本站未开启在线支付"
              :description="topUpLink ? '请点击下方按钮前往管理员配置的充值页面。' : '超级管理员尚未配置充值链接，请改用兑换码充值或联系管理员。'"
            />
            <UButton
              color="primary"
              icon="i-heroicons-arrow-top-right-on-square"
              block
              :disabled="!topUpLink"
              @click="goExternalLink"
            >
              前往充值页面
            </UButton>
          </div>

          <div v-else class="space-y-4">
            <!-- 金额档位 -->
            <div>
              <div class="mb-2 text-sm text-gray-600">选择金额（美元）</div>
              <div class="flex flex-wrap gap-2">
                <UButton
                  v-for="amount in amountOptions"
                  :key="amount"
                  :color="selectedAmount === amount && !customAmount ? 'primary' : 'neutral'"
                  :variant="selectedAmount === amount && !customAmount ? 'solid' : 'outline'"
                  size="sm"
                  @click="selectAmount(amount)"
                >
                  ${{ amount }}
                </UButton>
              </div>
            </div>

            <UFormField label="自定义金额（美元）" :help="`最低 $${minTopUp.toFixed(2)}`">
              <UInputNumber
                v-model="customAmount"
                :min="0"
                :step="1"
                :step-snapping="false"
                placeholder="输入自定义金额"
                class="w-full"
              />
            </UFormField>

            <!-- 支付方式 -->
            <div>
              <div class="mb-2 text-sm text-gray-600">选择支付方式</div>
              <div class="flex flex-wrap gap-2">
                <UButton
                  v-for="method in payMethods"
                  :key="method.type"
                  :variant="selectedMethod === method.type ? 'solid' : 'outline'"
                  :color="selectedMethod === method.type ? 'primary' : 'neutral'"
                  size="sm"
                  :style="selectedMethod === method.type ? methodStyle(method) : undefined"
                  @click="selectedMethod = method.type"
                >
                  {{ method.name }}
                  <span v-if="Number(method.min_topup ?? 0) > 0" class="ml-1 text-xs opacity-80">
                    起充 ${{ Number(method.min_topup).toFixed(2) }}
                  </span>
                </UButton>
              </div>
            </div>

            <!-- 到账预览 -->
            <div class="rounded-md border border-gray-100 bg-gray-50 p-3 text-sm">
              <div class="flex items-center justify-between">
                <span class="text-gray-500">应付金额</span>
                <span class="font-medium">${{ finalAmount > 0 ? finalAmount.toFixed(2) : '0.00' }}</span>
              </div>
              <div class="mt-1 flex items-center justify-between">
                <span class="text-gray-500">预计到账</span>
                <span class="font-medium">{{ renderQuota(estimatedQuota, quotaConfig) }}</span>
              </div>
              <p v-if="amountError" class="mt-2 text-xs text-red-500">{{ amountError }}</p>
            </div>

            <UButton
              color="primary"
              icon="i-heroicons-credit-card"
              block
              :loading="paying"
              :disabled="!!amountError"
              @click="pay"
            >
              {{ paying ? '正在创建订单...' : '立即支付' }}
            </UButton>
            <p class="text-xs text-gray-400">
              支付完成后额度由网关回调自动入账，通常几秒内到账；若长时间未到账请把订单号提供给管理员核销。
            </p>
          </div>
        </UCard>

        <!-- ------------------------------------------------------------ -->
        <!-- 兑换码                                                        -->
        <!-- ------------------------------------------------------------ -->
        <UCard>
          <template #header>
            <div class="font-semibold">{{ t('topup.redeem_code.title', '兑换码充值') }}</div>
          </template>
          <div class="space-y-3">
            <UInput
              v-model="redeemCode"
              :placeholder="t('topup.redeem_code.placeholder', '请输入兑换码')"
              class="w-full"
              @keyup.enter="redeem"
            />
            <div class="flex gap-2">
              <UButton color="neutral" variant="outline" icon="i-heroicons-clipboard" @click="pasteCode">
                {{ t('topup.redeem_code.paste', '粘贴') }}
              </UButton>
              <UButton color="primary" :loading="redeeming" class="flex-1" @click="redeem">
                {{ redeeming ? t('topup.redeem_code.submitting', '兑换中...') : t('topup.redeem_code.submit', '立即兑换') }}
              </UButton>
            </div>
            <p class="text-xs text-gray-400">
              兑换码由管理员生成，一次性使用；兑换成功后额度立即计入钱包余额。
            </p>
          </div>
        </UCard>
      </div>
    </PageCard>

    <!-- ------------------------------------------------------------------ -->
    <!-- 订单历史                                                            -->
    <!-- ------------------------------------------------------------------ -->
    <PageCard :title="t('wallet.orders.title', '充值订单')">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b border-gray-200 text-left text-gray-500">
            <tr>
              <th class="p-2">订单号</th>
              <th class="p-2">金额</th>
              <th class="oa-hide-on-mobile p-2">实付</th>
              <th class="p-2">到账额度</th>
              <th class="oa-hide-on-mobile p-2">支付方式</th>
              <th class="p-2">状态</th>
              <th class="oa-hide-on-mobile p-2">创建时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="order in visibleOrders" :key="order.id" class="border-b border-gray-100 hover:bg-gray-50">
              <td class="p-2">
                <code class="text-xs">{{ order.trade_no }}</code>
              </td>
              <td class="p-2 whitespace-nowrap">{{ renderCents(order.amount_cents) }}</td>
              <td class="oa-hide-on-mobile p-2 whitespace-nowrap text-gray-500">
                {{ renderCents(order.money_cents) }}
              </td>
              <td class="p-2 whitespace-nowrap">{{ renderQuota(order.quota, quotaConfig) }}</td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">
                {{ order.payment_method || '—' }}
                <span v-if="order.payment_provider" class="text-xs text-gray-400">（{{ order.payment_provider }}）</span>
              </td>
              <td class="p-2">
                <StatusBadge :color="statusOf(order).color" :label="statusOf(order).label" />
              </td>
              <td class="oa-hide-on-mobile p-2 text-gray-500">{{ timestamp2string(order.create_time) }}</td>
            </tr>
            <tr v-if="visibleOrders.length === 0">
              <td colspan="7" class="p-8 text-center text-gray-400">
                {{ ordersLoading ? t('common.loading', '加载中...') : t('common.none', '无') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="mt-4 flex flex-wrap items-center justify-between gap-2">
        <UButton color="neutral" variant="outline" size="sm" :loading="ordersLoading" @click="refreshOrders">
          {{ t('common.refresh', '刷新') }}
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
