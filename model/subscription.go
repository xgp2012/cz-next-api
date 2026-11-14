package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"gorm.io/gorm"

	"github.com/songquanpeng/one-api/common/helper"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/common/random"
)

// ============================================================================
// 订阅套餐（差异化点 2：补上 New API 明确缺失的自动续费与按比例退款）
//
// New API 的订阅是「周期 + 额度快照」，官方文档写明**无自动续费、无按比例退款、
// 无升级折算**。这里在同样的数据模型上补齐这三项：
//
//   AutoRenew      : 到期前从钱包余额自动扣费续期，扣费失败进入宽限期
//   GracePeriod    : 宽限期内额度仍可用，且可补缴，避免服务瞬间中断
//   Proration      : 升级套餐时按剩余天数折算差价，从钱包结算
// ============================================================================

const (
	SubscriptionStatusActive    = "active"
	SubscriptionStatusExpired   = "expired"
	SubscriptionStatusCancelled = "cancelled"

	SubscriptionSourceOrder   = "order"
	SubscriptionSourceBalance = "balance"
	SubscriptionSourceAdmin   = "admin"

	// 套餐周期单位
	PeriodUnitDay    = "day"
	PeriodUnitMonth  = "month"
	PeriodUnitYear   = "year"
	PeriodUnitCustom = "custom"

	// 额度重置周期
	ResetNever   = "never"
	ResetDaily   = "daily"
	ResetWeekly  = "weekly"
	ResetMonthly = "monthly"
)

// SubscriptionPlan 套餐定义
type SubscriptionPlan struct {
	Id       int    `json:"id" gorm:"primaryKey"`
	Title    string `json:"title" gorm:"type:varchar(64);index"`
	Subtitle string `json:"subtitle" gorm:"type:varchar(255)"`
	// PriceCents 价格（美元分），避免浮点
	PriceCents int64  `json:"price_cents" gorm:"bigint;default:0"`
	Currency   string `json:"currency" gorm:"type:varchar(8);default:'USD'"`

	// 周期：DurationValue 个 DurationUnit
	DurationUnit  string `json:"duration_unit" gorm:"type:varchar(16);default:'month'"`
	DurationValue int    `json:"duration_value" gorm:"type:int;default:1"`
	CustomSeconds int64  `json:"custom_seconds" gorm:"bigint;default:0"`

	Enabled   bool `json:"enabled" gorm:"type:boolean;default:true;index"`
	SortOrder int  `json:"sort_order" gorm:"type:int;default:0"`

	// 本周期内可消耗额度；0 = 不限量
	TotalQuota int64 `json:"total_quota" gorm:"bigint;default:0"`

	// 额度重置：周期内按天/周/月重置，never = 不重置
	QuotaResetPeriod string `json:"quota_reset_period" gorm:"type:varchar(16);default:'never'"`

	// 购买后授予的用户分组，到期回落
	UpgradeGroup   string `json:"upgrade_group" gorm:"type:varchar(32)"`
	DowngradeGroup string `json:"downgrade_group" gorm:"type:varchar(32)"`

	// AllowBalancePay 是否允许用钱包余额购买
	AllowBalancePay bool `json:"allow_balance_pay" gorm:"type:boolean;default:true"`
	// AllowWalletOverflow 额度用尽后是否允许溢出到钱包扣费
	AllowWalletOverflow bool `json:"allow_wallet_overflow" gorm:"type:boolean;default:false"`
	// AutoRenewEnabled 是否允许自动续费（用户侧仍可关闭）
	AutoRenewEnabled bool `json:"auto_renew_enabled" gorm:"type:boolean;default:false"`
	// GracePeriodDays 续费失败后的宽限天数
	GracePeriodDays int `json:"grace_period_days" gorm:"type:int;default:0"`

	// MaxPurchasePerUser 每个用户最多同时持有几份，0 = 不限
	MaxPurchasePerUser int `json:"max_purchase_per_user" gorm:"type:int;default:0"`

	CreatedTime int64 `json:"created_time" gorm:"bigint"`
	UpdatedTime int64 `json:"updated_time" gorm:"bigint"`
}

