/**
 * 后端 API 的数据结构。
 * 全部来自 Go 侧的 model/*.go，字段名保持后端 JSON 的 snake_case 原样，
 * 避免在前后端之间做无谓的命名映射（这是原有 React 实现的做法）。
 */

/** 统一响应包：{ success, message, data } —— 控制面大量使用 HTTP 200 + success:false */
export interface ApiResponse<T = any> {
  success: boolean
  message?: string
  data?: T
}

/** 用户角色：0 游客 / 1 普通 / 10 管理员 / 100 超管 */
export const ROLE_GUEST = 0
export const ROLE_COMMON = 1
export const ROLE_ADMIN = 10
export const ROLE_ROOT = 100

export interface User {
  id: number
  username: string
  display_name?: string
  role: number
  status: number
  email?: string
  github_id?: string
  wechat_id?: string
  lark_id?: string
  access_token?: string
  quota: number
  used_quota: number
  request_count: number
  group: string
  aff_code?: string
  inviter_id?: number
}

/** /api/status 返回的运行时信息，前端大量依赖（系统名、Logo、开关位） */
export interface Status {
  version: string
  start_time: number
  email_verification: boolean
  github_oauth: boolean
  github_client_id: string
  lark_client_id: string
  system_name: string
  logo: string
  footer_html: string
  wechat_qrcode: string
  wechat_login: boolean
  server_address: string
  turnstile_check: boolean
  turnstile_site_key: string
  top_up_link: string
  chat_link: string
  quota_per_unit: number
  display_in_currency: boolean
  oidc: boolean
  oidc_client_id: string
  oidc_well_known: string
  oidc_authorization_endpoint: string
  oidc_token_endpoint: string
  oidc_userinfo_endpoint: string
}

export interface Channel {
  id: number
  type: number
  key: string
  status: number
  name: string
  created_time: number
  test_time: number
  response_time: number
  balance: number
  balance_updated_time: number
  models: string
  group: string
  used_quota: number
  model_mapping: string
  system_prompt?: string
  /** OpenAPI SDK 中要求的 Base URL（仅部分渠道类型使用） */
  base_url?: string
  priority: number
  weight: number
  config: string
  other: string
  other_info?: string
  // 以下为前端本地派生字段
  test_model?: string
  model_options?: SelectOption[]
  deleted?: boolean
}

export interface Token {
  id: number
  user_id: number
  key: string
  status: number
  name: string
  created_time: number
  accessed_time: number
  expired_time: number
  remain_quota: number
  unlimited_quota: boolean
  used_quota: number
  models?: string | string[]
  subnet: string
  deleted?: boolean
}

export interface Redemption {
  id: number
  user_id: number
  key: string
  status: number
  name: string
  quota: number
  created_time: number
  redeemed_time: number
  count?: number
  deleted?: boolean
}

export interface Log {
  id: number
  user_id: number
  created_at: number
  type: number
  content: string
  username: string
  token_name: string
  model_name: string
  quota: number
  prompt_tokens: number
  completion_tokens: number
  channel: number
  request_id: string
  elapsed_time: number
  is_stream: boolean
  system_prompt_reset: boolean
  /** 附加信息 JSON 字符串（旧实现在详情里展示） */
  other?: string
  deleted?: boolean
}

export interface OptionItem {
  key: string
  value: string
}

/**
 * 控制台的按日聚合行。
 *
 * 注意：后端 model.LogStatistic 没有 json tag，序列化后字段名是 Go 的导出名
 * （Day / ModelName / ...），因此这里必须保持大驼峰，不能改成 snake_case。
 */
export interface LogStatistic {
  Day: string
  ModelName: string
  RequestCount: number
  Quota: number
  PromptTokens: number
  CompletionTokens: number
}

/** 兼容旧命名的别名 */
export type DashboardRow = LogStatistic

/**
 * 通用下拉选项。
 * 同时提供 text/key（历史代码习惯）与 label/value（@nuxt/ui 的 USelect /
 * USelectMenu 约定），这样两套控件都能直接用同一个数组，无需各自 map 一遍。
 */
