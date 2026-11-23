// crashlog.go 桌面壳的崩溃留痕。
//
// 为什么必须有：panic 让窗口开不出来时，进程直接退出，Dock 里一闪、屏幕上一片
// 空白，试用用户什么线索都拿不到。失败必须能被找到：panic 先把报告追加进
// 数据目录的 crash.log（UTC 时间、程序版本、panic 值、堆栈），数据目录写不进
// （不存在、只读）就退回系统临时目录的 openlaw-crash.log。
//
// 不吞 panic：写完原样 re-panic，stderr 输出与退出码保持原生行为（fail-visible
// 纪律不因留痕而改动）。后台 goroutine 里的 panic 不经此处，照旧只上 stderr——
// 这是窗口级兜底，不是全进程捕获，如实写明。
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// crashFallback 数据目录不可写时的落盘位置。变量而非内联，测试得以重定向。
var crashFallback = filepath.Join(os.TempDir(), "openlaw-crash.log")

// crashOnPanic 在 main 的 defer 里挂住窗口主链路的 panic。
func crashOnPanic(dataDir string) {
	if r := recover(); r != nil {
		// 留痕自身失败不再叠加失败：writeCrashReport 已含回退，最后的错误只上日志。
		if err := writeCrashReport(dataDir, panicReport(r)); err != nil {
			log.Printf("崩溃日志写不进任何位置: %v", err)
		}
		panic(r)
	}
}

// panicReport 组装报告正文：时间、版本、panic 值、堆栈。
func panicReport(r any) string {
	return fmt.Sprintf("panic %s version %s: %v\n%s\n",
		time.Now().UTC().Format(time.RFC3339), version, r, debug.Stack())
}

// writeCrashReport 先写数据目录，失败退回系统临时目录。两次都失败才报错误。
func writeCrashReport(dataDir, report string) error {
	if err := appendCrash(filepath.Join(dataDir, "crash.log"), report); err == nil {
		return nil
	}
	return appendCrash(crashFallback, report)
}

func appendCrash(path, report string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprint(f, report)
	return err
}
