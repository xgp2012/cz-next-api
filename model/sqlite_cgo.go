//go:build cgo

package model

// 默认构建（CGO 可用）：使用 mattn/go-sqlite3，性能更好，
// 支持完整的 SQLite 特性。需要本机装有 gcc。
//
// 若机器上没有 gcc（`CGO_ENABLED=0`），请改用 `-tags sqlite_purego`
// 构建，见 sqlite_purego.go。不加任何 tag 直接构建会在运行时报
// "go-sqlite3 requires cgo to work. This is a stub" —— 那个错误来自
// mattn 驱动在 CGO 关闭时的桩实现，编译期看不出来，只有真的启动才炸。

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// openSQLiteDriver 用 cgo 版驱动打开数据库
func openSQLiteDriver(dsn string) gorm.Dialector {
	return sqlite.Open(dsn)
}