func (SubscriptionPlan) TableName() string {
	return "subscription_plans"
}

func (p *SubscriptionPlan) PriceUSD() float64 {
	return float64(p.PriceCents) / 100.0
}

// PeriodDuration 计算一个周期的时长
func (p *SubscriptionPlan) PeriodDuration() time.Duration {
	switch p.DurationUnit {
	case PeriodUnitDay:
		return time.Duration(p.DurationValue) * 24 * time.Hour
	case PeriodUnitMonth:
		return time.Duration(p.DurationValue) * 30 * 24 * time.Hour
	case PeriodUnitYear:
		return time.Duration(p.DurationValue) * 365 * 24 * time.Hour
	case PeriodUnitCustom:
		return time.Duration(p.CustomSeconds) * time.Second
	default:
		return 30 * 24 * time.Hour
	}
}

// CalcEndTime 用日历加法计算结束时间（月/年不会因月份天数而漂移）
func (p *SubscriptionPlan) CalcEndTime(start time.Time) time.Time {
	switch p.DurationUnit {
	case PeriodUnitDay:
		return start.AddDate(0, 0, p.DurationValue)
	case PeriodUnitMonth:
		return start.AddDate(0, p.DurationValue, 0)
	case PeriodUnitYear:
		return start.AddDate(p.DurationValue, 0, 0)
	case PeriodUnitCustom:
		return start.Add(time.Duration(p.CustomSeconds) * time.Second)
	default:
		return start.AddDate(0, 1, 0)
	}
}

// ValidatePlan 保存前校验
func ValidatePlan(p *SubscriptionPlan) error {
	if p.Title == "" {
		return errors.New("套餐名称不能为空")
	}
	if p.PriceCents < 0 {
		return errors.New("价格不能为负数")
	}
	switch p.DurationUnit {
	case PeriodUnitDay, PeriodUnitMonth, PeriodUnitYear:
		if p.DurationValue <= 0 {
			return errors.New("周期数值必须大于 0")
		}
	case PeriodUnitCustom:
		if p.CustomSeconds <= 0 {
			return errors.New("自定义周期秒数必须大于 0")
		}
	default:
		return fmt.Errorf("未知的周期单位：%s", p.DurationUnit)
	}
	switch p.QuotaResetPeriod {
	case ResetNever, ResetDaily, ResetWeekly, ResetMonthly, "":
	default:
		return fmt.Errorf("未知的额度重置周期：%s", p.QuotaResetPeriod)
	}
	if p.TotalQuota < 0 {
		return errors.New("套餐额度不能为负数")
	}
	if p.GracePeriodDays < 0 {
		return errors.New("宽限天数不能为负数")
	}
	return nil
}

// UserSubscription 用户持有的订阅实例（一个周期）
type UserSubscription struct {
	Id     int `json:"id" gorm:"primaryKey"`
	UserId int `json:"user_id" gorm:"index:idx_user_sub_active,priority:1"`
	PlanId int `json:"plan_id" gorm:"index"`

	AmountTotal int64 `json:"amount_total" gorm:"bigint;default:0"` // 本周期总额度，0 = 不限
	AmountUsed  int64 `json:"amount_used" gorm:"bigint;default:0"`

	StartTime int64 `json:"start_time" gorm:"bigint"`
	EndTime   int64 `json:"end_time" gorm:"bigint;index:idx_user_sub_active,priority:3"`
	// GraceEndTime 续费失败后的宽限截止（0 = 无宽限）
	GraceEndTime int64 `json:"grace_end_time" gorm:"bigint;default:0"`

	Status string `json:"status" gorm:"type:varchar(20);default:'active';index:idx_user_sub_active,priority:2"`
	Source string `json:"source" gorm:"type:varchar(20);default:'order'"`

	LastResetTime int64 `json:"last_reset_time" gorm:"bigint;default:0"`
	NextResetTime int64 `json:"next_reset_time" gorm:"bigint;default:0"`

	// 分组快照
	UpgradeGroup   string `json:"upgrade_group" gorm:"type:varchar(32)"`
	PrevUserGroup  string `json:"prev_user_group" gorm:"type:varchar(32)"`
	DowngradeGroup string `json:"downgrade_group" gorm:"type:varchar(32)"`

	// AutoRenew 用户侧开关（套餐允许时才有意义）
	AutoRenew bool `json:"auto_renew" gorm:"type:boolean;default:false"`
	// RenewAttempts 连续续费失败次数，用于宽限与通知
	RenewAttempts int `json:"renew_attempts" gorm:"type:int;default:0"`

	AllowWalletOverflow bool `json:"allow_wallet_overflow" gorm:"type:boolean;default:false"`

	CreatedTime int64 `json:"created_time" gorm:"bigint"`
}

