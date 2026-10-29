#!/usr/bin/env bash
# 产出 macOS .app 包（带定位用途声明）。
#
# 为什么必须有 .app：TCC（系统权限）只认带 bundle 身份且已签名的应用；裸二进制
# 没有身份，webview 的 navigator.geolocation 一律被拒（2026-10-29 实测确认）。
#
# 用法：packaging/macos/build-app.sh [输出路径]      默认 dist/OpenLaw.app（dist/ 已 gitignore）
#       TAGS=desktop,production,devtools packaging/macos/build-app.sh dist/OpenLaw-Dev.app
#       （devtools 变体带控制台，便于实测；两个包共用同一 bundle id，授权互通）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="${1:-$ROOT/dist/OpenLaw.app}"
BIN="$ROOT/dist/openlaw-desktop"
TAGS="${TAGS:-desktop,production}"

echo "==> 构建二进制（标签：${TAGS}，必须含 desktop,production，否则链接的是 stub 绑定）"
go build -tags "$TAGS" -o "$BIN" ./desktop

echo "==> 组装 bundle：${OUT}"
rm -rf "$OUT"
mkdir -p "$OUT/Contents/MacOS" "$OUT/Contents/Resources"
cp "$ROOT/packaging/macos/Info.plist" "$OUT/Contents/Info.plist"
printf 'APPL????' > "$OUT/Contents/PkgInfo"
# 此处的文件名必须与 Info.plist 的 CFBundleExecutable 一致（desktop/bundle_test.go 门禁核对）
cp "$BIN" "$OUT/Contents/MacOS/OpenLaw"
chmod +x "$OUT/Contents/MacOS/OpenLaw"

echo "==> ad-hoc 签名（TCC 需要签名身份；无开发者证书，重建后系统可能要求重新授权）"
codesign --force --sign - "$OUT"

echo "==> 自检"
plutil -lint "$OUT/Contents/Info.plist"
codesign --verify --verbose=2 "$OUT"

echo
echo "已产出：${OUT}"
echo "启动：open \"${OUT}\""
echo "语料目录：.app 位于仓库 dist/ 下时自动使用仓库 data/（见 desktop/dataresolve.go）；"
echo "         放到别处时用环境变量 OPENLAW_DATA=/path/to/data，或："
echo "         open \"${OUT}\" --args -data /path/to/data"
