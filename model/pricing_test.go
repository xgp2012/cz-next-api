package model

import (
	"encoding/json"
	"testing"
	"time"
)

// withQuotaPerUnit 把 QuotaPerUnit 临时固定为 500000（1 美元 = 500000 额度），
// 使测试断言与真实部署一致，同时不受 config 包状态影响。
func withQuotaPerUnit(t *testing.T) {
	t.Helper()
	old := quotaPerUnitProvider
	SetQuotaPerUnitProvider(func() float64 { return 500000 })
	t.Cleanup(func() { quotaPerUnitProvider = old })
}

// approx 判断浮点是否在容差内。计价涉及 ceil 与浮点乘除，必须留容差。
func approx(got, want, tol int64) bool {
	d := got - want
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func TestPriceUsage_PerToken(t *testing.T) {
	withQuotaPerUnit(t)
	rule := &PricingRule{
		Id:          1,
		BillingMode: BillingModePerToken,
		InputRatio:  2.5,  // $0.005 / 1K tokens
		OutputRatio: 10.0, // $0.02 / 1K tokens
	}
	// 1000 输入 + 1000 输出，分组倍率 1
	// 输入：1000 × 2.5 × (0.002/1000) = $0.005
	// 输出：1000 × 10  × (0.002/1000) = $0.02
	// 合计 $0.025 → 0.025 × 500000 = 12500
	res, ok := PriceUsage(rule, 1.0, Usage{PromptTokens: 1000, CompletionTokens: 1000})
	if !ok {
		t.Fatal("expected rule to match")
	}
	if !approx(res.Quota, 12500, 1) {
		t.Fatalf("quota = %d, want ~12500", res.Quota)
	}

	// 分组倍率 2 → 翻倍
	res2, _ := PriceUsage(rule, 2.0, Usage{PromptTokens: 1000, CompletionTokens: 1000})
	if !approx(res2.Quota, 25000, 2) {
		t.Fatalf("quota with groupRatio 2 = %d, want ~25000", res2.Quota)
	}
}

func TestPriceUsage_PerCall(t *testing.T) {
	withQuotaPerUnit(t)
	rule := &PricingRule{
		Id:           2,
		BillingMode:  BillingModePerCall,
		PerCallPrice: 0.02, // $0.02 一次
	}
	// 与 token 数无关：无论多少 token 都是 $0.02 = 10000 额度
	for _, u := range []Usage{
		{},
		{PromptTokens: 100000, CompletionTokens: 50000},
	} {
		res, ok := PriceUsage(rule, 1.0, u)
		if !ok {
			t.Fatal("expected match")
		}
		if !approx(res.Quota, 10000, 1) {
			t.Fatalf("per-call quota = %d, want ~10000", res.Quota)
		}
	}
	// 分组倍率 0.8（VIP 折扣）
	res, _ := PriceUsage(rule, 0.8, Usage{})
	if !approx(res.Quota, 8000, 1) {
		t.Fatalf("per-call with discount = %d, want ~8000", res.Quota)
	}
}

func TestPriceUsage_Tiered(t *testing.T) {
	withQuotaPerUnit(t)
	tiers := []PriceTier{
		{Tier: 1, UpToTokens: 1000, InputRatio: 4, OutputRatio: 8},
		{Tier: 2, UpToTokens: 0, InputRatio: 2, OutputRatio: 4}, // 无上界
	}
	b, _ := json.Marshal(tiers)
	rule := &PricingRule{
		Id:          3,
		BillingMode: BillingModeTieredByTokens,
		Tiers:       string(b),
	}

	// 总量 500（≤1000）→ tier 1，输入 500 × 4 × 0.002/1000 = $0.004 → 2000
	res, ok := PriceUsage(rule, 1.0, Usage{PromptTokens: 500, CompletionTokens: 0})
	if !ok {
		t.Fatal("expected match")
	}
	if res.Tier != 1 {
		t.Fatalf("tier = %d, want 1", res.Tier)
	}
	if !approx(res.Quota, 2000, 1) {
		t.Fatalf("tier1 quota = %d, want ~2000", res.Quota)
	}

	// 总量 2000（>1000）→ tier 2，输入 2000 × 2 × 0.002/1000 = $0.008 → 4000
	res2, _ := PriceUsage(rule, 1.0, Usage{PromptTokens: 2000, CompletionTokens: 0})
	if res2.Tier != 2 {
		t.Fatalf("tier = %d, want 2", res2.Tier)
	}
	if !approx(res2.Quota, 4000, 1) {
		t.Fatalf("tier2 quota = %d, want ~4000", res2.Quota)
	}

	// 边界：恰好 1000 → 仍属 tier 1
	resB, _ := PriceUsage(rule, 1.0, Usage{PromptTokens: 1000})
	if resB.Tier != 1 {
		t.Fatalf("boundary tier = %d, want 1", resB.Tier)
	}
}

func TestPriceUsage_CacheHit(t *testing.T) {
	withQuotaPerUnit(t)
	rule := &PricingRule{
		Id:            4,
		BillingMode:   BillingModePerToken,
		InputRatio:    10,
		OutputRatio:   20,
		CacheHitRatio: 1, // 命中价是输入的 1/10
	}
	// 1000 输入中 800 命中缓存，200 未命中
	// 未命中：200 × 10 × 0.002/1000 = $0.004
	// 命中  ：800 × 1  × 0.002/1000 = $0.0016
	// 合计 $0.0056 → 2800
	res, ok := PriceUsage(rule, 1.0, Usage{PromptTokens: 1000, CacheHitTokens: 800})
	if !ok {
		t.Fatal("expected match")
	}
	if !approx(res.Quota, 2800, 2) {
		t.Fatalf("cache quota = %d, want ~2800", res.Quota)
	}

	// 没有缓存配置时，缓存命中也按普通输入价计（不应少算）
	plain := &PricingRule{Id: 5, BillingMode: BillingModePerToken, InputRatio: 10, OutputRatio: 20}
	resPlain, _ := PriceUsage(plain, 1.0, Usage{PromptTokens: 1000, CacheHitTokens: 800})
	// 1000 × 10 × 0.002/1000 = $0.02 → 10000
	if !approx(resPlain.Quota, 10000, 2) {
		t.Fatalf("plain quota = %d, want ~10000", resPlain.Quota)
	}
}

func TestPricingRule_Matches(t *testing.T) {
	now := time.Date(2025, 1, 1, 14, 0, 0, 0, time.Local) // 14:00

	cases := []struct {
		name  string
		rule  PricingRule
		model string
		group string
		ct    int
		want  bool
	}{
		{"任意模型", PricingRule{Status: 1, InputRatio: 1}, "gpt-4", "default", 1, true},
		{"精确模型命中", PricingRule{Status: 1, Model: "gpt-4", InputRatio: 1}, "gpt-4", "default", 1, true},
		{"精确模型未命中", PricingRule{Status: 1, Model: "gpt-4", InputRatio: 1}, "gpt-3.5", "default", 1, false},
		{"通配模型命中", PricingRule{Status: 1, Model: "gpt-4*", InputRatio: 1}, "gpt-4o-mini", "default", 1, true},
		{"通配模型未命中", PricingRule{Status: 1, Model: "gpt-4*", InputRatio: 1}, "claude-3", "default", 1, false},
		{"分组命中", PricingRule{Status: 1, Group: "vip", InputRatio: 1}, "gpt-4", "vip", 1, true},
		{"分组未命中", PricingRule{Status: 1, Group: "vip", InputRatio: 1}, "gpt-4", "default", 1, false},
		{"多分组命中", PricingRule{Status: 1, Group: "vip,svip", InputRatio: 1}, "gpt-4", "svip", 1, true},
		{"渠道类型命中", PricingRule{Status: 1, ChannelType: 1, InputRatio: 1}, "gpt-4", "default", 1, true},
		{"渠道类型未命中", PricingRule{Status: 1, ChannelType: 1, InputRatio: 1}, "gpt-4", "default", 3, false},
		{"渠道类型任意", PricingRule{Status: 1, ChannelType: -1, InputRatio: 1}, "gpt-4", "default", 99, true},
		{"零值渠道类型视为任意", PricingRule{Status: 1, InputRatio: 1}, "gpt-4", "default", 99, true},
		{"禁用规则不命中", PricingRule{Status: 2, InputRatio: 1}, "gpt-4", "default", 1, false},
		{"时间窗内", PricingRule{Status: 1, InputRatio: 1, TimeWindows: `[{"start_hour":8,"end_hour":18}]`}, "gpt-4", "default", 1, true},
		{"时间窗外", PricingRule{Status: 1, InputRatio: 1, TimeWindows: `[{"start_hour":20,"end_hour":23}]`}, "gpt-4", "default", 1, false},
		{"跨零点窗口外", PricingRule{Status: 1, InputRatio: 1, TimeWindows: `[{"start_hour":22,"end_hour":6}]`}, "gpt-4", "default", 1, false},
		{"白窗覆盖当前时刻", PricingRule{Status: 1, InputRatio: 1, TimeWindows: `[{"start_hour":9,"end_hour":17}]`}, "gpt-4", "default", 1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.rule.Matches(c.model, c.group, c.ct, now); got != c.want {
				t.Fatalf("Matches() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestTimeWindow_MidnightCrossing(t *testing.T) {
	w := TimeWindow{StartHour: 22, EndHour: 6}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local)
	for hour, want := range map[int]bool{
		0: true, 1: true, 5: true, 6: false, 12: false, 21: false, 22: true, 23: true,
	} {
		got := w.Matches(base.Add(time.Duration(hour) * time.Hour))
		if got != want {
			t.Fatalf("hour %d: got %v, want %v", hour, got, want)
		}
	}
}

func TestValidatePricingRule(t *testing.T) {
	validTiers, _ := json.Marshal([]PriceTier{{Tier: 1, UpToTokens: 0, InputRatio: 1}})

	cases := []struct {
		name    string
		rule    PricingRule
		wantErr bool
	}{
		{"空名称", PricingRule{BillingMode: BillingModePerToken, InputRatio: 1}, true},
		{"按量计费缺倍率", PricingRule{Name: "a", BillingMode: BillingModePerToken}, true},
		{"按量计费合法", PricingRule{Name: "a", BillingMode: BillingModePerToken, InputRatio: 1}, false},
		{"按次计费缺价格", PricingRule{Name: "a", BillingMode: BillingModePerCall}, true},
		{"按次计费合法", PricingRule{Name: "a", BillingMode: BillingModePerCall, PerCallPrice: 0.01}, false},
		{"阶梯无档位", PricingRule{Name: "a", BillingMode: BillingModeTieredByTokens}, true},
		{"阶梯合法", PricingRule{Name: "a", BillingMode: BillingModeTieredByTokens, Tiers: string(validTiers)}, false},
		{"未知计费模式", PricingRule{Name: "a", BillingMode: "wat"}, true},
		{"负数倍率", PricingRule{Name: "a", BillingMode: BillingModePerToken, InputRatio: -1}, true},
		{"非法时间窗", PricingRule{Name: "a", BillingMode: BillingModePerToken, InputRatio: 1, TimeWindows: "{not json"}, true},
		{"时间窗小时越界", PricingRule{Name: "a", BillingMode: BillingModePerToken, InputRatio: 1, TimeWindows: `[{"start_hour":30,"end_hour":1}]`}, true},
		{"时间窗合法", PricingRule{Name: "a", BillingMode: BillingModePerToken, InputRatio: 1, TimeWindows: `[{"start_hour":8,"end_hour":18}]`}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePricingRule(&c.rule)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidatePricingRule() err = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestValidatePricingRule_TierOrdering(t *testing.T) {
	// 只有最后一档可以无上界
	bad, _ := json.Marshal([]PriceTier{
		{Tier: 1, UpToTokens: 0, InputRatio: 1},
		{Tier: 2, UpToTokens: 1000, InputRatio: 2},
	})
	r := PricingRule{Name: "a", BillingMode: BillingModeTieredByTokens, Tiers: string(bad)}
	if err := ValidatePricingRule(&r); err == nil {
		t.Fatal("expected error: 非末档不能无上界")
	}
}

func TestResolvePrice_FallsBackToLegacy(t *testing.T) {
	withQuotaPerUnit(t)
	// 没有任何规则时，必须回落到旧倍率，且结算公式与改造前一致
	p := model_ResolvePriceNoRules(t)
	if p.IsRuleBased() {
		t.Fatal("expected legacy path when no rules configured")
	}
	res := p.Settle(Usage{PromptTokens: 1000, CompletionTokens: 1000})
	if res.BillingMode != "legacy_ratio" {
		t.Fatalf("billing mode = %q, want legacy_ratio", res.BillingMode)
	}
	// 旧公式：ceil 前是 (prompt + completion × completionRatio) × modelRatio × groupRatio
	// 只要是正数且量级合理即可（具体值依赖内置 ratio 表）
	if res.Quota <= 0 {
		t.Fatalf("legacy quota should be positive, got %d", res.Quota)
	}
}

// model_ResolvePriceNoRules 在不触碰数据库的前提下解析价格。
// 把缓存标记为已加载且为空，模拟「未配置任何规则」的部署。
func model_ResolvePriceNoRules(t *testing.T) CachedPrice {
	t.Helper()
	pricingRuleLock.Lock()
	pricingRuleCache = nil
	pricingRuleDirty = false
	pricingRuleLock.Unlock()
	t.Cleanup(InvalidatePricingRules)
	return ResolvePrice(PriceContext{Model: "gpt-3.5-turbo", Group: "default", ChannelType: 1})
}

func TestEstimatePreConsume(t *testing.T) {
	withQuotaPerUnit(t)
	rule := &PricingRule{Id: 1, BillingMode: BillingModePerToken, InputRatio: 2.5}
	// 1000 × 2.5 × 0.002/1000 = $0.005 → 2500
	q, ok := EstimatePreConsume(rule, 1.0, 1000)
	if !ok {
		t.Fatal("expected ok")
	}
	if !approx(q, 2500, 1) {
		t.Fatalf("estimate = %d, want ~2500", q)
	}

	// 按次计费：预扣就是单次价格
	perCall := &PricingRule{Id: 2, BillingMode: BillingModePerCall, PerCallPrice: 0.02}
	q2, ok := EstimatePreConsume(perCall, 1.0, 1000)
	if !ok {
		t.Fatal("expected ok")
	}
	if !approx(q2, 10000, 1) {
		t.Fatalf("per-call estimate = %d, want ~10000", q2)
	}
}

func TestQuotaFromUSD_MinimumOne(t *testing.T) {
	withQuotaPerUnit(t)
	// 极小金额也必须保底 1，否则会产生 0 额度的「免费请求」
	if got := quotaFromUSD(0.000000001); got != 1 {
		t.Fatalf("quotaFromUSD(tiny) = %d, want 1", got)
	}
	if got := USDToQuota(0); got != 0 {
		t.Fatalf("USDToQuota(0) = %d, want 0", got)
	}
	if got := USDToQuota(1); got != 500000 {
		t.Fatalf("USDToQuota(1) = %d, want 500000", got)
	}
}
