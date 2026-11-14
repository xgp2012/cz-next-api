package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/songquanpeng/one-api/common/helper"
	"github.com/songquanpeng/one-api/common/logger"
)

// ============================================================================
// 钱包充值订单（差异化点：统一金额语义）
//
// New API 的问题：不同网关对 Amount/Money 的解释不一致（Epay 用 Amount×QuotaPerUnit，
// Stripe 用 Money×QuotaPerUnit，Creem 直接把 Amount 当 quota），导致同一笔钱入账
// 额度不同，运维对账时极易踩坑。
//
// 这里的约定（**唯一口径**）：
//   Amount   = 用户购买的「美元金额」（decimal 语义，落库为分避免浮点误差）
//   Quota    = 最终入账额度，= Amount × QuotaPerUnit，由服务端唯一计算
//   Money    = 实际支付金额（可能因分组折扣 ≠ Amount），仅用于对账展示
// 所有网关走同一条入账路径，不存在按网关分叉。
// ============================================================================

const (
	TopUpStatusPending  = "pending"
	TopUpStatusSuccess  = "success"
	TopUpStatusFailed   = "failed"
	TopUpStatusExpired  = "expired"
	TopUpStatusRefunded = "refunded"
)

const (
	PaymentProviderEpay    = "epay"
	PaymentProviderWebhook = "webhook" // 通用自定义网关，按签名回调
	PaymentProviderManual  = "manual"  // 管理员手工入账
)

// TopUp 充值订单
type TopUp struct {
	Id     int `json:"id"`
	UserId int `json:"user_id" gorm:"index"`
	// TradeNo 商户订单号，全局唯一。回调幂等的锚点。
	TradeNo string `json:"trade_no" gorm:"type:varchar(64);uniqueIndex"`
	// Amount 购买金额（美元），单位：分。100 = $1.00
	AmountCents int64 `json:"amount_cents" gorm:"bigint;default:0"`
	// Money 实际支付金额（美元），单位：分；用于对账展示
	MoneyCents int64 `json:"money_cents" gorm:"bigint;default:0"`
	// Quota 本次入账额度（服务端唯一计算）
	Quota int64 `json:"quota" gorm:"bigint;default:0"`

	PaymentMethod   string `json:"payment_method" gorm:"type:varchar(50)"`   // alipay / wxpay / custom1
	PaymentProvider string `json:"payment_provider" gorm:"type:varchar(50)"` // epay / webhook / manual

	Status       string `json:"status" gorm:"type:varchar(20);default:'pending';index"`
	CreateTime   int64  `json:"create_time" gorm:"bigint;index"`
	CompleteTime int64  `json:"complete_time" gorm:"bigint;default:0"`
	// CallbackPayload 网关原始回调（截断保存，便于排查）
	CallbackPayload string `json:"-" gorm:"type:text"`
	// Remark 备注：管理员手工入账时记录原因
	Remark string `json:"remark" gorm:"type:varchar(255)"`

	GroupRatio float64 `json:"group_ratio" gorm:"type:double;default:1"`
}

func (TopUp) TableName() string {
	return "top_ups"
}

func (t *TopUp) AmountUSD() float64 {
	return float64(t.AmountCents) / 100.0
}

func (t *TopUp) MoneyUSD() float64 {
	return float64(t.MoneyCents) / 100.0
}

// ---------------------------------------------------------------------------
// 订单号生成
// ---------------------------------------------------------------------------

// tradeNoSeq 进程内单调自增序号，用于消解同一毫秒内的并发下单冲突。
//
// 为什么不能只靠随机段：random.GetRandomString 内部每次都会 rand.Seed(time.Now())，
// 同一纳秒内连续调用会得到完全相同的序列，于是「同用户 + 同毫秒」的两单必然撞号。
// 订单号是 UNIQUE 索引，撞号会让下单直接失败（用户看到报错），必须根除。
// 自增序号在单节点内绝对唯一，配合毫秒时间戳即全局唯一。
var tradeNoSeq uint64

// GenerateTopUpTradeNo 生成唯一订单号：USR{userId}NO{base36 序号}{ms 时间戳}
//
// 字符集限制在 [0-9A-Za-z]，因为 ValidateTradeNo 会拒绝其他字符，
// 且 EPay 的 out_trade_no 对特殊字符不友好。
func GenerateTopUpTradeNo(userId int) string {
	seq := atomic.AddUint64(&tradeNoSeq, 1)
	return fmt.Sprintf("USR%dNO%s%s", userId,
		strconv.FormatUint(seq, 36),
		strconv.FormatInt(time.Now().UnixNano()/1e6, 36))
}

