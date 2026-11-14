package model

import (
	"context"
	"testing"
	"time"

	"github.com/songquanpeng/one-api/common/helper"
)

// ============================================================================
// 订阅生命周期测试
//
// 重点覆盖三个「会扣钱或会中断服务」的场景：
//   ① 余额购买：余额不足必须整体失败，不能出现「扣了钱没建订阅」
//   ② 自动续费：余额足够自动续期、余额不足进入宽限
//   ③ 到期回落：分组回到订阅前的值，且不能覆盖管理员手工改过的分组
// ============================================================================

func seedPlan(t *testing.T, priceCents int64, totalQuota int64) *SubscriptionPlan {
	t.Helper()
	p := &SubscriptionPlan{
		Title:            "测试套餐",
		PriceCents:       priceCents,
		DurationUnit:     PeriodUnitMonth,
		DurationValue:    1,
		Enabled:          true,
		TotalQuota:       totalQuota,
		AllowBalancePay:  true,
		AutoRenewEnabled: true,
		GracePeriodDays:  3,
		UpgradeGroup:     "vip",
		DowngradeGroup:   "default",
	}
	if err := CreatePlan(p); err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}
	return p
}

func TestPlan_PeriodDuration(t *testing.T) {
	cases := []struct {
		unit   string
		value  int
		custom int64
		// 期望的月数/天数增量，用 AddDate 校验
		wantMonths int
	}{
		{PeriodUnitMonth, 1, 0, 1},
		{PeriodUnitMonth, 3, 0, 3},
	}
	for _, c := range cases {
		p := &SubscriptionPlan{DurationUnit: c.unit, DurationValue: c.value, CustomSeconds: c.custom}
		start := time.Date(2025, 1, 31, 12, 0, 0, 0, time.Local)
		got := p.CalcEndTime(start)
		want := start.AddDate(0, c.wantMonths, 0)
		if !got.Equal(want) {
			t.Fatalf("CalcEndTime(%s×%d) = %v, want %v", c.unit, c.value, got, want)
		}
	}

	// 年周期
	p := &SubscriptionPlan{DurationUnit: PeriodUnitYear, DurationValue: 2}
	start := time.Date(2025, 6, 15, 0, 0, 0, 0, time.Local)
	if got, want := p.CalcEndTime(start), start.AddDate(2, 0, 0); !got.Equal(want) {
		t.Fatalf("year CalcEndTime = %v, want %v", got, want)
	}

	// 自定义秒数
	p2 := &SubscriptionPlan{DurationUnit: PeriodUnitCustom, CustomSeconds: 3600}
	if got := p2.CalcEndTime(start); got.Sub(start) != time.Hour {
		t.Fatalf("custom CalcEndTime delta = %v, want 1h", got.Sub(start))
	}
}