export interface SelectOption {
  key: string | number
  text: string
  value: string | number
  label?: string
  [key: string]: any
}

/* ==========================================================================
 * 商业化层（定价 / 充值 / 订阅 / 推广 / 营收）
 *
 * 字段名一律保持 Go 侧 json tag 的 snake_case 原样，理由与文件开头相同。
 * 金额口径（唯一约定，改动前请先读 model/topup.go 的注释）：
 *   - *_cents  ：美元「分」，100 = $1.00，后端只用整数分传输，前端不做浮点存储；
 *   - *_quota  ：系统额度，1 美元 = quota_per_unit（默认 500000）；
 *   - *_ratio  ：倍率，ratio = 1 ⇔ $0.002 / 1K tokens ⇔ $2 / 1M tokens。
 * ========================================================================== */

// ---------------------------------------------------------------------------
// 定价（价格墙 + 定价规则）
// ---------------------------------------------------------------------------

/** 计费模式，与 Go 侧 model.BillingMode 严格一致 */
export type BillingMode = 'per_token' | 'per_call' | 'tiered_by_tokens'

/** 阶梯档位：up_to_tokens = 0 表示「无上界」，只允许出现在最后一档 */
export interface PriceTier {
  tier: number
  up_to_tokens: number
  input_ratio: number
  output_ratio: number
  cache_hit_ratio?: number
  cache_write_ratio?: number
}

/** 分时定价窗口（0-23 小时，左闭右开；start == end 视为全天） */
export interface PriceTimeWindow {
  start_hour: number
  end_hour: number
}

/**
 * 价格墙上的一行。
 *
 * 后端 GET /api/pricing 有两个版本的数据形态，前端两种都要能消费：
 *   - 新形态：直接给已解析好的 models 数组（每组倍率一行，便于表格展示）；
 *   - 现状形态：给 rules（规则数组）+ legacy_ratios（未命中规则时回落到旧倍率），
 *     由页面自行展开成多行。因此这里所有字段都是可选的。
 */
export interface PricingWallItem {
  model: string
  billing_mode?: BillingMode
  input_ratio?: number
  output_ratio?: number
  cache_hit_ratio?: number
  cache_write_ratio?: number
  per_call_price?: number
  tier?: number
  cached?: boolean
  description?: string
}

/** GET /api/pricing 的 data；兼容「models 数组」与「rules + legacy_ratios」两种形态 */
export interface PricingWallResponse {
  models?: PricingWallItem[]
  rules?: PricingRule[]
  legacy_ratios?: Record<string, number>
  group_ratio?: number
  group_ratios?: Record<string, number>
  quota_per_unit?: number
  ratio_explanation?: string
}

/** 一条定价规则（管理员 CRUD 用；time_windows / tiers 后端落库为 JSON 字符串） */
export interface PricingRule {
  id: number
  name: string
  note?: string
  /** 1 启用 / 2 禁用，与 model.PricingRuleStatus* 对应 */
  status: number
  /** 支持 "prefix:*" 通配，空 = 任意模型 */
  model: string
  /** 用户分组，空 = 任意；支持逗号分隔多个 */
  group?: string
  /** -1 = 任意渠道类型 */
  channel_type: number
  /** JSON 数组字符串：[{start_hour,end_hour}] */
  time_windows?: string
  billing_mode: BillingMode
  input_ratio: number
  output_ratio: number
  cache_hit_ratio?: number
  cache_write_ratio?: number
  /** 单位：美元（按次计费专用） */
  per_call_price?: number
  /** JSON 数组字符串：[{tier,up_to_tokens,input_ratio,output_ratio}] */
  tiers?: string
  /** 越大越优先 */
  priority: number
  /** 同优先级内的小序号 */
  order: number
  created_time?: number
  updated_time?: number
  /** 仅前端使用：本地删除后的置灰标记 */
  deleted?: boolean
}

// ---------------------------------------------------------------------------
// 钱包充值
// ---------------------------------------------------------------------------

/** 订单状态，与 Go 侧 model.TopUpStatus* 对应 */
export type TopUpStatus = 'pending' | 'success' | 'failed' | 'expired' | 'refunded'