// ValidateTradeNo 校验订单号格式，避免把任意字符串当订单号查库
func ValidateTradeNo(tradeNo string) error {
	if tradeNo == "" {
		return errors.New("订单号为空")
	}
	if len(tradeNo) > 64 {
		return errors.New("订单号过长")
	}
	// 只允许字母数字，防止 SQL/日志注入
	for _, r := range tradeNo {
		if !(r >= '0' && r <= '9') && !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') {
			return errors.New("订单号包含非法字符")
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 错误
// ---------------------------------------------------------------------------

var (
	ErrTopUpNotFound         = errors.New("订单不存在")
	ErrTopUpStatusInvalid    = errors.New("订单状态不允许此操作")
	ErrTopUpAlreadyDone      = errors.New("订单已完成") // 幂等：调用方应视为成功
	ErrTopUpProviderMismatch = errors.New("支付网关与订单不匹配")
	ErrWalletQuotaLimit      = errors.New("钱包额度已达上限")
	ErrAmountTooLow          = errors.New("支付金额过低")
	ErrQuotaOverflow         = errors.New("入账额度溢出")
)

// ---------------------------------------------------------------------------
// 下单
// ---------------------------------------------------------------------------

// CreateTopUp 创建一个 pending 订单。quota 由调用方用 CalcTopUpQuota 计算。
// 注意：这里**不**动用户额度，只有回调确认后才入账。
func CreateTopUp(t *TopUp) error {
	if t.UserId <= 0 {
		return errors.New("用户无效")
	}
	if t.AmountCents <= 0 {
		return ErrAmountTooLow
	}
	if t.Quota <= 0 {
		return ErrQuotaOverflow
	}
	if t.TradeNo == "" {
		t.TradeNo = GenerateTopUpTradeNo(t.UserId)
	}
	if err := ValidateTradeNo(t.TradeNo); err != nil {
		return err
	}
	if t.Status == "" {
		t.Status = TopUpStatusPending
	}
	if t.CreateTime == 0 {
		t.CreateTime = helper.GetTimestamp()
	}
	return DB.Create(t).Error
}

// GetTopUpByTradeNo 按订单号查询
func GetTopUpByTradeNo(tradeNo string) (*TopUp, error) {
	if err := ValidateTradeNo(tradeNo); err != nil {
		return nil, err
	}
	var t TopUp
	err := DB.Where("trade_no = ?", tradeNo).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTopUpNotFound
	}
	return &t, err
}

// GetUserTopUps 用户自己的订单（倒序）
func GetUserTopUps(userId int, startIdx int, num int) ([]*TopUp, error) {
	var items []*TopUp
	err := DB.Where("user_id = ?", userId).Order("id desc").Limit(num).Offset(startIdx).Find(&items).Error
	return items, err
}

// GetAllTopUps 管理员查看全部订单
func GetAllTopUps(startIdx int, num int, provider string, status string) ([]*TopUp, error) {
	query := DB.Model(&TopUp{})
	if provider != "" {
		query = query.Where("payment_provider = ?", provider)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var items []*TopUp
	err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&items).Error
	return items, err
}

// ---------------------------------------------------------------------------
// 入账（核心：幂等 + 原子）
// ---------------------------------------------------------------------------

// CompleteTopUp 把订单标记为成功并给用户加额度。
//
// 幂等保证（照抄并强化 New API 的设计）：
//  1. 整个流程在一个事务里，先对订单行 SELECT ... FOR UPDATE 加行锁，
//     跨节点安全（不依赖进程内锁或 Redis）。
//  2. 状态已是 success → 返回 ErrTopUpAlreadyDone，调用方视为成功（不重复入账）。
//  3. 状态不是 pending → 返回 ErrTopUpStatusInvalid。
//  4. 额度累加用**条件 UPDATE**（WHERE quota <= Max - ?），而不是「先读再写」，
//     因此两个并发回调不可能同时绕过上限。
//  5. 非事务性副作用（日志、缓存刷新）在事务提交**之后**执行。
func CompleteTopUp(ctx context.Context, tradeNo string, provider string, payload string, maxWalletQuota int64) (alreadyDone bool, err error) {
	if err := ValidateTradeNo(tradeNo); err != nil {
		return false, err
	}

	var (
		userId    int
		quota     int64
		amountUSD float64
	)

	err = DB.Transaction(func(tx *gorm.DB) error {
		var t TopUp
		// 行锁：MySQL/PostgreSQL 生效；SQLite 忽略但单写者特性已保证串行
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("trade_no = ?", tradeNo).First(&t).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTopUpNotFound
			}
			return err
		}

		// 网关不匹配：显式拒绝，防止 A 网关的回调核销 B 网关的订单
		if provider != "" && t.PaymentProvider != provider {
			return ErrTopUpProviderMismatch
		}

		// 幂等：已完成 → 静默成功
		if t.Status == TopUpStatusSuccess {
			return ErrTopUpAlreadyDone
		}
		if t.Status != TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		// 条件 UPDATE：只有「加完之后仍不超过上限」才生效
		var res *gorm.DB
		if maxWalletQuota > 0 {
			res = tx.Model(&User{}).
				Where("id = ? AND quota <= ?", t.UserId, maxWalletQuota-t.Quota).
				Update("quota", gorm.Expr("quota + ?", t.Quota))
		} else {
			res = tx.Model(&User{}).Where("id = ?", t.UserId).
				Update("quota", gorm.Expr("quota + ?", t.Quota))
		}
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// 区分「用户不存在」与「超过上限」
			var cnt int64
			tx.Model(&User{}).Where("id = ?", t.UserId).Count(&cnt)
			if cnt == 0 {
				return errors.New("用户不存在")
			}
			return ErrWalletQuotaLimit
		}

		// 标记订单完成
		upd := map[string]interface{}{
			"status":           TopUpStatusSuccess,
			"complete_time":    helper.GetTimestamp(),
			"callback_payload": truncate(payload, 4000),
		}
		if err := tx.Model(&TopUp{}).Where("id = ?", t.Id).Updates(upd).Error; err != nil {
			return err
		}

		userId = t.UserId
		quota = t.Quota
		amountUSD = t.AmountUSD()
		return nil
	})

	if errors.Is(err, ErrTopUpAlreadyDone) {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	// ---- 事务外副作用 ----
	RecordTopupLog(ctx, userId, fmt.Sprintf("在线充值 $%.2f（%s）", amountUSD, provider), int(quota))
	if e := CacheUpdateUserQuota(ctx, userId); e != nil {
		logger.Error(ctx, "failed to refresh user quota cache: "+e.Error())
	}
	return false, nil
}

