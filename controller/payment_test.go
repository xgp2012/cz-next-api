package controller

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/model"
)

// md5Hex 测试内独立实现的 MD5 小写十六进制，用于和生产代码交叉验证
//
// 为什么要自己再实现一遍：如果测试直接复用 epaySign 的内部拼接函数，
// 一旦拼接规则（顺序/空值排除）被改错，两边会一起错，测试形同虚设。
func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestEpaySign 锁定 EPay 签名的字段顺序、空值排除与密钥拼接方式
func TestEpaySign(t *testing.T) {
	const testKey = "test-epay-secret-key"

	// 完整参数：全部 9 个签名字段都有值
	fullParams := map[string]string{
		"pid":          "1001",
		"type":         "alipay",
		"out_trade_no": "USR1NOABC1231700000000",
		"notify_url":   "https://example.com/api/user/epay/notify",
		"return_url":   "https://example.com/console/topup",
		"name":         "支付宝",
		"money":        "10.00",
		"sitename":     "One API",
		"trade_status": "TRADE_SUCCESS",
		// 以下字段不参与签名
		"sign":      "whatever",
		"sign_type": "MD5",
	}

	// 手工按 ASCII 升序拼装期望值，明确写出顺序，
	// 这样任何人改动 epaySignFields 都会让本用例失败并暴露意图。
	expectedBase := strings.Join([]string{
		"money=10.00",
		"name=支付宝",
		"notify_url=https://example.com/api/user/epay/notify",
		"out_trade_no=USR1NOABC1231700000000",
		"pid=1001",
		"return_url=https://example.com/console/topup",
		"sitename=One API",
		"trade_status=TRADE_SUCCESS",
		"type=alipay",
	}, "&")
	expectedSign := md5Hex(expectedBase + testKey)

	// 断言手工排序确实等于 sort.Strings 的结果，防止我们观察到的顺序是巧合
	sorted := make([]string, 0, 9)
	for _, key := range []string{"money", "name", "notify_url", "out_trade_no", "pid", "return_url", "sitename", "trade_status", "type"} {
		sorted = append(sorted, key)
	}
	assert.True(t, sort.StringsAreSorted(sorted), "测试里写死的字段顺序必须已是 ASCII 升序")

	tests := []struct {
		name   string
		params map[string]string
		key    string
		want   string
	}{
		{
			name:   "完整参数_按字典序拼接后直接拼密钥",
			params: fullParams,
			key:    testKey,
			want:   expectedSign,
		},
		{
			name: "空值字段被排除",
			params: map[string]string{
				"pid":          "1001",
				"type":         "alipay",
				"out_trade_no": "USR1NOABC1231700000000",
				"money":        "10.00",
				"name":         "", // 空值：必须被排除
				"notify_url":   "", // 空值：必须被排除
			},
			key: testKey,
			// 期望值只需 pid/type/out_trade_no/money 四个字段
			want: md5Hex("money=10.00&out_trade_no=USR1NOABC1231700000000&pid=1001&type=alipay" + testKey),
		},
		{
			name: "仅空白字符也视为空值",
			params: map[string]string{
				"pid":   "1001",
				"money": "10.00",
				"name":  "   ",
			},
			key:  testKey,
			want: md5Hex("money=10.00&pid=1001" + testKey),
		},
		{
			name:   "没有签名字段时退化为MD5(密钥)",
			params: map[string]string{"sign_type": "MD5"},
			key:    testKey,
			want:   md5Hex(testKey),
		},
		{
			name: "不参与签名的字段被忽略",
			params: map[string]string{
				"pid":       "1001",
				"money":     "10.00",
				"sign":      "deadbeef",
				"sign_type": "MD5",
				"extra":     "should-be-ignored",
			},
			key:  testKey,
			want: md5Hex("money=10.00&pid=1001" + testKey),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, epaySign(tc.params, tc.key))
		})
	}

	// 输出必须是 32 位小写十六进制
	sign := epaySign(fullParams, testKey)
	assert.Len(t, sign, 32)
	assert.Equal(t, strings.ToLower(sign), sign)
}

