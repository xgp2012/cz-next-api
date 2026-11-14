package model

import (
	"time"
)

// ============================================================================
// 营收聚合
//
// 口径说明（容易踩坑，集中写在这里，前端与运营都以此为准）：
//
//   - 收入 来自 top_ups 表中 status = success 的订单，按 complete_time 归日。
//     单位是**分（cents）**，字段名统一带 _cents 后缀，避免和"额度"混淆。
//     为什么不用 Quota 计收入：订阅订单的 Quota 为 0（它是"买来的额度"，
//     不是"入账额度"），用 Quota 会让订阅收入整体消失。
//
//   - 消耗 来自 logs 表中 type = LogTypeConsume 的行，按 created_at 归日，
//     单位是**额度（quota）**。折算成钱需要除以 QuotaPerUnit。
//
//   - 毛利 = (收入 - 消耗折钱) / 收入。这是**账面口径**：用户充了钱没消费
//     的部分仍算作已确认收入，所以新站点毛利会虚高，需要结合"未消耗余额"看。
//     这个字段是给运营看趋势的，不能当财务口径用。
// ============================================================================

// RevenueDailyRow 单日聚合
type RevenueDailyRow struct {
	Date          string `json:"date"`
	IncomeCents   int64  `json:"income_cents"`
	ConsumedQuota int64  `json:"consumed_quota"`
	RequestCount  int64  `json:"request_count"`
	ActiveUsers   int64  `json:"active_users"`
	// PayingUsers 当日有成功入账的去重用户数（不是"当日消费用户数"）
	PayingUsers int64 `json:"paying_users"`
}

// ModelRevenueRow 模型维度的消耗排行
type ModelRevenueRow struct {
	Model string `json:"model"`
	Quota int64  `json:"quota"`
	Count int64  `json:"count"`
}

// UserRevenueRow 用户维度的消耗排行
type UserRevenueRow struct {
	UserId   int    `json:"user_id"`
	Username string `json:"username"`
	Quota    int64  `json:"quota"`
	Count    int64  `json:"count"`
}

// RevenueStats 看板聚合结果
//
// 金额字段一律以 _cents 结尾且为整数分；比例字段为 0~1 的小数。
type RevenueStats struct {
	Days int `json:"days"`

	// ---- 汇总（区间内）----
	TotalIncomeCents   int64 `json:"total_income_cents"`
	TotalConsumedQuota int64 `json:"total_consumed_quota"`
	// TotalConsumedCents 是把消耗额度折算成钱后的值，用于和收入同口径比较
	TotalConsumedCents int64 `json:"total_consumed_cents"`
	// GrossMarginCents 账面毛利（分），可能为负（补贴期）
	GrossMarginCents int64 `json:"gross_margin_cents"`
	// GrossMargin 毛利率 = 毛利 / 收入；收入为 0 时为 0，避免除零
	GrossMargin float64 `json:"gross_margin"`
	// ArpuCents 人均收入（分）= 总收入 / 付费用户数
	ArpuCents int64 `json:"arpu_cents"`

	PayingUsers int64 `json:"paying_users"`
	// ConsumingUsers 区间内有消费的去重用户数
	ConsumingUsers int64 `json:"consuming_users"`

	// ---- 订阅 ----
	ActiveSubscriptions    int64   `json:"active_subscriptions"`
	AutoRenewSubscriptions int64   `json:"auto_renew_subscriptions"`
	RenewRate              float64 `json:"renew_rate"`

	// ---- 时间序列与排行 ----
	Daily     []RevenueDailyRow `json:"daily"`
	TopModels []ModelRevenueRow `json:"top_models"`
	TopUsers  []UserRevenueRow  `json:"top_users"`

	// ---- 口径说明（随响应下发，前端可直接展示，避免口径争议）----
	Note string `json:"note"`
}

// dayKey 把 unix 时间戳转成 YYYY-MM-DD（本地时区）
func dayKey(ts int64) string {
	return time.Unix(ts, 0).Format("2006-01-02")
}

// consumedQuotaToCents 把消耗额度折算成"分"
func consumedQuotaToCents(quota int64) int64 {
	perUnit := quotaPerUnitProvider()
	if perUnit <= 0 {
		return 0
	}
	// 额度 → 美元 → 分
	return int64(float64(quota) / perUnit * 100)
}

