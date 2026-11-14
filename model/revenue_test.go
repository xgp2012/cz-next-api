package model

import (
	"testing"

	"github.com/songquanpeng/one-api/common/helper"
)

// ============================================================================
// 营收聚合测试
//
// 这块的口径最容易被误读（收入是"分"、消耗是"额度"），
// 所以测试的重点不是"数字对不对"，而是**口径与边界**：
//   - 订阅订单 Quota=0，收入不能因此消失
//   - 没有数据的日期要补 0（否则前端折线图断档）
//   - 收入为 0 时不能除零
// ============================================================================

func seedTopUp(t *testing.T, userId int, moneyCents int64, quota int64, status string, completeTime int64) *TopUp {
	t.Helper()
	o := &TopUp{
		UserId:          userId,
		AmountCents:     moneyCents,
		MoneyCents:      moneyCents,
		Quota:           quota,
		PaymentProvider: PaymentProviderEpay,
		PaymentMethod:   "alipay",
		Status:          status,
		CompleteTime:    completeTime,
	}
	if err := CreateTopUp(o); err != nil {
		t.Fatalf("seed topup: %v", err)
	}
	return o
}

func TestBuildRevenueStats_IncomeIsCentsNotQuota(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &TopUp{}, &UserSubscription{}, &Log{}, &User{})
	u := seedUser(t, "rev_user", 0, "")
	now := helper.GetTimestamp()

	// 用户充了 $10 → AmountCents=1000, Quota=5,000,000
	seedTopUp(t, u.Id, 1000, 5_000_000, TopUpStatusSuccess, now)

	// 订阅订单：Quota 为 0（这是"买来的额度"，不是"入账额度"）
	DB.Create(&TopUp{
		UserId: u.Id, AmountCents: 2000, MoneyCents: 2000, Quota: 0,
		PaymentProvider: PaymentProviderBalance(), Status: TopUpStatusSuccess,
		CompleteTime: now,
	})

	// 一笔 pending 订单不应计入收入
	seedTopUp(t, u.Id, 9999, 999_999, TopUpStatusPending, now)

	stats, err := BuildRevenueStats(7)
	if err != nil {
		t.Fatalf("BuildRevenueStats() error: %v", err)
	}

	// 收入 = 1000 + 2000 = 3000 分（$30），且完全不受 Quota 影响
	if stats.TotalIncomeCents != 3000 {
		t.Fatalf("TotalIncomeCents = %d, want 3000 (cents, incl. zero-quota subscription order)",
			stats.TotalIncomeCents)
	}
	// pending 订单没有被算进去
	if stats.PayingUsers != 1 {
		t.Fatalf("PayingUsers = %d, want 1", stats.PayingUsers)
	}
	if stats.ArpuCents != 3000 {
		t.Fatalf("ArpuCents = %d, want 3000", stats.ArpuCents)
	}
}

func TestBuildRevenueStats_DailySeriesHasNoGaps(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &TopUp{}, &UserSubscription{}, &Log{}, &User{})
	u := seedUser(t, "gap_user", 0, "")
	now := helper.GetTimestamp()

	seedTopUp(t, u.Id, 5000, 250_000, TopUpStatusSuccess, now)

	stats, err := BuildRevenueStats(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Daily) != 7 {
		t.Fatalf("Daily has %d entries, want 7 (must be gap-free)", len(stats.Daily))
	}
	// 只有今天有收入，其余 6 天必须是 0 而不是缺失
	var withIncome, zeroIncome int
	for _, d := range stats.Daily {
		if d.IncomeCents > 0 {
			withIncome++
		} else if d.IncomeCents == 0 {
			zeroIncome++
		}
	}
	if withIncome != 1 || zeroIncome != 6 {
		t.Fatalf("daily income distribution: %d days with income, %d zero; want 1 and 6",
			withIncome, zeroIncome)
	}
	// 日期必须严格递增且唯一
	for i := 1; i < len(stats.Daily); i++ {
		if stats.Daily[i].Date <= stats.Daily[i-1].Date {
			t.Fatalf("daily dates not strictly increasing: %q then %q",
				stats.Daily[i-1].Date, stats.Daily[i].Date)
		}
	}
}

