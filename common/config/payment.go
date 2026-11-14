package config

import "github.com/songquanpeng/one-api/common/env"

// ============================================================================
// 支付 / 充值相关配置
//
// 为什么单独拆一个文件：
//   1. config.go 已经很长，支付是可选功能，集中放在这里便于审计「哪些开关会影响钱」。
//   2. 支付配置**全部**可以用环境变量注入，避免密钥落进数据库选项表被
//      /api/option 接口以明文回显（getOptions 只脱敏名字含 Secret/Token 的键，
//      EpayKey 这类名字不在白名单里，放数据库里迟早泄露）。
//   3. 这里的值在进程启动时读取一次即可：改配置应该走「改环境变量 + 重启」，
//      而不是热更新，避免下单与回调读到不一致的商户号/密钥导致签名对不上。
//
// 安全约定：
//   - EpayKey / WebhookSecret 只参与签名计算，任何函数都不得返回或打印它们；
//   - 密钥为空时必须视为「功能未开启」，否则等价于任何人都能伪造签名。
// ============================================================================

// PayAddress EPay 网关地址，例如 https://pay.example.com（末尾不要带 /）
//
// 为什么需要：EPay 协议本身没有服务发现，下单时要把浏览器 POST 到
// {PayAddress}/submit.php，回调地址也要指向本站，因此必须显式配置。
var PayAddress = env.String("PAY_ADDRESS", "")

// EpayId EPay 商户 ID（协议字段 pid）
//
// 为什么需要：pid 既是下单参数，也是回调验签的组成部分（参与签名），
// 校验回调时我们还会比对 pid 是否等于本商户号，防止别人拿自己的订单回调我们。
var EpayId = env.String("EPAY_ID", "")

// EpayKey EPay 商户密钥，**只用于签名的尾部拼接，绝不写日志/返回前端**
//
// 为什么需要：EPay 的 MD5 签名机制里密钥就是唯一凭证，泄露即可伪造回调白嫖额度。
var EpayKey = env.String("EPAY_KEY", "")

// PayMethods 可用的支付方式列表，JSON 数组，元素结构见 model.PayMethod：
//
//	[{"name":"支付宝","type":"alipay","color":"#1677ff","min_topup":10}]
//
// 为什么做成 JSON 字符串而不是结构体切片：config 包不 import model 包
// （model 依赖 config，反向引用会形成循环依赖），所以只能存字符串，
// 由 controller 侧调用 model.ParsePayMethods 解析。
var PayMethods = env.String("PAY_METHODS", "[]")

// AmountOptions 前端快捷金额按钮（单位：美元），JSON 数组
//
// 为什么放服务端：不同站点客单价差异很大，前端写死会导致每次换站点都要重新构建前端。
var AmountOptions = env.String("AMOUNT_OPTIONS", "[10,20,50,100,200,500]")

// MinTopUp 单笔最小充值金额（美元），是所有支付方式的兜底下限
//
// 为什么需要：低于网关手续费的充值纯亏钱，必须有一个全局下限。
// 单个支付方式还可以用 PayMethod.MinTopUp 单独抬高下限。
var MinTopUp = env.Float64("MIN_TOPUP", 1.0)

// MaxWalletQuota 单个用户钱包额度上限，0 表示不限
//
// 为什么需要：防止被刷或被大额误充后积累巨量额度无法回收；
// 注意它由 model.CompleteTopUp 用「条件 UPDATE」实现，并发回调也不会越过上限。
var MaxWalletQuota int64 = int64(env.Int("MAX_WALLET_QUOTA", 0))

// CustomCallbackAddress 覆盖回调地址前缀；为空则使用 ServerAddress
//
// 为什么需要：反代 / CDN / 内网部署时，进程看到的 ServerAddress 可能与
// 支付网关能访问到的公网地址不同，必须允许手工指定。
var CustomCallbackAddress = env.String("CUSTOM_CALLBACK_ADDRESS", "")

// WebhookSecret 通用 webhook 支付网关的签名密钥（HMAC-SHA256）
//
// 为什么需要：/api/pay/notify 是无人值守的入账入口，没有签名就等于
// 任何人都能给自己加额度。空值视为「未开启」，回调一律拒绝。
var WebhookSecret = env.String("WEBHOOK_SECRET", "")