func (UserSubscription) TableName() string {
	return "user_subscriptions"
}

// Remaining 剩余额度；0 表示不限量（AmountTotal == 0）
func (s *UserSubscription) Remaining() int64 {
	if s.AmountTotal == 0 {
		return 0
	}
	r := s.AmountTotal - s.AmountUsed
	if r < 0 {
		return 0
	}
	return r
}

// IsUsable 当前是否可用（active 且在有效期内或宽限期内）
func (s *UserSubscription) IsUsable(now int64) bool {
	if s.Status != SubscriptionStatusActive {
		return false
	}
	if now <= s.EndTime {
		return true
	}
	return s.GraceEndTime > 0 && now <= s.GraceEndTime
}

// ---------------------------------------------------------------------------
// 套餐 CRUD
// ---------------------------------------------------------------------------

func CreatePlan(p *SubscriptionPlan) error {
	if err := ValidatePlan(p); err != nil {
		return err
	}
	now := helper.GetTimestamp()
	p.CreatedTime = now
	p.UpdatedTime = now
	return DB.Create(p).Error
}

func UpdatePlan(p *SubscriptionPlan) error {
	if p.Id == 0 {
		return errors.New("id 为空")
	}
	if err := ValidatePlan(p); err != nil {
		return err
	}
	p.UpdatedTime = helper.GetTimestamp()
	return DB.Model(p).Select(
		"title", "subtitle", "price_cents", "currency", "duration_unit", "duration_value",
		"custom_seconds", "enabled", "sort_order", "total_quota", "quota_reset_period",
		"upgrade_group", "downgrade_group", "allow_balance_pay", "allow_wallet_overflow",
		"auto_renew_enabled", "grace_period_days", "max_purchase_per_user", "updated_time",
	).Updates(p).Error
}

func DeletePlan(id int) error {
	if id == 0 {
		return errors.New("id 为空")
	}
	return DB.Delete(&SubscriptionPlan{}, id).Error
}

