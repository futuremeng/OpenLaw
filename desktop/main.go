// openlaw-desktop（OL-10）：开源法律法规数据库桌面客户端（Wails v2，技术栈对齐
// OpenCourt）。它是 CLI 的 UI 壳：法规库状态、人工确认闸口、获取任务都走与
// cmd/openlaw-* 完全相同的代码路径（store/ingest/validate），不新增任何写入
// 逻辑，也不绕过 pending 人工闸口纪律。
//
// 构建（macOS/Windows 需 CGO——webview 依赖；核心库仍纯 Go，只有桌面壳例外；
// 必须带 wails 构建标签，与 wails CLI 的发布形态一致）：
//
//	go build -tags desktop,production -o dist/openlaw-desktop ./desktop
//	./dist/openlaw-desktop -data data
//
//	需要真实定位（macOS 系统授权）时必须打包为 .app：
//
//	packaging/macos/build-app.sh        # 产出 dist/OpenLaw.app（含定位用途声明）
package main

import (
	"embed"
	"flag"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

// AppTitle 窗口标题（并须与 packaging/macos/Info.plist 的 CFBundleDisplayName 一致，
// 由 desktop/title_test.go 门禁）。产品自称一律用产品名，不用内部旧称——
// 见 docs/design-ui-language.md「产品自称」条。
const AppTitle = "开源法律法规数据库"

func main() {
	dataDir := flag.String("data", "data", "数据目录（db/、md/、incoming/、manifest/）")
	edition := flag.String("edition", EditionFull, "发行版：full=数据维护版（含复核/标引/采集）；reader=只读查询版（不给写路径入口）")
	flag.Parse()
	flagSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "data" {
			flagSet = true
		}
	})

	// .app 经 Finder 启动时工作目录是 "/"：按程序位置回退找数据目录，并把来源如实
	// 打印（界面「库总览 · 技术信息」同步展示）。绝不静默 MkdirAll 出一个空数据库。
	exe, _ := os.Executable()
	dir, src := resolveDataDir(*dataDir, flagSet, exe, fileIsDir, os.Getenv)
	log.Printf("法规库数据位置：%s（来源：%s）", dir, src)

	app := NewApp(dir)
	app.dataSrc = src
	app.SetEdition(*edition)
	log.Printf("发行版：%s", *edition)
	if err := app.Open(); err != nil {
		// 启动失败也开窗：错误在界面内如实显示（fail-visible，不静默）
		log.Printf("打开法规库失败: %v", err)
	}

	err := wails.Run(&options.App{
		Title:     AppTitle,
		Width:     1280,
		Height:    860,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []any{
			app,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
