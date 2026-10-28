package main

import (
	"strings"
	"testing"
)

func TestCorpusCheckNoManifestHonest(t *testing.T) {
	a := NewApp(t.TempDir())
	if err := a.Open(); err != nil {
		t.Fatal(err)
	}
	v, err := a.CorpusCheck()
	if err != nil {
		t.Fatal(err)
	}
	if v.ManifestOK || v.ManifestErr == "" {
		t.Fatalf("无 manifest 必须如实报告校验失败: %+v", v)
	}
	if v.BrokenCount != 0 {
		t.Fatalf("空语料断链应为 0: %+v", v)
	}
	m := a.ManifestStatus()
	if m.Exists || m.VerifyErr == "" {
		t.Fatalf("manifest 缺失应如实: %+v", m)
	}
}

func TestPackGuards(t *testing.T) {
	a := NewApp(t.TempDir())
	if msg := a.PackVerify(""); msg == "" {
		t.Fatal("空包目录必须报错")
	} else if !strings.Contains(msg, "包目录") {
		t.Fatalf("报错文案不对: %s", msg)
	}
	if msg := a.PackVerify("packs/nope"); msg == "" {
		t.Fatal("不存在的包必须校验失败")
	}
	if _, err := a.StartPackBuildTask("packs/out", "", "", ""); err == nil {
		t.Fatal("build 缺私钥路径必须拒绝")
	}
	if _, err := a.StartPackBuildTask("packs/out", "/nonexistent/key", "", ""); err == nil {
		t.Fatal("私钥不可读必须拒绝")
	}
	if _, err := a.StartPackImportTask(""); err == nil {
		t.Fatal("import 缺包目录必须拒绝")
	}
}

func TestReloadRiskCache(t *testing.T) {
	a := NewApp(t.TempDir())
	a.risk = map[string][]string{"x": {"inferred_date"}}
	a.ReloadRiskCache()
	a.riskMu.Lock()
	defer a.riskMu.Unlock()
	if a.risk != nil {
		t.Fatal("重载后缓存应清空，下次读取重扫")
	}
}
