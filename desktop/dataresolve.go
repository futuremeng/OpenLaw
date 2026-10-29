// dataresolve.go 语料目录解析。
//
// 为什么必须有：.app 经 Finder/open 启动时工作目录是 "/"，默认相对路径 "data"
// 会指向不存在的位置，而 App.Open() 会 MkdirAll——结果是静默打开一个空语料库，
// 用户以为在看真实数据。这违反诚实展示纪律，所以目录必须解析，且来源必须如实展示。
package main

import (
	"os"
	"path/filepath"
)

// envDataDir 桌面端语料目录的显式覆盖口（与 -data 同义，便于 .app 场景配置）。
const envDataDir = "OPENLAW_DATA"

// resolveDataDir 决定语料目录，同时返回来源说明（来源要能展示给用户，不能含糊）。
//
// 优先级：命令行显式 -data > 环境变量 OPENLAW_DATA > 可执行文件相邻（.app 从
// Finder 启动时唯一可靠的线索）> 当前目录默认值（既有开发流程，保持不破坏）。
func resolveDataDir(explicit string, flagSet bool, exePath string, isDir func(string) bool, getenv func(string) string) (string, string) {
	if flagSet && explicit != "" {
		return explicit, "命令行 -data"
	}
	if v := getenv(envDataDir); v != "" {
		return v, "环境变量 " + envDataDir
	}
	if exePath != "" {
		dir := filepath.Dir(exePath)
		for i := 0; i < 6; i++ { // MacOS→Contents→OpenLaw.app→dist→仓库根
			cand := filepath.Join(dir, "data")
			if isDir(filepath.Join(cand, "db")) {
				return cand, "随程序位置"
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return explicit, "当前目录（默认）"
}

// fileIsDir 供 resolveDataDir 使用的默认目录检查器。
func fileIsDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
