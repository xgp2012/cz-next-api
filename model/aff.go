package model

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/songquanpeng/one-api/common/helper"
	"github.com/songquanpeng/one-api/common/logger"
)

// ============================================================================
// 邀请分成（差异化点 3：真实 revenue share）
//
// New API 的邀请奖励是「注册时一次性发放 + 进入 pending 池手工转移」，
// 官方文档自己也承认这不是分成（no revenue-share / no commission percentage）。
//
// 这里升级为**持续分成**：
//   被邀请人每次消费 → 按 CommissionRate 计算佣金 → 记入 AffCommission 流水
//   → 累加到邀请人的 aff_quota 待结算池 → 邀请人可一键「转正」到钱包余额
//
// 全链路可审计：每一笔佣金都能追溯到具体的消费日志与订单。
// ============================================================================

const (
	AffCommissionStatusPending   = "pending"   // 待结算
	AffCommissionStatusSettled   = "settled"   // 已入账
	AffCommissionStatusCancelled = "cancelled" // 已撤销
)

// AffCommission 一笔邀请佣金流水
type AffCommission struct {
	Id int `json:"id" gorm:"primaryKey"`

	InviterId int `json:"inviter_id" gorm:"index:idx_aff_inviter_status,priority:1"`
	InviteeId int `json:"invitee_id" gorm:"index"`

	// SourceType 佣金来源：consume（消费分成）/ topup（充值分成）
	SourceType string `json:"source_type" gorm:"type:varchar(20);default:'consume'"`
	// SourceQuota 被邀请人本次消费的原始额度
	SourceQuota int64 `json:"source_quota" gorm:"bigint;default:0"`
	// Rate 本次分成比例（快照，便于事后追溯，不受配置变更影响）
	Rate float64 `json:"rate" gorm:"type:double;default:0"`
	// CommissionQuota 本笔佣金额度
	CommissionQuota int64 `json:"commission_quota" gorm:"bigint;default:0"`

	Status string `json:"status" gorm:"type:varchar(20);default:'pending';index:idx_aff_inviter_status,priority:2"`

	// LogId 关联的消费日志（可空）
	LogId int64 `json:"log_id" gorm:"bigint;default:0;index"`
	// TradeNo 关联的充值订单号（充值分成时使用）
	TradeNo string `json:"trade_no" gorm:"type:varchar(64);default:''"`

	CreatedTime int64 `json:"created_time" gorm:"bigint;index"`
	SettledTime int64 `json:"settled_time" gorm:"bigint;default:0"`
}

func (AffCommission) TableName() string {
	return "aff_commissions"
}

// ------------------------- 用户侧的推广账户字段 -------------------------
// 为保持 User 表向后兼容，新增的字段通过 AutoMigrate 自动补列。
// aff_quota / aff_history_quota 存在 user_aff_stats 表里，避免改动 User 结构
// 影响所有现有的 Select("*") 查询与 JSON 输出。

