// location.go 定位授权状态的人话映射（与 cgo 实现解耦，任何平台可测）。
//
// 诚实纪律：授权被拒、受限、未弹窗、平台不支持、缺 bundle 声明——每一种都要说清
// 楚「为什么不行」和「现在能用什么」，不得含糊带过，更不得假装定位成功。
package main

import "time"

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

// RequestLocationPermission 用户显式点击「获取我的位置」时申请系统定位授权。
// 只在 macOS 且已打包（有 bundle 身份 + plist 定位用途声明）时才真正申请；
// 不读取位置、不联网——坐标仍由 webview 取出后交 Go 离线判定。
//
// 前提不满足时直接如实说明，不去调用申请（避免无谓的系统噪声）；系统弹窗是异步的，
// 状态仍为「未决定」时最多轮询 10 秒等待用户应答，超时后如实报告，绝不谎报已授权。
func (a *App) RequestLocationPermission() LocationPermission {
	st, hasKey, bundled := locationStatus()
	if st == locStatusUnsupported || !hasKey || !bundled {
		return locationPermissionView(st, hasKey, bundled)
	}
	if st == locNotDetermined {
		st, _, _ = locationRequest()
		for i := 0; i < 40 && st == locNotDetermined; i++ {
			time.Sleep(250 * time.Millisecond)
			st, _, _ = locationStatus()
		}
	}
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
		return LocationPermission{"authorized", "已获授权，正在读取位置（仅本机离线判定，不联网）。"}
	case locDenied:
		return LocationPermission{"denied", "系统已拒绝本应用定位。可在「系统设置 → 隐私与安全性 → 定位服务」改为允许后重试；在此之前可用左侧下拉选择地区，或点「按选定地点试判定」。"}
	case locRestricted:
		return LocationPermission{"restricted", "本机策略限制了定位权限（受限），无法使用系统定位。请用左侧下拉选择地区，或点「按选定地点试判定」。"}
	default:
		return LocationPermission{"not_determined", "未取得定位授权：系统弹窗未出现，或未在弹窗中应答。请在弹窗中选「允许」后重试；也可先用左侧下拉选择地区，或点「按选定地点试判定」。"}
	}
}
