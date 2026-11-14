package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/model"
	billingratio "github.com/songquanpeng/one-api/relay/billing/ratio"
)

// ============================================================================
// 定价规则管理 + 价格墙
// ============================================================================

// GetPricingWall 公开的模型价格墙。
//
// 数据来源的优先级与真实计费一致：先规则，后旧倍率。这样用户看到的价
// 就是实际被扣的价，不会出现「页面写一套、扣费另一套」的信任问题。
func GetPricingWall(c *gin.Context) {
	rules := model.GetActivePricingRules()

	rulesView := make([]gin.H, 0, len(rules))
	for _, r := range rules {
		rulesView = append(rulesView, gin.H{
			"id":                r.Id,
			"name":              r.Name,
			"model":             r.Model,
			"group":             r.Group,
			"channel_type":      r.ChannelType,
			"billing_mode":      r.BillingMode,
			"input_ratio":       r.InputRatio,
			"output_ratio":      r.OutputRatio,
			"cache_hit_ratio":   r.CacheHitRatio,
			"cache_write_ratio": r.CacheWriteRatio,
			"per_call_price":    r.PerCallPrice,
			"tiers":             r.ParseTiers(),
			"time_windows":      r.ParseTimeWindows(),
			"priority":          r.Priority,
			"note":              r.Note,
		})
	}

	// 旧倍率也一并暴露：未命中任何规则的模型走的正是这套价格
	legacy := make(map[string]float64, len(billingratio.ModelRatio))
	for k, v := range billingratio.ModelRatio {
		legacy[k] = v
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"rules":          rulesView,
			"legacy_ratios":  legacy,
			"group_ratios":   billingratio.GroupRatio,
			"quota_per_unit": config.QuotaPerUnit,
			// 说明字段：前端直接展示，避免用户在价格口径上产生疑问
			"ratio_explanation": "ratio = 1 表示 $0.002 / 1K tokens（$2 / 1M tokens）",
		},
	})
}

// GetPricingRules 管理员：全部规则（含禁用）
func GetPricingRules(c *gin.Context) {
	rules, err := model.GetAllPricingRules()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": rules})
}

// CreatePricingRule 管理员：新建规则
func CreatePricingRule(c *gin.Context) {
	var rule model.PricingRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	rule.Id = 0
	if err := model.CreatePricingRule(&rule); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": rule})
}

// UpdatePricingRule 管理员：更新规则
func UpdatePricingRule(c *gin.Context) {
	var rule model.PricingRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if rule.Id == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "id 为空"})
		return
	}
	if err := model.UpdatePricingRule(&rule); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// DeletePricingRule 管理员：删除规则
func DeletePricingRule(c *gin.Context) {
	id := parseIdParam(c)
	if err := model.DeletePricingRule(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
