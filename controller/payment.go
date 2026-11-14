package controller

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/ctxkey"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/model"
)

// ============================================================================
// 充值 / 支付控制器
//
// 设计原则（与 model/topup.go 的统一金额语义配套）：
//  1. 下单接口只创建 pending 订单，**绝不**在这里加额度——只有网关回调能入账。
//  2. 所有回调走 model.CompleteTopUp 这一条路径，它是幂等且带行锁的，
//     控制器不重复实现「查状态-改状态-加额度」的逻辑。
//  3. 回调接口写给网关看的是**纯文本** success/fail（EPay 协议如此），
//     不能返回 JSON，否则网关会判定回调失败并无限重试。
//  4. 业务错误一律 HTTP 200 + {success:false,message}，只有鉴权失败才用 401。
//  5. 密钥（EpayKey / WebhookSecret）只参与计算，**不落日志、不返回前端**。
// ============================================================================

// epaySignFields EPay 参与签名的字段
//
// 为什么写死列表而不是「把 params 里所有字段都签」：EPay 只对协议约定的
// 这几个字段签名，多余的字段（比如 sign_type 本身、网关回带的自定义字段）
// 参与签名会导致与网关算出的结果不一致，回调永远验不过。
var epaySignFields = []string{
	"money", "name", "notify_url", "out_trade_no",
	"pid", "return_url", "sitename", "trade_status", "type",
}

// epaySign 计算 EPay 协议签名。
//
// 规则（EPay 官方）：
//  1. 取参与签名的字段，**跳过值为空的字段**；
//  2. 按参数名的 ASCII 字典序升序排列；
//  3. 拼成 k1=v1&k2=v2&... 的形式（value **不做** URL 编码，签名对象是原始值）；
//  4. 末尾**直接**拼接商户密钥（不是 key=密钥）；
//  5. 对整个字符串做 MD5，输出小写十六进制。
//
// 为什么要这么严格：签名是回调前唯一的安全边界，字段顺序/空值处理
// 只要差一点，网关回调就会验签失败，表现为「用户已付款但额度没到」。
func epaySign(params map[string]string, key string) string {
	base := epaySignBase(params)
	if base == "" {
		// 没有任何有效字段时，签名就是 MD5(密钥)，这样空参数集合也不会算出空串
		sum := md5.Sum([]byte(key))
		return hex.EncodeToString(sum[:])
	}
	sum := md5.Sum([]byte(base + key))
	return hex.EncodeToString(sum[:])
}

