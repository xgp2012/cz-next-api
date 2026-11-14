//go:build !cgo

package model

// CGO 不可用时的 SQLite 驱动：glebarez/sqlite 是 mattn/go-sqlite3 的
// 纯 Go 移植（底层用 modernc.org/sqlite），无需 gcc。
//
// 为什么需要这个分支：构建阶段 CGO_ENABLED=0 完全没问题，但 mattn 驱动
// 在这种情况下只是一个桩，要等到 main() 里真的 Open 数据库才会报
// "go-sqlite3 requires cgo to work. This is a stub" 然后 FatalLog 退出。
// 也就是说「编译通过」并不代表「能启动」，而没有 gcc 的机器（Windows
// 默认安装、精简容器镜像）就是这个状态。默认使用 SQLite 的小站点会
// 直接起不来，且错误信息对使用者毫无指向性。
//
// 这里按构建条件自动切换，使用者不需要额外加 tag：
//   CGO_ENABLED=1 → mattn（默认，性能更好）
//   CGO_ENABLED=0 → modernc（纯 Go，开箱即用）

import (
	glebarezsqlite "github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// openSQLiteDriver 用纯 Go 驱动打开数据库
func openSQLiteDriver(dsn string) gorm.Dialector {
	return glebarezsqlite.Open(dsn)
}