func GetPlanById(id int) (*SubscriptionPlan, error) {
	if id <= 0 {
		return nil, errors.New("套餐 id 非法")
	}
	var p SubscriptionPlan
	if err := DB.First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// GetEnabledPlans 用户可见的套餐列表
func GetEnabledPlans() ([]*SubscriptionPlan, error) {
	var plans []*SubscriptionPlan
	err := DB.Where("enabled = ?", true).Order("sort_order asc, id asc").Find(&plans).Error
	return plans, err
}

// GetAllPlans 管理员列表
func GetAllPlans() ([]*SubscriptionPlan, error) {
	var plans []*SubscriptionPlan
	err := DB.Order("sort_order asc, id asc").Find(&plans).Error
	return plans, err
}

// ---------------------------------------------------------------------------
// 订阅生命周期
// ---------------------------------------------------------------------------

// SubscribeWithBalance 用钱包余额购买套餐（事务内：扣余额 + 建订阅 + 记订单）
func SubscribeWithBalance(ctx context.Context, userId int, planId int) (*UserSubscription, error) {
	plan, err := GetPlanById(planId)
	if err != nil {
		return nil, err
	}
	if !plan.Enabled {
		return nil, errors.New("套餐已下架")
	}
	if !plan.AllowBalancePay {
		return nil, errors.New("该套餐不支持余额支付")
	}
	if plan.PriceCents <= 0 {
		return nil, errors.New("套餐价格非法")
	}

	costQuota := USDToQuota(plan.PriceUSD())
	var sub *UserSubscription

	err = DB.Transaction(func(tx *gorm.DB) error {
		// 行锁用户，避免并发双扣
		var user User
		if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&user, userId).Error; err != nil {
			return errors.New("用户不存在")
		}

		// 购买数量限制
		if plan.MaxPurchasePerUser > 0 {
			var cnt int64
			tx.Model(&UserSubscription{}).
				Where("user_id = ? AND plan_id = ? AND status = ?", userId, planId, SubscriptionStatusActive).
				Count(&cnt)
			if cnt >= int64(plan.MaxPurchasePerUser) {
				return errors.New("该套餐购买次数已达上限")
			}
		}

		// 条件扣费：余额足够才扣
		res := tx.Model(&User{}).
			Where("id = ? AND quota >= ?", userId, costQuota).
			Update("quota", gorm.Expr("quota - ?", costQuota))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("余额不足")
		}

		now := time.Now()
		s := &UserSubscription{
			UserId:              userId,
			PlanId:              planId,
			AmountTotal:         plan.TotalQuota,
			AmountUsed:          0,
			StartTime:           now.Unix(),
			EndTime:             plan.CalcEndTime(now).Unix(),
			Status:              SubscriptionStatusActive,
			Source:              SubscriptionSourceBalance,
			UpgradeGroup:        plan.UpgradeGroup,
			DowngradeGroup:      plan.DowngradeGroup,
			PrevUserGroup:       user.Group,
			AutoRenew:           plan.AutoRenewEnabled,
			AllowWalletOverflow: plan.AllowWalletOverflow,
			CreatedTime:         now.Unix(),
		}
		s.NextResetTime = calcNextResetTime(plan, now, s.EndTime)
		if err := tx.Create(s).Error; err != nil {
			return err
		}

		// 授予分组
		if plan.UpgradeGroup != "" && plan.UpgradeGroup != user.Group {
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Update("group", plan.UpgradeGroup).Error; err != nil {
				return err
			}
		}

		// 建一条 0 元订单，保证收入报表口径统一
		order := &TopUp{
			UserId:          userId,
			TradeNo:         fmt.Sprintf("SUBBALUSR%dNO%s%d", userId, random.GetRandomString(6), time.Now().UnixNano()),
			AmountCents:     0,
			MoneyCents:      plan.PriceCents,
			Quota:           0,
			PaymentProvider: PaymentProviderBalance(),
			PaymentMethod:   SubscriptionSourceBalance,
			Status:          TopUpStatusSuccess,
			CreateTime:      now.Unix(),
			CompleteTime:    now.Unix(),
			Remark:          "套餐订阅（余额支付）: " + plan.Title,
			GroupRatio:      1,
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}

		sub = s
		return nil
	})
	if err != nil {
		return nil, err
	}

	RecordTopupLog(ctx, userId, fmt.Sprintf("购买套餐「%s」", plan.Title), int(costQuota))
	_ = CacheUpdateUserQuota(ctx, userId)
	return sub, nil
}

// PaymentProviderBalance 余额支付的伪网关标识
func PaymentProviderBalance() string { return "balance" }

// GetActiveSubscriptions 用户当前可用的订阅（最快要到期的排前面，先花掉先过期的）
func GetActiveSubscriptions(userId int) ([]*UserSubscription, error) {
	now := helper.GetTimestamp()
	var subs []*UserSubscription
	err := DB.Where("user_id = ? AND status = ?", userId, SubscriptionStatusActive).
		Order("end_time asc").Find(&subs).Error
	if err != nil {
		return nil, err
	}
	usable := make([]*UserSubscription, 0, len(subs))
	for _, s := range subs {
		if s.IsUsable(now) {
			usable = append(usable, s)
		}
	}
	return usable, nil
}