// epaySignBase 返回签名前的待签名串（不含密钥），仅供 epaySign 与测试使用
//
// 为什么要单独抽出来：测试需要按「同样的规则但独立实现」重算一遍来锁定
// 字段顺序与空值排除行为，如果测试直接调用 epaySign 就变成了自证。
func epaySignBase(params map[string]string) string {
	pairs := make([]string, 0, len(epaySignFields))
	for _, field := range epaySignFields {
		value, ok := params[field]
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		pairs = append(pairs, field+"="+value)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

// epayVerify 校验 EPay 回调签名
//
// 为什么要自己再取一次 sign：params 里可能带有 sign/sign_type（它们不参与签名），
// 这里把它们剔除后重算，再与网关送来的 sign 恒定时间比较。
// 返回值 false 时调用方必须回复 fail，不能回复 success。
func epayVerify(params map[string]string, key string) bool {
	sign := strings.ToLower(strings.TrimSpace(params["sign"]))
	if sign == "" || key == "" {
		// 密钥没配置时一律拒绝：空密钥等于任何人都能伪造签名
		return false
	}
	expected := epaySign(params, key)
	return hmac.Equal([]byte(expected), []byte(sign))
}

// ---------------------------------------------------------------------------
// 金额计算
// ---------------------------------------------------------------------------

// topUpGroupRatio 返回下单时的分组折扣倍率。
//
// 现状：恒为 1.0。
// 为什么先不做折扣：项目当前的 config 里没有 GroupRatioForTopUp 这样的配置项，
// 硬编码一个「分组优惠」会让运维既看不到也改不了，属于隐性行为；
// 而拿 relay 的用量倍率（GroupRatio）当充值折扣是错误语义——
// 那是「同一个 token 消耗多少额度」的系数，二者乘在一起会让充值金额
// 与到账额度的关系变得不可解释。
//
// 扩展点：等 config 增加 GroupRatioForTopUp map 后，这里改成按 userGroup 查表即可，
// 调用方（下单 / 报价）都无需改动，因为倍率的语义已经收敛到这一个函数。
func topUpGroupRatio(userGroup string) float64 {
	return 1.0
}

// normalizeTopUpAmountCents 校验并归一化下单金额（单位：分）
//
// 为什么单独抽函数：下单（CreateEpayOrder）与报价（GetPayAmount）必须用
// **完全一致**的校验规则，否则会出现「报价成功但下单失败」的割裂体验；
// 同时它也是纯函数，便于单测。
func normalizeTopUpAmountCents(amountCents int64) (int64, error) {
	if amountCents <= 0 {
		return 0, errors.New("充值金额必须大于 0")
	}
	minCents := int64(config.MinTopUp*100 + 0.5)
	if minCents < 1 {
		minCents = 1
	}
	if amountCents < minCents {
		// 用美元展示，贴合用户在前端看到的输入单位
		return 0, fmt.Errorf("充值金额不能低于 %.2f 美元", float64(minCents)/100.0)
	}
	return amountCents, nil
}

// findPayMethod 按 type 查找支付方式配置
//
// 为什么找不到就直接拒绝：未在 PayMethods 中登记的类型无法确定
// 名称/最低金额/网关参数，放行只会产生无法支付也无法核销的脏订单。
func findPayMethod(methods []model.PayMethod, methodType string) (*model.PayMethod, error) {
	methodType = strings.TrimSpace(methodType)
	if methodType == "" {
		return nil, errors.New("请选择支付方式")
	}
	for i := range methods {
		if methods[i].Type == methodType {
			return &methods[i], nil
		}
	}
	return nil, errors.New("不支持的支付方式：" + methodType)
}

// checkMethodMinTopUp 校验该支付方式自身的起充金额
//
// 为什么要二级校验：全局 MinTopUp 只是兜底，某些渠道（如微信小额的固定费率）
// 的起充门槛远高于全局值，必须按方式单独把关。
func checkMethodMinTopUp(method *model.PayMethod, amountCents int64) error {
	if method == nil || method.MinTopUp <= 0 {
		return nil
	}
	minCents := int64(method.MinTopUp*100 + 0.5)
	if amountCents < minCents {
		return fmt.Errorf("%s 单笔最低充值 %.2f 美元", method.Name, method.MinTopUp)
	}
	return nil
}

// centsToMoneyString 把「分」格式化成 EPay 要求的「元」字符串
//
// 为什么不用 fmt.Sprintf("%.2f", cents/100)：浮点除法在整数分转元时
// 可能出现 9.999999999 这样的尾数，格式化后变成 "10.00" 还行，
// 但像 1234 分这样的大数在某些值上会算出 "12.3399999" → 用整数拆位最稳。
// EPay 的签名对 money 字符串敏感，一位不对就验签失败。
func centsToMoneyString(cents int64) string {
	if cents < 0 {
		cents = 0
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// ---------------------------------------------------------------------------
// 1. 下单：POST /api/user/pay
// ---------------------------------------------------------------------------

// PayRequest 下单请求体
//
// 字段名与前端 web/air 的 TopUp 页面保持一致（amount / method），
// 单位统一为「分」，避免浮点金额在网络传输中丢精度。
type PayRequest struct {
	// Amount 购买金额，单位：分（= AmountCents）。例如 1000 表示 10.00 美元。
	Amount int64 `json:"amount"`
	// Method 支付方式 type，如 alipay / wxpay，必须出现在 config.PayMethods 中。
	Method string `json:"method"`
}

// CreateEpayOrder 创建 EPay 支付订单并返回提交表单所需的数据。
//
// 流程：校验金额与支付方式 → 计算入账额度 → 落 pending 订单 → 算签名 → 返回表单字段。
// 注意：这里**不**加额度，额度只在回调验签通过后由 model.CompleteTopUp 入账。
func CreateEpayOrder(c *gin.Context) {
	ctx := c.Request.Context()
	userId := c.GetInt(ctxkey.Id)

	if !config.PaymentEnabled() {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未开启在线充值",
		})
		return
	}

	var req PayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数解析失败：" + err.Error(),
		})
		return
	}

	amountCents, err := normalizeTopUpAmountCents(req.Amount)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	method, err := findPayMethod(model.ParsePayMethods(config.PayMethods), req.Method)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if err := checkMethodMinTopUp(method, amountCents); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// 分组倍率：当前恒为 1，详见 topUpGroupRatio 的说明。
	group, _ := model.CacheGetUserGroup(userId)
	ratio := topUpGroupRatio(group)
	if ratio <= 0 {
		ratio = 1
	}
	// MoneyCents 为实际应付金额，AmountCents 为购买金额；四舍五入到分
	moneyCents := int64(float64(amountCents)*ratio + 0.5)
	if moneyCents <= 0 {
		moneyCents = amountCents
	}

	// 入账额度由服务端唯一计算：用户买到的额度永远按 AmountCents 折算，
	// 折扣体现在「少付钱」而不是「多给额度」，这样对账时 AmountCents×QuotaPerUnit 恒等。
	quota := model.USDToQuota(float64(amountCents) / 100.0)

	tradeNo := model.GenerateTopUpTradeNo(userId)
	topUp := &model.TopUp{
		UserId:          userId,
		TradeNo:         tradeNo,
		AmountCents:     amountCents,
		MoneyCents:      moneyCents,
		Quota:           quota,
		PaymentMethod:   method.Type,
		PaymentProvider: model.PaymentProviderEpay,
		Status:          model.TopUpStatusPending,
		GroupRatio:      1,
	}
	if err := model.CreateTopUp(topUp); err != nil {
		logger.Error(ctx, fmt.Sprintf("创建充值订单失败 user=%d amount=%d: %s", userId, amountCents, err.Error()))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "创建订单失败：" + err.Error(),
		})
		return
	}

	base := config.CallbackBaseAddress()
	notifyURL := base + "/api/user/epay/notify"
	returnURL := base + "/console/topup?trade_no=" + tradeNo

	params := map[string]string{
		"pid":          config.EpayId,
		"type":         method.Type,
		"out_trade_no": tradeNo,
		"notify_url":   notifyURL,
		"return_url":   returnURL,
		"name":         method.Name,
		"money":        centsToMoneyString(moneyCents),
		"sitename":     config.SystemName,
	}
	params["sign"] = epaySign(params, config.EpayKey)
	params["sign_type"] = "MD5"

	logger.Info(ctx, fmt.Sprintf("用户 %d 创建充值订单 %s：金额 %s 美元，额度 %d，方式 %s",
		userId, tradeNo, centsToMoneyString(amountCents), quota, method.Type))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"trade_no": tradeNo,
			"url":      strings.TrimSuffix(config.PayAddress, "/") + "/submit.php",
			"params":   params,
		},
	})
}

