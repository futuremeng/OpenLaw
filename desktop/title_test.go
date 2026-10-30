// title_test.go：产品自称门禁。窗口标题与打包显示名必须一致，且用户可见文件里
// 不得残留内部旧称（docs/design-ui-language.md「产品自称」条）。
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppTitleIsProductName(t *testing.T) {
	if AppTitle != "开源法律法规数据库" {
		t.Errorf("窗口标题应为产品名，实际 %q", AppTitle)
	}
	if strings.Contains(AppTitle, "语料") {
		t.Errorf("窗口标题不得含内部说法「语料」：AppTitle=%q", AppTitle)
	}
}

// TestPackagedDisplayNameMatchesAppTitle .app 在 Dock/Finder/关于框里显示的名字
// 必须与窗口标题同一说法，不能一处新一处旧。
func TestPackagedDisplayNameMatchesAppTitle(t *testing.T) {
	p := filepath.Join("..", "packaging", "macos", "Info.plist")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取打包描述文件失败: %v", err)
	}
	s := string(raw)
	if strings.Contains(s, "语料") {
		t.Errorf("打包描述文件不得含「语料」字样: %s", p)
	}
	name := plistValue(s, "CFBundleDisplayName")
	if name == "" {
		t.Fatal("Info.plist 缺少 CFBundleDisplayName")
	}
	if name != AppTitle {
		t.Errorf("打包显示名必须与窗口标题一致: plist=%q 程序=%q", name, AppTitle)
	}
}

// TestNoLegacyNicknameInUserFacingFiles 用户可见文件（窗口/界面/打包/打包脚本）
// 不得残留产品旧称；核心库里作为数据层内部术语的「语料」不在本门禁范围。
func TestNoLegacyNicknameInUserFacingFiles(t *testing.T) {
	files := []string{
		"main.go", "app.go", "dataresolve.go",
		filepath.Join("frontend", "dist", "index.html"),
		filepath.Join("frontend", "dist", "assets", "app.js"),
		filepath.Join("..", "packaging", "macos", "Info.plist"),
		filepath.Join("..", "packaging", "macos", "build-app.sh"),
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		if strings.Contains(string(raw), "语料台") {
			t.Errorf("%s 仍残留产品旧称「语料台」", f)
		}
	}
}
