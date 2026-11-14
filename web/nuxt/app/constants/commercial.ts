import type { BillingMode, SelectOption, TopUpStatus } from '~/utils/types'

/**
 * 商业化层的共享常量与展示辅助。
 *
 * 与 constants/channel.ts 同一定位：把「后端枚举 ↔ 前端展示」的映射集中在一处，
 * 避免同一个状态码在五个页面里各写一遍 switch。
 *
 * ⚠️ 这里的每一项都必须与 Go 侧保持一致，改动前先确认对应文件：
 *   - 计费模式  model/pricing.go 的 BillingMode*
 *   - 支付方式  common/config/payment.go 的 PayMethods 选项
 *   - 计费偏好  controller/subscription.go 的 Preference*
 *   - 订单状态  model/topup.go 的 TopUpStatus*
 */

// ---------------------------------------------------------------------------
// 计费模式
// ---------------------------------------------------------------------------

export const BILLING_MODE_PER_TOKEN: BillingMode = 'per_token'
export const BILLING_MODE_PER_CALL: BillingMode = 'per_call'
export const BILLING_MODE_TIERED: BillingMode = 'tiered_by_tokens'

/**
 * 计费模式选项。同时带 key/text（历史习惯）与 value/label（@nuxt/ui 约定），
 * 这样 USelect 与表格渲染可以直接复用同一份数组。
 */
export const BILLING_MODES: SelectOption[] = [
  {
    key: BILLING_MODE_PER_TOKEN,
    value: BILLING_MODE_PER_TOKEN,
    text: '按量计费',
    label: '按量计费（按 token 数 × 倍率）',
    description: '输入 / 输出分别按倍率计价，缓存命中可单独定价',
    color: 'primary',
  },
  {
    key: BILLING_MODE_PER_CALL,
    value: BILLING_MODE_PER_CALL,
    text: '按次计费',
    label: '按次计费（每次请求固定价）',
    description: '与 token 数无关，适合绘图、语音等固定成本接口',
    color: 'warning',
  },
  {
    key: BILLING_MODE_TIERED,
    value: BILLING_MODE_TIERED,
    text: '阶梯计费',
    label: '阶梯计费（按 token 用量分档）',
    description: '总量落在哪一档就整体用该档单价，最后一档可设无上界',
    color: 'info',
  },
]

/** billing_mode -> 选项 */
export const BILLING_MODE_MAP: Record<string, SelectOption> = BILLING_MODES.reduce(
  (acc, option) => {
    acc[String(option.value)] = option
    return acc
  },
  {} as Record<string, SelectOption>,
)

/** 计费模式的中文展示名，未知值原样返回（便于排查后端新增枚举） */
export function billingModeLabel(mode?: string): string {
  if (!mode) return '按量计费'
  const option = BILLING_MODE_MAP[mode]
  return option ? String(option.text) : mode
}

// ---------------------------------------------------------------------------
// 定价规则状态
// ---------------------------------------------------------------------------

export const PRICING_RULE_STATUS_ENABLED = 1
export const PRICING_RULE_STATUS_DISABLED = 2

export const PRICING_RULE_STATUS_MAP: Record<number, { color: 'success' | 'error' | 'neutral', label: string }> = {
  [PRICING_RULE_STATUS_ENABLED]: { color: 'success', label: '启用' },
  [PRICING_RULE_STATUS_DISABLED]: { color: 'error', label: '禁用' },
}

/** 任意渠道类型（PricingRule.channel_type = -1） */
export const CHANNEL_TYPE_ANY = -1

// ---------------------------------------------------------------------------
// 价格换算
// ---------------------------------------------------------------------------

/**
 * 倍率 -> 每百万 token 的美元价格。
 *
 * 换算依据（与 model/pricing.go 的 usdPerRatioUnit 完全一致）：
 *   ratio = 1 ⇒ $0.002 / 1K tokens ⇒ $0.002 × 1000 = $2 / 1M tokens
 * 所以 usdPer1M = ratio × 0.002 × 1000 = ratio × 2。
 *
 * 之所以把常量写成表达式而不是直接写 2：让「$x / 1M」这个数字可被追溯，
 * 换计价基准时只需改 usdPerRatioUnit 一处。
 */
export const USD_PER_RATIO_UNIT = 0.002 / 1000 // ratio = 1 时每 token 的价格
export const TOKENS_PER_MILLION = 1_000_000

export function renderPricePerMillion(ratio: number | undefined | null, precision = 2): string {
  const value = Number(ratio ?? 0)
  if (!Number.isFinite(value)) return '$0.00'
  const usd = value * USD_PER_RATIO_UNIT * TOKENS_PER_MILLION
  return `$${usd.toFixed(precision)}`
}

