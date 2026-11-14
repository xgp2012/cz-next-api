package model

import (
	"context"
	"testing"

	"github.com/songquanpeng/one-api/common/helper"
)

// ============================================================================
// 邀请分成测试
//
// 这是"钱"的路径，重点验证三类容易出错的地方：
//   ① 只有**真实消费**才计提，注册/充值不产生佣金（防小号套利）
//   ② 比例在计提时快照落库，事后改配置不篡改历史流水
//   ③ 转正必须原子：不能出现"扣了 pending 但没加余额"或反之
// ============================================================================

// withCommission 临时开启分成并恢复原配置
func withCommission(t *testing.T, rate float64, minQuota int64) {
	t.Helper()
	old := CurrentCommissionConfig()
	SetCommissionConfigProvider(func() CommissionConfig {
		return CommissionConfig{Enabled: true, DefaultRate: rate, MinQuota: minQuota}
	})
	t.Cleanup(func() {
		SetCommissionConfigProvider(func() CommissionConfig { return old })
	})
}

// seedInvitee 创建一个有邀请人的用户
func seedInvitee(t *testing.T, username string, inviterId int) *User {
	t.Helper()
	u := seedUser(t, username, 0, "")
	u.InviterId = inviterId
	if err := DB.Model(&User{}).Where("id = ?", u.Id).Update("inviter_id", inviterId).Error; err != nil {
		t.Fatalf("set inviter: %v", err)
	}
	return u
}

func TestRecordInvite(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &UserAffStats{}, &User{})
	inviter := seedUser(t, "inviter_a", 0, "")

	for i := 0; i < 3; i++ {
		if err := RecordInvite(inviter.Id); err != nil {
			t.Fatalf("RecordInvite() error: %v", err)
		}
	}
	stats, err := GetAffStats(inviter.Id)
	if err != nil {
		t.Fatal(err)
	}
	if stats.InviteCount != 3 {
		t.Fatalf("InviteCount = %d, want 3", stats.InviteCount)
	}
}

func TestRecordInvite_InvalidUser(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &UserAffStats{}, &User{})
	// 无效邀请人不应写入统计，也不应报错（注册流程不能因此中断）
	if err := RecordInvite(0); err != nil {
		t.Fatalf("RecordInvite(0) should be a no-op, got: %v", err)
	}
	if err := RecordInvite(-5); err != nil {
		t.Fatalf("RecordInvite(-5) should be a no-op, got: %v", err)
	}
}

func TestAccrueConsumeCommission_Basic(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &AffCommission{}, &UserAffStats{}, &User{})
	withCommission(t, 0.05, 0)

	inviter := seedUser(t, "inviter_b", 0, "")
	invitee := seedInvitee(t, "invitee_b", inviter.Id)

	ctx := context.Background()
	// 消费 100000 额度，5% → 5000
	if err := AccrueConsumeCommission(ctx, invitee.Id, 100000, 0); err != nil {
		t.Fatalf("AccrueConsumeCommission() error: %v", err)
	}

	stats, err := GetAffStats(inviter.Id)
	if err != nil {
		t.Fatal(err)
	}
	if stats.PendingQuota != 5000 {
		t.Fatalf("PendingQuota = %d, want 5000", stats.PendingQuota)
	}
	if stats.HistoryQuota != 5000 {
		t.Fatalf("HistoryQuota = %d, want 5000", stats.HistoryQuota)
	}

	// 必须留下可审计的流水行，且比例被快照
	var recs []AffCommission
	DB.Where("inviter_id = ?", inviter.Id).Find(&recs)
	if len(recs) != 1 {
		t.Fatalf("commission rows = %d, want 1", len(recs))
	}
	if recs[0].Rate != 0.05 {
		t.Fatalf("snapshot rate = %v, want 0.05", recs[0].Rate)
	}
	if recs[0].SourceQuota != 100000 {
		t.Fatalf("SourceQuota = %d, want 100000", recs[0].SourceQuota)
	}
	if recs[0].InviteeId != invitee.Id {
		t.Fatalf("InviteeId = %d, want %d", recs[0].InviteeId, invitee.Id)
	}
	if recs[0].Status != AffCommissionStatusPending {
		t.Fatalf("Status = %q, want pending", recs[0].Status)
	}
}

