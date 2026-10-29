// openlaw-desktop（OL-10）：OpenLaw 语料台桌面客户端（Wails v2，技术栈对齐
// OpenCourt）。它是 CLI 的 UI 壳：语料状态、人工确认闸口、采集任务都走与
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

// wails v2.17 darwin 生产构建引用 UTType（拖拽支持）但未链接 UniformTypeIdentifiers
// 框架——在本桌面壳内补齐链接指令，构建不依赖外部环境变量。
// cgo 仅桌面壳需要；OpenLaw 核心库保持纯 Go。

/*
#cgo darwin LDFLAGS: -framework UniformTypeIdentifiers
*/
import "C"

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

func main() {
	dataDir := flag.String("data", "data", "数据目录（db/、md/、incoming/、manifest/）")
	flag.Parse()
	flagSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "data" {
			flagSet = true
		}
	})

	// .app 经 Finder 启动时工作目录是 "/"：按程序位置回退找语料目录，并把来源如实
	// 打印（界面「库总览」同步展示）。绝不静默 MkdirAll 出一个空语料库。
	exe, _ := os.Executable()
	dir, src := resolveDataDir(*dataDir, flagSet, exe, fileIsDir, os.Getenv)
	log.Printf("语料目录：%s（来源：%s）", dir, src)

	app := NewApp(dir)
	app.dataSrc = src
	if err := app.Open(); err != nil {
		// 启动失败也开窗：错误在界面内如实显示（fail-visible，不静默）
		log.Printf("打开语料失败: %v", err)
	}

	err := wails.Run(&options.App{
		Title:     "OpenLaw 语料台",
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