/** 倍率 -> 「$X.XX / 1M tokens」，表格里最常用的展示 */
export function renderRatioAsPricePerMillion(ratio: number | undefined | null, precision = 2): string {
  return `${renderPricePerMillion(ratio, precision)} / 1M tokens`
}

/** 按次计费价格 -> 「$X.XX / 次」（后端单位已经是美元） */
export function renderPerCallPrice(price: number | undefined | null, precision = 2): string {
  const value = Number(price ?? 0)
  if (!Number.isFinite(value)) return '$0.00 / 次'
  // 按次价格通常很小（如 $0.01），保留 4 位有效展示更友好，但不超过 4 位
  return `$${value.toFixed(precision)} / 次`
}

/** 分 -> 「$X.XX」 */
export function renderCents(cents: number | undefined | null, precision = 2): string {
  const value = Number(cents ?? 0)
  if (!Number.isFinite(value)) return '$0.00'
  return `$${(value / 100).toFixed(precision)}`
}

/** 分 -> 美元数值（用于输入框回填或与数值型配置比较） */
export function centsToUsd(cents: number | undefined | null): number {
  const value = Number(cents ?? 0)
  return Number.isFinite(value) ? value / 100 : 0
}

/** 美元 -> 分，四舍五入到整数分（避免浮点误差落到数据库） */
export function usdToCents(usd: number | string | undefined | null): number {
  const value = Number(usd ?? 0)
  if (!Number.isFinite(value)) return 0
  return Math.round(value * 100)
}

// ---------------------------------------------------------------------------
// 支付方式
// ---------------------------------------------------------------------------

/**
 * 支付方式的兜底预设。
 *
 * 正常情况下列表由 GET /api/user/topup/info 下发（管理员在 PayMethods 里配置），
 * 这里的预设只在「接口还没返回」或「配置为空」时用于占位，
 * 保证充值页永远不会是一片空白。
 */
export const PAY_METHOD_PRESETS: SelectOption[] = [
  { key: 'alipay', value: 'alipay', text: '支付宝', label: '支付宝', color: '#1677ff', icon: 'i-heroicons-device-phone-mobile' },
  { key: 'wxpay', value: 'wxpay', text: '微信支付', label: '微信支付', color: '#07c160', icon: 'i-heroicons-chat-bubble-oval-left' },
  { key: 'qqpay', value: 'qqpay', text: 'QQ 钱包', label: 'QQ 钱包', color: '#12b7f5', icon: 'i-heroicons-paper-airplane' },
]

// ---------------------------------------------------------------------------
// 订单状态
// ---------------------------------------------------------------------------

export const TOPUP_STATUS_MAP: Record<TopUpStatus, { color: 'success' | 'info' | 'warning' | 'error' | 'neutral', label: string }> = {
  pending: { color: 'warning', label: '待支付' },
  success: { color: 'success', label: '已完成' },
  failed: { color: 'error', label: '失败' },
  expired: { color: 'neutral', label: '已过期' },
  refunded: { color: 'info', label: '已退款' },
}

/** 订单状态展示信息，未知状态回退为「未知」而不是渲染原始字符串 */
export function topUpStatus(status?: string): { color: 'success' | 'info' | 'warning' | 'error' | 'neutral', label: string } {
  if (status && status in TOPUP_STATUS_MAP) {
    return TOPUP_STATUS_MAP[status as TopUpStatus]
  }
  return { color: 'neutral', label: '未知' }
}

// ---------------------------------------------------------------------------
// 订阅相关
// ---------------------------------------------------------------------------

export const SUBSCRIPTION_PREFERENCE_SUBSCRIPTION_FIRST = 'subscription_first'
export const SUBSCRIPTION_PREFERENCE_WALLET_FIRST = 'wallet_first'
export const SUBSCRIPTION_PREFERENCE_SUBSCRIPTION_ONLY = 'subscription_only'
export const SUBSCRIPTION_PREFERENCE_WALLET_ONLY = 'wallet_only'

/**
 * 计费偏好选项。
 * description 是必须的：四种组合的差别对用户并不直观，
 * 页面上要把「先扣哪个、用完了怎么办」讲清楚，否则会被当成收费陷阱。
 */
