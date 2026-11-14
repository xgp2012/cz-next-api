package controller

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/ctxkey"
	"github.com/songquanpeng/one-api/common/random"
	"github.com/songquanpeng/one-api/model"
)

// ============================================================================
// 订阅 / 套餐 控制面
// ============================================================================

// GetSubscriptionPlans 用户可见套餐列表（公开：登录前也能看到价格）
func GetSubscriptionPlans(c *gin.Context) {
	plans, err := model.GetEnabledPlans()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"plans": plans,
			// 前端需要知道 1 美元等于多少额度来展示「等价额度」
			"quota_per_unit": config.QuotaPerUnit,
			// 是否已配置支付网关：决定订阅页是否显示"在线支付"入口。
			// 复用 config 的判定函数，避免各处自己拼条件而漏掉某个配置项。
			"pay_enabled": config.PaymentEnabled(),
			// 余额支付是独立的：只要用户有余额就能买，与是否接支付网关无关
			"balance_pay_enabled": true,
		},
	})
}

// GetSelfSubscription 当前用户的订阅 + 计费偏好
func GetSelfSubscription(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	subs, err := model.GetUserSubscriptions(userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	plans, _ := model.GetEnabledPlans()
	planMap := make(map[int]*model.SubscriptionPlan, len(plans))
	for _, p := range plans {
		planMap[p.Id] = p
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"subscriptions": subs,
			"plans":         planMap,
			"preference":    getBillingPreference(userId),
		},
	})
}

// 计费偏好：订阅优先 / 钱包优先 / 仅订阅 / 仅钱包
//
// 这里没有引入新表，而是复用 Option 表按用户存一行 key：
// "BillingPreference:{userId}" = 字符串。这样不破坏既有 schema，
// 也天然享受 OptionMap 的同步机制。
const (
	PreferenceSubscriptionFirst = "subscription_first"
	PreferenceWalletFirst       = "wallet_first"
	PreferenceSubscriptionOnly  = "subscription_only"
	PreferenceWalletOnly        = "wallet_only"
)

func billingPreferenceKey(userId int) string {
	return fmt.Sprintf("BillingPreference:%d", userId)
}

func getBillingPreference(userId int) string {
	config.OptionMapRWMutex.RLock()
	v := config.OptionMap[billingPreferenceKey(userId)]
	config.OptionMapRWMutex.RUnlock()
	if v == "" {
		return PreferenceSubscriptionFirst
	}
	return v
}

// UpdateBillingPreference 用户切换计费偏好
func UpdateBillingPreference(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	var req struct {
		Preference string `json:"preference"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	switch req.Preference {
	case PreferenceSubscriptionFirst, PreferenceWalletFirst, PreferenceSubscriptionOnly, PreferenceWalletOnly:
	default:
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "未知的计费偏好"})
		return
	}
	if err := model.UpdateOption(billingPreferenceKey(userId), req.Preference); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// PaySubscriptionWithBalance 用余额购买套餐
func PaySubscriptionWithBalance(c *gin.Context) {
	ctx := c.Request.Context()
	userId := c.GetInt(ctxkey.Id)
	var req struct {
		PlanId int `json:"plan_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	sub, err := model.SubscribeWithBalance(ctx, userId, req.PlanId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": sub})
}

// ToggleSubscriptionAutoRenew 用户开关某个订阅的自动续费
func ToggleSubscriptionAutoRenew(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	var req struct {
		Id        int  `json:"id"`
		AutoRenew bool `json:"auto_renew"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.SetAutoRenew(userId, req.Id, req.AutoRenew); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// ---------------------------- 管理员：套餐 CRUD ----------------------------

func GetAdminPlans(c *gin.Context) {
	plans, err := model.GetAllPlans()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": plans})
}

func CreateAdminPlan(c *gin.Context) {
	var plan model.SubscriptionPlan
	if err := c.ShouldBindJSON(&plan); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	plan.Id = 0
	if err := model.CreatePlan(&plan); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": plan})
}

func UpdateAdminPlan(c *gin.Context) {
	var plan model.SubscriptionPlan
	if err := c.ShouldBindJSON(&plan); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.UpdatePlan(&plan); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func DeleteAdminPlan(c *gin.Context) {
	id := parseIdParam(c)
	if err := model.DeletePlan(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// InvalidateUserSubscription 管理员作废一个订阅
func InvalidateUserSubscription(c *gin.Context) {
	var req struct {
		Id int `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.CancelSubscriptionByAdmin(req.Id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// parseIdParam 解析路径参数 :id，非法时返回 0（调用方会得到"id 为空"错误）
func parseIdParam(c *gin.Context) int {
	id := 0
	_, _ = fmt.Sscanf(c.Param("id"), "%d", &id)
	return id
}

// ============================================================================
// 推广（邀请分成）
// ============================================================================

// GetAffInfo 推广中心首页数据
func GetAffInfo(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	user, err := model.GetUserById(userId, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	// 邀请码是"按需生成"的：历史上只有 GET /api/user/aff（旧 GetAffCode）
	// 会补写 aff_code，而该路由已由本接口接管。若不在这里补生成，
	// 所有新用户的推广链接都会是 ".../register?aff="，即邀请功能静默失效。
	if user.AffCode == "" {
		user.AffCode = random.GetRandomString(4)
		if err := user.Update(false); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
	}
	stats, err := model.GetAffStats(userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	serverAddress := config.ServerAddress
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"aff_code":        user.AffCode,
			"aff_link":        fmt.Sprintf("%s/register?aff=%s", serverAddress, user.AffCode),
			"invite_count":    stats.InviteCount,
			"pending_quota":   stats.PendingQuota,
			"history_quota":   stats.HistoryQuota,
			"commission_rate": model.CurrentCommissionConfig().RateFor(userId),
			"min_transfer":    model.MinAffTransferQuota(),
			"enabled":         model.CurrentCommissionConfig().Enabled,
		},
	})
}

// GetAffCommissions 佣金流水
func GetAffCommissions(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	startIdx := parsePagination(c)
	items, err := model.GetAffCommissions(userId, startIdx, config.ItemsPerPage)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": items})
}

// GetAffInvitees 我邀请的用户及其贡献
//
// 为什么单独一个接口而不是塞进 GetAffInfo：邀请人数可能上百，
// 列表需要独立加载与限制条数，塞进概览会让首页响应体不可控。
func GetAffInvitees(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	items, err := model.GetInvitees(userId, config.ItemsPerPage)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": items})
}

// TransferAffQuota 把待结算佣金转入钱包
func TransferAffQuota(c *gin.Context) {
	ctx := c.Request.Context()
	userId := c.GetInt(ctxkey.Id)
	quota, err := model.TransferAffQuotaToWallet(ctx, userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": quota})
}

// parsePagination 解析游标式分页参数 ?p=
func parsePagination(c *gin.Context) int {
	p := 0
	_, _ = fmt.Sscanf(c.Query("p"), "%d", &p)
	if p < 0 {
		p = 0
	}
	return p * config.ItemsPerPage
}
