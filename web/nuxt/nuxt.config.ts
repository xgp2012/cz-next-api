// See https://nuxt.com/docs/api/configuration/nuxt-config
//
// 渲染策略：SSR + 静态预渲染混合。
//
// - 公开页（/ /about /login /register /reset）在 `nuxt generate` 时被预渲染成
//   完整 HTML，保留首屏体验，也让这些页面在爬虫下可直接读取内容。
// - 管理页（/channel /token /log ...）不在预渲染列表内：Go 侧的 SPA 回落
//   会下发同一个 index.html 外壳，浏览器挂载后由 vue-router 接管。
// - nitro.preset = 'static'：产物只有 .output/public 一个目录，
//   不含任何 Node 运行时，可以直接被 main.go 的 go:embed 打进单二进制。

/** 构建期版本号（等价于旧的 REACT_APP_VERSION），由 build-nuxt.sh 注入 */
const appVersion = process.env.REACT_APP_VERSION || ''

export default defineNuxtConfig({
  compatibilityDate: '2024-11-01',
  devtools: { enabled: false },

  modules: ['@nuxt/ui'],

  // 自动导入：utils / composables / constants 下的辅助函数在组件内无需显式 import
  imports: {
    dirs: ['utils', 'composables', 'constants'],
  },

  ssr: true,

  css: ['~/assets/css/main.css'],

  ui: {
    // 关闭 @nuxt/ui 自带的 @nuxt/fonts。
    // 该模块会在构建期访问 fonts.bunny.net / fonts.googleapis.com 拉取字体：
    // 一是让构建依赖外网（离线或内网 CI 会卡住甚至失败），
    // 二是把字体产物写进二进制、增大体积。
    // 改用系统字体栈（见 assets/css/main.css），零网络请求、零额外体积。
    // 注意：该开关必须放在 ui 下——@nuxt/ui 的 moduleDependencies
    // 读取的是 nuxt.options.ui.fonts，写在顶层不会生效。
    fonts: false,
    theme: {
      colors: ['primary', 'secondary', 'success', 'info', 'warning', 'error', 'neutral'],
    },
  },

  app: {
    head: {
      htmlAttrs: { lang: 'zh-CN' },
      title: 'One API',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'theme-color', content: '#ffffff' },
        {
          name: 'description',
          content:
            'OpenAI 接口聚合管理，支持多种渠道包括 Azure，可用于二次分发管理 key，仅单可执行文件，已打包好 Docker 镜像，一键部署，开箱即用',
        },
      ],
      link: [{ rel: 'icon', href: '/logo.png' }],
    },
  },

  // 构建期版本号（等价于原来的 REACT_APP_VERSION），由 build-nuxt.sh 注入
  runtimeConfig: {
    public: {
      appVersion,
    },
  },

  nitro: {
    // 纯静态产物：只生成 .output/public，不需要 Node 运行时
    preset: 'static',
    prerender: {
      crawlLinks: false,
      failOnError: false,
      // 只预渲染公开页；管理页依赖登录态，预渲染没有意义
      routes: ['/', '/about', '/login', '/register', '/reset'],
    },
  },

  // 图标：只用本地安装的 @iconify-json/* 集合，禁止运行期访问 api.iconify.design。
  // 默认行为是「本地找不到就去外网下载」，这在离线部署时会退化成图标空白
  // （构建日志里表现为 “loading icon ... timed out”）。
  icon: {
    mode: 'css',
    serverBundle: {
      collections: ['heroicons', 'lucide', 'simple-icons'],
    },
    clientBundle: {
      // 把用到的图标内联进客户端产物，避免任何运行期请求
      scan: true,
      sizeLimitKb: 512,
    },
  },

  // 开发期把后端接口代理到本地 Go 服务（等价于原来 CRA 的 package.json "proxy"）
  vite: {
    server: {
      proxy: {
        '/api': { target: 'http://localhost:3000', changeOrigin: true },
        '/v1': { target: 'http://localhost:3000', changeOrigin: true },
        '/dashboard': { target: 'http://localhost:3000', changeOrigin: true },
      },
    },
  },

  typescript: {
    strict: false,
    typeCheck: false,
  },
})
