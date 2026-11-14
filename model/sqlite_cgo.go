//go:build cgo

package model

// 默认构建（CGO 可用）：使用 mattn/go-sqlite3，性能更好，
// 支持完整的 SQLite 特性。需要本机装有 gcc。
//
// 切换到纯 Go 驱动不需要加任何 tag，直接控制 CGO 即可：
//   CGO_ENABLED=1 → 本文件（mattn）
//   CGO_ENABLED=0 → sqlite_purego.go（glebarez，纯 Go）
//
// 注意：`CGO_ENABLED=0` 时若仍用 mattn 驱动，编译期完全不报错，
// 只有真的启动、Open 数据库时才炸：
//   "go-sqlite3 requires cgo to work. This is a stub"
// 这个桩实现藏在 mattn 里，正是本文件要绕开的那个坑。

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// openSQLiteDriver 用 cgo 版驱动打开数据库
func openSQLiteDriver(dsn string) gorm.Dialector {
	return sqlite.Open(dsn)
}
