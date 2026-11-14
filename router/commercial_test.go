package router

import (
	"sort"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/songquanpeng/one-api/common"
)

// ============================================================================
// 路由注册回归测试
//
// 为什么值得专门测路由：商业化功能的路由分散在 SetPaymentRouter 与
// SetCommercialRouter 两个文件里，且回调接口刻意**不加**鉴权。
// 一旦有人重构时漏调用、改错前缀、或误给回调加上 UserAuth，
// 编译与既有单测都不会报错，但线上会表现为
// "支付回调 401，订单永远 pending" 或 "接口 404"。
// 这里把"应该存在哪些路由"和"哪些路由必须无鉴权"固化成断言。
// ============================================================================

// collectRoutes 递归收集 engine 上注册的所有路由（method + fullPath）
func collectRoutes(routes gin.RoutesInfo) map[string]bool {
	out := make(map[string]bool, len(routes))
	for _, r := range routes {
		out[r.Method+" "+r.Path] = true
	}
	return out
}

// buildAPIEngine 构造一个挂着全部 /api 路由的引擎。
//
// 注意：这里直接调用 SetApiRouter(engine)，与 main.go 的真实装配路径一致，
// 避免"测的是另一套注册逻辑"这种假阳性。
//
// 有两处必须模拟 main.go 的全局装配，否则测试会因为环境缺失而 panic，
// 而不是因为被断言的行为出错：
//
//  1. session 中间件：middleware.authHelper 第一步就是 sessions.Default(c)，
//     它是 MustGet，没有 session 中间件会直接 panic。
//     main.go:128-129 装了它，这里必须照做。
//  2. Redis 关闭：common.RedisEnabled 的零值是 true，而测试环境没有 redis
//     客户端（RDB == nil）。限流中间件在**注册时**就根据这个标志决定用
//     redis 实现还是内存实现，选错会在 rdb.LLen 上空指针。
func buildAPIEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	defer func() { common.RedisEnabled = oldRedis }()

	engine := gin.New()
	store := cookie.NewStore([]byte("test-session-secret"))
	engine.Use(sessions.Sessions("session", store))
	SetApiRouter(engine)
	return engine
}

func TestCommercialRoutesRegistered(t *testing.T) {
	engine := buildAPIEngine()
	routes := collectRoutes(engine.Routes())

	// 必须存在的路由清单。
	// 这份清单刻意手写而非从代码推导——它的价值就在于"人看一眼就知道
	// 该有哪些对外接口"，并能在重构时报警。
	required := []string{
		// ---- 定价 ----
		"GET /api/pricing",
		"GET /api/pricing_rules/",
		"POST /api/pricing_rules/",
		"PUT /api/pricing_rules/",
		"DELETE /api/pricing_rules/:id",

		// ---- 套餐 / 订阅 ----
		"GET /api/subscription/plans",
		"GET /api/subscription/self/",
		"PUT /api/subscription/self/preference",
		"PUT /api/subscription/self/auto_renew",
		"POST /api/subscription/balance/pay",
		"GET /api/subscription/plans/admin/",
		"POST /api/subscription/plans/admin/",
		"PUT /api/subscription/plans/admin/",
		"DELETE /api/subscription/plans/admin/:id",
		"POST /api/subscription/invalidate",

		// ---- 推广分成 ----
		"GET /api/user/aff/",
		"GET /api/user/aff/commissions",
		"GET /api/user/aff/invitees",
		"POST /api/user/aff_transfer",

		// ---- 营收看板 ----
		"GET /api/admin/revenue",

		// ---- 充值与支付 ----
		"GET /api/user/topup/info",
		"GET /api/user/topup/self",
		"POST /api/user/pay",
		"POST /api/user/amount",
		"GET /api/user/epay/notify",
		"POST /api/user/epay/notify",
		"POST /api/pay/notify",
		"GET /api/user/topup",
		"POST /api/user/topup/complete",
	}

	var missing []string
	for _, want := range required {
		if !routes[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("以下商业化路由未注册：\n  %s", strings.Join(missing, "\n  "))
	}
}

// TestPaymentCallbacksAreUnauthenticated 守住一个安全上极关键的约束：
// 支付回调由网关调用，不带会话 cookie。
// 一旦有人"顺手"给回调加上 UserAuth，编译和单测都不会报错，
// 但线上所有支付回调会返回 401，订单永远停在 pending——用户付了钱却没到账。
//
// 这里通过检查回调路由的 handler 链来验证。gin 不暴露中间件数量，
// 因此改为**行为验证**：构造请求打回调接口，断言它没有进入鉴权分支。
func TestPaymentCallbacksAreUnauthenticated(t *testing.T) {
	engine := buildAPIEngine()
	routes := collectRoutes(engine.Routes())

	// 回调必须存在
	for _, p := range []string{
		"GET /api/user/epay/notify",
		"POST /api/user/epay/notify",
		"POST /api/pay/notify",
	} {
		if !routes[p] {
			t.Fatalf("回调路由缺失：%s", p)
		}
	}

	// 行为验证：未携带任何凭证请求回调，不应得到 401/403。
	// 未配置网关时应得到"未开启"类业务响应（HTTP 200 + success:false），
	// 或签名校验失败（同样是 HTTP 200 + success:false / 纯文本 fail）。
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/user/epay/notify"},
		{"POST", "/api/pay/notify"},
	} {
		w := performRequest(engine, tc.method, tc.path, nil)
		if w.Code == 401 || w.Code == 403 {
			t.Fatalf("回调 %s %s 返回了 %d，说明被加上了会话鉴权；"+
				"支付网关不会带 cookie，这会导致所有回调失败", tc.method, tc.path, w.Code)
		}
	}
}