func TestAccrueConsumeCommission_RateSnapshotIsImmutable(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &AffCommission{}, &UserAffStats{}, &User{})
	withCommission(t, 0.05, 0)

	inviter := seedUser(t, "snap_inviter", 0, "")
	invitee := seedInvitee(t, "snap_invitee", inviter.Id)
	ctx := context.Background()

	if err := AccrueConsumeCommission(ctx, invitee.Id, 100000, 0); err != nil {
		t.Fatal(err)
	}

	// 事后把比例改成 20%
	SetCommissionConfigProvider(func() CommissionConfig {
		return CommissionConfig{Enabled: true, DefaultRate: 0.20}
	})
	if err := AccrueConsumeCommission(ctx, invitee.Id, 100000, 0); err != nil {
		t.Fatal(err)
	}

	var recs []AffCommission
	DB.Where("inviter_id = ?", inviter.Id).Order("id asc").Find(&recs)
	if len(recs) != 2 {
		t.Fatalf("commission rows = %d, want 2", len(recs))
	}
	// 第一笔必须仍是 5%，不能被新配置改写
	if recs[0].Rate != 0.05 || recs[0].CommissionQuota != 5000 {
		t.Fatalf("first row rate/quota = %v/%d, want 0.05/5000 (historical snapshot must not change)",
			recs[0].Rate, recs[0].CommissionQuota)
	}
	// 第二笔按新比例
	if recs[1].Rate != 0.20 || recs[1].CommissionQuota != 20000 {
		t.Fatalf("second row rate/quota = %v/%d, want 0.20/20000",
			recs[1].Rate, recs[1].CommissionQuota)
	}
	// 累计 = 5000 + 20000
	stats, _ := GetAffStats(inviter.Id)
	if stats.HistoryQuota != 25000 {
		t.Fatalf("HistoryQuota = %d, want 25000", stats.HistoryQuota)
	}
}

func TestAccrueConsumeCommission_Disabled(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &AffCommission{}, &UserAffStats{}, &User{})
	// 默认配置是关闭的
	old := CurrentCommissionConfig()
	SetCommissionConfigProvider(func() CommissionConfig {
		return CommissionConfig{Enabled: false, DefaultRate: 0.05}
	})
	t.Cleanup(func() { SetCommissionConfigProvider(func() CommissionConfig { return old }) })

	inviter := seedUser(t, "off_inviter", 0, "")
	invitee := seedInvitee(t, "off_invitee", inviter.Id)

	if err := AccrueConsumeCommission(context.Background(), invitee.Id, 100000, 0); err != nil {
		t.Fatal(err)
	}
	var cnt int64
	DB.Model(&AffCommission{}).Count(&cnt)
	if cnt != 0 {
		t.Fatalf("commission rows = %d, want 0 when disabled", cnt)
	}
}

func TestAccrueConsumeCommission_MinQuotaFilter(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &AffCommission{}, &UserAffStats{}, &User{})
	withCommission(t, 0.05, 10000) // 低于 10000 不计

	inviter := seedUser(t, "min_inviter", 0, "")
	invitee := seedInvitee(t, "min_invitee", inviter.Id)
	ctx := context.Background()

	// 9999 < 10000 → 不计
	if err := AccrueConsumeCommission(ctx, invitee.Id, 9999, 0); err != nil {
		t.Fatal(err)
	}
	// 10000 == 10000 → 计（边界必须包含）
	if err := AccrueConsumeCommission(ctx, invitee.Id, 10000, 0); err != nil {
		t.Fatal(err)
	}

	var cnt int64
	DB.Model(&AffCommission{}).Count(&cnt)
	if cnt != 1 {
		t.Fatalf("commission rows = %d, want 1 (only the == threshold one)", cnt)
	}
}

func TestAccrueConsumeCommission_NoInviter(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &AffCommission{}, &UserAffStats{}, &User{})
	withCommission(t, 0.05, 0)

	// 无人邀请的用户消费，不应产生任何佣金，且不应报错
	lonely := seedUser(t, "lonely_user", 0, "")
	if err := AccrueConsumeCommission(context.Background(), lonely.Id, 100000, 0); err != nil {
		t.Fatalf("should be a silent no-op, got: %v", err)
	}
	var cnt int64
	DB.Model(&AffCommission{}).Count(&cnt)
	if cnt != 0 {
		t.Fatalf("commission rows = %d, want 0", cnt)
	}
}

func TestAccrueConsumeCommission_SelfInvite(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &AffCommission{}, &UserAffStats{}, &User{})
	withCommission(t, 0.05, 0)

	// 自己邀请自己：必须被拒绝，否则等于自己给自己发钱
	u := seedUser(t, "self_invite", 0, "")
	DB.Model(&User{}).Where("id = ?", u.Id).Update("inviter_id", u.Id)

	if err := AccrueConsumeCommission(context.Background(), u.Id, 100000, 0); err != nil {
		t.Fatal(err)
	}
	var cnt int64
	DB.Model(&AffCommission{}).Count(&cnt)
	if cnt != 0 {
		t.Fatalf("self-invite produced %d commission rows, want 0", cnt)
	}
}