// ---------------------------------------------------------------------------
// 2. EPay 异步通知：GET|POST /api/user/epay/notify
// ---------------------------------------------------------------------------

// EpayNotify 处理 EPay 支付结果回调。
//
// **必须是纯文本响应**：EPay 只有在响应体包含 "success" 时才认为通知成功，
// 否则会按 30s/分钟/小时 的节奏反复重试，返回 JSON 会导致重复通知。
// 幂等：model.CompleteTopUp 对已完成订单返回 alreadyDone=true 且 err=nil，
// 这时仍然回 success，让网关停止重试。
func EpayNotify(c *gin.Context) {
	ctx := c.Request.Context()

	// 网关的 Content-Type 通常是 application/x-www-form-urlencoded，
	// 但个别实现会用 multipart 或 GET 查询串，FormValue 三者都能覆盖。
	if err := c.Request.ParseForm(); err != nil {
		logger.Error(ctx, "EPay 回调表单解析失败: "+err.Error())
		epayReplyFail(c)
		return
	}

	params := make(map[string]string, len(c.Request.Form))
	for k, v := range c.Request.Form {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}

	tradeNo := params["out_trade_no"]
	// 只记录订单号与交易状态，绝不打印 sign / 任何密钥
	logger.Info(ctx, fmt.Sprintf("收到 EPay 回调 trade_no=%s trade_status=%s", tradeNo, params["trade_status"]))

	if !epayVerify(params, config.EpayKey) {
		logger.Error(ctx, fmt.Sprintf("EPay 回调验签失败 trade_no=%s", tradeNo))
		epayReplyFail(c)
		return
	}

	// 幂等：非成功状态直接忽略（EPay 也会推 TRADE_CLOSED 等状态）
	if params["trade_status"] != "TRADE_SUCCESS" {
		logger.Info(ctx, fmt.Sprintf("EPay 回调状态非成功，忽略 trade_no=%s status=%s", tradeNo, params["trade_status"]))
		c.String(http.StatusOK, "success")
		return
	}

	raw, _ := json.Marshal(params)
	alreadyDone, err := model.CompleteTopUp(ctx, tradeNo, model.PaymentProviderEpay, string(raw), config.MaxWalletQuota)
	if err != nil {
		if errors.Is(err, model.ErrTopUpProviderMismatch) ||
			errors.Is(err, model.ErrTopUpStatusInvalid) ||
			errors.Is(err, model.ErrTopUpNotFound) ||
			errors.Is(err, model.ErrWalletQuotaLimit) {
			logger.Error(ctx, fmt.Sprintf("EPay 回调入账失败 trade_no=%s: %s", tradeNo, err.Error()))
			epayReplyFail(c)
			return
		}
		logger.Error(ctx, fmt.Sprintf("EPay 回调入账异常 trade_no=%s: %s", tradeNo, err.Error()))
		epayReplyFail(c)
		return
	}
	if alreadyDone {
		// 重复通知：订单早已完成，直接确认，避免网关继续重试
		logger.Info(ctx, fmt.Sprintf("EPay 回调重复通知，订单已完成 trade_no=%s", tradeNo))
	}

	c.String(http.StatusOK, "success")
}

