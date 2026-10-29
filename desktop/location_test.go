package main

import (
	"strings"
	"testing"
)

// TestLocationPermissionView 每种授权状态都必须给出「原因 + 出路」：
// 未授权时不得只说「失败」，必须指向仍然可用的两条路径（手动选地区 / 试判定）。
func TestLocationPermissionView(t *testing.T) {
	cases := []struct {
		name      string
		st        int
		hasKey    bool
		bundled   bool
		wantSt    string
		wantRoute bool
	}{
		{name: "非 mac 平台", st: locStatusUnsupported, wantSt: "unsupported", wantRoute: true},
		{name: "裸二进制必须说明缺什么", st: locNotDetermined, hasKey: false, bundled: false, wantSt: "unbundled", wantRoute: true},
		{name: "有 bundle 但缺定位声明", st: locNotDetermined, hasKey: false, bundled: true, wantSt: "unbundled", wantRoute: true},
		{name: "已拒绝", st: locDenied, hasKey: true, bundled: true, wantSt: "denied", wantRoute: true},
		{name: "受限", st: locRestricted, hasKey: true, bundled: true, wantSt: "restricted", wantRoute: true},
		{name: "弹窗未应答", st: locNotDetermined, hasKey: true, bundled: true, wantSt: "not_determined", wantRoute: true},
		{name: "使用期间授权", st: locAuthorizedWhenInUse, hasKey: true, bundled: true, wantSt: "authorized"},
		{name: "始终授权", st: locAuthorizedAlways, hasKey: true, bundled: true, wantSt: "authorized"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := locationPermissionView(tc.st, tc.hasKey, tc.bundled)
			if got.Status != tc.wantSt {
				t.Errorf("status 应为 %q，实际 %q", tc.wantSt, got.Status)
			}
			if got.Msg == "" {
				t.Fatal("必须给出界面可读的说明")
			}
			if tc.wantRoute && !strings.Contains(got.Msg, "试判定") {
				t.Errorf("未授权时必须指向仍然可用的出路（试判定/手动选地区）: %q", got.Msg)
			}
			if tc.wantSt == "authorized" && !strings.Contains(got.Msg, "不联网") {
				t.Errorf("已授权说明仍须声明不联网: %q", got.Msg)
			}
			// 诚实纪律：绝不允许把未授权说成成功
			if tc.st != locAuthorizedWhenInUse && tc.st != locAuthorizedAlways && tc.wantSt != "authorized" {
				if strings.Contains(got.Msg, "已获授权") {
					t.Errorf("不得谎报授权状态: %q", got.Msg)
				}
			}
		})
	}
}

// TestRequestLocationPermissionHonest 当前机器上真实调用一次：结果必须落在已知
// 状态集合内，且未授权时给出出路。离线可跑（不联网，不依赖真实位置）。
func TestRequestLocationPermissionHonest(t *testing.T) {
	got := (&App{}).RequestLocationPermission()
	switch got.Status {
	case "unsupported", "unbundled", "authorized", "denied", "restricted", "not_determined":
	default:
		t.Errorf("未知授权状态 %q（msg=%q）", got.Status, got.Msg)
	}
	if got.Msg == "" {
		t.Error("必须给出说明")
	}
	if got.Status != "authorized" && !strings.Contains(got.Msg, "试判定") {
		t.Errorf("未取得授权时必须给出出路: %q", got.Msg)
	}
}