func TestValidatePlan(t *testing.T) {
	cases := []struct {
		name    string
		plan    SubscriptionPlan
		wantErr bool
	}{
		{"空标题", SubscriptionPlan{DurationUnit: PeriodUnitMonth, DurationValue: 1}, true},
		{"周期为 0", SubscriptionPlan{Title: "a", DurationUnit: PeriodUnitMonth, DurationValue: 0}, true},
		{"未知周期单位", SubscriptionPlan{Title: "a", DurationUnit: "week"}, true},
		{"负数价格", SubscriptionPlan{Title: "a", PriceCents: -1, DurationUnit: PeriodUnitMonth, DurationValue: 1}, true},
		{"自定义周期缺秒数", SubscriptionPlan{Title: "a", DurationUnit: PeriodUnitCustom}, true},
		{"合法月付", SubscriptionPlan{Title: "a", DurationUnit: PeriodUnitMonth, DurationValue: 1}, false},
		{"合法自定义", SubscriptionPlan{Title: "a", DurationUnit: PeriodUnitCustom, CustomSeconds: 3600}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePlan(&c.plan)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidatePlan() err = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestSubscribeWithBalance_InsufficientQuota(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &UserSubscription{}, &SubscriptionPlan{}, &TopUp{}, &User{}, &Log{})
	ctx := context.Background()

	plan := seedPlan(t, 1000, 500000)      // $10 套餐
	u := seedUser(t, "poor_user", 100, "") // 余额远不够

	_, err := SubscribeWithBalance(ctx, u.Id, plan.Id)
	if err == nil {
		t.Fatal("expected error on insufficient balance")
	}

	// 余额不能被动过
	var got User
	DB.First(&got, u.Id)
	if got.Quota != 100 {
		t.Fatalf("quota changed on failed purchase: %d, want 100", got.Quota)
	}
	// 不能留下半截订阅
	var cnt int64
	DB.Model(&UserSubscription{}).Where("user_id = ?", u.Id).Count(&cnt)
	if cnt != 0 {
		t.Fatalf("subscription rows created on failed purchase: %d", cnt)
	}
}

func TestSubscribeWithBalance_Success(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &UserSubscription{}, &SubscriptionPlan{}, &TopUp{}, &User{}, &Log{})
	ctx := context.Background()

	plan := seedPlan(t, 1000, 500000)                    // $10
	u := seedUser(t, "rich_user", 10_000_000, "default") // $20

	sub, err := SubscribeWithBalance(ctx, u.Id, plan.Id)
	if err != nil {
		t.Fatalf("SubscribeWithBalance() error: %v", err)
	}
	if sub.AmountTotal != 500000 {
		t.Fatalf("AmountTotal = %d, want 500000", sub.AmountTotal)
	}

	// 余额被扣掉 $10 = 5,000,000 额度
	var got User
	DB.First(&got, u.Id)
	if got.Quota != 10_000_000-5_000_000 {
		t.Fatalf("quota after purchase = %d, want %d", got.Quota, 10_000_000-5_000_000)
	}
	// 分组被升级
	if got.Group != "vip" {
		t.Fatalf("group = %q, want vip", got.Group)
	}
	// 记录了来源分组，便于到期回落
	if sub.PrevUserGroup != "default" {
		t.Fatalf("PrevUserGroup = %q, want default", sub.PrevUserGroup)
	}
	// 生成了一条对账订单
	var orderCnt int64
	DB.Model(&TopUp{}).Where("user_id = ? AND payment_provider = ?", u.Id, PaymentProviderBalance()).Count(&orderCnt)
	if orderCnt != 1 {
		t.Fatalf("balance order count = %d, want 1", orderCnt)
	}
}

func TestSubscribeWithBalance_NotAllowed(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &UserSubscription{}, &SubscriptionPlan{}, &TopUp{}, &User{})
	ctx := context.Background()

	plan := seedPlan(t, 1000, 500000)
	plan.AllowBalancePay = false
	if err := UpdatePlan(plan); err != nil {
		t.Fatal(err)
	}
	u := seedUser(t, "nobal_user", 10_000_000, "")
	if _, err := SubscribeWithBalance(ctx, u.Id, plan.Id); err == nil {
		t.Fatal("expected error when balance pay disabled")
	}
}

func TestConsumeSubscription_OrderAndLimit(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &UserSubscription{}, &SubscriptionPlan{}, &User{})
	u := seedUser(t, "consume_sub_user", 0, "")
	now := time.Now().Unix()

	// 两个订阅：A 先到期（额度 100），B 后到期（额度 1000）
	a := &UserSubscription{
		UserId: u.Id, PlanId: 1, AmountTotal: 100, AmountUsed: 0,
		StartTime: now - 100, EndTime: now + 1000, Status: SubscriptionStatusActive,
	}
	b := &UserSubscription{
		UserId: u.Id, PlanId: 1, AmountTotal: 1000, AmountUsed: 0,
		StartTime: now - 100, EndTime: now + 5000, Status: SubscriptionStatusActive,
	}
	DB.Create(a)
	DB.Create(b)

	// 消费 150：应先吃光 A 的 100，再从 B 扣 50
	subId, deducted, err := ConsumeSubscription(u.Id, 150)
	if err != nil {
		t.Fatal(err)
	}
	if deducted != 150 {
		t.Fatalf("deducted = %d, want 150", deducted)
	}
	if subId != a.Id {
		t.Fatalf("first consumed sub = %d, want A(%d)", subId, a.Id)
	}
	var gotA, gotB UserSubscription
	DB.First(&gotA, a.Id)
	DB.First(&gotB, b.Id)
	if gotA.AmountUsed != 100 {
		t.Fatalf("A used = %d, want 100 (exhausted fully)", gotA.AmountUsed)
	}
	if gotB.AmountUsed != 50 {
		t.Fatalf("B used = %d, want 50 (remainder)", gotB.AmountUsed)
	}

	// 再消费 2000：A 已空，B 只剩 950，合计只能扣 950
	_, deducted2, err := ConsumeSubscription(u.Id, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if deducted2 != 950 {
		t.Fatalf("second deducted = %d, want 950 (B remaining)", deducted2)
	}
}