// epayReplyFail 向 EPay 回复失败（纯文本）
//
// 为什么用 c.String 而不是 c.JSON：见 EpayNotify 的说明，
// 网关只认正文里的 "success" 字样，JSON 会被当成失败而重试。
func epayReplyFail(c *gin.Context) {
	c.String(http.StatusOK, "fail")
}

// ---------------------------------------------------------------------------
// 3. 通用 webhook 网关：POST /api/pay/notify
// ---------------------------------------------------------------------------

// WebhookNotifyRequest 通用 webhook 网关的回调报文
//
// 约定：调用方必须用 config.WebhookSecret 对**原始请求体**做 HMAC-SHA256，
// 结果小写十六进制放进 X-Pay-Signature 头。金额字段仅用于日志核对，
// 真正的入账额度一律取订单里服务端算好的 Quota，不信任回调里的金额。
type WebhookNotifyRequest struct {
	TradeNo     string `json:"trade_no"`
	Status      string `json:"status"`
	AmountCents int64  `json:"amount_cents"`
}

// WebhookSignHeader 签名头名称
const WebhookSignHeader = "X-Pay-Signature"

// webhookSignature 计算回调体的 HMAC-SHA256 小写十六进制签名
//
// 为什么单独抽函数：验签逻辑必须可测且只能有一份实现，
// 控制器与测试共用，避免「测试用的算法和线上不一致」的假绿灯。
func webhookSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// WebhookNotify 处理通用 webhook 支付网关的支付结果通知。
//
// 安全模型：接口无鉴权中间件，完全依赖 X-Pay-Signature 的 HMAC 校验；
// 未配置 WebhookSecret 时直接拒绝，避免「空密钥 = 谁都能签名」的经典漏洞。
func WebhookNotify(c *gin.Context) {
	ctx := c.Request.Context()

	if !config.WebhookEnabled() {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "webhook 未配置",
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		logger.Error(ctx, "webhook 回调读取请求体失败: "+err.Error())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "读取请求体失败",
		})
		return
	}

	got := strings.ToLower(strings.TrimSpace(c.GetHeader(WebhookSignHeader)))
	if got == "" {
		logger.Error(ctx, "webhook 回调缺少签名头")
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "缺少签名",
		})
		return
	}
	// 恒定时间比较，避免通过响应时间差逐字节爆破签名
	if !hmac.Equal([]byte(webhookSignature(body, config.WebhookSecret)), []byte(got)) {
		logger.Error(ctx, "webhook 回调验签失败")
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "签名校验失败",
		})
		return
	}

	var req WebhookNotifyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		logger.Error(ctx, "webhook 回调报文解析失败: "+err.Error())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "报文格式错误",
		})
		return
	}

	// 只处理成功状态；其它状态（如 failed / refunded）记日志后忽略
	if !strings.EqualFold(strings.TrimSpace(req.Status), "success") {
		logger.Info(ctx, fmt.Sprintf("webhook 回调状态非成功，忽略 trade_no=%s status=%s", req.TradeNo, req.Status))
		c.JSON(http.StatusOK, gin.H{"success": true})
		return
	}

	alreadyDone, err := model.CompleteTopUp(ctx, req.TradeNo, model.PaymentProviderWebhook, string(body), config.MaxWalletQuota)
	if err != nil {
		if errors.Is(err, model.ErrTopUpProviderMismatch) ||
			errors.Is(err, model.ErrTopUpStatusInvalid) ||
			errors.Is(err, model.ErrTopUpNotFound) ||
			errors.Is(err, model.ErrWalletQuotaLimit) {
			logger.Error(ctx, fmt.Sprintf("webhook 回调入账失败 trade_no=%s: %s", req.TradeNo, err.Error()))
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		logger.Error(ctx, fmt.Sprintf("webhook 回调入账异常 trade_no=%s: %s", req.TradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "入账失败",
		})
		return
	}
	if alreadyDone {
		logger.Info(ctx, fmt.Sprintf("webhook 回调重复通知，订单已完成 trade_no=%s", req.TradeNo))
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ---------------------------------------------------------------------------
// 4. 充值信息：GET /api/user/topup/info
// ---------------------------------------------------------------------------

// GetTopUpInfo 返回充值页所需的全部配置
//
// 为什么要有这个接口：金额档位、支付方式、起充门槛、是否显示在线充值入口
// 都由服务端决定，前端不做任何硬编码，换站点只需改配置。
// 注意：绝不返回 EpayId / EpayKey / WebhookSecret。
func GetTopUpInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"pay_methods":      getPayMethodsOrEmpty(),
			"amount_options":   parseAmountOptions(config.AmountOptions),
			"min_top_up":       config.MinTopUp,
			"pay_enabled":      config.PaymentEnabled(),
			"top_up_link":      config.TopUpLink,
			"max_wallet_quota": config.MaxWalletQuota,
			"quota_per_unit":   config.QuotaPerUnit,
		},
	})
}

