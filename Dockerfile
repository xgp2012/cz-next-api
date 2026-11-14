# syntax=docker/dockerfile:1

# ============================================================================
# One API（cz-next-api）镜像构建
#
# 关键点：web/build 是被 .gitignore 忽略的构建产物（只提交了 web/build/.gitkeep），
# 而 main.go 里有 `//go:embed all:web/build/*`。这意味着**必须**先在镜像内
# 生成前端产物，再执行 go build，否则干净 checkout 下 go:embed 会因为
# 没有匹配文件而直接编译失败。所以下面严格保持「先建前端、再建后端」的顺序。
#
# 不要试图把 web/build 提交进仓库来绕过这一步：那会让每次前端改动都产生
# 上百个文件的 diff（文件名是哈希），代码评审基本失效。
# ============================================================================

# ---------------------------------------------------------------------------
# Stage 1: 构建 Nuxt 前端 -> /web/build/<theme>
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM node:22 AS builder

# 默认构建 default 主题；如需其他主题，传 --build-arg BUILD_THEME=xxx
ARG BUILD_THEME=default

WORKDIR /web

# 先只拷贝依赖清单，让 npm 层缓存能命中——
# 否则每次改一个 .vue 文件都要重装全部依赖，构建时间会翻几倍。
COPY ./web/nuxt/package.json ./web/nuxt/package-lock.json ./nuxt/
RUN npm ci --prefix /web/nuxt --no-audit --no-fund

# 再拷贝源码与构建脚本
COPY ./VERSION .
COPY ./web/THEMES ./web/build-nuxt.sh ./
COPY ./web/nuxt ./nuxt

# build-nuxt.sh 内部会再判断一次 node_modules（这里已装好，用 SKIP_INSTALL=1 跳过）
RUN REACT_APP_VERSION="$(cat ./VERSION)" \
    BUILD_THEME="$BUILD_THEME" \
    SKIP_INSTALL=1 \
    sh ./build-nuxt.sh "$BUILD_THEME"

# 断言产物存在：make:embed 需要它，早失败比在 go build 阶段报错更好定位
RUN test -f "/web/build/$BUILD_THEME/index.html" || \
    (echo "!! frontend build produced no index.html" >&2; exit 1)

# ---------------------------------------------------------------------------
# Stage 2: 编译 Go 后端（把前端产物 embed 进单二进制）
# ---------------------------------------------------------------------------
FROM golang:alpine AS builder2

# sqlite 驱动需要 cgo，因此这里保留 gcc + sqlite-dev（CGO_ENABLED=1）
RUN apk add --no-cache \
    gcc \
    musl-dev \
    sqlite-dev \
    build-base

ENV GO111MODULE=on \
    CGO_ENABLED=1 \
    GOOS=linux

WORKDIR /build

# 依赖层单独缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 必须先放入前端产物，go:embed 在编译期求值
COPY --from=builder /web/build ./web/build

RUN go build -trimpath \
      -ldflags "-s -w -X 'github.com/songquanpeng/one-api/common.Version=$(cat VERSION)' -linkmode external -extldflags '-static'" \
      -o one-api

# ---------------------------------------------------------------------------
# Stage 3: 运行镜像
# ---------------------------------------------------------------------------
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder2 /build/one-api /

EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/one-api"]