func TestConsumeSubscription_Unlimited(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &UserSubscription{}, &User{})
	u := seedUser(t, "unlimited_user", 0, "")
	now := time.Now().Unix()
	s := &UserSubscription{
		UserId: u.Id, PlanId: 1, AmountTotal: 0, // 0 = 不限量
		StartTime: now - 10, EndTime: now + 10000, Status: SubscriptionStatusActive,
	}
	DB.Create(s)

	_, deducted, err := ConsumeSubscription(u.Id, 999999)
	if err != nil {
		t.Fatal(err)
	}
	if deducted != 999999 {
		t.Fatalf("unlimited deducted = %d, want 999999", deducted)
	}
}

func TestExpireDueSubscriptions_GracePeriod(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &UserSubscription{}, &SubscriptionPlan{}, &User{}, &TopUp{}, &Log{})
	plan := seedPlan(t, 1000, 500000)
	u := seedUser(t, "grace_user", 0, "vip")
	now := helper.GetTimestamp()

	s := &UserSubscription{
		UserId: u.Id, PlanId: plan.Id, AmountTotal: 500000,
		StartTime: now - 86400*31, EndTime: now - 60, // 刚过期
		Status: SubscriptionStatusActive, UpgradeGroup: "vip",
		PrevUserGroup: "default", DowngradeGroup: "default",
		AutoRenew: false, // 关闭自动续费
	}
	DB.Create(s)

	n, err := ExpireDueSubscriptions(10)
	if err != nil {
		t.Fatalf("ExpireDueSubscriptions() error: %v", err)
	}
	if n != 1 {
		t.Fatalf("processed %d, want 1", n)
	}

	var got UserSubscription
	DB.First(&got, s.Id)
	// 第一次过期不应立即作废，而是进入宽限期
	if got.Status != SubscriptionStatusActive {
		t.Fatalf("status = %q, want active (in grace)", got.Status)
	}
	if got.GraceEndTime == 0 {
		t.Fatal("grace_end_time should be set")
	}
	// 分组尚未回落
	var gu User
	DB.First(&gu, u.Id)
	if gu.Group != "vip" {
		t.Fatalf("group = %q, want vip during grace", gu.Group)
	}

	// 把宽限期也推过去，再跑一次 → 应该彻底过期并回落分组
	DB.Model(&UserSubscription{}).Where("id = ?", s.Id).
		Update("grace_end_time", helper.GetTimestamp()-10)
	if _, err := ExpireDueSubscriptions(10); err != nil {
		t.Fatal(err)
	}
	DB.First(&got, s.Id)
	if got.Status != SubscriptionStatusExpired {
		t.Fatalf("status = %q, want expired", got.Status)
	}
	DB.First(&gu, u.Id)
	if gu.Group != "default" {
		t.Fatalf("group after expiry = %q, want default", gu.Group)
	}
}

func TestExpireDueSubscriptions_AutoRenewSuccess(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &UserSubscription{}, &SubscriptionPlan{}, &User{}, &TopUp{}, &Log{})
	plan := seedPlan(t, 1000, 500000)                // $10
	u := seedUser(t, "renew_user", 6_000_000, "vip") // $12，足够续一次
	now := helper.GetTimestamp()

	s := &UserSubscription{
		UserId: u.Id, PlanId: plan.Id, AmountTotal: 500000,
		StartTime: now - 86400*30, EndTime: now - 60,
		Status: SubscriptionStatusActive, AutoRenew: true,
		UpgradeGroup: "vip", DowngradeGroup: "default", PrevUserGroup: "default",
	}
	DB.Create(s)

	if _, err := ExpireDueSubscriptions(10); err != nil {
		t.Fatal(err)
	}

	var got UserSubscription
	DB.First(&got, s.Id)
	if got.Status != SubscriptionStatusActive {
		t.Fatalf("status = %q, want active after auto renew", got.Status)
	}
	if got.EndTime <= now {
		t.Fatalf("EndTime = %d should be extended beyond now=%d", got.EndTime, now)
	}
	if got.AmountUsed != 0 {
		t.Fatalf("AmountUsed = %d, want 0 (reset on renew)", got.AmountUsed)
	}
	// 余额扣掉 $10
	var gu User
	DB.First(&gu, u.Id)
	if gu.Quota != 6_000_000-5_000_000 {
		t.Fatalf("quota = %d, want %d", gu.Quota, 6_000_000-5_000_000)
	}
}

