package model

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/songquanpeng/one-api/common/helper"
)

// ============================================================================
// 充值订单的幂等与原子性测试
//
// 这块是整个商业化里最危险的部分：一旦回调重复入账，就是真金白银的损失。
// 因此下面每条断言都对应一个具体的攻击/故障场景。
// ============================================================================

func TestCreateTopUp_Validation(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &TopUp{}, &User{})
	u := seedUser(t, "create_topup_user", 0, "")

	cases := []struct {
		name    string
		topup   TopUp
		wantErr error
	}{
		{"金额为 0", TopUp{UserId: u.Id, AmountCents: 0, Quota: 100}, ErrAmountTooLow},
		{"金额为负", TopUp{UserId: u.Id, AmountCents: -1, Quota: 100}, ErrAmountTooLow},
		{"额度为 0", TopUp{UserId: u.Id, AmountCents: 100, Quota: 0}, ErrQuotaOverflow},
		{"用户无效", TopUp{UserId: 0, AmountCents: 100, Quota: 100}, nil}, // 用户校验错误信息不同，只断言有错
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tt := c.topup
			err := CreateTopUp(&tt)
			if c.wantErr != nil && !errors.Is(err, c.wantErr) {
				t.Fatalf("CreateTopUp() err = %v, want %v", err, c.wantErr)
			}
			if err == nil {
				t.Fatalf("CreateTopUp() expected error, got nil")
			}
		})
	}

	// 正常创建
	good := TopUp{UserId: u.Id, AmountCents: 1000, Quota: 500000, PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay"}
	if err := CreateTopUp(&good); err != nil {
		t.Fatalf("CreateTopUp() unexpected error: %v", err)
	}
	if good.TradeNo == "" {
		t.Fatal("TradeNo should be generated")
	}
	if good.Status != TopUpStatusPending {
		t.Fatalf("status = %q, want pending", good.Status)
	}
}

func TestCompleteTopUp_Idempotent(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &TopUp{}, &User{}, &Log{})
	ctx := context.Background()
	u := seedUser(t, "idem_user", 0, "")

	order := &TopUp{
		UserId: u.Id, AmountCents: 1000, Quota: 500000,
		PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay",
	}
	if err := CreateTopUp(order); err != nil {
		t.Fatal(err)
	}

	// 第一次回调：入账
	already, err := CompleteTopUp(ctx, order.TradeNo, PaymentProviderEpay, "payload", 0)
	if err != nil {
		t.Fatalf("first CompleteTopUp() error: %v", err)
	}
	if already {
		t.Fatal("first call should not be 'already done'")
	}

	var got User
	if err := DB.First(&got, u.Id).Error; err != nil {
		t.Fatal(err)
	}
	if got.Quota != 500000 {
		t.Fatalf("quota after first credit = %d, want 500000", got.Quota)
	}

	// 第二次回调（网关重试）：必须幂等，不能再加钱
	already, err = CompleteTopUp(ctx, order.TradeNo, PaymentProviderEpay, "payload", 0)
	if err != nil {
		t.Fatalf("second CompleteTopUp() should not error, got: %v", err)
	}
	if !already {
		t.Fatal("second call should report alreadyDone = true")
	}
	if err := DB.First(&got, u.Id).Error; err != nil {
		t.Fatal(err)
	}
	if got.Quota != 500000 {
		t.Fatalf("quota after duplicate callback = %d, want 500000 (no double credit)", got.Quota)
	}
}

func TestCompleteTopUp_ProviderMismatch(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &TopUp{}, &User{}, &Log{})
	ctx := context.Background()
	u := seedUser(t, "mismatch_user", 0, "")

	order := &TopUp{
		UserId: u.Id, AmountCents: 1000, Quota: 500000,
		PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay",
	}
	if err := CreateTopUp(order); err != nil {
		t.Fatal(err)
	}

	// 用 webhook 网关的身份去核销 epay 订单 —— 必须被拒绝
	_, err := CompleteTopUp(ctx, order.TradeNo, PaymentProviderWebhook, "", 0)
	if !errors.Is(err, ErrTopUpProviderMismatch) {
		t.Fatalf("err = %v, want ErrTopUpProviderMismatch", err)
	}

	var got User
	DB.First(&got, u.Id)
	if got.Quota != 0 {
		t.Fatalf("quota should remain 0 on mismatch, got %d", got.Quota)
	}
}

func TestCompleteTopUp_UnknownOrder(t *testing.T) {
	setupTestDB(t)
	ctx := context.Background()
	_, err := CompleteTopUp(ctx, "NOSUCHORDER123", PaymentProviderEpay, "", 0)
	if !errors.Is(err, ErrTopUpNotFound) {
		t.Fatalf("err = %v, want ErrTopUpNotFound", err)
	}
}

func TestCompleteTopUp_InvalidTradeNo(t *testing.T) {
	setupTestDB(t)
	ctx := context.Background()
	for _, bad := range []string{"", "has space", "semi;colon", "quote'"} {
		_, err := CompleteTopUp(ctx, bad, PaymentProviderEpay, "", 0)
		if err == nil {
			t.Fatalf("trade no %q should be rejected", bad)
		}
	}
}

