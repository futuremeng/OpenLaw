// openlaw-govcn：gov.cn 官方库导入 CLI（docs/sources-govcn.md）。
// -source xzfgk（默认）= 国家行政法规库 www.gov.cn/zhengce/xzfgk/；
// -source gzk        = 国家规章库 www.gov.cn/zhengce/xxgk/gjgzk/（部门规章+地方政府规章）。
// 默认离线：本命令不运行则绝不联网。两段式合规（详见 ingest/govcn.go 头注释）：
//   - 清单人工导出（浏览器脚本 tools/govcn-xzfgk-export.user.js / tools/govgz-export.user.js；
//     sousuo 检索域 robots 禁止自动化，Athena 接口带鉴权——绝不逆向）；
//   - 详情页按清单抓取（主站 robots 允许 + 主机锁 + 限速 + import_log + HTML 留证）。
//
// 流程：list.json → 逐条抓取/解析 → 日期（落款推断优先，绝不猜）→ Importer 入库
// （行政法规/部门规章/地方政府规章 / pending 人工闸口 / 版本链接续）→ CSV 报告（failed 即退出码 1）。
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	var (
		dataDir = flag.String("data", "data", "数据目录")
		source  = flag.String("source", "xzfgk", "官方库：xzfgk=国家行政法规库；gzk=国家规章库")
		listP   = flag.String("list", "", "人工导出的清单 list.json（必填）")
		report  = flag.String("report", "", "CSV 报告输出路径（默认 data/incoming/govcn/report_<ts>.csv）")
		delay   = flag.Duration("delay", 2*time.Second, "详情页抓取间隔（礼貌限速，勿低于 2s）")
		limit   = flag.Int("limit", 0, "最多处理条数（0=全部；分批跑防压力）")
		dryRun  = flag.Bool("dry-run", false, "只抓取与解析（留证 HTML/md），不入库")
	)
	flag.Parse()

	if *listP == "" {
		fatal(fmt.Errorf("-list 必填（浏览器人工导出的 list.json；程序不代为检索）"))
	}
	if *source != "xzfgk" && *source != "gzk" {
		fatal(fmt.Errorf("-source 只能为 xzfgk（行政法规库）或 gzk（国家规章库）"))
	}
	subdir := "govcn"
	if *source == "gzk" {
		subdir = "govgz"
	}
	if *delay < 2*time.Second {
		fatal(fmt.Errorf("-delay 不得低于 2s（礼貌限速纪律）"))
	}
	entries, err := ingest.LoadGovcnList(*listP)
	if err != nil {
		fatal(err)
	}
	if *limit > 0 && *limit < len(entries) {
		entries = entries[:*limit]
	}

	if err := os.MkdirAll(filepath.Join(*dataDir, "db"), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	g := &ingest.Govcn{DataDir: *dataDir, DB: db, MinInterval: *delay, DryRun: *dryRun, Kind: *source}
	im := &ingest.Importer{DataDir: *dataDir, DB: db}

	rp := *report
	if rp == "" {
		rp = filepath.Join(*dataDir, "incoming", subdir, "report_"+time.Now().UTC().Format("20060102T150405")+".csv")
	}
	if err := os.MkdirAll(filepath.Dir(rp), 0o755); err != nil {
		fatal(err)
	}
	f, err := os.Create(rp)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"url", "doc_id", "status", "articles", "reason"})

	cnt := map[string]int{}
	ctx := context.Background()
	for i, e := range entries {
		it := g.FetchAndImport(ctx, im, e)
		cnt[it.Status]++
		if err := w.Write([]string{it.URL, it.DocID, it.Status, fmt.Sprint(it.Article), it.Reason}); err != nil {
			fatal(err)
		}
		w.Flush()
		fmt.Printf("[%d/%d] %s %s %s\n", i+1, len(entries), it.Status, it.DocID, truncate(it.Reason, 80))
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fatal(err)
	}
	fmt.Printf("govcn 完成: 共 %d 条 %v\n报告: %s\n", len(entries), cnt, rp)
	fmt.Println("提示: 全部条目 confirm_status=pending，用 openlaw-confirm 复核后方可视为已证。")
	if cnt["failed"] > 0 {
		os.Exit(1)
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", strings.TrimSpace(err.Error()))
	os.Exit(1)
}
