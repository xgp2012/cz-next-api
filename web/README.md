# One API 的前端界面

前端已重构为 **Nuxt 4** 实现，产物为纯静态文件，由 Go 二进制通过 `go:embed` 内嵌分发。

## 目录结构

```
web/
├── nuxt/            # Nuxt 4 前端源码（当前唯一在维护的主题）
├── build/           # 构建产物，被 main.go 的 go:embed 内嵌（不纳入版本控制）
├── build-nuxt.sh    # 构建脚本：nuxt/ -> build/<theme>/
├── THEMES           # 参与构建的主题列表
└── README.md
```

## 构建

```sh
# 在仓库根目录执行
sh web/build-nuxt.sh              # 构建 THEMES 中列出的全部主题
sh web/build-nuxt.sh default      # 只构建 default
SKIP_INSTALL=1 sh web/build-nuxt.sh   # 依赖已装好时跳过 npm install
```

脚本会执行 `nuxt generate`（Nitro `preset: static`），并把 `.output/public` 的内容
复制到 `web/build/<theme>/`。之后 `go build` 即可把前端打进单二进制：

```sh
go build -o one-api .
```

## 开发

需要同时跑后端与前端开发服务器：

```sh
# 终端 1：后端
go run .            # 监听 :3000

# 终端 2：前端（Vite 会把 /api /v1 /dashboard 代理到 :3000）
cd web/nuxt
npm install
npm run dev         # 监听 :3001，浏览器访问这个端口
```

## 设计要点

### 渲染策略：SSR + 静态预渲染混合

- 公开页（`/`、`/about`、`/login`、`/register`、`/reset`）在构建期被预渲染成完整
  HTML，首屏无白屏、内容可被爬虫读取。
- 管理页（`/channel`、`/token`、`/log` 等）不预渲染：它们依赖登录态，构建机上
  既没有后端也没有 cookie，预渲染没有意义。访问这些路径时 Go 侧下发同一个
  `index.html` 外壳，浏览器挂载后由 vue-router 接管。
- 因此**所有面向用户的数据请求都必须放在 `onMounted` 中**，不能进 SSR。
  `app/utils/http.ts` 在服务端会直接返回占位结果，避免构建期发起无意义的请求。

### 后端契约

- 控制面 `/api/*` 绝大多数业务错误都返回 **HTTP 200 + `{success:false, message}`**，
  只有鉴权失败、限流、panic 才会出现 401/403/429/500。
  判断成功与否一律以 `success` 字段为准，不要依赖 HTTP 状态码。
- 登录态是 cookie session（`gin-contrib/sessions`），前端不发送 `Authorization` 头。
- 列表接口是**游标式分页**：`?p=0` 返回前 10 条，`?p=1` 返回第 11–20 条；
  返回条数少于 10 即表示已到末页（`ITEMS_PER_PAGE = 10`）。

### 与原实现的差异

- UI 库从 Semantic UI React 换成 **@nuxt/ui**（Tailwind CSS 4）。
- 图表不再依赖 recharts：控制台的 7 点折线用内联 SVG 手绘，零额外依赖。
- i18n 不再依赖 `@nuxtjs/i18n`：历史语言包用的是 i18next 的 `{{var}}` 插值语法，
  而 vue-i18n 的编译器把 `{{...}}` 当作嵌套占位符直接报错。现在由
  `app/utils/i18n.ts` 直接加载 JSON 并做运行期字符串插值，同时兼容
  `{{var}}` 与 `{var}` 两种写法。
- 构建期不访问外网：已关闭 `@nuxt/fonts`，图标集合（heroicons / lucide /
  simple-icons）通过 `@iconify-json/*` 本地安装并在构建时内联，
  因此离线与内网 CI 都能正常构建（详见下方 `nuxt.config.ts` 中的注释）。
## 新增主题

1. 在 `web` 下新建主题目录（或复用 `nuxt/`，通过 `nuxt.config.ts` 区分品牌样式）。
2. 确认 `web/build-nuxt.sh` 能产出 `web/build/<主题名>/index.html`。
3. 把主题名加入 `web/THEMES`。
4. 把主题名加入 `common/config/config.go` 的 `ValidThemes`，
   并同步 `web/nuxt/app/components/SettingOther.vue` 的 `themeOptions`——
   三处必须一致，否则管理员会在设置页切到没有构建产物的主题。

> 说明：`router/web.go` 在主题产物缺失时会回落到 `default` 并打印警告；
> 若连 `default` 都不存在，进程会直接退出而不是提供一个空白页面。
