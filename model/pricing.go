package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/songquanpeng/one-api/common/logger"
)

// ============================================================================
// 定价规则引擎（差异化能力 1）
//
// 与 New API 的差异：它用 9 组互斥的 map（ModelRatio / CompletionRatio /
// ModelPrice / ImageRatio / AudioRatio / AudioCompletionRatio / CacheRatio /
// CreateCacheRatio / GroupRatio / GroupGroupRatio）表达价格，彼此之间有隐式
// 优先级，运营改价时极易互相覆盖，且无法表达「阶梯」「分时」这类真实商业定价。
//
// 这里改为「有序规则链 + 显式 tier」：
//
//	匹配（match）：model / group / channelType / 时间窗
//	计价（calc） ：per_token | per_call | tiered_by_tokens
//
// 规则按 Priority 降序、Order 升序匹配，第一条命中即生效；全部不命中时回落到
// 旧的 ModelRatio/GroupRatio（向后兼容：未配置规则的部署行为完全不变）。
// ============================================================================

// BillingMode 计费模式
type BillingMode string

const (
	// BillingModePerToken 按 token 计费（输入 × 输入价 + 输出 × 输出价）
	BillingModePerToken BillingMode = "per_token"
	// BillingModePerCall 按次计费（每次请求固定价，与 token 数无关）
	BillingModePerCall BillingMode = "per_call"
	// BillingModeTieredByTokens 按 token 用量阶梯计费（总量落在哪一档就用哪档单价）
	BillingModeTieredByTokens BillingMode = "tiered_by_tokens"
)

// PriceTier 阶梯档位
//
// UpToTokens 为该档的**累计 token 上界**（含）。最后一档应设为 0 表示「无上界」。
// 例如：
//
//	{tier 1, up_to_tokens: 1000000, input_ratio: 2.5}
//	{tier 2, up_to_tokens: 0,       input_ratio: 2.0}
//
// 表示「前 100 万 token 按 2.5 计价，超出部分整体按 2.0」。
type PriceTier struct {
	Tier            int     `json:"tier"`
	UpToTokens      int64   `json:"up_to_tokens"`
	InputRatio      float64 `json:"input_ratio"`
	OutputRatio     float64 `json:"output_ratio"`
	CacheHitRatio   float64 `json:"cache_hit_ratio"`
	CacheWriteRatio float64 `json:"cache_write_ratio"`
}

// TimeWindow 分时定价窗口（0-23 小时，左闭右开）
type TimeWindow struct {
	StartHour int `json:"start_hour"`
	EndHour   int `json:"end_hour"`
}

// Matches 判断某个时刻是否落在窗口内。StartHour == EndHour 视为全天。
// 支持跨零点窗口（如 22 → 6）。
func (w TimeWindow) Matches(t time.Time) bool {
	if w.StartHour == w.EndHour {
		return true
	}
	h := t.Hour()
	if w.StartHour < w.EndHour {
		return h >= w.StartHour && h < w.EndHour
	}
	// 跨零点
	return h >= w.StartHour || h < w.EndHour
}

// PricingRule 一条定价规则
type PricingRule struct {
	Id     int    `json:"id" gorm:"primaryKey"`
	Name   string `json:"name" gorm:"type:varchar(64);index"`     // 规则名，便于运营识别
	Note   string `json:"note" gorm:"type:varchar(255)"`          // 备注
	Status int    `json:"status" gorm:"type:int;default:1;index"` // 1 启用 / 2 禁用

	// ---- 匹配条件（留空 = 不限制）----
	Model       string `json:"model" gorm:"type:varchar(128);index"`    // 支持 "prefix:*" 通配
	Group       string `json:"group" gorm:"type:varchar(64);index"`     // 用户分组，空=任意
	ChannelType int    `json:"channel_type" gorm:"type:int;default:-1"` // -1 = 任意渠道类型
	TimeWindows string `json:"time_windows" gorm:"type:text"`           // JSON: [{start_hour,end_hour}]

	// ---- 计价 ----
	BillingMode BillingMode `json:"billing_mode" gorm:"type:varchar(32);default:'per_token'"`
	// Per-token 计价（ratio 语义与 One API 一致：1 = $0.002/1K tokens）
	InputRatio      float64 `json:"input_ratio" gorm:"type:double;default:0"`
	OutputRatio     float64 `json:"output_ratio" gorm:"type:double;default:0"`
	CacheHitRatio   float64 `json:"cache_hit_ratio" gorm:"type:double;default:0"`   // 缓存命中价，0 = 不单独计价
	CacheWriteRatio float64 `json:"cache_write_ratio" gorm:"type:double;default:0"` // 缓存写入价，0 = 不单独计价
	// Per-call 计价（单位：美元）
	PerCallPrice float64 `json:"per_call_price" gorm:"type:double;default:0"`
	// 阶梯计价
	Tiers string `json:"tiers" gorm:"type:text"`

	// ---- 调度 ----
	Priority int `json:"priority" gorm:"type:int;default:0;index"` // 越大越优先
	Order    int `json:"order" gorm:"type:int;default:0"`          // 同优先级内的小序号

	CreatedTime int64 `json:"created_time" gorm:"bigint"`
	UpdatedTime int64 `json:"updated_time" gorm:"bigint"`
}

