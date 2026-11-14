package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	glebarezsqlite "github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/model"
)

// TestMain 为 router 包的测试准备最小可用环境。
//
// 为什么需要它：路由测试的价值之一是"公开接口确实不需要登录"。
// 要断言这一点就必须真的把请求打到 handler 上，而 handler 会访问数据库。
// 没有 DB 时 gorm 会在 (*DB).getInstance 上空指针 panic——
// 那是环境缺失导致的失败，不是被断言的行为出错，会把真正的回归信号淹没。
//
// 用 glebarez/sqlite（纯 Go，无 CGO）而不是项目自带的驱动：
// 后者的底层 mattn/go-sqlite3 需要 gcc，没装 gcc 的机器上
// `go test ./router/` 会先在链接阶段失败。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	// common.RedisEnabled 零值为 true，测试环境没有 redis 客户端
	common.RedisEnabled = false

	dsn := "file:router_test?mode=memory&cache=shared&_pragma=busy_timeout(5000)"
	db, err := gorm.Open(glebarezsqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "router tests: failed to open sqlite: %v\n", err)
		os.Exit(1)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Token{}, &model.Channel{}, &model.Redemption{},
		&model.Log{}, &model.Option{}, &model.Ability{},
		&model.PricingRule{}, &model.TopUp{}, &model.SubscriptionPlan{},
		&model.UserSubscription{}, &model.AffCommission{}, &model.UserAffStats{},
	); err != nil {
		fmt.Fprintf(os.Stderr, "router tests: failed to migrate: %v\n", err)
		os.Exit(1)
	}
	model.DB = db
	model.LOG_DB = db

	os.Exit(m.Run())
}

// performRequest 发一个不带任何凭证的请求，返回 Recorder。
// 断言的是"路由存在且鉴权中间件已生效"，不关心业务结果是否正确
// （业务正确性由 model/controller 层单测负责）。
func performRequest(engine *gin.Engine, method, path string, body []byte) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(string(body)))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}