/** 一种支付方式（后端 PayMethods 选项解析结果） */
export interface PayMethod {
  name: string
  /** 支付方式标识，下单时作为 method 传回，如 alipay / wxpay */
  type: string
  /** 前端的品牌色（后端下发，直接用） */
  color?: string
  /** 该方式的单笔最低金额（美元） */
  min_topup?: number
}

/** GET /api/user/topup/info 的 data：充值页所需的全部服务端配置 */
export interface TopUpInfo {
  pay_methods: PayMethod[]
  /** 快捷金额档位（美元），例如 [10, 20, 50] */
  amount_options: number[]
  /** 全局起充金额（美元） */
  min_top_up: number
  /** 是否开启在线充值；false 时应改为展示 top_up_link */
  pay_enabled: boolean
  /** 管理员配置的外部充值链接 */
  top_up_link?: string
  max_wallet_quota?: number
  quota_per_unit?: number
}

/** 充值订单（GET /api/user/topup/self 的行） */
export interface TopUp {
  id: number
  user_id: number
  trade_no: string
  /** 购买金额（分） */
  amount_cents: number
  /** 实付金额（分），含分组折扣时与 amount_cents 不同 */
  money_cents: number
  /** 本次入账额度（服务端唯一计算） */
  quota: number
  payment_method: string
  payment_provider: string
  status: TopUpStatus
  create_time: number
  complete_time: number
  remark?: string
  group_ratio?: number
}

/** POST /api/user/pay 的 data：把 params 以表单 POST 提交到 url 即进入收银台 */
export interface PayOrderResult {
  trade_no: string
  url: string
  params: Record<string, string>
}

// ---------------------------------------------------------------------------
// 订阅套餐
// ---------------------------------------------------------------------------

/** 套餐周期单位 */
export type PeriodUnit = 'day' | 'month' | 'year' | 'custom'

/** 额度重置周期 */
export type QuotaResetPeriod = 'never' | 'daily' | 'weekly' | 'monthly'

export interface SubscriptionPlan {
  id: number
  title: string
  subtitle?: string
  /** 价格（美元分） */
  price_cents: number
  currency?: string
  duration_unit: PeriodUnit
  duration_value: number
  custom_seconds?: number
  enabled: boolean
  sort_order?: number
  /** 本周期可用额度，0 = 不限量 */
  total_quota: number
  quota_reset_period?: QuotaResetPeriod
  /** 购买后授予的用户分组，到期回落 */
  upgrade_group?: string
  downgrade_group?: string
  allow_balance_pay?: boolean
  allow_wallet_overflow?: boolean
  auto_renew_enabled?: boolean
  grace_period_days?: number
  max_purchase_per_user?: number
  created_time?: number
  updated_time?: number
}

/** GET /api/subscription/plans 的 data（plans 也可能直接是数组，见页面归一化） */
export interface SubscriptionPlansResponse {
  plans: SubscriptionPlan[]
  quota_per_unit?: number
  pay_enabled?: boolean
}

/** 订阅实例状态 */
export type SubscriptionStatus = 'active' | 'expired' | 'cancelled'

/** 订阅来源 */
export type SubscriptionSource = 'order' | 'balance' | 'admin'

/** 用户持有的订阅实例（一个周期） */
export interface UserSubscription {
  id: number
  user_id: number
  plan_id: number
  /** 本周期总额度，0 = 不限量 */
  amount_total: number
  amount_used: number
  start_time: number
  end_time: number
  /** 续费失败后的宽限截止（0 = 无宽限） */
  grace_end_time?: number
  status: SubscriptionStatus
  source?: SubscriptionSource
  last_reset_time?: number
  next_reset_time?: number
  upgrade_group?: string
  prev_user_group?: string
  downgrade_group?: string
  auto_renew?: boolean
  renew_attempts?: number
  allow_wallet_overflow?: boolean
  created_time?: number
}