func TestCompleteTopUp_MaxWalletQuota(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &TopUp{}, &User{}, &Log{})
	ctx := context.Background()
	u := seedUser(t, "limit_user", 1_000_000, "")

	order := &TopUp{
		UserId: u.Id, AmountCents: 1000, Quota: 500000,
		PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay",
	}
	if err := CreateTopUp(order); err != nil {
		t.Fatal(err)
	}

	// 上限 1_200_000：当前 1_000_000 + 500_000 > 1_200_000，必须失败
	_, err := CompleteTopUp(ctx, order.TradeNo, PaymentProviderEpay, "", 1_200_000)
	if !errors.Is(err, ErrWalletQuotaLimit) {
		t.Fatalf("err = %v, want ErrWalletQuotaLimit", err)
	}

	var got User
	DB.First(&got, u.Id)
	if got.Quota != 1_000_000 {
		t.Fatalf("quota should be unchanged, got %d", got.Quota)
	}

	// 订单仍是 pending，允许提高上限后重试
	var o TopUp
	DB.Where("trade_no = ?", order.TradeNo).First(&o)
	if o.Status != TopUpStatusPending {
		t.Fatalf("order status = %q, want still pending", o.Status)
	}

	// 放宽上限后应当成功
	if _, err := CompleteTopUp(ctx, order.TradeNo, PaymentProviderEpay, "", 1_500_000); err != nil {
		t.Fatalf("retry with higher limit should succeed, got: %v", err)
	}
	DB.First(&got, u.Id)
	if got.Quota != 1_500_000 {
		t.Fatalf("quota after retry = %d, want 1500000", got.Quota)
	}
}

func TestCompleteTopUp_TopUpStatusInvalid(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &TopUp{}, &User{}, &Log{})
	ctx := context.Background()
	u := seedUser(t, "expired_user", 0, "")

	order := &TopUp{
		UserId: u.Id, AmountCents: 1000, Quota: 500000,
		PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay",
		Status: TopUpStatusExpired,
	}
	if err := CreateTopUp(order); err != nil {
		t.Fatal(err)
	}
	_, err := CompleteTopUp(ctx, order.TradeNo, PaymentProviderEpay, "", 0)
	if !errors.Is(err, ErrTopUpStatusInvalid) {
		t.Fatalf("err = %v, want ErrTopUpStatusInvalid", err)
	}
}

func TestExpireStaleTopUps(t *testing.T) {
	setupTestDB(t)
	cleanTable(t, &TopUp{}, &User{})
	u := seedUser(t, "expire_user", 0, "")

	fresh := &TopUp{UserId: u.Id, AmountCents: 1000, Quota: 500000, PaymentProvider: PaymentProviderEpay}
	if err := CreateTopUp(fresh); err != nil {
		t.Fatal(err)
	}
	old := &TopUp{UserId: u.Id, AmountCents: 1000, Quota: 500000, PaymentProvider: PaymentProviderEpay}
	if err := CreateTopUp(old); err != nil {
		t.Fatal(err)
	}
	// 把 old 的创建时间改到 2 小时前，fresh 钉在「10 分钟前」。
	//
	// 这里刻意不用「现在」：ExpireStaleTopUps 内部是 create_time < now-1h，
	// 若 fresh 恰好等于 now，一旦跨秒就会与阈值相等而被误判为超时。
	// 用 10 分钟前既远离 1 小时阈值，也远离秒级抖动。
	now := helper.GetTimestamp()
	DB.Model(&TopUp{}).Where("id = ?", fresh.Id).Update("create_time", now-600)
	DB.Model(&TopUp{}).Where("id = ?", old.Id).Update("create_time", now-7200)

	n, err := ExpireStaleTopUps(time.Hour)
	if err != nil {
		t.Fatalf("ExpireStaleTopUps() error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expired %d orders, want 1", n)
	}

	var o TopUp
	if err := DB.Where("trade_no = ?", old.TradeNo).First(&o).Error; err != nil {
		t.Fatalf("query old order: %v", err)
	}
	if o.Status != TopUpStatusExpired {
		t.Fatalf("old order status = %q, want expired", o.Status)
	}
	// 复用变量时必须显式归零：GORM 的 First 会带上结构体中已有的主键，
	// 不清空的话这里会再查到 old 这一行，得到与 fresh 无关的结果。
	var f TopUp
	if err := DB.Where("trade_no = ?", fresh.TradeNo).First(&f).Error; err != nil {
		t.Fatalf("query fresh order: %v", err)
	}
	if f.Status != TopUpStatusPending {
		t.Fatalf("fresh order status = %q, want pending", f.Status)
	}
}

func TestQuoteAmount_USDToQuota(t *testing.T) {
	withQuotaPerUnit(t)
	// 1 美元 → 500000
	if got := USDToQuota(1.0); got != 500000 {
		t.Fatalf("USDToQuota(1) = %d, want 500000", got)
	}
	// 0.01 美元 → 5000
	if got := USDToQuota(0.01); got != 5000 {
		t.Fatalf("USDToQuota(0.01) = %d, want 5000", got)
	}
	// 反向
	if got := QuotaToUSD(500000); got != 1.0 {
		t.Fatalf("QuotaToUSD(500000) = %f, want 1.0", got)
	}
}
