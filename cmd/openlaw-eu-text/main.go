// openlaw-eu-text：欧盟全文导入 CLI（docs/sources-eu-text.md，M1）。
// 默认离线：本命令不运行则绝不联网。数据源为 Cellar representation 层
// （官方开放程序化入口，四步链 expression→manifestation→item，无 WAF）。
// 流程收敛到共享 runner ingest.RunEUText——与桌面端「获取最新法规」同一代码路径：
// FetchText（FORMEX ACT XML 首选）→ ParseFormexACT（条文边界来自 <ARTICLE> 结构元素，
// 零猜测）→ Importer 入库（document.lang=eng、parser_version 落库、
// confirm_status=pending 人工闸口）。前置：law 已由 openlaw-eu 建立。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	var (
		dataDir = flag.String("data", "data", "数据目录")
		celex   = flag.String("celex", "", "CELEX 编号（必填，如 32016R0679）")
		lang    = flag.String("lang", "eng", "正文语言（ISO 639-3，如 eng/deu/fra）")
		format  = flag.String("format", "fmx4", "全文格式：fmx4（FORMEX XML，首选）| xhtml（CONVEX 回退）")
		version = flag.String("version-no", "OJ原文", "版本号（law_version.version_no）")
		name    = flag.String("name", "", "法规名称（留空用库内既有名称；库内为回退名时用 Cellar 官方全称）")
		dryRun  = flag.Bool("dry-run", false, "只获取与解析，不入库")
		batch   = flag.String("batch", "", "批量模式：队列文件（JSON 数组，断点续传；RL-4）")
		report  = flag.String("report", "", "批量模式：批次报告 CSV 路径（留空=队列同目录）")
		circuit = flag.Int("circuit", 5, "批量模式：连续限流熔断部数")
		probe   = flag.Bool("probe", false, "配额探测（RL-2）：只测 robots+expression 两层窗口状态，不抓取不入库")
		timeout = flag.Duration("timeout", 60*time.Second, "单次请求超时")
		delay   = flag.Duration("delay", 2*time.Second, "Cellar 请求间隔（礼貌限速）")
		asJSON  = flag.Bool("json", false, "以 JSON 输出结果")
	)
	flag.Parse()

	if *celex == "" && *batch == "" {
		fatal(fmt.Errorf("-celex 必填（CELEX 编号，如 32016R0679）；批量模式用 -batch <队列文件>"))
	}
	if err := os.MkdirAll(filepath.Join(*dataDir, "db"), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	c := &ingest.Cellar{DataDir: *dataDir, DB: db,
		Policy: ingest.FetchPolicy{Timeout: *timeout}, MinInterval: *delay}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 探测模式：与正式抓取同一指纹链（policy/Transport/UA/robots 缓存）测窗口，
	// 退出码 0=窗口开 / 3=限流中（任务编排据此排 RL-1 pilot）。
	if *probe {
		pr, err := c.Probe(ctx, *celex, *lang)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			b, _ := json.MarshalIndent(pr, "", "  ")
			fmt.Println(string(b))
		} else {
			fmt.Printf("robots      %d（allow=%v）\n", pr.RobotsStatus, pr.RobotsAllow)
			fmt.Printf("expression  %d（ok=%v）\n", pr.ExprStatus, pr.ExprOK)
			fmt.Printf("verdict     %s\n", pr.Suggest)
		}
		if !pr.Open {
			if pr.RateLimited {
				os.Exit(3)
			}
			os.Exit(2)
		}
		return
	}

	if *batch != "" {
		bctx, bcancel := context.WithTimeout(context.Background(), 12*time.Hour)
		defer bcancel()
		res, err := ingest.RunEUBatch(bctx, *dataDir, db, c, ingest.EUBatchOptions{
			QueuePath: *batch, ReportPath: *report, CircuitBreak: *circuit, DryRun: *dryRun,
		}, func(done, total int, celex, status string) {
			fmt.Printf("  [%d/%d] %s %s\n", done, total, celex, status)
		})
		if res == nil {
			fatal(err)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "批量中止（队列与已获取部分已如实保留）:", err)
		}
		if *asJSON {
			b, _ := json.MarshalIndent(res, "", "  ")
			fmt.Println(string(b))
		} else {
			fmt.Printf("完成 %d | 失败 %d | 待采 %d\n", res.Done, res.Failed, res.Pending)
			if res.Aborted {
				fmt.Printf("熔断中止：连续 %d 部限流——剩余如实留待采，改日再排（429 后拉长间隔而非缩短）\n", res.Consecutive429)
			}
			if res.Cancelled {
				fmt.Println("已取消：已获取部分如实保留，重跑同一队列即断点续传")
			}
			fmt.Println("批次报告:", res.ReportPath)
		}
		if res.Aborted || res.Failed > 0 {
			os.Exit(3)
		}
		return
	}

	res, err := ingest.RunEUText(ctx, *dataDir, db, c, ingest.EUTextOptions{
		Celex: *celex, Lang: *lang, Format: *format, VersionNo: *version,
		Name: *name, DryRun: *dryRun,
	})
	if err != nil {
		fatal(err)
	}
	if *asJSON {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
		return
	}
	fmt.Printf("law_id      %s\n", res.LawID)
	fmt.Printf("标题        %s\n", res.Title)
	fmt.Printf("条文        %d 条（结构化边界，parser=%s）\n", res.Articles, res.ParserVersion)
	fmt.Printf("正文        %s（%.1f KB, sha256=%s…）\n", res.RawPath, float64(res.Size)/1024, first16(res.SHA256))
	if res.DryRun {
		fmt.Println("dry-run     未入库（去掉 -dry-run 执行导入）")
		return
	}
	fmt.Printf("doc_id      %s（version %s）\n", res.DocID, res.VersionID)
	fmt.Printf("确认状态    pending（人工闸口：openlaw-confirm sample/apply）\n")
	if res.ChainIssues > 0 {
		fmt.Printf("断链检测    %d 项异常（openlaw-check 复核）\n", res.ChainIssues)
	} else {
		fmt.Println("断链检测    通过")
	}
}

func first16(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(2)
}