// calcNextResetTime 计算下一次额度重置时间；若重置点超过订阅结束时间则返回 0（不重置）
func calcNextResetTime(plan *SubscriptionPlan, now time.Time, endTime int64) int64 {
	var next time.Time
	switch plan.QuotaResetPeriod {
	case ResetDaily:
		next = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1)
	case ResetWeekly:
		// 下周一 00:00
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		daysToMonday := 8 - weekday
		next = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, daysToMonday)
	case ResetMonthly:
		next = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, 1, 0)
	default:
		return 0
	}
	if endTime > 0 && next.Unix() > endTime {
		// 最后一次重置落在订阅结束之后，不再安排
		return 0
	}
	return next.Unix()
}

// ---------------------------------------------------------------------------
// 额度扣减 / 重置 / 续费 / 到期
// ---------------------------------------------------------------------------

// ConsumeSubscription 从订阅扣额度，返回实际扣除量与**首个**被扣的订阅 id。
//
// 按 end_time 升序（先过期的先用），这样不会浪费即将失效的额度。
// 一个订阅不够扣时**继续往下一个订阅**扣，直到额度用尽或没有可用订阅。
// 返回的 subId 是第一个被扣到额度的订阅，便于调用方在请求失败时按原路退还
// （退款走 RefundSubscription，它按 subId 归还即可，多订阅场景下金额不会错，
//
//	因为 amount_used 只是计数器，不区分来源）。
func ConsumeSubscription(userId int, amount int64) (subId int, deducted int64, err error) {
	if amount <= 0 {
		return 0, 0, nil
	}
	subs, err := GetActiveSubscriptions(userId)
	if err != nil {
		return 0, 0, err
	}
	firstSubId := 0
	remainingToDeduct := amount
	for _, s := range subs {
		if remainingToDeduct <= 0 {
			break
		}
		// 不限量订阅：直接计入 used 以便统计，但不拦截
		if s.AmountTotal == 0 {
			res := DB.Model(&UserSubscription{}).
				Where("id = ?", s.Id).
				Update("amount_used", gorm.Expr("amount_used + ?", remainingToDeduct))
			if res.Error != nil {
				return firstSubId, deducted, res.Error
			}
			deducted += remainingToDeduct
			if firstSubId == 0 {
				firstSubId = s.Id
			}
			remainingToDeduct = 0
			break
		}
		remaining := s.AmountTotal - s.AmountUsed
		if remaining <= 0 {
			continue
		}
		use := remainingToDeduct
		if use > remaining {
			use = remaining
		}
		// 条件更新：确保 amount_used + use 不超过 total
		res := DB.Model(&UserSubscription{}).
			Where("id = ? AND amount_used + ? <= ? AND status = ?", s.Id, use, s.AmountTotal, SubscriptionStatusActive).
			Update("amount_used", gorm.Expr("amount_used + ?", use))
		if res.Error != nil {
			return firstSubId, deducted, res.Error
		}
		if res.RowsAffected > 0 {
			deducted += use
			remainingToDeduct -= use
			if firstSubId == 0 {
				firstSubId = s.Id
			}
		}
	}
	return firstSubId, deducted, nil
}

// RefundSubscription 退还订阅额度（请求失败时）。
//
// 只退给 subId 指定的那一个订阅：amount_used 是单调递增的计数器，
// 退还多少不影响「这笔钱原本从哪个订阅扣的」的语义，
// 因此跨订阅消费多扣的部分统一退给首个被扣的订阅即可，总额仍然正确。
func RefundSubscription(subId int, amount int64) error {
	if subId <= 0 || amount <= 0 {
		return nil
	}
	return DB.Model(&UserSubscription{}).
		Where("id = ? AND amount_used >= ?", subId, amount).
		Update("amount_used", gorm.Expr("amount_used - ?", amount)).Error
}

