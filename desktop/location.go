// location.go 定位授权状态的人话映射（与 cgo 实现解耦，任何平台可测）。
//
// 诚实纪律：授权被拒、受限、未弹窗、平台不支持、缺 bundle 声明——每一种都要说清
// 楚「为什么不行」和「现在能用什么」，不得含糊带过，更不得假装定位成功。
package main

import "fmt"

// CoreLocation 的 CLAuthorizationStatus 取值（<CoreLocation/CLAuthorizationStatus.h>）。
const (
	locStatusUnsupported   = -1 // 非 macOS 平台
	locNotDetermined       = 0
	locRestricted          = 1
	locDenied              = 2
	locAuthorizedAlways    = 3
	locAuthorizedWhenInUse = 4
)

// LocationPermission 授权检查结果：status 供前端分支，msg 是给用户看的原因与出路。
type LocationPermission struct {
	Status string `json:"status"`
	Msg    string `json:"msg"`
}

// RequestLocationPermission 发起系统定位授权申请，并立即返回当下状态——**不阻塞等待弹窗**。
//
// 为什么不能阻塞：绑定调用这条路径会占住主线程，而 CLLocationManager 只能在主队列工作；
// 在绑定里 sleep 轮询，主队列就永远排不到，弹窗反而不会出现（实测踩过）。因此发起后
// 立即返回，由前端轮询 LocationPermissionStatus 直到授权落地或明确被拒。
// 只在 macOS 且已打包（bundle 身份 + plist 定位用途声明）时才真正申请；
// 本应用不调用任何在线地理编码服务：坐标由系统定位服务给出（macOS 可能经由 Wi-Fi
// 定位数据库），把坐标落到地区这一步全部在本机离线完成。
func (a *App) RequestLocationPermission() LocationPermission {
	st, hasKey, bundled := locationStatus()
	if st == locStatusUnsupported || !hasKey || !bundled {
		return locationPermissionView(st, hasKey, bundled)
	}
	if st == locNotDetermined {
		st, _, _ = locationRequest()
	}
	return locationPermissionView(st, hasKey, bundled)
}

// LocationPermissionStatus 只读当前授权状态（前端在等待用户应答弹窗期间轮询用）。
func (a *App) LocationPermissionStatus() LocationPermission {
	st, hasKey, bundled := locationStatus()
	return locationPermissionView(st, hasKey, bundled)
}

// locationPermissionView 把授权状态映射为界面可读的结论（原因 + 出路）。
func locationPermissionView(st int, hasUsageKey, bundled bool) LocationPermission {
	if st == locStatusUnsupported {
		return LocationPermission{"unsupported", "当前平台不提供系统定位。请用左侧下拉选择地区，或点「按选定地点试判定」。"}
	}
	if !bundled || !hasUsageKey {
		return LocationPermission{"unbundled", "当前以裸二进制运行，系统不会授予定位（缺少应用包身份与定位用途声明）。请用左侧下拉选择地区，或点「按选定地点试判定」；需要真实定位请按 docs/deploy.md 打包 .app 后运行。"}
	}
	switch st {
	case locAuthorizedWhenInUse, locAuthorizedAlways:
		return LocationPermission{"authorized", "已获授权，正在读取坐标（坐标来自系统定位服务；地区判定在本机离线完成）。"}
	case locDenied:
		return LocationPermission{"denied", "系统已拒绝本应用定位。可在「系统设置 → 隐私与安全性 → 定位服务」改为允许后重试；在此之前可用左侧下拉选择地区，或点「按选定地点试判定」。"}
	case locRestricted:
		return LocationPermission{"restricted", "本机策略限制了定位权限（受限），无法使用系统定位。请用左侧下拉选择地区，或点「按选定地点试判定」。"}
	default:
		return LocationPermission{"not_determined", "未取得定位授权：系统弹窗未出现，或未在弹窗中应答。请在弹窗中选「允许」后重试；也可先用左侧下拉选择地区，或点「按选定地点试判定」。"}
	}
}

// —— 单次定位（坐标取自系统定位服务，再交 Go 离线判定地区）——
//
// 为什么不走 webview：Wails v2.17 的 WKWebView 未实现
// webView:requestGeolocationPermissionForFrame:decisionHandler:，即使系统层面已授权，
// JS 的 navigator.geolocation 仍会被 webview 默认拒绝（2026-10-29 实测踩过：
// CoreLocation 已报 AuthorizedAlways，前端却收到 PERMISSION_DENIED）。
const (
	fixUnsupported  = -2
	fixUnauthorized = -1
	fixNotStarted   = 0
	fixWorking      = 1
	fixOK           = 2
	fixFailed       = 3
	kCLErrorDenied  = 1 // <CoreLocation/CLError.h>
)

// LocationFix 单次定位结果：state 供前端分支，lat/lon/acc 仅在 ok 时有意义。
type LocationFix struct {
	State string  `json:"state"`
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
	Acc   float64 `json:"acc"`
	Msg   string  `json:"msg"`
}

// StartLocationFix 发起一次定位并立即返回当下状态——同样不得阻塞（主队列要留给
// CoreLocation 回调）。前端轮询 LocationFixStatus 直到 ok / failed。
func (a *App) StartLocationFix() LocationFix {
	st, hasKey, bundled := locationStatus()
	if st == locStatusUnsupported || !hasKey || !bundled {
		return locationFixView(fixUnsupported, 0, 0, -1, 0)
	}
	if st != locAuthorizedAlways && st != locAuthorizedWhenInUse {
		return locationFixView(fixUnauthorized, 0, 0, -1, 0)
	}
	return locationFixView(locationFixStart(), 0, 0, -1, 0)
}

// LocationFixStatus 只读最近一次定位的状态与坐标（前端轮询用，不改变状态）。
func (a *App) LocationFixStatus() LocationFix {
	state, lat, lon, acc, cerr := locationFix()
	return locationFixView(state, lat, lon, acc, cerr)
}

// locationFixView 把定位状态映射为界面可读的结论（原因 + 出路）。
func locationFixView(state int, lat, lon, acc float64, cerr int) LocationFix {
	switch state {
	case fixUnsupported:
		return LocationFix{"unsupported", 0, 0, -1, "当前平台不提供系统定位。请用左侧下拉选择地区，或点「按选定地点试判定」。"}
	case fixUnauthorized:
		return LocationFix{"unauthorized", 0, 0, -1, "尚未取得系统定位授权，读不到坐标。请先点「获取我的位置」并在系统弹窗中选「允许」；也可用左侧下拉选择地区，或点「按选定地点试判定」。"}
	case fixWorking:
		return LocationFix{"working", 0, 0, -1, "正在读取坐标…"}
	case fixOK:
		accMsg := "精度未知"
		if acc >= 0 {
			accMsg = fmt.Sprintf("水平精度约 %d 米", int(acc+0.5))
		}
		return LocationFix{"ok", lat, lon, acc, "已取得坐标（" + accMsg + "）。"}
	case fixFailed:
		why := "系统定位服务未能给出坐标"
		if cerr == kCLErrorDenied {
			why = "系统定位服务拒绝了本次读取（可能定位服务已关闭，或权限被撤回）"
		}
		return LocationFix{"failed", 0, 0, -1, why + "。可用左侧下拉选择地区，或点「按选定地点试判定」。"}
	default:
		return LocationFix{"not_started", 0, 0, -1, "尚未发起定位。点「获取我的位置」后再看结果；也可用左侧下拉选择地区，或点「按选定地点试判定」。"}
	}
}
