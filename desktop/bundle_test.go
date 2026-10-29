package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestMacOSBundleContract .app 打包契约（门禁）。
//
// 为什么必须门禁化：TCC 只认「带 bundle 身份 + 已签名 + plist 有定位用途声明」的
// 应用，缺任一项时 navigator.geolocation 只会给出无信息量的 PERMISSION_DENIED；
// 而 CFBundleExecutable 与脚本复制的文件名不一致时 .app 直接起不来——两者都不会在
// 构建期报错，属于只在运行期暴露的漂移。
func TestMacOSBundleContract(t *testing.T) {
	plist, err := os.ReadFile(filepath.Join("..", "packaging", "macos", "Info.plist"))
	if err != nil {
		t.Fatalf("读取 Info.plist 失败: %v", err)
	}
	script, err := os.ReadFile(filepath.Join("..", "packaging", "macos", "build-app.sh"))
	if err != nil {
		t.Fatalf("读取 build-app.sh 失败: %v", err)
	}
	s := string(plist)

	for _, k := range []string{
		"CFBundleName", "CFBundleExecutable", "CFBundleIdentifier",
		"CFBundlePackageType", "LSMinimumSystemVersion",
		"NSLocationWhenInUseUsageDescription", "NSLocationUsageDescription",
	} {
		if !strings.Contains(s, "<key>"+k+"</key>") {
			t.Errorf("Info.plist 缺少 %s（缺定位声明则系统不会弹窗）", k)
		}
	}

	// 用途声明必须如实：点明坐标来自系统定位服务，并声明不上传、不做 IP 定位
	// （诚实展示纪律；「不联网」这种绝对说法对 macOS 定位服务并不准确，不得采用）。
	i := strings.Index(s, "NSLocationWhenInUseUsageDescription")
	if i >= 0 {
		end := i + 600
		if end > len(s) {
			end = len(s)
		}
		seg := s[i:end]
		for _, need := range []string{"系统定位服务", "不上传", "IP 定位"} {
			if !strings.Contains(seg, need) {
				t.Errorf("定位用途声明必须包含 %q（如实说明坐标来源与边界）", need)
			}
		}
		if strings.Contains(seg, "不联网") {
			t.Error("定位用途声明不得声称「不联网」：macOS 定位服务可能经由 Wi-Fi 定位数据库")
		}
	}

	// 可执行名一致性：plist 的 CFBundleExecutable 必须等于脚本复制进 MacOS/ 的文件名。
	exe := plistValue(s, "CFBundleExecutable")
	if exe == "" {
		t.Fatal("CFBundleExecutable 必须非空")
	}
	if !strings.Contains(string(script), "Contents/MacOS/"+exe) {
		t.Errorf("build-app.sh 复制的可执行文件名必须与 CFBundleExecutable（%s）一致", exe)
	}
	// bundle id 必须稳定且非占位（TCC 授权按 bundle id 记忆）。
	if id := plistValue(s, "CFBundleIdentifier"); id == "" || strings.Contains(id, "CHANGE") {
		t.Errorf("CFBundleIdentifier 无效: %q", id)
	}
	// 变体必须能换 bundle id：同一 id 注册在两个路径时，LaunchServices 的身份解析
	// 会二义，TCC 随之含糊（实测踩过），所以脚本必须支持 BUNDLE_ID 覆盖并写回 plist。
	sc := string(script)
	if !strings.Contains(sc, "BUNDLE_ID=") || !strings.Contains(sc, "-replace CFBundleIdentifier") {
		t.Error("build-app.sh 必须支持 BUNDLE_ID 覆盖并写回 Info.plist（正式包与实测包不得共用 bundle id）")
	}
	// 签名：TCC 需要签名身份，脚本必须签名并自检。
	for _, need := range []string{"codesign --force --sign", "plutil -lint", "codesign --verify"} {
		if !strings.Contains(string(script), need) {
			t.Errorf("build-app.sh 必须包含 %s", need)
		}
	}
}

// TestShellVarFollowedByCJK bash 会把紧随 $VAR 的非 ASCII 字节算进变量名
// （实测踩过：`$TAGS；` → "TAGS？: unbound variable"，脚本在 set -u 下直接退出）。
// 中文提示语里插值必须写成 ${VAR}，故把这条规则门禁化。
func TestShellVarFollowedByCJK(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "packaging", "macos", "build-app.sh"))
	if err != nil {
		t.Fatalf("读取 build-app.sh 失败: %v", err)
	}
	re := regexp.MustCompile(`\$[A-Za-z_][A-Za-z0-9_]*[^\x00-\x7F]`)
	for _, m := range re.FindAllString(string(script), -1) {
		t.Errorf("shell 变量插值后紧跟非 ASCII 字符会被并入变量名，请改用 ${} 形式：%q", m)
	}
}

// plistValue 取出 <key>k</key> 之后第一个 <string> 的内容（本仓库 plist 为扁平结构）。
func plistValue(s, k string) string {
	i := strings.Index(s, "<key>"+k+"</key>")
	if i < 0 {
		return ""
	}
	rest := s[i:]
	j := strings.Index(rest, "<string>")
	if j < 0 {
		return ""
	}
	rest = rest[j+len("<string>"):]
	if e := strings.Index(rest, "</string>"); e >= 0 {
		return strings.TrimSpace(rest[:e])
	}
	return ""
}