// BuildRevenueStats 聚合近 days 天的营收数据
//
// 为什么在 Go 侧归日而不是用 SQL 的日期函数：SQLite 用 strftime、
// MySQL 用 FROM_UNIXTIME、PostgreSQL 用 to_timestamp，三套方言各不相同，
// 写三份分支的维护成本高于把行拉回来归并。数据量受 days 限制（≤90 天），
// 且看板是低频接口，这个取舍是划算的。
func BuildRevenueStats(days int) (*RevenueStats, error) {
	stats := &RevenueStats{
		Days: days,
		Note: "收入按订单成功时间归日，单位为分；消耗按日志时间归日，单位为额度。" +
			"毛利为账面口径（已确认收入 - 已消耗成本），未消费余额仍计入收入。",
	}
	if DB == nil {
		stats.Daily = []RevenueDailyRow{}
		stats.TopModels = []ModelRevenueRow{}
		stats.TopUsers = []UserRevenueRow{}
		return stats, nil
	}
	if days <= 0 {
		days = 7
	}
	stats.Days = days

	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, -(days - 1))
	startTS := start.Unix()

	// ---- 1. 收入：成功订单按 complete_time 归日 ----
	type rawTopUp struct {
		CompleteTime int64
		MoneyCents   int64
		UserId       int
	}
	var topUps []rawTopUp
	if err := DB.Model(&TopUp{}).
		Select("complete_time, money_cents, user_id").
		Where("status = ? AND complete_time >= ?", TopUpStatusSuccess, startTS).
		Find(&topUps).Error; err != nil {
		return nil, err
	}
	incomeByDay := make(map[string]int64)
	payingUsersByDay := make(map[string]map[int]struct{})
	payingUsersAll := make(map[int]struct{})
	for _, t := range topUps {
		k := dayKey(t.CompleteTime)
		incomeByDay[k] += t.MoneyCents
		stats.TotalIncomeCents += t.MoneyCents
		if payingUsersByDay[k] == nil {
			payingUsersByDay[k] = make(map[int]struct{})
		}
		payingUsersByDay[k][t.UserId] = struct{}{}
		payingUsersAll[t.UserId] = struct{}{}
	}

	// ---- 2. 消耗：logs 按天聚合 ----
	type consumeRow struct {
		CreatedAt int64
		Quota     int
		UserId    int
	}
	var consumes []consumeRow
	if LOG_DB != nil {
		if err := LOG_DB.Model(&Log{}).
			Select("created_at, quota, user_id").
			Where("type = ? AND created_at >= ?", LogTypeConsume, startTS).
			Find(&consumes).Error; err != nil {
			return nil, err
		}
	}
	consumeByDay := make(map[string]int64)
	countByDay := make(map[string]int64)
	usersByDay := make(map[string]map[int]struct{})
	consumingUsersAll := make(map[int]struct{})
	for _, l := range consumes {
		k := dayKey(l.CreatedAt)
		consumeByDay[k] += int64(l.Quota)
		countByDay[k]++
		stats.TotalConsumedQuota += int64(l.Quota)
		if usersByDay[k] == nil {
			usersByDay[k] = make(map[int]struct{})
		}
		usersByDay[k][l.UserId] = struct{}{}
		consumingUsersAll[l.UserId] = struct{}{}
	}

	// ---- 3. 按天铺满（没有数据的日期也要出现，否则折线图会断档）----
	daily := make([]RevenueDailyRow, 0, days)
	for i := 0; i < days; i++ {
		k := start.AddDate(0, 0, i).Format("2006-01-02")
		daily = append(daily, RevenueDailyRow{
			Date:          k,
			IncomeCents:   incomeByDay[k],
			ConsumedQuota: consumeByDay[k],
			RequestCount:  countByDay[k],
			ActiveUsers:   int64(len(usersByDay[k])),
			PayingUsers:   int64(len(payingUsersByDay[k])),
		})
	}
	stats.Daily = daily

	// ---- 4. 派生指标 ----
	stats.PayingUsers = int64(len(payingUsersAll))
	stats.ConsumingUsers = int64(len(consumingUsersAll))
	stats.TotalConsumedCents = consumedQuotaToCents(stats.TotalConsumedQuota)
	stats.GrossMarginCents = stats.TotalIncomeCents - stats.TotalConsumedCents
	if stats.TotalIncomeCents > 0 {
		stats.GrossMargin = float64(stats.GrossMarginCents) / float64(stats.TotalIncomeCents)
	}
	if stats.PayingUsers > 0 {
		stats.ArpuCents = stats.TotalIncomeCents / stats.PayingUsers
	}

	// ---- 5. 订阅指标 ----
	nowTS := now.Unix()
	DB.Model(&UserSubscription{}).
		Where("status = ? AND end_time > ?", SubscriptionStatusActive, nowTS).
		Count(&stats.ActiveSubscriptions)
	DB.Model(&UserSubscription{}).
		Where("status = ? AND end_time > ? AND auto_renew = ?", SubscriptionStatusActive, nowTS, true).
		Count(&stats.AutoRenewSubscriptions)
	if stats.ActiveSubscriptions > 0 {
		stats.RenewRate = float64(stats.AutoRenewSubscriptions) / float64(stats.ActiveSubscriptions)
	}

	// ---- 6. 排行榜 ----
	topModels := make([]ModelRevenueRow, 0, 10)
	if LOG_DB != nil {
		LOG_DB.Model(&Log{}).
			Select("model_name as model, SUM(quota) as quota, COUNT(*) as count").
			Where("type = ? AND created_at >= ?", LogTypeConsume, startTS).
			Group("model_name").
			Order("quota desc").
			Limit(10).
			Scan(&topModels)
	}
	stats.TopModels = topModels

	// 用户排行需要 join 出用户名。为了不依赖具体方言的关联语法，
	// 先取 Top N 的 (user_id, quota)，再逐批查用户名。
	type userAgg struct {
		UserId int
		Quota  int64
		Count  int64
	}
	var userAggs []userAgg
	if LOG_DB != nil {
		LOG_DB.Model(&Log{}).
			Select("user_id, SUM(quota) as quota, COUNT(*) as count").
			Where("type = ? AND created_at >= ?", LogTypeConsume, startTS).
			Group("user_id").
			Order("quota desc").
			Limit(10).
			Scan(&userAggs)
	}
	topUsers := make([]UserRevenueRow, 0, len(userAggs))
	if len(userAggs) > 0 {
		ids := make([]int, 0, len(userAggs))
		for _, u := range userAggs {
			ids = append(ids, u.UserId)
		}
		var users []User
		DB.Select("id, username").Where("id IN ?", ids).Find(&users)
		nameById := make(map[int]string, len(users))
		for _, u := range users {
			nameById[u.Id] = u.Username
		}
		for _, u := range userAggs {
			topUsers = append(topUsers, UserRevenueRow{
				UserId:   u.UserId,
				Username: nameById[u.UserId],
				Quota:    u.Quota,
				Count:    u.Count,
			})
		}
	}
	stats.TopUsers = topUsers

	return stats, nil
}