func TestTransferAffQuotaToWallet(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &AffCommission{}, &UserAffStats{}, &User{}, &Log{})
	withCommission(t, 0.05, 0)

	inviter := seedUser(t, "transfer_inviter", 0, "")
	invitee := seedInvitee(t, "transfer_invitee", inviter.Id)
	ctx := context.Background()

	// 消费 20,000,000 额度 → 佣金 5% = 1,000,000 额度（= $2，满足 1 美元门槛）
	if err := AccrueConsumeCommission(ctx, invitee.Id, 20_000_000, 0); err != nil {
		t.Fatal(err)
	}
	stats, _ := GetAffStats(inviter.Id)
	if stats.PendingQuota != 1_000_000 {
		t.Fatalf("PendingQuota = %d, want 1000000", stats.PendingQuota)
	}

	moved, err := TransferAffQuotaToWallet(ctx, inviter.Id)
	if err != nil {
		t.Fatalf("TransferAffQuotaToWallet() error: %v", err)
	}
	if moved != 1_000_000 {
		t.Fatalf("moved = %d, want 1000000", moved)
	}

	// 余额到账
	var got User
	DB.First(&got, inviter.Id)
	if got.Quota != 1_000_000 {
		t.Fatalf("wallet quota = %d, want 1000000", got.Quota)
	}
	// pending 归零，但 history 保留（累计收益是历史事实）
	stats2, _ := GetAffStats(inviter.Id)
	if stats2.PendingQuota != 0 {
		t.Fatalf("PendingQuota after transfer = %d, want 0", stats2.PendingQuota)
	}
	if stats2.HistoryQuota != 1_000_000 {
		t.Fatalf("HistoryQuota = %d, want 1000000 (history must be preserved)", stats2.HistoryQuota)
	}
}

func TestTransferAffQuotaToWallet_NothingToSettle(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &AffCommission{}, &UserAffStats{}, &User{})
	withCommission(t, 0.05, 0)

	u := seedUser(t, "empty_pending_user", 0, "")
	if _, err := TransferAffQuotaToWallet(context.Background(), u.Id); err == nil {
		t.Fatal("expected error when there is nothing to settle")
	}
}

func TestTransferAffQuotaToWallet_BelowMinimum(t *testing.T) {
	setupTestDB(t)
	withQuotaPerUnit(t)
	cleanTable(t, &AffCommission{}, &UserAffStats{}, &User{})
	withCommission(t, 0.05, 0)

	inviter := seedUser(t, "small_pending_inviter", 0, "")
	// 直接塞一个很小的待结算额（100 额度），远低于 1 美元门槛（500000）
	if err := ensureAffStats(DB, inviter.Id); err != nil {
		t.Fatal(err)
	}
	DB.Model(&UserAffStats{}).Where("user_id = ?", inviter.Id).Update("pending_quota", 100)

	if _, err := TransferAffQuotaToWallet(context.Background(), inviter.Id); err == nil {
		t.Fatal("expected error when below the minimum transfer threshold")
	}
	// 失败时余额不能被改动
	var got User
	DB.First(&got, inviter.Id)
	if got.Quota != 0 {
		t.Fatalf("quota = %d, want 0 (failed transfer must not credit)", got.Quota)
	}
}

func TestGetInviteesAndCommissions(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &AffCommission{}, &UserAffStats{}, &User{})
	withCommission(t, 0.10, 0)

	inviter := seedUser(t, "list_inviter", 0, "")
	a := seedInvitee(t, "list_invitee_a", inviter.Id)
	b := seedInvitee(t, "list_invitee_b", inviter.Id)
	ctx := context.Background()

	AccrueConsumeCommission(ctx, a.Id, 100000, 0)
	AccrueConsumeCommission(ctx, b.Id, 100000, 0)

	items, err := GetAffCommissions(inviter.Id, 0, 10)
	if err != nil {
		t.Fatalf("GetAffCommissions() error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("commissions = %d, want 2", len(items))
	}

	invitees, err := GetInvitees(inviter.Id, 10)
	if err != nil {
		t.Fatalf("GetInvitees() error: %v", err)
	}
	if len(invitees) != 2 {
		t.Fatalf("invitees = %d, want 2", len(invitees))
	}
	// 每个被邀请人应带上累计贡献的佣金
	for _, iv := range invitees {
		if iv.CommissionQuota != 10000 {
			t.Fatalf("invitee %d commission = %d, want 10000", iv.Id, iv.CommissionQuota)
		}
	}
}

func TestMinAffTransferQuota(t *testing.T) {
	withQuotaPerUnit(t)
	// 未配置时回落到 1 美元等值额度（QuotaPerUnit = 500000）
	SetAffMinTransferQuotaProvider(func() int64 { return 0 })
	if got := MinAffTransferQuota(); got != 500000 {
		t.Fatalf("MinAffTransferQuota() = %d, want 500000", got)
	}
}

func TestMinAffTransferQuota_Configurable(t *testing.T) {
	withQuotaPerUnit(t)
	// 显式配置优先于默认的 1 美元
	SetAffMinTransferQuotaProvider(func() int64 { return 1000 })
	t.Cleanup(func() { SetAffMinTransferQuotaProvider(func() int64 { return 0 }) })

	if got := MinAffTransferQuota(); got != 1000 {
		t.Fatalf("MinAffTransferQuota() = %d, want 1000 (explicit config wins)", got)
	}
	// 配置为 0 时必须回落，而不能把门槛变成 0——
	// 否则任何人都能把 1 额度转出去，等于关掉了这道风控。
	SetAffMinTransferQuotaProvider(func() int64 { return 0 })
	if got := MinAffTransferQuota(); got != 500000 {
		t.Fatalf("MinAffTransferQuota() = %d, want 500000 when unset", got)
	}
}

var _ = helper.GetTimestamp
