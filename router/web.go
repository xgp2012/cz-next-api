package router

import (
	"embed"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/controller"
	"github.com/songquanpeng/one-api/middleware"
)

// SetWebRouter 挂载前端静态资源。
//
// 前端现在是 Nuxt 4 的「SSR + 静态预渲染」混合产物（web/build/<theme>/），
// 与旧版 React SPA 的关键差异及处理方式：
//
//  1. Nuxt 会为每个预渲染路由生成 <route>/index.html（如 /about/index.html）。
//     gin-contrib/static 的目录索引行为不可靠，所以先尝试读取静态文件，
//     命中则直接返回；未命中才进入 SPA/SSR 回落逻辑。
//  2. 路由的 HTML 需要正确的 Content-Type，并且不应被 Cache() 中间件的
//     「max-age=604800」误伤——HTML 外壳必须可随时更新，
//     否则用户会长期停留在旧版本的前端壳上。
//  3. 带 hash 的 /_nuxt/* 资源可以长期缓存，由 Cache() 统一设置。
//
// 回落顺序（NoRoute）：
//
//	API/Relay 路径 -> 404 JSON
//	已知页面 HTML   -> 该页面的预渲染 HTML
//	其余前端路径    -> 首页 index.html（由前端路由接管，即 SPA 行为）
func SetWebRouter(router *gin.Engine, buildFS embed.FS) {
	webRoot := fmt.Sprintf("web/build/%s", config.Theme)

	// 主题目录缺失时不静默退化成「空白前端页」，而是回落到 default，
	// 并把原因打进日志——否则管理员配置一个没构建过的主题，
	// 服务看起来正常启动，访问却是一个空白页，排查成本很高。
	if !themeEmbedded(buildFS, webRoot) {
		fallback := "web/build/default"
		if config.Theme == "default" || !themeEmbedded(buildFS, fallback) {
			// 必须硬退出：panic 会被外层 gin.Recovery() 捕获，
			// 进程会继续带着「没有前端路由的 gin 实例」运行，
			// 表现为服务看似正常启动、页面却是空白。
			logger.FatalLog(fmt.Sprintf(
				"前端产物不存在：web/build/%s，且无可用回落。请先执行 web/build-nuxt.sh 构建前端，"+
					"或检查 THEME 环境变量是否正确", config.Theme))
			return
		}
		logger.SysError(fmt.Sprintf(
			"前端产物不存在：web/build/%s，已回落到 web/build/default。"+
				"请先执行 web/build-nuxt.sh 构建对应主题", config.Theme))
		webRoot = fallback
	}

	indexPageData, _ := buildFS.ReadFile(path.Join(webRoot, "index.html"))

	// 预渲染页面的 HTML 缓存在内存里：数量有限（十几页），体积小，避免每次请求都读 embed FS
	prerendered := make(map[string][]byte)
	collectPrerenderedPages(buildFS, webRoot, prerendered)

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache())
	router.Use(static.Serve("/", common.EmbedFolder(buildFS, webRoot)))

	router.NoRoute(func(c *gin.Context) {
		requestPath := c.Request.URL.Path
		if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") {
			controller.RelayNotFound(c)
			return
		}

		// 1) 预渲染页面：/about -> /about/index.html
		if page, ok := prerendered[normalizeRequestPath(requestPath)]; ok {
			writeHTML(c, page)
			return
		}

		// 2) 其余路径交给前端路由（未预渲染的管理页、动态路由等）
		writeHTML(c, indexPageData)
	})
}

// themeEmbedded 判断某个主题的前端产物是否已嵌入当前二进制。
func themeEmbedded(buildFS embed.FS, webRoot string) bool {
	_, err := buildFS.ReadFile(path.Join(webRoot, "index.html"))
	return err == nil
}

// writeHTML 统一输出 HTML：禁用强缓存，保证前端壳能随版本更新。
func writeHTML(c *gin.Context, content []byte) {
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "text/html; charset=utf-8", content)
}

// normalizeRequestPath 把请求路径归一化为不带前缀/后缀的形式，
// 使 "/about"、"/about/"、"/about/index.html" 都能命中同一份预渲染产物。
func normalizeRequestPath(p string) string {
	p = strings.TrimSuffix(p, "/index.html")
	p = strings.Trim(p, "/")
	if p == "" {
		return "/"
	}
	return "/" + p
}

// collectPrerenderedPages 扫描 embed FS 中所有 <dir>/index.html。
// 之所以不写死路由表，是为了让前端新增页面后无需同步修改 Go 代码。
func collectPrerenderedPages(buildFS embed.FS, webRoot string, out map[string][]byte) {
	entries, err := buildFS.ReadDir(webRoot)
	if err != nil {
		return
	}
	// 根目录的 index.html 同时作为 SPA 回落页使用，这里一并登记
	for _, entry := range entries {
		if entry.IsDir() {
			collectPageRecursively(buildFS, path.Join(webRoot, entry.Name()), "/"+entry.Name(), out)
		}
	}
}

func collectPageRecursively(buildFS embed.FS, dir string, urlPath string, out map[string][]byte) {
	indexPath := path.Join(dir, "index.html")
	if data, err := buildFS.ReadFile(indexPath); err == nil {
		out[normalizeRequestPath(urlPath)] = data
	}
	children, err := buildFS.ReadDir(dir)
	if err != nil {
		return
	}
	for _, child := range children {
		if child.IsDir() {
			collectPageRecursively(buildFS, path.Join(dir, child.Name()), urlPath+"/"+child.Name(), out)
		}
	}
}
