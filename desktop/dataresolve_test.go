package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveDataDir .app 从 Finder 启动时 CWD 是 "/"，必须按程序位置回退到仓库
// data/；否则 App.Open() 会 MkdirAll 出一个空语料库，用户却以为在看真实数据。
func TestResolveDataDir(t *testing.T) {
	const exe = "/repo/dist/OpenLaw.app/Contents/MacOS/OpenLaw"
	cases := []struct {
		name     string
		explicit string
		flagSet  bool
		env      string
		exe      string
		existing map[string]bool
		wantDir  string
		wantSrc  string
	}{
		{
			name: "显式 -data 最高优先", explicit: "/tmp/_corpus", flagSet: true,
			wantDir: "/tmp/_corpus", wantSrc: "命令行 -data",
		},
		{
			name: "环境变量次之", explicit: "data", env: "/srv/openlaw-data",
			exe: exe, existing: map[string]bool{"/repo/data/db": true},
			wantDir: "/srv/openlaw-data", wantSrc: "环境变量 OPENLAW_DATA",
		},
		{
			name: "bundle 回退到同仓库 data", explicit: "data", exe: exe,
			existing: map[string]bool{"/repo/data/db": true},
			wantDir:  "/repo/data", wantSrc: "随程序位置",
		},
		{
			name: "bundle 放在别处、找不到语料时退回默认并如实标注", explicit: "data",
			exe: "/opt/OpenLaw.app/Contents/MacOS/OpenLaw", existing: map[string]bool{"/repo/data/db": true},
			wantDir: "data", wantSrc: "当前目录（默认）",
		},
		{
			name: "只有 data 没有 data/db 不算语料库", explicit: "data", exe: exe,
			existing: map[string]bool{"/repo/data": true},
			wantDir:  "data", wantSrc: "当前目录（默认）",
		},
		{
			name: "无 exe 信息（异常）时退回默认", explicit: "data",
			wantDir: "data", wantSrc: "当前目录（默认）",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isDir := func(p string) bool { return tc.existing[filepath.Clean(p)] }
			getenv := func(k string) string {
				if k == envDataDir {
					return tc.env
				}
				return ""
			}
			dir, src := resolveDataDir(tc.explicit, tc.flagSet, tc.exe, isDir, getenv)
			if dir != tc.wantDir {
				t.Errorf("语料目录应为 %q，实际 %q", tc.wantDir, dir)
			}
			if src != tc.wantSrc {
				t.Errorf("来源应如实标注为 %q，实际 %q", tc.wantSrc, src)
			}
			if src == "" {
				t.Error("来源不得为空：必须可展示给用户")
			}
		})
	}
}

// TestFileIsDir 真实文件系统上的检查器：存在的目录为真，文件与不存在路径为假。
func TestFileIsDir(t *testing.T) {
	dir := t.TempDir()
	if !fileIsDir(dir) {
		t.Error("已存在目录应判为真")
	}
	if fileIsDir(filepath.Join(dir, "不存在")) {
		t.Error("不存在路径应判为假")
	}
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}
	if fileIsDir(f) {
		t.Error("普通文件不应判为目录")
	}
}