// AffCommissionEnabled 是否启用邀请分成
//
// 为什么需要：分成是营销功能，默认关闭，避免小站点在没算清楚成本前就漏钱。
//
// 计提口径（重要）：佣金按**被邀请人的实际消费**比例计提
// （见 model.AccrueConsumeCommission），不是注册奖励，也不是充值返现。
// 这样邀请人只能从"真实产生的收入"里分成，无法靠注册小号套利。
var AffCommissionEnabled = env.Bool("AFF_COMMISSION_ENABLED", false)

// AffCommissionRate 佣金比例（0.05 = 5%）
//
// 该值在**计提时快照**写入 aff_commissions.rate，之后修改本配置
// 不会影响历史流水，保证已产生的佣金可审计。
var AffCommissionRate = env.Float64("AFF_COMMISSION_RATE", 0.05)

// AffCommissionMinQuota 单笔消费低于该额度时不计提佣金，0 表示不限制
//
// 为什么需要：小额消费产生的佣金不足以覆盖结算成本，用门槛过滤掉碎流水。
var AffCommissionMinQuota int64 = int64(env.Int("AFF_COMMISSION_MIN_QUOTA", 0))

// AffMinTransferQuota 推广收益转正到钱包的最小额度，0 = 回落为 1 美元等值额度
//
// 为什么需要可配置：固定 1 美元门槛对小额分成很不友好——按 5% 分成的站点，
// 被邀请人要消费满 20 美元才攒够 1 美元佣金，邀请人可能几个月看不到到账，
// 直接打击推广积极性。不同客单价的站点应能各自设定。
//
// 单位是**额度**（quota），不是美元；换算关系见 QUOTA_PER_UNIT。
var AffMinTransferQuota int64 = int64(env.Int("AFF_MIN_TRANSFER_QUOTA", 0))

// TopUpOrderExpireMinutes 订单有效期（分钟），超时后由定时任务置为 expired
//
// 为什么需要：pending 订单长期堆积不仅污染列表，还会让「同一订单重复支付」
// 的排查变得困难；给定时任务一个明确的超时阈值。
var TopUpOrderExpireMinutes = env.Int("TOPUP_ORDER_EXPIRE_MINUTES", 30)

// PaymentEnabled 在线充值（EPay 协议）是否已正确配置。
//
// 为什么需要把「已配置」和「已开启」区分开：EPay 下单需要同时有网关地址与
// 商户号，只有其中一个的情况下单必然失败，提前判定可以让前端直接隐藏入口，
// 而不是让用户点了按钮才收到一个看不懂的报错。
func PaymentEnabled() bool {
	return PayAddress != "" && EpayId != ""
}

// WebhookEnabled 通用 webhook 自建网关是否已配置签名密钥。
//
// 为什么要和 PaymentEnabled 分开判断：两者是互斥的部署形态，
// EPay 走 /api/user/epay/notify（MD5 + 商户密钥），
// 自建网关走 /api/pay/notify（HMAC-SHA256 + WebhookSecret），
// 混用一个开关会让「只配了其中一个」的部署出现难以排查的半可用状态。
func WebhookEnabled() bool {
	return WebhookSecret != ""
}

// CallbackBaseAddress 回调地址前缀：优先 CustomCallbackAddress，其次 ServerAddress。
//
// 为什么需要这一层覆盖：反代 / CDN / 内网部署时，进程看到的 ServerAddress
// 往往和支付网关实际能访问到的公网地址不一致，必须允许手工指定。
//
// 为什么要循环去掉末尾的全部斜杠：实测网关在签名与白名单校验时比对的是
// 完整 URL 字符串，base 末尾带 "/" 拼出来就是 "//api/user/epay/notify"，
// 会导致验签结果与网关记录不一致。这里保证无论配几个斜杠，结果只有一段分隔符。
func CallbackBaseAddress() string {
	base := CustomCallbackAddress
	if base == "" {
		base = ServerAddress
	}
	for len(base) > 1 && base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	return base
}