// TestEpaySignChangesWithAnyField 证明任一字段或被密钥变化都会改变签名
//
// 为什么重要：如果存在「改了钱数签名却不变」的漏洞，攻击者就能用 1 元订单
// 的回调核销 100 元订单，这是支付系统最致命的缺陷之一。
func TestEpaySignChangesWithAnyField(t *testing.T) {
	const testKey = "test-epay-secret-key"

	base := map[string]string{
		"pid":          "1001",
		"type":         "alipay",
		"out_trade_no": "USR1NOABC1231700000000",
		"notify_url":   "https://example.com/api/user/epay/notify",
		"return_url":   "https://example.com/console/topup",
		"name":         "支付宝",
		"money":        "10.00",
		"sitename":     "One API",
		"trade_status": "TRADE_SUCCESS",
	}
	baseSign := epaySign(base, testKey)

	for field, mutated := range map[string]string{
		"pid":          "1002",
		"type":         "wxpay",
		"out_trade_no": "USR1NOABC1231700000001",
		"notify_url":   "https://example.com/api/user/epay/notify2",
		"return_url":   "https://example.com/console/topup2",
		"name":         "微信支付",
		"money":        "10.01", // 金额差一分也必须改变签名
		"sitename":     "One API 2",
		"trade_status": "TRADE_CLOSED",
	} {
		t.Run(field, func(t *testing.T) {
			tampered := make(map[string]string, len(base))
			for k, v := range base {
				tampered[k] = v
			}
			tampered[field] = mutated
			assert.NotEqual(t, baseSign, epaySign(tampered, testKey),
				"字段 %s 被篡改后签名必须变化", field)
		})
	}

	// 换密钥也必须变化
	assert.NotEqual(t, baseSign, epaySign(base, testKey+"x"))
	// 空密钥与真实密钥必须不同（空密钥场景由 epayVerify 直接拒绝）
	assert.NotEqual(t, baseSign, epaySign(base, ""))
}

// TestEpayVerify 校验回调验签：合法通过、被篡改则失败
func TestEpayVerify(t *testing.T) {
	const testKey = "test-epay-secret-key"

	newParams := func() map[string]string {
		return map[string]string{
			"pid":          "1001",
			"type":         "alipay",
			"out_trade_no": "USR1NOABC1231700000000",
			"notify_url":   "https://example.com/api/user/epay/notify",
			"return_url":   "https://example.com/console/topup",
			"name":         "支付宝",
			"money":        "10.00",
			"sitename":     "One API",
			"trade_status": "TRADE_SUCCESS",
			"sign_type":    "MD5",
		}
	}
	signed := func() map[string]string {
		p := newParams()
		p["sign"] = epaySign(p, testKey)
		return p
	}

	t.Run("合法签名通过", func(t *testing.T) {
		assert.True(t, epayVerify(signed(), testKey))
	})

	t.Run("签名大写也通过", func(t *testing.T) {
		p := signed()
		p["sign"] = strings.ToUpper(p["sign"])
		assert.True(t, epayVerify(p, testKey))
	})

	t.Run("金额被篡改则失败", func(t *testing.T) {
		p := signed()
		p["money"] = "99.00"
		assert.False(t, epayVerify(p, testKey))
	})

	t.Run("订单号被篡改则失败", func(t *testing.T) {
		p := signed()
		p["out_trade_no"] = "USR1NOABC1231700000099"
		assert.False(t, epayVerify(p, testKey))
	})

	t.Run("缺少签名则失败", func(t *testing.T) {
		p := newParams()
		assert.False(t, epayVerify(p, testKey))
	})

	t.Run("密钥不匹配则失败", func(t *testing.T) {
		assert.False(t, epayVerify(signed(), "another-key"))
	})

	t.Run("密钥为空时一律拒绝", func(t *testing.T) {
		p := signed()
		// 即使攻击者用空密钥重新签名，也必须被拒绝
		p["sign"] = epaySign(p, "")
		assert.False(t, epayVerify(p, ""))
	})

	t.Run("凭空构造的签名失败", func(t *testing.T) {
		p := newParams()
		p["sign"] = strings.Repeat("a", 32)
		assert.False(t, epayVerify(p, testKey))
	})
}