func TestBuildRevenueStats_ConsumptionAndGrossMargin(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &TopUp{}, &UserSubscription{}, &Log{}, &User{})
	u := seedUser(t, "margin_user", 0, "")
	now := helper.GetTimestamp()

	// 收入 $10 = 1000 分
	seedTopUp(t, u.Id, 1000, 5_000_000, TopUpStatusSuccess, now)

	// 消耗 2,500,000 额度 = $5 = 500 分
	DB.Create(&Log{
		UserId: u.Id, Type: LogTypeConsume, Quota: 2_500_000,
		CreatedAt: now, ModelName: "gpt-4", Username: "margin_user",
	})

	stats, err := BuildRevenueStats(7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalConsumedQuota != 2_500_000 {
		t.Fatalf("TotalConsumedQuota = %d, want 2500000", stats.TotalConsumedQuota)
	}
	if stats.TotalConsumedCents != 500 {
		t.Fatalf("TotalConsumedCents = %d, want 500", stats.TotalConsumedCents)
	}
	// 毛利 = 1000 - 500 = 500 分，毛利率 0.5
	if stats.GrossMarginCents != 500 {
		t.Fatalf("GrossMarginCents = %d, want 500", stats.GrossMarginCents)
	}
	if stats.GrossMargin < 0.49 || stats.GrossMargin > 0.51 {
		t.Fatalf("GrossMargin = %f, want ~0.5", stats.GrossMargin)
	}
	// 模型排行
	if len(stats.TopModels) != 1 || stats.TopModels[0].Model != "gpt-4" {
		t.Fatalf("TopModels = %+v, want one entry for gpt-4", stats.TopModels)
	}
	if stats.TopModels[0].Quota != 2_500_000 {
		t.Fatalf("TopModels quota = %d, want 2500000", stats.TopModels[0].Quota)
	}
	// 用户排行要带上用户名（否则前端只能显示 id）
	if len(stats.TopUsers) != 1 || stats.TopUsers[0].Username != "margin_user" {
		t.Fatalf("TopUsers = %+v, want username resolved", stats.TopUsers)
	}
}

func TestBuildRevenueStats_NoDivisionByZero(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &TopUp{}, &UserSubscription{}, &Log{}, &User{})

	// 完全没有数据：不能 panic，比例字段必须是 0 而不是 NaN
	stats, err := BuildRevenueStats(3)
	if err != nil {
		t.Fatalf("BuildRevenueStats() on empty data error: %v", err)
	}
	if stats.GrossMargin != 0 {
		t.Fatalf("GrossMargin = %f, want 0 when income is 0", stats.GrossMargin)
	}
	if stats.RenewRate != 0 {
		t.Fatalf("RenewRate = %f, want 0 when no subscriptions", stats.RenewRate)
	}
	if stats.ArpuCents != 0 {
		t.Fatalf("ArpuCents = %d, want 0", stats.ArpuCents)
	}
	if len(stats.Daily) != 3 {
		t.Fatalf("Daily has %d entries, want 3", len(stats.Daily))
	}
	// 排行榜必须是非 nil 空切片，这样前端 JSON 拿到的是 [] 而不是 null
	if stats.TopModels == nil {
		t.Fatal("TopModels should be an empty slice, not nil")
	}
	if stats.TopUsers == nil {
		t.Fatal("TopUsers should be an empty slice, not nil")
	}
}

func TestBuildRevenueStats_SubscriptionMetrics(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &TopUp{}, &UserSubscription{}, &Log{}, &User{})
	u := seedUser(t, "sub_metrics_user", 0, "")
	now := helper.GetTimestamp()

	// 3 个生效订阅，其中 1 个开了自动续费 → RenewRate = 1/3
	for i := 0; i < 3; i++ {
		DB.Create(&UserSubscription{
			UserId: u.Id, PlanId: 1, AmountTotal: 1000,
			StartTime: now - 100, EndTime: now + 86400,
			Status: SubscriptionStatusActive, AutoRenew: i == 0,
		})
	}
	// 已过期的订阅不应计入
	DB.Create(&UserSubscription{
		UserId: u.Id, PlanId: 1, AmountTotal: 1000,
		StartTime: now - 86400*10, EndTime: now - 100,
		Status: SubscriptionStatusExpired,
	})

	stats, err := BuildRevenueStats(7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ActiveSubscriptions != 3 {
		t.Fatalf("ActiveSubscriptions = %d, want 3", stats.ActiveSubscriptions)
	}
	if stats.AutoRenewSubscriptions != 1 {
		t.Fatalf("AutoRenewSubscriptions = %d, want 1", stats.AutoRenewSubscriptions)
	}
	want := 1.0 / 3.0
	if stats.RenewRate < want-0.01 || stats.RenewRate > want+0.01 {
		t.Fatalf("RenewRate = %f, want ~%f", stats.RenewRate, want)
	}
}

func TestBuildRevenueStats_ExcludesFailedAndRefunded(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &TopUp{}, &UserSubscription{}, &Log{}, &User{})
	u := seedUser(t, "status_user", 0, "")
	now := helper.GetTimestamp()

	seedTopUp(t, u.Id, 1000, 500_000, TopUpStatusSuccess, now)
	seedTopUp(t, u.Id, 5000, 2_500_000, TopUpStatusFailed, now)
	seedTopUp(t, u.Id, 7000, 3_500_000, TopUpStatusRefunded, now)
	seedTopUp(t, u.Id, 9000, 4_500_000, TopUpStatusExpired, now)

	stats, err := BuildRevenueStats(7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalIncomeCents != 1000 {
		t.Fatalf("TotalIncomeCents = %d, want 1000 (only the success order)", stats.TotalIncomeCents)
	}
}