// getPayMethodsOrEmpty 返回支付方式列表，未配置时返回空数组而非 nil
//
// 为什么要把 nil 转成空数组：JSON 里 null 会让前端 `methods.map` 直接崩，
// 而一个空数组是安全的可迭代值。
func getPayMethodsOrEmpty() []model.PayMethod {
	methods := model.ParsePayMethods(config.PayMethods)
	if methods == nil {
		return []model.PayMethod{}
	}
	return methods
}

// parseAmountOptions 解析快捷金额档位（美元）
//
// 为什么解析失败时返回默认档位而不是空：档位只影响 UI 便捷性，
// 配错了不该让整个充值页不可用；退回一份保守的默认值体验最平滑。
func parseAmountOptions(raw string) []float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []float64{10, 20, 50, 100, 200, 500}
	}
	var options []float64
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		logger.SysError("invalid AmountOptions config: " + err.Error())
		return []float64{10, 20, 50, 100, 200, 500}
	}
	if options == nil {
		return []float64{}
	}
	return options
}

// ---------------------------------------------------------------------------
// 5. 用户自己的订单：GET /api/user/topup/self
// ---------------------------------------------------------------------------

// GetSelfTopUps 返回当前用户的充值订单（倒序，按页）
//
// 为什么用 ?p= 而不是游标：与日志/令牌等既有列表接口保持一致，
// 前端的分页组件可以直接复用。
func GetSelfTopUps(c *gin.Context) {
	p, _ := strconv.Atoi(c.Query("p"))
	if p < 0 {
		p = 0
	}
	userId := c.GetInt(ctxkey.Id)
	items, err := model.GetUserTopUps(userId, p*config.ItemsPerPage, config.ItemsPerPage)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    items,
	})
}