export const SUBSCRIPTION_PREFERENCES: SelectOption[] = [
  {
    key: SUBSCRIPTION_PREFERENCE_SUBSCRIPTION_FIRST,
    value: SUBSCRIPTION_PREFERENCE_SUBSCRIPTION_FIRST,
    text: '订阅优先',
    label: '订阅优先',
    description: '优先消耗订阅额度，用尽后自动转为扣钱包余额。',
    color: 'primary',
  },
  {
    key: SUBSCRIPTION_PREFERENCE_WALLET_FIRST,
    value: SUBSCRIPTION_PREFERENCE_WALLET_FIRST,
    text: '钱包优先',
    label: '钱包优先',
    description: '优先扣钱包余额，余额不足时才消耗订阅额度。',
    color: 'secondary',
  },
  {
    key: SUBSCRIPTION_PREFERENCE_SUBSCRIPTION_ONLY,
    value: SUBSCRIPTION_PREFERENCE_SUBSCRIPTION_ONLY,
    text: '仅订阅',
    label: '仅订阅',
    description: '只使用订阅额度，额度用尽后请求会被拒绝，不会扣钱包余额。',
    color: 'warning',
  },
  {
    key: SUBSCRIPTION_PREFERENCE_WALLET_ONLY,
    value: SUBSCRIPTION_PREFERENCE_WALLET_ONLY,
    text: '仅钱包',
    label: '仅钱包',
    description: '只扣钱包余额，订阅额度保留不动（适合想攒着以后用）。',
    color: 'neutral',
  },
]

/** 偏好 -> 选项，及取值兜底 */
export function subscriptionPreferenceOption(preference?: string): SelectOption {
  const found = SUBSCRIPTION_PREFERENCES.find((option) => option.value === preference)
  if (found) return found
  return SUBSCRIPTION_PREFERENCES[0] as SelectOption
}

export const SUBSCRIPTION_STATUS_MAP: Record<string, { color: 'success' | 'error' | 'warning' | 'neutral', label: string }> = {
  active: { color: 'success', label: '生效中' },
  expired: { color: 'neutral', label: '已过期' },
  cancelled: { color: 'error', label: '已取消' },
}

export function subscriptionStatus(status?: string): { color: 'success' | 'error' | 'warning' | 'neutral', label: string } {
  if (status && status in SUBSCRIPTION_STATUS_MAP) {
    return SUBSCRIPTION_STATUS_MAP[status] as { color: 'success' | 'error' | 'warning' | 'neutral', label: string }
  }
  return { color: 'neutral', label: '未知' }
}

/** 周期单位展示名 */
export const SUBSCRIPTION_PERIOD_UNITS: SelectOption[] = [
  { key: 'day', value: 'day', text: '天', label: '天' },
  { key: 'month', value: 'month', text: '个月', label: '个月' },
  { key: 'year', value: 'year', text: '年', label: '年' },
  { key: 'custom', value: 'custom', text: '自定义（秒）', label: '自定义（秒）' },
]

/** 把套餐周期渲染成「1 个月」这样的短语 */
export function renderPlanPeriod(unit?: string, value?: number, customSeconds?: number): string {
  switch (unit) {
    case 'day':
      return `${value ?? 1} 天`
    case 'month':
      return `${value ?? 1} 个月`
    case 'year':
      return `${value ?? 1} 年`
    case 'custom':
      return `${customSeconds ?? 0} 秒`
    default:
      return `${value ?? 1} 个月`
  }
}

// ---------------------------------------------------------------------------
// 推广分成
// ---------------------------------------------------------------------------

export const AFF_COMMISSION_STATUS_MAP: Record<string, { color: 'success' | 'warning' | 'error' | 'neutral', label: string }> = {
  pending: { color: 'warning', label: '待结算' },
  settled: { color: 'success', label: '已入账' },
  cancelled: { color: 'error', label: '已撤销' },
}

export function affCommissionStatus(status?: string): { color: 'success' | 'warning' | 'error' | 'neutral', label: string } {
  if (status && status in AFF_COMMISSION_STATUS_MAP) {
    return AFF_COMMISSION_STATUS_MAP[status] as { color: 'success' | 'warning' | 'error' | 'neutral', label: string }
  }
  return { color: 'neutral', label: '未知' }
}

/** 比例 -> 「5%」；后端 rate 是 0-1 的小数 */
export function renderRate(rate: number | undefined | null, precision = 2): string {
  const value = Number(rate ?? 0)
  if (!Number.isFinite(value)) return '0%'
  return `${(value * 100).toFixed(precision)}%`
}

// ---------------------------------------------------------------------------
// 图表
// ---------------------------------------------------------------------------

/** 营收折线图两条线的颜色（与 dashboard 的内联 SVG 用色保持同一套视觉） */
export const REVENUE_SERIES_COLORS = {
  income: '#21ba45',
  consumed: '#f2711c',
} as const