func (PricingRule) TableName() string {
	return "pricing_rules"
}

// ParseTimeWindows 解析时间窗 JSON
func (r *PricingRule) ParseTimeWindows() []TimeWindow {
	if strings.TrimSpace(r.TimeWindows) == "" {
		return nil
	}
	var windows []TimeWindow
	if err := json.Unmarshal([]byte(r.TimeWindows), &windows); err != nil {
		logger.SysError(fmt.Sprintf("pricing rule %d: invalid time_windows: %s", r.Id, err.Error()))
		return nil
	}
	return windows
}

// ParseTiers 解析阶梯 JSON
func (r *PricingRule) ParseTiers() []PriceTier {
	if strings.TrimSpace(r.Tiers) == "" {
		return nil
	}
	var tiers []PriceTier
	if err := json.Unmarshal([]byte(r.Tiers), &tiers); err != nil {
		logger.SysError(fmt.Sprintf("pricing rule %d: invalid tiers: %s", r.Id, err.Error()))
		return nil
	}
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].Tier < tiers[j].Tier })
	return tiers
}

// matchModel 支持精确匹配与 "prefix:*" 通配
func matchModel(pattern string, model string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if pattern == model {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(model, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

// ChannelTypeAny 表示「不限渠道类型」。
//
// 为什么把 0 和 -1 都当作「任意」：channeltype.Unknown = 0 不是真实渠道类型，
// Go 的结构体零值又恰好是 0。如果只有 -1 表示任意，运营在前端漏填 / 后端反序列化
// 缺字段时会静默变成「只匹配 Unknown 渠道」，规则永远不生效却没有任何报错。
// 两种哨兵都接受，是对零值陷阱的显式兜底。
const ChannelTypeAny = -1

// isChannelTypeAny 判断渠道类型条件是否放行
func isChannelTypeAny(r *PricingRule) bool {
	return r.ChannelType == ChannelTypeAny || r.ChannelType == 0
}

// Matches 判断规则是否命中当前请求
func (r *PricingRule) Matches(model, group string, channelType int, now time.Time) bool {
	if r.Status != PricingRuleStatusEnabled {
		return false
	}
	if !matchModel(r.Model, model) {
		return false
	}
	// Group 支持逗号分隔的多个分组
	if r.Group != "" {
		hit := false
		for _, g := range strings.Split(r.Group, ",") {
			if strings.TrimSpace(g) == group {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if !isChannelTypeAny(r) && r.ChannelType != channelType {
		return false
	}
	if windows := r.ParseTimeWindows(); len(windows) > 0 {
		hit := false
		for _, w := range windows {
			if w.Matches(now) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

const (
	PricingRuleStatusEnabled  = 1
	PricingRuleStatusDisabled = 2
)

// ---------------------------------------------------------------------------
// 规则缓存：规则量很小（通常 < 100 条），全量驻留内存，写操作后失效
// ---------------------------------------------------------------------------

var (
	pricingRuleLock  sync.RWMutex
	pricingRuleCache []*PricingRule
	pricingRuleDirty = true
)

// InvalidatePricingRules 让缓存失效，下次查询重新从数据库加载
func InvalidatePricingRules() {
	pricingRuleLock.Lock()
	defer pricingRuleLock.Unlock()
	pricingRuleDirty = true
}

// loadPricingRulesLocked 调用方必须已持有写锁
func loadPricingRulesLocked() {
	pricingRuleDirty = false
	if DB == nil {
		pricingRuleCache = nil
		return
	}
	var rules []*PricingRule
	err := DB.Where("status = ?", PricingRuleStatusEnabled).Find(&rules).Error
	if err != nil {
		// 表可能尚未迁移（测试环境），静默降级为「无规则」
		logger.SysError("failed to load pricing rules: " + err.Error())
		pricingRuleCache = nil
		return
	}
	// 优先级降序、同优先级按 Order 升序
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].Order < rules[j].Order
	})
	pricingRuleCache = rules
}

// GetActivePricingRules 返回当前生效的规则（已排序），调用方不应修改返回的切片
func GetActivePricingRules() []*PricingRule {
	pricingRuleLock.RLock()
	if !pricingRuleDirty {
		rules := pricingRuleCache
		pricingRuleLock.RUnlock()
		return rules
	}
	pricingRuleLock.RUnlock()

	pricingRuleLock.Lock()
	defer pricingRuleLock.Unlock()
	if pricingRuleDirty {
		loadPricingRulesLocked()
	}
	return pricingRuleCache
}

// MatchPricingRule 按优先级返回第一条命中的规则，无命中返回 nil
func MatchPricingRule(model, group string, channelType int, now time.Time) *PricingRule {
	for _, r := range GetActivePricingRules() {
		if r.Matches(model, group, channelType, now) {
			return r
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 计价
// ---------------------------------------------------------------------------

// Usage 一次请求的真实用量
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	CacheHitTokens   int64
	CacheWriteTokens int64
}

// QuotaResult 计价结果
type QuotaResult struct {
	Quota                int64       `json:"quota"`
	BillingMode          BillingMode `json:"billing_mode"`
	Tier                 int         `json:"tier"`    // 命中的阶梯（per_token/per_call 为 0）
	RuleId               int         `json:"rule_id"` // 命中的规则 id，0 = 回落到旧倍率
	EffectiveInputRatio  float64     `json:"effective_input_ratio"`
	EffectiveOutputRatio float64     `json:"effective_output_ratio"`
	Detail               string      `json:"detail"` // 人类可读的计价说明，写入日志
}

// 计价基准与 One API 对齐：ratio = 1 表示 $0.002 / 1K tokens
const usdPerRatioUnit = 0.002 / 1000.0

// quotaFromUSD 把美元金额换算成 quota，向上取整，保底 1
func quotaFromUSD(usd float64) int64 {
	// QuotaPerUnit 是「1 美元 = 多少 quota」，由 config 提供
	perUnit := getQuotaPerUnit()
	q := int64(usd*perUnit + 0.999999)
	if q < 1 {
		q = 1
	}
	return q
}

// getQuotaPerUnit 间接读取，避免 model 包直接 import config 造成循环依赖问题
// （config 不 import model，这里可以安全直接引用）
func getQuotaPerUnit() float64 {
	return quotaPerUnitProvider()
}

// quotaPerUnitProvider 由 init 时或测试中注入，默认从 config 读取
var quotaPerUnitProvider = func() float64 { return 500000 }

// SetQuotaPerUnitProvider 允许测试或上层注入 QuotaPerUnit 的读取方式
func SetQuotaPerUnitProvider(fn func() float64) {
	if fn != nil {
		quotaPerUnitProvider = fn
	}
}

// PriceUsage 用给定规则计算 quota，规则为 nil 时返回 (0,false) 表示未命中
func PriceUsage(rule *PricingRule, groupRatio float64, u Usage) (QuotaResult, bool) {
	if rule == nil {
		return QuotaResult{}, false
	}
	res := QuotaResult{RuleId: rule.Id, BillingMode: rule.BillingMode}

	switch rule.BillingMode {
	case BillingModePerCall:
		if rule.PerCallPrice <= 0 {
			return QuotaResult{}, false
		}
		q := quotaFromUSD(rule.PerCallPrice * groupRatio)
		res.Quota = q
		res.Detail = fmt.Sprintf("按次计费：$%.4f × 分组倍率 %.2f", rule.PerCallPrice, groupRatio)
		return res, true

	case BillingModeTieredByTokens:
		tiers := rule.ParseTiers()
		if len(tiers) == 0 {
			return QuotaResult{}, false
		}
		total := u.PromptTokens + u.CompletionTokens
		if u.CacheHitTokens > 0 {
			total -= u.CacheHitTokens
		}
		if total < 0 {
			total = 0
		}
		chosen := tiers[len(tiers)-1]
		for _, t := range tiers {
			// UpToTokens == 0 表示无上界
			if t.UpToTokens == 0 || total <= t.UpToTokens {
				chosen = t
				break
			}
		}
		res.Tier = chosen.Tier
		res.EffectiveInputRatio = chosen.InputRatio
		res.EffectiveOutputRatio = chosen.OutputRatio
		quota := tierQuota(chosen, groupRatio, u)
		res.Quota = quota
		res.Detail = fmt.Sprintf("阶梯计费：Tier %d（≤%d tokens）输入 %.4f 输出 %.4f × 分组倍率 %.2f",
			chosen.Tier, chosen.UpToTokens, chosen.InputRatio, chosen.OutputRatio, groupRatio)
		return res, true

	default: // per_token
		if rule.InputRatio <= 0 && rule.OutputRatio <= 0 && rule.PerCallPrice <= 0 {
			return QuotaResult{}, false
		}
		res.EffectiveInputRatio = rule.InputRatio
		res.EffectiveOutputRatio = rule.OutputRatio
		res.Quota = tokenQuota(rule.InputRatio, rule.OutputRatio, rule.CacheHitRatio, rule.CacheWriteRatio, groupRatio, u)
		res.Detail = fmt.Sprintf("按量计费：输入 %.4f 输出 %.4f 缓存命中 %.4f × 分组倍率 %.2f",
			rule.InputRatio, rule.OutputRatio, rule.CacheHitRatio, groupRatio)
		return res, true
	}
}

// tierQuota 用某个阶梯档位计算 quota
func tierQuota(t PriceTier, groupRatio float64, u Usage) int64 {
	prompt := u.PromptTokens
	completion := u.CompletionTokens
	// 缓存命中的 prompt tokens 按 cache_hit_ratio 单独计价
	if u.CacheHitTokens > 0 && t.CacheHitRatio > 0 && u.CacheHitTokens <= prompt {
		prompt -= u.CacheHitTokens
	}
	usd := (float64(prompt)*t.InputRatio + float64(completion)*t.OutputRatio) * usdPerRatioUnit
	if u.CacheHitTokens > 0 && t.CacheHitRatio > 0 {
		usd += float64(u.CacheHitTokens) * t.CacheHitRatio * usdPerRatioUnit
	}
	if u.CacheWriteTokens > 0 && t.CacheWriteRatio > 0 {
		usd += float64(u.CacheWriteTokens) * t.CacheWriteRatio * usdPerRatioUnit
	}
	return quotaFromUSD(usd * groupRatio)
}

// tokenQuota 按 token 总计费
func tokenQuota(inputRatio, outputRatio, cacheHitRatio, cacheWriteRatio, groupRatio float64, u Usage) int64 {
	prompt := u.PromptTokens
	// 缓存命中的部分从普通输入里扣掉，避免重复计费
	if u.CacheHitTokens > 0 && cacheHitRatio > 0 && u.CacheHitTokens <= prompt {
		prompt -= u.CacheHitTokens
	}
	usd := (float64(prompt)*inputRatio + float64(u.CompletionTokens)*outputRatio) * usdPerRatioUnit
	if u.CacheHitTokens > 0 && cacheHitRatio > 0 {
		usd += float64(u.CacheHitTokens) * cacheHitRatio * usdPerRatioUnit
	}
	if u.CacheWriteTokens > 0 && cacheWriteRatio > 0 {
		usd += float64(u.CacheWriteTokens) * cacheWriteRatio * usdPerRatioUnit
	}
	return quotaFromUSD(usd * groupRatio)
}

// EstimatePromptTokens 预扣场景：只有 prompt tokens，用输入价估算
// 返回 0 表示命中按次计费（固定价）、或未命中规则
func EstimatePreConsume(rule *PricingRule, groupRatio float64, promptTokens int64) (int64, bool) {
	if rule == nil {
		return 0, false
	}
	switch rule.BillingMode {
	case BillingModePerCall:
		res, ok := PriceUsage(rule, groupRatio, Usage{})
		return res.Quota, ok
	case BillingModeTieredByTokens:
		tiers := rule.ParseTiers()
		if len(tiers) == 0 {
			return 0, false
		}
		chosen := tiers[len(tiers)-1]
		for _, t := range tiers {
			if t.UpToTokens == 0 || promptTokens <= t.UpToTokens {
				chosen = t
				break
			}
		}
		usd := float64(promptTokens) * chosen.InputRatio * usdPerRatioUnit
		return quotaFromUSD(usd * groupRatio), true
	default:
		usd := float64(promptTokens) * rule.InputRatio * usdPerRatioUnit
		return quotaFromUSD(usd * groupRatio), true
	}
}

// ---------------------------------------------------------------------------
// 规则 CRUD
// ---------------------------------------------------------------------------

var ErrPricingRuleNotFound = errors.New("定价规则不存在")

// ValidatePricingRule 保存前的合法性校验
func ValidatePricingRule(r *PricingRule) error {
	if r.Name == "" {
		return errors.New("规则名称不能为空")
	}
	switch r.BillingMode {
	case BillingModePerToken:
		if r.InputRatio < 0 || r.OutputRatio < 0 {
			return errors.New("倍率不能为负数")
		}
		if r.InputRatio == 0 && r.OutputRatio == 0 {
			return errors.New("按量计费至少需要配置输入倍率或输出倍率")
		}
	case BillingModePerCall:
		if r.PerCallPrice <= 0 {
			return errors.New("按次计费需要配置大于 0 的单次价格")
		}
	case BillingModeTieredByTokens:
		tiers := r.ParseTiers()
		if len(tiers) == 0 {
			return errors.New("阶梯计费需要配置至少一个阶梯")
		}
		// 必须且只能有最后一档为无上界
		for i, t := range tiers {
			if t.UpToTokens == 0 && i != len(tiers)-1 {
				return errors.New("只有最后一个阶梯可以设置为无上界（up_to_tokens = 0）")
			}
			if t.UpToTokens < 0 {
				return errors.New("阶梯上界不能为负数")
			}
		}
	case "":
		return errors.New("必须指定计费模式")
	default:
		return fmt.Errorf("未知的计费模式：%s", r.BillingMode)
	}
	if r.ChannelType < ChannelTypeAny {
		return errors.New("渠道类型非法")
	}
	if w := r.ParseTimeWindows(); strings.TrimSpace(r.TimeWindows) != "" && w == nil {
		return errors.New("时间窗 JSON 格式非法")
	}
	for _, w := range r.ParseTimeWindows() {
		if w.StartHour < 0 || w.StartHour > 23 || w.EndHour < 0 || w.EndHour > 23 {
			return errors.New("时间窗小时必须在 0-23 之间")
		}
	}
	return nil
}

// CreatePricingRule 新建规则
func CreatePricingRule(r *PricingRule) error {
	if err := ValidatePricingRule(r); err != nil {
		return err
	}
	now := time.Now().Unix()
	r.CreatedTime = now
	r.UpdatedTime = now
	if r.Status == 0 {
		r.Status = PricingRuleStatusEnabled
	}
	if err := DB.Create(r).Error; err != nil {
		return err
	}
	InvalidatePricingRules()
	return nil
}

// UpdatePricingRule 更新规则（全字段覆盖）
func UpdatePricingRule(r *PricingRule) error {
	if r.Id == 0 {
		return errors.New("id 为空")
	}
	if err := ValidatePricingRule(r); err != nil {
		return err
	}
	r.UpdatedTime = time.Now().Unix()
	if err := DB.Model(r).Select(
		"name", "note", "status", "model", "group", "channel_type", "time_windows",
		"billing_mode", "input_ratio", "output_ratio", "cache_hit_ratio", "cache_write_ratio",
		"per_call_price", "tiers", "priority", "order", "updated_time",
	).Updates(r).Error; err != nil {
		return err
	}
	InvalidatePricingRules()
	return nil
}

// DeletePricingRule 删除规则
func DeletePricingRule(id int) error {
	if id == 0 {
		return errors.New("id 为空")
	}
	res := DB.Delete(&PricingRule{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPricingRuleNotFound
	}
	InvalidatePricingRules()
	return nil
}

// GetAllPricingRules 后台列表（含禁用规则），按优先级排序
func GetAllPricingRules() ([]*PricingRule, error) {
	var rules []*PricingRule
	err := DB.Order("priority desc, `order` asc, id asc").Find(&rules).Error
	return rules, err
}