// ---------------------------------------------------------------------------
// 6. 管理员：全部订单 + 手工结算
// ---------------------------------------------------------------------------

// GetAllTopUpsHandler 管理员查看全部充值订单，支持 ?provider= 与 ?status= 过滤
//
// 为什么要过滤：对账时最常做的两件事就是「看某个网关的订单」
// 和「找出所有 pending / expired 的异常订单」。
func GetAllTopUpsHandler(c *gin.Context) {
	p, _ := strconv.Atoi(c.Query("p"))
	if p < 0 {
		p = 0
	}
	provider := c.Query("provider")
	status := c.Query("status")
	items, err := model.GetAllTopUps(p*config.ItemsPerPage, config.ItemsPerPage, provider, status)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    items,
	})
}

// AdminCompleteTopUpRequest 手工结算请求体
//
// 为什么必须带备注：手工结算绕过网关，是资金安全审计里最需要追溯的操作，
// 备注就是审计线索（model.AdminCompleteTopUp 在备注为空时会直接拒绝）。
type AdminCompleteTopUpRequest struct {
	TradeNo string `json:"trade_no"`
	Remark  string `json:"remark"`
}

// AdminCompleteTopUpHandler 管理员手工把订单标记为已完成并入账（对账补偿用）
//
// 典型场景：用户确实付了钱但网关回调丢失/验签失败，需要人工补单。
// 幂等由 model.CompleteTopUp 保证，重复提交不会重复加额度。
func AdminCompleteTopUpHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var req AdminCompleteTopUpRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数解析失败：" + err.Error(),
		})
		return
	}
	req.TradeNo = strings.TrimSpace(req.TradeNo)
	if err := model.ValidateTradeNo(req.TradeNo); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	// 审计：记录操作的管理员身份，出问题时能定位到人
	adminId := c.GetInt(ctxkey.Id)
	if err := model.AdminCompleteTopUp(ctx, req.TradeNo, fmt.Sprintf("%s (admin=%d)", req.Remark, adminId)); err != nil {
		logger.Error(ctx, fmt.Sprintf("管理员 %d 手工结算订单 %s 失败: %s", adminId, req.TradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// ---------------------------------------------------------------------------
// 7. 报价：POST /api/user/amount
// ---------------------------------------------------------------------------

// GetPayAmount 在不创建订单的前提下报价：返回应付金额与可得额度
//
// 为什么需要：前端在用户切换金额/支付方式时要实时显示「到账约多少额度」，
// 如果每改一次就下一个订单，会制造大量 pending 脏数据。
// 它复用与下单完全相同的校验与计算函数，保证报价与最终订单不会不一致。
func GetPayAmount(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	var req PayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数解析失败：" + err.Error(),
		})
		return
	}
	amountCents, err := normalizeTopUpAmountCents(req.Amount)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	// 支付方式可选：不传则只按全局规则报价
	if strings.TrimSpace(req.Method) != "" {
		method, err := findPayMethod(model.ParsePayMethods(config.PayMethods), req.Method)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		if err := checkMethodMinTopUp(method, amountCents); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}

	group, _ := model.CacheGetUserGroup(userId)
	ratio := topUpGroupRatio(group)
	if ratio <= 0 {
		ratio = 1
	}
	moneyCents := int64(float64(amountCents)*ratio + 0.5)
	if moneyCents <= 0 {
		moneyCents = amountCents
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"money": moneyCents,
			"quota": model.USDToQuota(float64(amountCents) / 100.0),
		},
	})
}
