package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 数据目录可写：报告落在 crash.log，含版本与堆栈。
func TestWriteCrashReportToDataDir(t *testing.T) {
	dir := t.TempDir()
	if err := writeCrashReport(dir, panicReport("boom")); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "crash.log"))
	if err != nil {
		t.Fatalf("crash.log 不存在: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "version "+version) {
		t.Errorf("报告应含程序版本 %s，实得：%s", version, text)
	}
	if !strings.Contains(text, "crashOnPanic") && !strings.Contains(text, "goroutine") {
		t.Errorf("报告应含堆栈，实得：%s", text)
	}
	if !strings.Contains(text, "panic ") || !strings.Contains(text, "boom") {
		t.Errorf("报告应含 panic 值，实得：%s", text)
	}
}

// 数据目录不可写（此处：不存在）：退回 crashFallback，不报错。
func TestWriteCrashReportFallsBack(t *testing.T) {
	fallback := filepath.Join(t.TempDir(), "openlaw-crash.log")
	old := crashFallback
	crashFallback = fallback
	t.Cleanup(func() { crashFallback = old })

	if err := writeCrashReport(filepath.Join(t.TempDir(), "不存在"), panicReport("boom")); err != nil {
		t.Fatalf("回退应成功: %v", err)
	}
	raw, err := os.ReadFile(fallback)
	if err != nil {
		t.Fatalf("回退文件不存在: %v", err)
	}
	if !strings.Contains(string(raw), "boom") {
		t.Errorf("回退文件应含 panic 值，实得：%s", raw)
	}
}

// 追加而非覆盖：两次 panic 都留痕。
func TestWriteCrashReportAppends(t *testing.T) {
	dir := t.TempDir()
	for _, msg := range []string{"first", "second"} {
		if err := writeCrashReport(dir, panicReport(msg)); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "crash.log"))
	text := string(raw)
	if !strings.Contains(text, "first") || !strings.Contains(text, "second") {
		t.Errorf("两次 panic 都应留痕，实得：%s", text)
	}
}

// crashOnPanic 不吞 panic：原值必须继续向外传播。
func TestCrashOnPanicRethrows(t *testing.T) {
	dir := t.TempDir()
	recovered := func() (r any) {
		defer func() { r = recover() }()
		func() {
			defer crashOnPanic(dir)
			panic("propagate")
		}()
		return nil
	}()
	if recovered == nil || recovered != "propagate" {
		t.Fatalf("panic 应原样外传，实得: %v", recovered)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "crash.log"))
	if err != nil || !strings.Contains(string(raw), "propagate") {
		t.Errorf("外传前也应留痕: %v %s", err, raw)
	}
}
