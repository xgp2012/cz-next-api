package random

import (
	"strings"
	"testing"
)

// ============================================================================
// 随机串唯一性回归测试
//
// 背景（这是一次真实缺陷的回归防护）：
//   原实现在每次 GetRandomString 调用时都执行 rand.Seed(time.Now().UnixNano())。
//   平台时钟粒度不足纳秒时，同一毫秒内的连续调用会得到完全相同的序列，
//   实测 1000 次调用只产出 51 个不同值，单个值最多重复 40 次。
//   兑换码、邀请码、订单号都以这些串为唯一键，一旦重复就是插入失败
//   （用户看到"生成失败"）或更糟的凭据撞车。
// ============================================================================

func TestGetRandomStringUniqueness(t *testing.T) {
	const n = 2000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		s := GetRandomString(16)
		if len(s) != 16 {
			t.Fatalf("GetRandomString(16) returned length %d", len(s))
		}
		if _, dup := seen[s]; dup {
			t.Fatalf("duplicate random string after %d draws: %q (unique so far: %d)", i, s, len(seen))
		}
		seen[s] = struct{}{}
	}
}

func TestGetRandomNumberStringUniqueness(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		s := GetRandomNumberString(12)
		if len(s) != 12 {
			t.Fatalf("GetRandomNumberString(12) returned length %d", len(s))
		}
		if strings.Trim(s, "0123456789") != "" {
			t.Fatalf("GetRandomNumberString returned non-digit characters: %q", s)
		}
		if _, dup := seen[s]; dup {
			t.Fatalf("duplicate random number string after %d draws: %q", i, s)
		}
		seen[s] = struct{}{}
	}
}

func TestGetUUIDUniqueness(t *testing.T) {
	const n = 2000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		s := GetUUID()
		if len(s) != 32 {
			t.Fatalf("GetUUID() length = %d, want 32 (dashes stripped)", len(s))
		}
		if strings.Contains(s, "-") {
			t.Fatalf("GetUUID() should have dashes stripped: %q", s)
		}
		if _, dup := seen[s]; dup {
			t.Fatalf("duplicate uuid after %d draws: %q", i, s)
		}
		seen[s] = struct{}{}
	}
}

func TestGenerateKeyShape(t *testing.T) {
	k := GenerateKey()
	if len(k) != 48 {
		t.Fatalf("GenerateKey() length = %d, want 48", len(k))
	}
	for _, c := range k {
		if !strings.ContainsRune(keyChars, c) {
			t.Fatalf("GenerateKey() contains character outside keyChars: %q", c)
		}
	}
}

func TestGetSecureRandomString(t *testing.T) {
	const n = 500
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		s := GetSecureRandomString(24)
		if len(s) != 24 {
			t.Fatalf("GetSecureRandomString(24) length = %d", len(s))
		}
		if _, dup := seen[s]; dup {
			t.Fatalf("duplicate secure random string: %q", s)
		}
		seen[s] = struct{}{}
	}
}

func TestRandRange(t *testing.T) {
	// 边界：max <= min 时必须返回 min，不能 panic（rand.Intn 对非正数会 panic）
	if got := RandRange(5, 5); got != 5 {
		t.Fatalf("RandRange(5,5) = %d, want 5", got)
	}
	if got := RandRange(5, 1); got != 5 {
		t.Fatalf("RandRange(5,1) = %d, want 5 (degenerate range)", got)
	}
	// 正常区间：必须落在 [min, max)
	for i := 0; i < 500; i++ {
		v := RandRange(10, 20)
		if v < 10 || v >= 20 {
			t.Fatalf("RandRange(10,20) = %d, out of range", v)
		}
	}
}
