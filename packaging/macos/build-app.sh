#!/usr/bin/env bash
# 产出 macOS .app 包（带定位用途声明）。
#
# 为什么必须有 .app：TCC（系统权限）只认带 bundle 身份且已签名的应用；裸二进制
# 没有身份，webview 的 navigator.geolocation 一律被拒（2026-10-29 实测确认）。
#
# 用法：packaging/macos/build-app.sh [输出路径]      默认 dist/OpenLaw.app（dist/ 已 gitignore）
#       TAGS=desktop,production,devtools BUNDLE_ID=com.github.futuremeng.openlaw.dev \
#         packaging/macos/build-app.sh dist/OpenLaw-Dev.app
#       （devtools 变体带控制台，便于实测。bundle id 必须与正式包区分：实测两个包共用
#        同一 bundle id 时，LaunchServices 会把同一 id 解析到两个路径，TCC 身份随之含糊）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="${1:-$ROOT/dist/OpenLaw.app}"
BIN="$ROOT/dist/openlaw-desktop"
TAGS="${TAGS:-desktop,production}"
BUNDLE_ID="${BUNDLE_ID:-com.github.futuremeng.openlaw}"

echo "==> 构建二进制（标签：${TAGS}，必须含 desktop,production，否则链接的是 stub 绑定）"
# 发布构建经 VERSION 环境变量注入 main.version（release.yml 在 tag 事件下导出）；
# 本地不设 VERSION 时保持树值，不静默丢注入。
if [ -n "${VERSION:-}" ]; then
	echo "==> 版本注入：${VERSION}"
	go build -tags "$TAGS" -ldflags "-X main.version=${VERSION}" -o "$BIN" ./desktop
else
	go build -tags "$TAGS" -o "$BIN" ./desktop
fi

echo "==> 组装 bundle：${OUT}"
rm -rf "$OUT"
mkdir -p "$OUT/Contents/MacOS" "$OUT/Contents/Resources"
cp "$ROOT/packaging/macos/Info.plist" "$OUT/Contents/Info.plist"
plutil -replace CFBundleIdentifier -string "${BUNDLE_ID}" "$OUT/Contents/Info.plist"
printf 'APPL????' > "$OUT/Contents/PkgInfo"
# 此处的文件名必须与 Info.plist 的 CFBundleExecutable 一致（desktop/bundle_test.go 门禁核对）
cp "$BIN" "$OUT/Contents/MacOS/OpenLaw"
chmod +x "$OUT/Contents/MacOS/OpenLaw"

echo "==> ad-hoc 签名（TCC 需要签名身份；无开发者证书，重建后系统可能要求重新授权）"
codesign --force --sign - "${OUT}"

# 注册到 LaunchServices。实测：未注册时 locationd 报
# 「The given bundleId or bundlePath is not a plugin or an app」+「identity unverified」，
# 系统因此不弹定位授权窗（弹窗前提是能把 bundle id 解析成应用记录）。
LSREG="/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
if [ -x "${LSREG}" ]; then
  echo "==> 注册 LaunchServices"
  "${LSREG}" -f "${OUT}"
else
  echo "==> 警告：未找到 lsregister，定位授权弹窗可能不出现（可手动把 .app 放入 /Applications）"
fi

echo "==> 自检"
plutil -lint "$OUT/Contents/Info.plist"
codesign --verify --verbose=2 "$OUT"

echo
echo "已产出：${OUT}"
echo "启动：open \"${OUT}\""
echo "数据目录：.app 位于仓库 dist/ 下时自动使用仓库 data/（见 desktop/dataresolve.go）；"
echo "         放到别处时用环境变量 OPENLAW_DATA=/path/to/data，或："
echo "         open \"${OUT}\" --args -data /path/to/data"
