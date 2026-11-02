// openlaw-stats：配额与限流可观测报表（docs/tasks-eu-rate-limit.md RL-5）。
// 默认离线：只读本机 import_log，绝不联网。数据源就是既有留痕——报表不新增请求，
// 配额决策（pilot 时机 / 分批速率）以它为准，不凭印象。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	// 子命令与 flag 顺序无关（`eu -data x` 与 `-data x eu` 同义）。
	sub := ""
	args := []string{}
	for _, a := range os.Args[1:] {
		if a == "eu" && sub == "" {
			sub = a
			continue
		}
		args = append(args, a)
	}
	fs := flag.NewFlagSet("openlaw-stats", flag.ExitOnError)
	dataDir := fs.String("data", "data", "数据目录")
	since := fs.String("since", "", "起始日期（含，如 2026-10-01）；留空 = 全部历史")
	asJSON := fs.Bool("json", false, "以 JSON 输出")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: openlaw-stats eu [-data 目录] [-since 日期] [-json]")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if sub != "eu" {
		fatal(fmt.Errorf("目前仅提供 eu 报表（import_log 的欧盟通道聚合视图）；其余法域另行扩展"))
	}
	db, err := store.OpenReadOnly(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	rep, err := ingest.EUQuotaStats(db, *since)
	if err != nil {
		fatal(err)
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		fatal(enc.Encode(rep))
		return
	}
	if len(rep.Days) == 0 {
		fmt.Println("import_log 中还没有欧盟通道的访问记录（探测与抓取都会留痕）。")
		return
	}
	fmt.Printf("%-12s %8s %8s %8s %8s %10s\n", "日期", "次数", "成功", "失败", "限流429", "字节")
	for _, d := range rep.Days {
		rate := 0.0
		if d.Requests > 0 {
			rate = 100 * float64(d.Limited429) / float64(d.Requests)
		}
		fmt.Printf("%-12s %8d %8d %8d %5d(%2.0f%%) %10d\n", d.Day, d.Requests, d.OK, d.Failed, d.Limited429, rate, d.Bytes)
	}
	if len(rep.Celexes) > 0 {
		fmt.Println("\n按法规编号的请求计数（配额消耗归因）：")
		for _, c := range rep.Celexes {
			fmt.Printf("  %s  请求 %d（成功 %d）\n", c.Celex, c.Requests, c.OK)
		}
	}
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "openlaw-stats:", err)
		os.Exit(1)
	}
}
