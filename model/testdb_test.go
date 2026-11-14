package model

// ============================================================================
// 测试专用：纯 Go 的 SQLite 内存库
//
// 为什么用 github.com/glebarez/sqlite 而不是项目本身的 gorm.io/driver/sqlite：
//   后者的底层是 mattn/go-sqlite3，需要 CGO；本仓库的 CI/开发机不一定装了 gcc，
//   一旦测试依赖 CGO，`go test ./...` 就会在前面报一个与业务无关的链接错误，
//   把真正要验证的计费/支付逻辑淹掉。
//   glebarez/sqlite 是 modernc.org/sqlite 的纯 Go 移植，无 CGO 依赖。
//
// 这个文件只被 _test.go 引用，不会进入生产二进制。
// ============================================================================

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	glebarezsqlite "github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/songquanpeng/one-api/common"
)

var (
	testDBOnce sync.Once
	testDB     *gorm.DB
	testDBErr  error
)

// setupTestDB 建立一个内存 SQLite，迁移本模块全部表，并把全局 DB / LOG_DB 指过去。
//
// 用 sync.Once 是因为 Go 的测试在同一个包内共享进程；每次重建内存库既慢
// 又容易让并行测试互相干扰。每个测试用例请自行清理自己写入的行。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	testDBOnce.Do(func() {
		dsn := fmt.Sprintf("file:testdb?mode=memory&cache=shared&_pragma=busy_timeout(5000)")
		db, err := gorm.Open(glebarezsqlite.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			testDBErr = err
			return
		}
		if err := db.AutoMigrate(
			&User{}, &Token{}, &Channel{}, &Redemption{}, &Log{}, &Option{}, &Ability{},
			&PricingRule{}, &TopUp{}, &SubscriptionPlan{}, &UserSubscription{},
			&AffCommission{}, &UserAffStats{},
		); err != nil {
			testDBErr = err
			return
		}
		testDB = db
	})
	if testDBErr != nil {
		t.Fatalf("failed to init test db: %v", testDBErr)
	}
	oldDB, oldLogDB := DB, LOG_DB
	DB = testDB
	LOG_DB = testDB
	// common.RedisEnabled 的零值是 true，而测试环境没有 redis 客户端。
	// 不关掉它，任何 Cache* 调用都会在 common.RedisGet 里空指针。
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		DB = oldDB
		LOG_DB = oldLogDB
		common.RedisEnabled = oldRedis
	})
	return testDB
}

// cleanTable 清空一张表，保证测试之间互不影响
func cleanTable(t *testing.T, tables ...interface{}) {
	t.Helper()
	for _, tbl := range tables {
		if err := DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(tbl).Error; err != nil {
			t.Fatalf("failed to clean table: %v", err)
		}
	}
}

// seedUser 插入一个测试用户，返回其 id。
//
// AccessToken 与 AffCode 必须显式赋值且互不相同：两列都是唯一索引，所有
// 测试用户都留空会在插入第二个用户时撞唯一键（空串与空串相同）。
//
// 注意不能直接用 random.GetRandomString 生成：它内部每次调用都执行
// rand.Seed(time.Now().UnixNano())，同一纳秒内连续调用会返回**相同**的串
// （见 random 包的实现），在测试里表现为随机的唯一键冲突。
// 这里改用进程内自增计数器，保证绝对唯一。
func seedUser(t *testing.T, username string, quota int64, group string) *User {
	t.Helper()
	if group == "" {
		group = "default"
	}
	seq := atomic.AddUint64(&testSeedSeq, 1)
	u := &User{
		Username:    username,
		Password:    "x",
		Status:      UserStatusEnabled,
		Quota:       quota,
		Group:       group,
		Role:        RoleCommonUser,
		AccessToken: fmt.Sprintf("%032d", seq),
		AffCode:     fmt.Sprintf("aff%05d", seq),
	}
	if err := DB.Create(u).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return u
}

// testSeedSeq 测试内自增序号，保证生成的唯一字段不冲突
var testSeedSeq uint64
