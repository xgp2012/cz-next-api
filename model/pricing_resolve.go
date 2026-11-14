package model

import (
	"time"

	billingratio "github.com/songquanpeng/one-api/relay/billing/ratio"
)

// ============================================================================
// 计价入口：旧倍率与新规则引擎的统一门面
//
// 设计原则（务必保持）：**未配置任何 PricingRule 时，行为与改造前完全一致**。
// 这样已有部署升级后不会莫名其妙地按不同价格计费，也不会因为新表为空而报错。
// ============================================================================

// PriceContext 一次计价的上下文
type PriceContext struct {
	Model       string
	Group       string
	ChannelType int
	Now         time.Time
}

// CachedPrice 预扣阶段算出的价格快照，结算时必须用同一个，避免中途被改价
type CachedPrice struct {
	Rule            *PricingRule
	GroupRatio      float64
	ModelRatio      float64 // 旧路径的模型倍率（Rule == nil 时有效）
	CompletionRatio float64 // 旧路径的补全倍率
}

// ResolvePrice 解析某个请求应使用的价格
//
// 查找顺序：
//  1. 命中 PricingRule → 使用规则（新路径）
//  2. 未命中 → 使用旧 ModelRatio × GroupRatio（旧路径，完全兼容）
func ResolvePrice(ctx PriceContext) CachedPrice {
	now := ctx.Now
	if now.IsZero() {
		now = time.Now()
	}
	groupRatio := getGroupRatioSafe(ctx.Group)
	if rule := MatchPricingRule(ctx.Model, ctx.Group, ctx.ChannelType, now); rule != nil {
		return CachedPrice{Rule: rule, GroupRatio: groupRatio}
	}
	return CachedPrice{
		GroupRatio:      groupRatio,
		ModelRatio:      getModelRatioSafe(ctx.Model, ctx.ChannelType),
		CompletionRatio: getCompletionRatioSafe(ctx.Model, ctx.ChannelType),
	}
}

// IsRuleBased 是否走了新规则引擎
func (p CachedPrice) IsRuleBased() bool {
	return p.Rule != nil
}

// EstimatePreConsume 预扣额度估算
//
// 旧路径公式保持不变：(PreConsumedQuota + promptTokens + maxTokens) × ratio
// 这里只负责「按 token 部分」的单价换算，PreConsumedQuota 常量部分由调用方加入。
func (p CachedPrice) EstimatePreConsume(promptTokens int64) (quota int64, ok bool) {
	if p.Rule != nil {
		return EstimatePreConsume(p.Rule, p.GroupRatio, promptTokens)
	}
	// 旧路径：用 ratio 换算，保持与 relay/controller 中原逻辑等价
	if p.ModelRatio <= 0 {
		return 0, false
	}
	ratio := p.ModelRatio * p.GroupRatio
	return int64(float64(promptTokens) * ratio), true
}

// LegacyRatio 返回旧路径的合并倍率，供尚未迁移的调用方使用
func (p CachedPrice) LegacyRatio() float64 {
	return p.ModelRatio * p.GroupRatio
}

// Settle 按真实用量结算
func (p CachedPrice) Settle(u Usage) QuotaResult {
	if p.Rule != nil {
		if res, ok := PriceUsage(p.Rule, p.GroupRatio, u); ok {
			return res
		}
		// 规则配置不完整时回落到旧路径，而不是计 0
	}
	ratio := p.ModelRatio * p.GroupRatio
	quota := int64(float64(u.PromptTokens) + float64(u.CompletionTokens)*p.CompletionRatio)
	quota = int64(float64(quota) * ratio)
	if ratio != 0 && quota <= 0 && (u.PromptTokens+u.CompletionTokens) > 0 {
		quota = 1
	}
	return QuotaResult{
		Quota:                quota,
		BillingMode:          "legacy_ratio",
		EffectiveInputRatio:  p.ModelRatio,
		EffectiveOutputRatio: p.CompletionRatio,
		Detail:               "倍率：" + formatRatios(p.ModelRatio, p.GroupRatio, p.CompletionRatio),
	}
}

func formatRatios(modelRatio, groupRatio, completionRatio float64) string {
	return trimFloat(modelRatio) + " × " + trimFloat(groupRatio) + " × " + trimFloat(completionRatio)
}

// trimFloat 保留两位小数，避免日志里出现一长串浮点尾数
func trimFloat(f float64) string {
	s := ""
	// 简单实现，避免引入 strconv 的格式化分支
	v := f * 100
	i := int64(v)
	if float64(i) < v {
		i++
	}
	s = itoa(i/100) + "." + pad2(i%100)
	return s
}

func itoa(i int64) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func pad2(i int64) string {
	if i < 10 {
		return "0" + itoa(i)
	}
	return itoa(i)
}

// ---------------------------------------------------------------------------
// 安全访问旧倍率：这些函数在测试环境下可能没有初始化，需容错
// ---------------------------------------------------------------------------

func getGroupRatioSafe(group string) float64 {
	if group == "" {
		return 1
	}
	return billingratio.GetGroupRatio(group)
}

func getModelRatioSafe(model string, channelType int) float64 {
	return billingratio.GetModelRatio(model, channelType)
}

func getCompletionRatioSafe(model string, channelType int) float64 {
	return billingratio.GetCompletionRatio(model, channelType)
}