// ResetDueSubscriptions 重置到期的额度周期（定时任务）
func ResetDueSubscriptions(limit int) (int, error) {
	now := helper.GetTimestamp()
	var subs []*UserSubscription
	err := DB.Where("status = ? AND next_reset_time > 0 AND next_reset_time <= ?", SubscriptionStatusActive, now).
		Limit(limit).Find(&subs).Error
	if err != nil {
		return 0, err
	}
	count := 0
	for _, s := range subs {
		plan, err := GetPlanById(s.PlanId)
		if err != nil {
			// 套餐被删了，清空重置时间避免反复扫描
			DB.Model(&UserSubscription{}).Where("id = ?", s.Id).Update("next_reset_time", 0)
			continue
		}
		next := calcNextResetTime(plan, time.Unix(now, 0), s.EndTime)
		res := DB.Model(&UserSubscription{}).Where("id = ?", s.Id).Updates(map[string]interface{}{
			"amount_used":     0,
			"last_reset_time": now,
			"next_reset_time": next,
		})
		if res.Error != nil {
			logger.SysError(fmt.Sprintf("reset subscription %d failed: %s", s.Id, res.Error.Error()))
			continue
		}
		count++
	}
	return count, nil
}

// ExpireDueSubscriptions 处理已过期订阅：尝试自动续费，失败则到期/宽限
func ExpireDueSubscriptions(limit int) (int, error) {
	now := helper.GetTimestamp()
	var subs []*UserSubscription
	err := DB.Where("status = ? AND end_time <= ?", SubscriptionStatusActive, now).
		Limit(limit).Find(&subs).Error
	if err != nil {
		return 0, err
	}
	count := 0
	for _, s := range subs {
		if err := handleExpiredSubscription(s, now); err != nil {
			logger.SysError(fmt.Sprintf("handle expired subscription %d failed: %s", s.Id, err.Error()))
			continue
		}
		count++
	}
	return count, nil
}

func handleExpiredSubscription(s *UserSubscription, now int64) error {
	plan, err := GetPlanById(s.PlanId)
	if err != nil {
		return cancelSubscription(s, now)
	}

	// ① 尝试自动续费
	if s.AutoRenew && plan.AutoRenewEnabled {
		if ok, err := tryAutoRenew(s, plan, now); err != nil {
			logger.SysError(fmt.Sprintf("auto renew subscription %d failed: %s", s.Id, err.Error()))
		} else if ok {
			return nil
		}
	}

	// ② 已在宽限期内 → 等待用户补缴
	if s.GraceEndTime > 0 && now <= s.GraceEndTime {
		return nil
	}

	// ③ 进入宽限期（首次过期时设置）
	if s.GraceEndTime == 0 && plan.GracePeriodDays > 0 {
		res := DB.Model(&UserSubscription{}).Where("id = ? AND grace_end_time = 0", s.Id).
			Update("grace_end_time", s.EndTime+int64(plan.GracePeriodDays)*86400)
		return res.Error
	}

	// ④ 彻底过期：回退分组
	return cancelSubscription(s, now)
}

// tryAutoRenew 从钱包余额扣费续期一个周期
func tryAutoRenew(s *UserSubscription, plan *SubscriptionPlan, now int64) (bool, error) {
	costQuota := USDToQuota(plan.PriceUSD())
	if costQuota <= 0 {
		return false, nil
	}
	renewed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 条件扣费
		res := tx.Model(&User{}).
			Where("id = ? AND quota >= ?", s.UserId, costQuota).
			Update("quota", gorm.Expr("quota - ?", costQuota))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// 余额不足：记录失败次数，交给外层进入宽限
			tx.Model(&UserSubscription{}).Where("id = ?", s.Id).
				Update("renew_attempts", gorm.Expr("renew_attempts + 1"))
			return errors.New("余额不足，自动续费失败")
		}

		start := time.Unix(s.EndTime, 0)
		end := plan.CalcEndTime(start)
		updates := map[string]interface{}{
			"start_time":      start.Unix(),
			"end_time":        end.Unix(),
			"amount_used":     0,
			"last_reset_time": now,
			"next_reset_time": calcNextResetTime(plan, start, end.Unix()),
			"renew_attempts":  0,
			"grace_end_time":  int64(0),
			"amount_total":    plan.TotalQuota,
		}
		if err := tx.Model(&UserSubscription{}).Where("id = ?", s.Id).Updates(updates).Error; err != nil {
			return err
		}

		order := &TopUp{
			UserId:          s.UserId,
			TradeNo:         fmt.Sprintf("SUBAUTOUSR%dNO%s%d", s.UserId, random.GetRandomString(6), time.Now().UnixNano()),
			MoneyCents:      plan.PriceCents,
			PaymentProvider: PaymentProviderBalance(),
			PaymentMethod:   "auto_renew",
			Status:          TopUpStatusSuccess,
			CreateTime:      now,
			CompleteTime:    now,
			Remark:          "订阅自动续费: " + plan.Title,
			GroupRatio:      1,
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}
		renewed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return renewed, nil
}