// TestNormalizeTopUpAmountCents 锁定金额校验规则（MinTopUp 美元 → 分）
func TestNormalizeTopUpAmountCents(t *testing.T) {
	originMin := config.MinTopUp
	defer func() { config.MinTopUp = originMin }()
	config.MinTopUp = 1.0

	tests := []struct {
		name    string
		in      int64
		wantVal int64
		wantErr bool
	}{
		{name: "正好等于下限", in: 100, wantVal: 100},
		{name: "高于下限", in: 1000, wantVal: 1000},
		{name: "低于下限", in: 99, wantErr: true},
		{name: "零", in: 0, wantErr: true},
		{name: "负数", in: -100, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeTopUpAmountCents(tc.in)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantVal, got)
		})
	}

	// 下限随配置变化：MinTopUp = 5 时 499 分必须被拒绝
	config.MinTopUp = 5.0
	_, err := normalizeTopUpAmountCents(499)
	assert.Error(t, err)
	got, err := normalizeTopUpAmountCents(500)
	assert.NoError(t, err)
	assert.Equal(t, int64(500), got)
}

// TestCentsToMoneyString 金额「分 → 元字符串」转换精度
//
// 为什么单独测：EPay 的 money 字段直接参与签名，
// 任何精度/格式抖动都会让回调验签失败。
func TestCentsToMoneyString(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{in: 0, want: "0.00"},
		{in: 1, want: "0.01"},
		{in: 10, want: "0.10"},
		{in: 99, want: "0.99"},
		{in: 100, want: "1.00"},
		{in: 1234, want: "12.34"},
		{in: 100000, want: "1000.00"},
		// 浮点除法容易出尾数误差的经典值
		{in: 123456789, want: "1234567.89"},
		{in: -5, want: "0.00"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, centsToMoneyString(tc.in), "cents=%d", tc.in)
	}
}

// TestFindPayMethod 支付方式查找（未知类型必须拒绝）
func TestFindPayMethod(t *testing.T) {
	methods := []model.PayMethod{
		{Name: "支付宝", Type: "alipay", MinTopUp: 10},
		{Name: "微信支付", Type: "wxpay", MinTopUp: 1},
	}

	got, err := findPayMethod(methods, "alipay")
	assert.NoError(t, err)
	assert.Equal(t, "支付宝", got.Name)

	_, err = findPayMethod(methods, "unionpay")
	assert.Error(t, err)

	_, err = findPayMethod(methods, "")
	assert.Error(t, err)

	_, err = findPayMethod(nil, "alipay")
	assert.Error(t, err)
}

// TestCheckMethodMinTopUp 支付方式自身的起充门槛
func TestCheckMethodMinTopUp(t *testing.T) {
	method := &model.PayMethod{Name: "支付宝", Type: "alipay", MinTopUp: 10}

	assert.NoError(t, checkMethodMinTopUp(method, 1000))
	assert.Error(t, checkMethodMinTopUp(method, 999))
	// MinTopUp 为 0 表示该方式不额外限制
	assert.NoError(t, checkMethodMinTopUp(&model.PayMethod{Name: "微信"}, 1))
	// nil 安全
	assert.NoError(t, checkMethodMinTopUp(nil, 1))
}

// TestWebhookSignature 通用 webhook 的 HMAC-SHA256 签名
func TestWebhookSignature(t *testing.T) {
	const secret = "test-webhook-secret"
	body := []byte(`{"trade_no":"USR1NOABC1700000000","status":"success","amount_cents":100}`)

	sig := webhookSignature(body, secret)
	assert.Len(t, sig, 64) // SHA-256 十六进制
	assert.Equal(t, strings.ToLower(sig), sig)

	// 确定性
	assert.Equal(t, sig, webhookSignature(body, secret))
	// 空密钥不应与真实密钥相同
	assert.NotEqual(t, sig, webhookSignature(body, ""))
	// 换密钥变化
	assert.NotEqual(t, sig, webhookSignature(body, secret+"x"))
	// 报文变化
	assert.NotEqual(t, sig, webhookSignature(append(body, ' '), secret))
	// 与测试内独立实现（hmac.Write）交叉验证，防止实现被误改
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	assert.Equal(t, hex.EncodeToString(mac.Sum(nil)), sig)
}

// TestParseAmountOptions 金额档位解析：非法 JSON 回退默认值
func TestParseAmountOptions(t *testing.T) {
	assert.Equal(t, []float64{10, 20}, parseAmountOptions("[10,20]"))
	assert.Equal(t, []float64{5.5, 10}, parseAmountOptions(" [5.5, 10] "))
	// 垃圾输入回退到默认档位，而不是空切片，避免充值页无按钮可选
	assert.NotEmpty(t, parseAmountOptions("not-json"))
	assert.NotEmpty(t, parseAmountOptions(""))
	assert.Equal(t, []float64{}, parseAmountOptions("[]"))
}
