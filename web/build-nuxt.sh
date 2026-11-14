#!/bin/sh
#
# 构建 Nuxt 前端并把静态产物放到 web/build/<theme>，供 main.go 的 go:embed 内嵌。
#
# 用法（在 one-api/web 目录下执行）：
#   ./build-nuxt.sh              # 构建 THEMES 中列出的全部主题
#   ./build-nuxt.sh default      # 只构建指定主题
#   SKIP_INSTALL=1 ./build-nuxt.sh   # 跳过 npm install（CI 中已缓存依赖时）
#
# 与旧 build.sh 的区别：
#   - 旧脚本遍历 THEMES 里的三套 React 主题；Nuxt 侧目前只有一套，但保留
#     「按主题构建」的结构，后续要加主题只需在 nuxt 目录下再加项目。
#   - 旧脚本用 `mv build ../build/$theme` 依赖 react-scripts 的输出目录名；
#     Nuxt 的静态产物固定在 .output/public，这里显式复制，避免 mv 的跨盘/跨目录问题。
#   - Nuxt 构建期间不需要访问外网（已关闭 @nuxt/fonts 与运行期图标下载），
#     因此可以在内网或离线 CI 中直接跑。

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
VERSION="$(cat "$ROOT/../VERSION" 2>/dev/null || echo 'v0.0.0')"

# 确定要构建的主题列表
if [ $# -gt 0 ]; then
  THEMES="$*"
else
  THEMES="$(cat "$ROOT/THEMES" 2>/dev/null || echo default)"
fi

echo "==> Building Nuxt frontend: themes='$THEMES' version=$VERSION"

for theme in $THEMES; do
  echo ""
  echo "==> Building theme: $theme"

  cd "$ROOT"

  if [ ! -d "$ROOT/nuxt/node_modules" ] || [ "$SKIP_INSTALL" != "1" ]; then
    echo "==> Installing dependencies"
    npm install --prefix "$ROOT/nuxt" --no-audit --no-fund
  fi

  echo "==> Generating static site (nitro preset=static)"
  REACT_APP_VERSION="$VERSION" npx --prefix "$ROOT/nuxt" nuxt generate --dir "$ROOT/nuxt"

  if [ ! -d "$ROOT/nuxt/.output/public" ]; then
    echo "!! build failed: $ROOT/nuxt/.output/public not found" >&2
    exit 1
  fi

  echo "==> Publishing to web/build/$theme"
  rm -rf "$ROOT/build/$theme"
  mkdir -p "$ROOT/build/$theme"
  # 只复制 public 内容：.output 里除了 public 只剩 nitro.json，无需进二进制
  cp -R "$ROOT/nuxt/.output/public/." "$ROOT/build/$theme/"

  echo "==> Done: $ROOT/build/$theme"
  find "$ROOT/build/$theme" -maxdepth 1 -mindepth 1 | sort
done

echo ""
echo "==> All themes built."