// TestProtectedCommercialRoutesRequireAuth 反向断言：业务接口必须拒绝匿名访问。
func TestProtectedCommercialRoutesRequireAuth(t *testing.T) {
	engine := buildAPIEngine()

	protected := []struct{ method, path string }{
		{"GET", "/api/pricing_rules/"},
		{"GET", "/api/subscription/self/"},
		{"POST", "/api/subscription/balance/pay"},
		{"GET", "/api/user/aff/"},
		{"POST", "/api/user/aff_transfer"},
		{"GET", "/api/admin/revenue"},
		{"GET", "/api/user/topup/info"},
		{"POST", "/api/user/pay"},
		{"GET", "/api/user/topup"},
		{"POST", "/api/user/topup/complete"},
		{"GET", "/api/subscription/plans/admin/"},
		{"POST", "/api/subscription/invalidate"},
	}
	for _, tc := range protected {
		w := performRequest(engine, tc.method, tc.path, nil)
		if w.Code != 401 && w.Code != 403 {
			t.Errorf("%s %s 匿名访问返回 %d，期望 401/403（鉴权缺失）",
				tc.method, tc.path, w.Code)
		}
	}
}

// TestPublicRoutesAccessible 公开接口不应要求登录：
// 价格墙与套餐列表是转化漏斗的第一步，登录前必须能看到。
func TestPublicRoutesAccessible(t *testing.T) {
	engine := buildAPIEngine()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/pricing"},
		{"GET", "/api/subscription/plans"},
	} {
		w := performRequest(engine, tc.method, tc.path, nil)
		if w.Code == 401 || w.Code == 403 {
			t.Errorf("%s %s 是公开接口，却要求登录（%d）", tc.method, tc.path, w.Code)
		}
	}
}

// TestNoDuplicateRoutes 守住一类极隐蔽的路由 bug：同一 method+path 被注册两次。
//
// gin 对此**不会报错**，而是静默保留先注册的那个。真实踩到过：
// router/api.go 里有历史遗留的 selfRoute.GET("/aff", controller.GetAffCode)
// （只返回一个裸邀请码字符串），而 router/commercial.go 又把
// GET /api/user/aff 注册为 controller.GetAffInfo（返回完整对象）。
// 两者 path 相同，旧的那个赢了 —— 前端拿到 "k5om" 而不是对象，
// 页面直接空白，而后端日志、编译、单测全部正常。
//
// 因此这里直接对 route 表做重复检测，而不是只断言"路由存在"：
// 断言存在无法发现"存在但被别的实现顶替"。
func TestNoDuplicateRoutes(t *testing.T) {
	engine := buildAPIEngine()

	seen := make(map[string]bool, len(engine.Routes()))
	var dups []string
	for _, r := range engine.Routes() {
		key := r.Method + " " + r.Path
		if seen[key] {
			dups = append(dups, key)
			continue
		}
		seen[key] = true
	}
	if len(dups) > 0 {
		sort.Strings(dups)
		t.Fatalf("发现重复注册的路由（gin 会静默保留先注册的那个）：\n  %s",
			strings.Join(dups, "\n  "))
	}
}

// TestAffOverviewIsNotShadowed 精确锁定一个真实踩过的 bug 的行为面。
//
// 背景：router/api.go 里曾有历史遗留的 selfRoute.GET("/aff", GetAffCode)
// （只返回裸邀请码字符串 "k5om"），而 router/commercial.go 又把同一
// method+path 注册为 GetAffInfo（返回完整对象）。gin 静默保留先注册的那个，
// 于是前端拿到字符串而不是对象，推广页面整体空白。
//
// 注意 gin 对 "/api/user/aff" 与 "/api/user/aff/" 的处理：本接口注册为
// group + "/"，所以不带尾斜杠的请求会先得到 301 重定向到带斜杠的路径。
// 因此断言的是「不是裸字符串」，而不是「一定 401」。
func TestAffOverviewIsNotShadowed(t *testing.T) {
	engine := buildAPIEngine()

	// 不带尾斜杠：允许 301 重定向（gin 的正常行为），但绝不能 200 返回裸字符串
	w := performRequest(engine, "GET", "/api/user/aff", nil)
	if w.Code == 200 {
		body := w.Body.String()
		if !strings.Contains(body, "aff_code") {
			t.Fatalf("GET /api/user/aff 返回 200 但不是 GetAffInfo 的对象："+
				"期望含 aff_code 字段，实际 %q（很可能被旧的 GetAffCode 顶替了）", body)
		}
	} else if w.Code != 301 && w.Code != 302 && w.Code != 401 && w.Code != 403 {
		t.Fatalf("GET /api/user/aff 返回了非预期的 %d", w.Code)
	}

	// 带尾斜杠：匿名必须被鉴权拦住
	w2 := performRequest(engine, "GET", "/api/user/aff/", nil)
	if w2.Code != 401 && w2.Code != 403 {
		t.Fatalf("GET /api/user/aff/ 匿名访问返回 %d，期望 401/403", w2.Code)
	}
}