// UserAffStats 邀请人的分成统计（与 User 一对一）
type UserAffStats struct {
	UserId int `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	// PendingQuota 待结算（已产生但未转正的佣金）
	PendingQuota int64 `json:"pending_quota" gorm:"bigint;default:0"`
	// HistoryQuota 历史累计佣金（永不减少）
	HistoryQuota int64 `json:"history_quota" gorm:"bigint;default:0"`
	// InviteCount 成功邀请人数
	InviteCount int   `json:"invite_count" gorm:"type:int;default:0"`
	UpdatedTime int64 `json:"updated_time" gorm:"bigint"`
}

func (UserAffStats) TableName() string {
	return "user_aff_stats"
}

// GetAffStats 读取推广统计，不存在时返回零值
func GetAffStats(userId int) (*UserAffStats, error) {
	stats := &UserAffStats{UserId: userId}
	if DB == nil {
		return stats, nil
	}
	err := DB.First(stats, "user_id = ?", userId).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return stats, nil
	}
	return stats, err
}

// ensureAffStats 保证行存在（upsert）
func ensureAffStats(tx *gorm.DB, userId int) error {
	return tx.Where(UserAffStats{UserId: userId}).
		FirstOrCreate(&UserAffStats{UserId: userId, UpdatedTime: helper.GetTimestamp()}).Error
}

// RecordInvite 注册时记录一次邀请关系（只统计人数，奖励金额由 Option 决定）
func RecordInvite(inviterId int) error {
	if inviterId <= 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := ensureAffStats(tx, inviterId); err != nil {
			return err
		}
		return tx.Model(&UserAffStats{}).Where("user_id = ?", inviterId).Updates(map[string]interface{}{
			"invite_count": gorm.Expr("invite_count + 1"),
			"updated_time": helper.GetTimestamp(),
		}).Error
	})
}

// ---------------------------------------------------------------------------
// 消费分成
// ---------------------------------------------------------------------------

// CommissionConfig 分成配置，由 Option 注入，避免 model 反向依赖 config
type CommissionConfig struct {
	Enabled bool
	// DefaultRate 默认分成比例（0-1），例如 0.05 = 5%
	DefaultRate float64
	// MinQuota 单笔低于该额度不计佣金，避免海量碎流水
	MinQuota int64
	// Overrides 按邀请人单独覆盖的比例（user_id -> rate）
	Overrides map[int]float64
}

var commissionConfigProvider = func() CommissionConfig {
	return CommissionConfig{Enabled: false, DefaultRate: 0.05}
}

// SetCommissionConfigProvider 由上层（config/main）注入实际配置
func SetCommissionConfigProvider(fn func() CommissionConfig) {
	if fn != nil {
		commissionConfigProvider = fn
	}
}

// CurrentCommissionConfig 读取当前分成配置
func CurrentCommissionConfig() CommissionConfig {
	return commissionConfigProvider()
}

// RateFor 返回某个邀请人的实际分成比例
func (c CommissionConfig) RateFor(inviterId int) float64 {
	if c.Overrides != nil {
		if r, ok := c.Overrides[inviterId]; ok {
			return r
		}
	}
	return c.DefaultRate
}

// AccrueConsumeCommission 在一笔消费结算后累加佣金。
//
// 调用时机：postConsumeQuota 成功后。失败的线上路径不影响主流程（仅记日志）。
func AccrueConsumeCommission(ctx context.Context, userId int, quota int64, logId int64) error {
	cfg := CurrentCommissionConfig()
	if !cfg.Enabled || quota <= 0 {
		return nil
	}
	if cfg.MinQuota > 0 && quota < cfg.MinQuota {
		return nil
	}
	if DB == nil {
		return nil
	}

	// 找出邀请人
	var user User
	if err := DB.Select("id", "inviter_id").First(&user, userId).Error; err != nil {
		return nil // 用户不存在或查询失败，静默跳过
	}
	if user.InviterId <= 0 || user.InviterId == userId {
		return nil
	}
	rate := cfg.RateFor(user.InviterId)
	if rate <= 0 || rate > 1 {
		return nil
	}
	commission := int64(float64(quota) * rate)
	if commission <= 0 {
		return nil
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		rec := &AffCommission{
			InviterId:       user.InviterId,
			InviteeId:       userId,
			SourceType:      "consume",
			SourceQuota:     quota,
			Rate:            rate,
			CommissionQuota: commission,
			Status:          AffCommissionStatusPending,
			LogId:           logId,
			CreatedTime:     helper.GetTimestamp(),
		}
		if err := tx.Create(rec).Error; err != nil {
			return err
		}
		if err := ensureAffStats(tx, user.InviterId); err != nil {
			return err
		}
		return tx.Model(&UserAffStats{}).Where("user_id = ?", user.InviterId).Updates(map[string]interface{}{
			"pending_quota": gorm.Expr("pending_quota + ?", commission),
			"history_quota": gorm.Expr("history_quota + ?", commission),
			"updated_time":  helper.GetTimestamp(),
		}).Error
	})
}

// ---------------------------------------------------------------------------
// 转正（pending → 钱包余额）
// ---------------------------------------------------------------------------

// 转正门槛：配置为 0 时回落到「1 美元等值额度」的默认值。
//
// 为什么做成可配置：固定 1 美元对小额分成场景很不友好——按 5% 分成的站点，
// 被邀请人要消费满 20 美元才能攒够 1 美元佣金，邀请人可能几个月都看不到钱到账，
// 直接影响推广积极性。不同客单价的站点应有不同的门槛。
var affMinTransferQuotaProvider = func() int64 { return 0 }

// SetAffMinTransferQuotaProvider 由上层注入配置（单位：额度）
func SetAffMinTransferQuotaProvider(fn func() int64) {
	if fn != nil {
		affMinTransferQuotaProvider = fn
	}
}

// MinAffTransferQuota 单次转正的最小额度
//
// 优先级：显式配置 > 1 美元等值额度（与 New API 语义一致）
func MinAffTransferQuota() int64 {
	if v := affMinTransferQuotaProvider(); v > 0 {
		return v
	}
	perUnit := quotaPerUnitProvider()
	q := int64(perUnit)
	if q < 1 {
		q = 1
	}
	return q
}

// TransferAffQuotaToWallet 把待结算佣金转入钱包余额
// 返回实际转入的额度
func TransferAffQuotaToWallet(ctx context.Context, userId int) (int64, error) {
	if userId <= 0 {
		return 0, errors.New("用户无效")
	}
	stats, err := GetAffStats(userId)
	if err != nil {
		return 0, err
	}
	if stats.PendingQuota <= 0 {
		return 0, errors.New("暂无可结算的推广收益")
	}
	minTransfer := MinAffTransferQuota()
	if stats.PendingQuota < minTransfer {
		return 0, fmt.Errorf("推广收益需满 %d 额度才能转出", minTransfer)
	}

	var transferred int64
	err = DB.Transaction(func(tx *gorm.DB) error {
		// 原子地清空 pending 并取出金额，避免并发双转
		res := tx.Model(&UserAffStats{}).
			Where("user_id = ? AND pending_quota >= ?", userId, stats.PendingQuota).
			Updates(map[string]interface{}{
				"pending_quota": 0,
				"updated_time":  helper.GetTimestamp(),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("收益正在结算中，请稍后重试")
		}
		transferred = stats.PendingQuota

		if err := tx.Model(&User{}).Where("id = ?", userId).
			Update("quota", gorm.Expr("quota + ?", transferred)).Error; err != nil {
			return err
		}
		// 把对应的 pending 流水标记为已结算
		return tx.Model(&AffCommission{}).
			Where("inviter_id = ? AND status = ?", userId, AffCommissionStatusPending).
			Update("status", AffCommissionStatusSettled).Error
	})
	if err != nil {
		return 0, err
	}

	RecordTopupLog(ctx, userId, "推广收益转正", int(transferred))
	if e := CacheUpdateUserQuota(ctx, userId); e != nil {
		logger.Error(ctx, "failed to refresh quota cache: "+e.Error())
	}
	return transferred, nil
}

// GetAffCommissions 邀请人的佣金流水（分页）
func GetAffCommissions(inviterId int, startIdx int, num int) ([]*AffCommission, error) {
	var items []*AffCommission
	err := DB.Where("inviter_id = ?", inviterId).Order("id desc").
		Limit(num).Offset(startIdx).Find(&items).Error
	return items, err
}

// InviteeBrief 邀请人视角下的一个被邀请人。
//
// 脱敏考量：只暴露 id 与 username，不含邮箱等联系方式——
// 邀请关系不足以成为查看他人隐私的授权。
type InviteeBrief struct {
	Id       int    `json:"id"`
	Username string `json:"username"`
	// Quota 该被邀请人累计消费的额度（用于理解佣金从何而来）
	Quota int64 `json:"quota"`
	// CommissionQuota 该被邀请人累计贡献的佣金
	CommissionQuota int64 `json:"commission_quota"`
	// CommissionCount 贡献的佣金笔数
	CommissionCount int64 `json:"commission_count"`
}

// GetInvitees 邀请人邀请的用户列表，附带每人累计贡献的佣金。
//
// 为什么要把贡献额一并返回：只看一个总数（"我赚了 5000"）无法判断
// 哪个渠道带来的用户质量高，而"哪个被邀请人贡献最多"才是邀请人真正需要的信息。
//
// 实现上刻意用两次查询而不是 JOIN 聚合：User 与 AffCommission 可能落在
// 不同库（LOG_DB / DB 的分离在本项目里是常态），JOIN 会依赖具体部署形态。
func GetInvitees(inviterId int, limit int) ([]*InviteeBrief, error) {
	if inviterId <= 0 {
		return []*InviteeBrief{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	type inviteeRow struct {
		Id       int
		Username string
	}
	var rows []inviteeRow
	if err := DB.Model(&User{}).
		Select("id, username").
		Where("inviter_id = ? AND status != ?", inviterId, UserStatusDeleted).
		Order("id desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*InviteeBrief, 0, len(rows))
	if len(rows) == 0 {
		return items, nil
	}

	ids := make([]int, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.Id)
	}

	// 批量取每个被邀请人的消费额与佣金贡献
	type aggRow struct {
		InviteeId       int
		Quota           int64
		CommissionQuota int64
		CommissionCount int64
	}
	var aggs []aggRow
	DB.Model(&AffCommission{}).
		Select("invitee_id, SUM(source_quota) as quota, SUM(commission_quota) as commission_quota, COUNT(*) as commission_count").
		Where("inviter_id = ? AND invitee_id IN ?", inviterId, ids).
		Group("invitee_id").
		Scan(&aggs)
	aggById := make(map[int]aggRow, len(aggs))
	for _, a := range aggs {
		aggById[a.InviteeId] = a
	}

	for _, r := range rows {
		item := &InviteeBrief{Id: r.Id, Username: r.Username}
		if a, ok := aggById[r.Id]; ok {
			item.Quota = a.Quota
			item.CommissionQuota = a.CommissionQuota
			item.CommissionCount = a.CommissionCount
		}
		items = append(items, item)
	}
	return items, nil
}