// AdminCompleteTopUp 管理员手工结算（对账补偿用）
func AdminCompleteTopUp(ctx context.Context, tradeNo string, remark string) error {
	if remark == "" {
		return errors.New("请填写备注")
	}
	t, err := GetTopUpByTradeNo(tradeNo)
	if err != nil {
		return err
	}
	ok, err := CompleteTopUp(ctx, tradeNo, "", "manual:"+remark, 0)
	if err == nil && ok {
		// 已经是成功状态，视为成功
		return nil
	}
	if err != nil {
		return err
	}
	logger.SysLog(fmt.Sprintf("topup %s manually completed by admin, remark=%s", t.TradeNo, remark))
	return nil
}

// ExpireStaleTopUps 把超时未支付的订单标记为过期（定时任务调用）
func ExpireStaleTopUps(olderThan time.Duration) (int64, error) {
	deadline := helper.GetTimestamp() - int64(olderThan.Seconds())
	res := DB.Model(&TopUp{}).
		Where("status = ? AND create_time < ?", TopUpStatusPending, deadline).
		Update("status", TopUpStatusExpired)
	return res.RowsAffected, res.Error
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ---------------------------------------------------------------------------
// 充值信息（前端 /api/user/topup/info 用）
// ---------------------------------------------------------------------------

// PayMethod 一种支付方式
type PayMethod struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Color    string  `json:"color"`
	MinTopUp float64 `json:"min_topup"`
}

// ParsePayMethods 解析 PayMethods 选项（JSON 数组）
//
// 格式：[{"name":"支付宝","type":"alipay","color":"#1677ff","min_topup":"10"}]
func ParsePayMethods(raw string) []PayMethod {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var methods []PayMethod
	if err := jsonUnmarshal(raw, &methods); err != nil {
		logger.SysError("invalid PayMethods option: " + err.Error())
		return nil
	}
	return methods
}