/** 计费偏好：订阅优先 / 钱包优先 / 仅订阅 / 仅钱包 */
export type BillingPreference
  = | 'subscription_first'
    | 'wallet_first'
    | 'subscription_only'
    | 'wallet_only'

/** GET /api/subscription/self 的 data */
export interface SelfSubscriptionResponse {
  subscriptions: UserSubscription[]
  /** plan_id -> 套餐详情，用于在订阅卡片上显示标题 */
  plans?: Record<string, SubscriptionPlan> | SubscriptionPlan[]
  preference: BillingPreference
}

// ---------------------------------------------------------------------------
// 推广 / 邀请分成
// ---------------------------------------------------------------------------

/** GET /api/user/aff 的 data */
export interface AffInfo {
  aff_code: string
  aff_link: string
  invite_count: number
  /** 已产生但未转正的佣金 */
  pending_quota: number
  /** 历史累计佣金（永不减少） */
  history_quota: number
  /** 当前分成比例（0-1） */
  commission_rate?: number
  /** 单次转正的最小额度 */
  min_transfer?: number
  /** 分成功能是否开启 */
  enabled?: boolean
}

/** 一笔佣金流水的状态 */
export type AffCommissionStatus = 'pending' | 'settled' | 'cancelled'

/** 一笔邀请佣金流水（GET /api/user/aff/commissions 的行） */
export interface AffCommission {
  id: number
  inviter_id: number
  invitee_id: number
  /** consume = 消费分成 / topup = 充值分成 */
  source_type?: string
  /** 被邀请人本次消费的原始额度 */
  source_quota: number
  /** 本次分成比例快照 */
  rate: number
  commission_quota: number
  status: AffCommissionStatus
  log_id?: number
  trade_no?: string
  created_time: number
  settled_time?: number
  /** 后端可能顺带回带被邀请人用户名，缺失时前端回退成 id */
  invitee_name?: string
  invitee_username?: string
}

/**
 * 我邀请的一个用户（GET /api/user/aff/invitees 的行）。
 *
 * 后端只暴露 id 与 username，不含邮箱等联系方式——
 * 邀请关系不足以成为查看他人隐私的授权。
 */
export interface AffInvitee {
  id: number
  username: string
  /** 该被邀请人累计消费的额度 */
  quota: number
  /** 该被邀请人累计贡献的佣金 */
  commission_quota: number
  /** 贡献的佣金笔数 */
  commission_count: number
}

// ---------------------------------------------------------------------------
// 营收看板
// ---------------------------------------------------------------------------

/** 单日营收 */
export interface RevenueDaily {
  date: string
  /** 当日入账金额（单位：分） */
  income_cents: number
  /** 当日消耗额度 */
  consumed_quota: number
  request_count: number
  active_users?: number
  /** 当日有成功入账的去重用户数 */
  paying_users?: number
}

export interface RevenueModelRow {
  model: string
  quota: number
  count: number
}

export interface RevenueUserRow {
  user_id?: number
  username: string
  quota: number
  count?: number
}

/**
 * GET /api/admin/revenue 的 data。
 *
 * 口径（与后端 model.RevenueStats 一致）：
 *   - 金额字段一律以 `_cents` 结尾且为整数分
 *   - 消耗字段以 `_quota` 结尾，单位为额度（除以 QuotaPerUnit 得美元）
 *   - 比例字段为 0~1 小数
 */
export interface RevenueStats {
  days?: number

  total_income_cents?: number
  total_consumed_quota?: number
  /** 消耗折算成钱（分），用于和收入同口径比较 */
  total_consumed_cents?: number
  /** 账面毛利（分），可能为负 */
  gross_margin_cents?: number
  /** 毛利率 = 毛利 / 收入，收入为 0 时为 0 */
  gross_margin?: number
  /** 人均收入（分） */
  arpu_cents?: number

  paying_users?: number
  consuming_users?: number

  active_subscriptions?: number
  auto_renew_subscriptions?: number
  renew_rate?: number

  daily?: RevenueDaily[]
  top_models?: RevenueModelRow[]
  top_users?: RevenueUserRow[]
  /** 后端口径说明，可直接展示 */
  note?: string
}
