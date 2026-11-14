package common

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-contrib/static"
)

// Credit: https://github.com/gin-contrib/static/issues/19

type embedFileSystem struct {
	http.FileSystem
}

// Exists 判断请求路径在嵌入文件系统里是否真实存在。
//
// gin-contrib/static 传进来的 path 带前导斜杠（例如 "/_nuxt/entry.css"），
// 而 io/fs 的路径规则要求「无前导斜杠、相对路径」，
// 直接 e.Open("/_nuxt/entry.css") 必定返回 ErrInvalid，导致所有静态资源
// 都被判定为不存在、全部落到前端 HTML 回落里。
// 这里先做路径规范化，再交给 Open。
func (e embedFileSystem) Exists(prefix string, p string) bool {
	clean := strings.TrimPrefix(path.Clean("/"+p), "/")
	if clean == "" || clean == "." {
		return false
	}
	f, err := e.Open(clean)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false
	}
	// 目录不当作静态资源，交给后续的 HTML 回落处理
	return !info.IsDir()
}

func EmbedFolder(fsEmbed embed.FS, targetPath string) static.ServeFileSystem {
	efs, err := fs.Sub(fsEmbed, targetPath)
	if err != nil {
		panic(err)
	}
	return embedFileSystem{
		FileSystem: http.FS(efs),
	}
}
