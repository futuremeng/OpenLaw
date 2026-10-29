//go:build darwin

// location_darwin.go macOS 定位授权查询/申请的 cgo 绑定（实现在 location_darwin.m）。
// 仅桌面壳需要 cgo；OpenLaw 核心库保持纯 Go。
package main

/*
#cgo darwin LDFLAGS: -framework CoreLocation -framework Foundation
// 两个框架都必需：CoreLocation 提供 CLLocationManager/授权状态，Foundation 提供
// NSBundle（核对 plist 定位用途声明与 bundle 形态）——去掉 Foundation 时，不带
// desktop 标签的 `go build ./...` 会因 _OBJC_CLASS_$_NSBundle 未定义而链接失败。
// 二者都会带入 -lobjc，与 wails 已有的 -lobjc 重复，于是链接期出现无害提示
// 「ld: warning: ignoring duplicate libraries: '-lobjc'」（退出码 0）。压制它需要
// -Wl,-no_warn_duplicate_libraries，而 Go 的 cgo 白名单拒绝 -Wl 开头的选项
// （要 CGO_LDFLAGS_ALLOW），与「构建不依赖外部环境变量」的纪律冲突，故如实保留。
int OpenLawLocationStatus(void);
int OpenLawRequestLocationAuthorization(void);
int OpenLawHasLocationUsageKey(void);
int OpenLawIsBundledApp(void);
int OpenLawStartLocationFix(void);
int OpenLawLocationFixState(void);
double OpenLawLocationLatitude(void);
double OpenLawLocationLongitude(void);
double OpenLawLocationAccuracy(void);
int OpenLawLocationFixError(void);
*/
import "C"

// locationStatus 读取当前授权状态与环境前提（plist 定位用途声明、是否 bundle 形态）。
func locationStatus() (status int, hasUsageKey, bundled bool) {
	return int(C.OpenLawLocationStatus()),
		C.OpenLawHasLocationUsageKey() == 1,
		C.OpenLawIsBundledApp() == 1
}

// locationRequest 向系统发起定位授权申请（仅在用户显式触发时调用）。
func locationRequest() (status int, hasUsageKey, bundled bool) {
	return int(C.OpenLawRequestLocationAuthorization()),
		C.OpenLawHasLocationUsageKey() == 1,
		C.OpenLawIsBundledApp() == 1
}

// locationFixStart 发起一次定位（异步），返回发起当下的状态。
func locationFixStart() int { return int(C.OpenLawStartLocationFix()) }

// locationFix 只读最近一次定位的状态与缓存坐标。
func locationFix() (state int, lat, lon, acc float64, cerr int) {
	return int(C.OpenLawLocationFixState()),
		float64(C.OpenLawLocationLatitude()),
		float64(C.OpenLawLocationLongitude()),
		float64(C.OpenLawLocationAccuracy()),
		int(C.OpenLawLocationFixError())
}