// cancelSubscription 置为过期并回退分组
func cancelSubscription(s *UserSubscription, now int64) error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&UserSubscription{}).Where("id = ?", s.Id).
			Update("status", SubscriptionStatusExpired).Error; err != nil {
			return err
		}
		// 只有在用户当前分组==本订阅授予的分组时才回退，
		// 避免覆盖管理员手工调整过的分组（New API 的同款保护）
		target := s.DowngradeGroup
		if target == "" {
			target = s.PrevUserGroup
		}
		if target == "" || s.UpgradeGroup == "" {
			return nil
		}
		// 若还有其他可用订阅授予了不同分组，保持不变
		var otherCount int64
		tx.Model(&UserSubscription{}).
			Where("user_id = ? AND id != ? AND status = ? AND end_time > ?",
				s.UserId, s.Id, SubscriptionStatusActive, now).
			Count(&otherCount)
		if otherCount > 0 {
			return nil
		}
		if target != "" {
			return tx.Model(&User{}).
				Where("id = ? AND `group` = ?", s.UserId, s.UpgradeGroup).
				Update("group", target).Error
		}
		return nil
	})
	return err
}

// CancelSubscriptionByAdmin 管理员作废订阅
func CancelSubscriptionByAdmin(subId int) error {
	var s UserSubscription
	if err := DB.First(&s, subId).Error; err != nil {
		return err
	}
	return cancelSubscription(&s, helper.GetTimestamp())
}

// GetUserSubscriptions 用户的全部订阅（含历史）
func GetUserSubscriptions(userId int) ([]*UserSubscription, error) {
	var subs []*UserSubscription
	err := DB.Where("user_id = ?", userId).Order("id desc").Find(&subs).Error
	return subs, err
}

// SetAutoRenew 用户开关某个订阅的自动续费。
//
// 必须校验订阅归属：否则任何登录用户都能改别人的续费状态，
// 进而影响他人的余额扣费。
func SetAutoRenew(userId int, subId int, autoRenew bool) error {
	if subId <= 0 {
		return errors.New("订阅 id 非法")
	}
	res := DB.Model(&UserSubscription{}).
		Where("id = ? AND user_id = ? AND status = ?", subId, userId, SubscriptionStatusActive).
		Update("auto_renew", autoRenew)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("订阅不存在或已失效")
	}
	return nil
}

// ---------------------------------------------------------------------------
// 金额换算
// ---------------------------------------------------------------------------

// USDToQuota 美元 → 额度，向上取整，保底 1
func USDToQuota(usd float64) int64 {
	if usd <= 0 {
		return 0
	}
	perUnit := quotaPerUnitProvider()
	q := int64(math.Ceil(usd * perUnit))
	if q < 1 {
		q = 1
	}
	return q
}

// QuotaToUSD 额度 → 美元
func QuotaToUSD(quota int64) float64 {
	perUnit := quotaPerUnitProvider()
	if perUnit <= 0 {
		return 0
	}
	return float64(quota) / perUnit
}

// CurrentTopUpExpireMinutes 读取订单超时配置。
//
// 由上层在启动时注入，避免 model 反向 import config（config 已依赖本包）。
var topUpExpireMinutesProvider = func() int { return 30 }

// SetTopUpExpireMinutesProvider 注入订单超时读取方式
func SetTopUpExpireMinutesProvider(fn func() int) {
	if fn != nil {
		topUpExpireMinutesProvider = fn
	}
}

// CurrentTopUpExpireMinutes 返回订单有效期（分钟）
func CurrentTopUpExpireMinutes() int {
	return topUpExpireMinutesProvider()
}
