package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGovcnTaskValidation(t *testing.T) {
	a := NewApp(t.TempDir())
	if _, err := a.StartGovcnTask(GovcnTaskOptions{Source: "bad", ListPath: "x"}); err == nil {
		t.Fatal("非法 source 必须拒绝")
	}
	if _, err := a.StartGovcnTask(GovcnTaskOptions{Source: "xzfgk"}); err == nil {
		t.Fatal("清单必填（程序不代为检索）")
	}
	if _, err := a.StartGovcnTask(GovcnTaskOptions{Source: "xzfgk", ListPath: "incoming/govcn/list_missing.json"}); err == nil {
		t.Fatal("清单不存在必须报错")
	}
}

func TestBrowserStatusAndGovcnLists(t *testing.T) {
	dir := t.TempDir()
	os.Unsetenv("OPENLAW_BROWSEROS_ENDPOINT")
	a := NewApp(dir)
	st := a.BrowserStatus()
	if st["enabled"] != false || st["guidance"] == "" {
		t.Fatalf("端点未配置应如实报告不可用+指引: %v", st)
	}
	if _, err := a.StartGovListTask(GovListTaskOptions{}); err == nil {
		t.Fatal("BrowserOS 端点未配置必须拒绝（默认离线纪律，绝不降级抓取）")
	}
	p := filepath.Join(dir, "incoming", "govgz", "list_agent_x.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "incoming", "govgz", "govlist_report_list_agent_x.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	lists := a.GovcnLists()
	if len(lists) != 1 || lists[0]["name"] != "govgz/list_agent_x.json" {
		t.Fatalf("清单发现不对（报告文件应排除）: %+v", lists)
	}
}