func TestExpireDueSubscriptions_AutoRenewInsufficient(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &UserSubscription{}, &SubscriptionPlan{}, &User{}, &TopUp{}, &Log{})
	plan := seedPlan(t, 1000, 500000)
	u := seedUser(t, "renew_poor_user", 100, "vip") // 余额不够
	now := helper.GetTimestamp()

	s := &UserSubscription{
		UserId: u.Id, PlanId: plan.Id, AmountTotal: 500000,
		StartTime: now - 86400*30, EndTime: now - 60,
		Status: SubscriptionStatusActive, AutoRenew: true,
		UpgradeGroup: "vip", DowngradeGroup: "default", PrevUserGroup: "default",
	}
	DB.Create(s)

	if _, err := ExpireDueSubscriptions(10); err != nil {
		t.Fatal(err)
	}

	var got UserSubscription
	DB.First(&got, s.Id)
	// 续费失败 → 进入宽限，不立刻中断服务
	if got.Status != SubscriptionStatusActive {
		t.Fatalf("status = %q, want active (grace) after failed renew", got.Status)
	}
	if got.GraceEndTime == 0 {
		t.Fatal("grace_end_time should be set after failed renew")
	}
	// 余额未动
	var gu User
	DB.First(&gu, u.Id)
	if gu.Quota != 100 {
		t.Fatalf("quota changed on failed renew: %d", gu.Quota)
	}
}

func TestCancelSubscription_DoesNotClobberManualGroup(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &UserSubscription{}, &User{}, &Log{})
	now := helper.GetTimestamp()
	// 用户当前分组已被管理员手工改成 "vip"，而订阅授予的也是 vip，
	// 但管理员随后又改成了 "svip" —— 回落时不应把它改回 default
	u := seedUser(t, "manual_group_user", 0, "svip")
	s := &UserSubscription{
		UserId: u.Id, PlanId: 1, AmountTotal: 100,
		StartTime: now - 100, EndTime: now - 1,
		Status:       SubscriptionStatusActive,
		UpgradeGroup: "vip", DowngradeGroup: "default", PrevUserGroup: "default",
	}
	DB.Create(s)

	if err := cancelSubscription(s, now); err != nil {
		t.Fatal(err)
	}
	var got User
	DB.First(&got, u.Id)
	if got.Group != "svip" {
		t.Fatalf("group = %q, want svip (manual change preserved)", got.Group)
	}
}

func TestSetAutoRenew_OwnershipCheck(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &UserSubscription{}, &User{})
	owner := seedUser(t, "owner_user", 0, "")
	attacker := seedUser(t, "attacker_user", 0, "")
	now := helper.GetTimestamp()

	s := &UserSubscription{
		UserId: owner.Id, PlanId: 1, AmountTotal: 100,
		StartTime: now - 10, EndTime: now + 10000, Status: SubscriptionStatusActive,
	}
	DB.Create(s)

	// 别人不能改
	if err := SetAutoRenew(attacker.Id, s.Id, true); err == nil {
		t.Fatal("expected error when toggling another user's subscription")
	}
	// 本人可以
	if err := SetAutoRenew(owner.Id, s.Id, true); err != nil {
		t.Fatalf("owner should be able to toggle: %v", err)
	}
	var got UserSubscription
	DB.First(&got, s.Id)
	if !got.AutoRenew {
		t.Fatal("auto_renew should be true")
	}
}

func TestResetDueSubscriptions(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &UserSubscription{}, &SubscriptionPlan{}, &User{})
	plan := seedPlan(t, 100, 1000)
	plan.QuotaResetPeriod = ResetDaily
	if err := UpdatePlan(plan); err != nil {
		t.Fatal(err)
	}
	u := seedUser(t, "reset_user", 0, "")
	now := helper.GetTimestamp()

	s := &UserSubscription{
		UserId: u.Id, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 800,
		StartTime: now - 86400, EndTime: now + 86400*30,
		Status:        SubscriptionStatusActive,
		NextResetTime: now - 10, // 已到期
	}
	DB.Create(s)

	n, err := ResetDueSubscriptions(10)
	if err != nil {
		t.Fatalf("ResetDueSubscriptions() error: %v", err)
	}
	if n != 1 {
		t.Fatalf("reset %d, want 1", n)
	}
	var got UserSubscription
	DB.First(&got, s.Id)
	if got.AmountUsed != 0 {
		t.Fatalf("AmountUsed = %d, want 0 after reset", got.AmountUsed)
	}
	if got.NextResetTime <= now {
		t.Fatalf("NextResetTime = %d should be in the future", got.NextResetTime)
	}
}
