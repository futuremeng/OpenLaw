package main

import (
	"strings"
	"testing"
	"time"
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
			if tc.wantSt == "authorized" {
				// 诚实纪律：坐标来自系统定位服务（macOS 可能经由其 Wi-Fi 定位数据库），
				// 因此不得声称「不联网」，但必须点明坐标来源与「判定在本机离线完成」。
				if !strings.Contains(got.Msg, "系统定位服务") || !strings.Contains(got.Msg, "离线") {
					t.Errorf("已授权说明必须点明坐标来源与离线判定: %q", got.Msg)
				}
				if strings.Contains(got.Msg, "不联网") {
					t.Errorf("已授权说明不得声称「不联网」: %q", got.Msg)
				}
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

// TestRequestLocationPermissionNonBlocking 申请必须立即返回：绑定调用这条路径会占住
// 主线程，而 CLLocationManager 只能在主队列工作——在 Go 侧阻塞等待弹窗，主队列就排不到，
// 弹窗永远不会出现（2026-10-29 实测踩过）。所以契约是「发起 + 立刻回状态」。
func TestRequestLocationPermissionNonBlocking(t *testing.T) {
	start := time.Now()
	got := (&App{}).RequestLocationPermission()
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("不得阻塞等待弹窗（应由前端轮询状态），实际耗时 %v", d)
	}
	if got.Status == "" || got.Msg == "" {
		t.Errorf("必须返回状态与说明: %+v", got)
	}
	if got.Status != "authorized" && !strings.Contains(got.Msg, "试判定") {
		t.Errorf("未取得授权时必须给出仍然可用的出路: %q", got.Msg)
	}
}

// TestLocationPermissionStatusReadOnly 轮询接口只读：不得改变授权状态。
func TestLocationPermissionStatusReadOnly(t *testing.T) {
	a := &App{}
	first := a.LocationPermissionStatus()
	switch first.Status {
	case "unsupported", "unbundled", "authorized", "denied", "restricted", "not_determined":
	default:
		t.Errorf("未知授权状态 %q", first.Status)
	}
	if first.Msg == "" {
		t.Error("必须给出说明")
	}
	for i := 0; i < 3; i++ {
		again := a.LocationPermissionStatus()
		if again.Status != first.Status {
			t.Errorf("只读轮询不得改变状态：%q → %q", first.Status, again.Status)
		}
	}
}

// TestLocationFixView 单次定位的每种状态都要给出原因与出路；只有 ok 才带坐标，
// 且必须如实报精度（精度未知就说未知，不得含糊）。
func TestLocationFixView(t *testing.T) {
	cases := []struct {
		state int
		want  string
	}{
		{fixUnsupported, "unsupported"},
		{fixUnauthorized, "unauthorized"},
		{fixWorking, "working"},
		{fixOK, "ok"},
		{fixFailed, "failed"},
		{fixNotStarted, "not_started"},
	}
	for _, c := range cases {
		got := locationFixView(c.state, 30.25, 120.16, 65, 0)
		if got.State != c.want {
			t.Errorf("状态 %d 应映射为 %q，实际 %q", c.state, c.want, got.State)
		}
		if got.Msg == "" {
			t.Errorf("状态 %q 必须给出说明", c.want)
		}
		if c.want != "ok" && (got.Lat != 0 || got.Lon != 0) {
			t.Errorf("非 ok 状态不得带坐标: %+v", got)
		}
		if c.want != "ok" && c.want != "working" && !strings.Contains(got.Msg, "试判定") {
			t.Errorf("状态 %q 必须给出仍然可用的出路: %q", c.want, got.Msg)
		}
	}
	ok := locationFixView(fixOK, 30.25, 120.16, 65, 0)
	if ok.Lat != 30.25 || ok.Lon != 120.16 {
		t.Errorf("ok 必须带坐标: %+v", ok)
	}
	if !strings.Contains(ok.Msg, "精度") {
		t.Errorf("ok 必须如实给出精度: %q", ok.Msg)
	}
	if !strings.Contains(locationFixView(fixOK, 1, 2, -1, 0).Msg, "精度未知") {
		t.Error("精度未知时必须如实说明")
	}
	if !strings.Contains(locationFixView(fixFailed, 0, 0, -1, kCLErrorDenied).Msg, "拒绝") {
		t.Error("被系统拒绝时应点明可能原因")
	}
}

// TestStartLocationFixNonBlocking 发起定位同样不得阻塞：绑定调用占住主线程时
// CoreLocation 回调排不到，坐标永远回不来。
func TestStartLocationFixNonBlocking(t *testing.T) {
	start := time.Now()
	got := (&App{}).StartLocationFix()
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("发起定位不得阻塞（应由前端轮询结果），实际耗时 %v", d)
	}
	switch got.State {
	case "unsupported", "unauthorized", "working", "ok", "failed", "not_started":
	default:
		t.Errorf("未知定位状态 %q（msg=%q）", got.State, got.Msg)
	}
	if got.Msg == "" {
		t.Error("必须给出说明")
	}
}
