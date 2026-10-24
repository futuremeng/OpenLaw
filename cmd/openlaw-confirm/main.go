// openlaw-confirm：pending 语料的人工确认流程（抽样 → 复核 → 落判 → 审计）。
// 原则：机器绝不自动确认。一切状态流转写 review_log（谁/何时/依据什么）；
// 批量确认必须署名（-reviewer）并写明依据（-reason），且默认 dry-run，须 -yes 执行。
// 子命令：
//
//	stats   确认进度与风险面统计（-json 机器可读）
//	sample  生成复核清单 CSV（高风险优先：日期推断/全文即一节/缺公布日；剩余按效力等级轮转）
//	apply   读裁决 CSV（doc_id,verdict,reviewer,note；verdict=confirm/reject/reset）落判
//	bulk    按条件批量流转（-source/-level 过滤；默认 dry-run，-yes 执行）
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	sub := os.Args[1]
	fs := flag.NewFlagSet(sub, flag.ExitOnError)
	dataDir := fs.String("data", "data", "数据目录")
	var (
		out     = fs.String("out", "", "sample：复核清单输出路径")
		n       = fs.Int("n", 200, "sample：清单上限条数（高风险全部入列后按等级轮转补齐；0=仅高风险）")
		reports = fs.String("reports", "", "sample：batch_report CSV 路径（逗号分隔，用于 inferred/whole 风险标记）")
		in      = fs.String("in", "", "apply：裁决 CSV 路径")
		source  = fs.String("source", "", "bulk：document.source_type LIKE（如 'flk%'）")
		level   = fs.String("level", "", "bulk：law.effect_level 精确匹配")
		verdict = fs.String("verdict", "confirm", "bulk：confirm/reject/reset")
		reviewr = fs.String("reviewer", "", "bulk：署名（必填）")
		reason  = fs.String("reason", "", "bulk：确认/驳回依据（必填）")
		yes     = fs.Bool("yes", false, "bulk：真正执行（缺省仅 dry-run 计数）")
		asJSON  = fs.Bool("json", false, "JSON 输出")
	)
	_ = fs.Parse(os.Args[2:])

	if err := os.MkdirAll(filepath.Join(*dataDir, "db"), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	c := &ingest.Confirmer{DataDir: *dataDir, DB: db}

	switch sub {
	case "stats":
		rs, err := ingest.LoadRiskSetFromProvenance(*dataDir)
		if err != nil {
			fatal(err)
		}
		if *reports != "" {
			more, err := ingest.LoadRiskSet(strings.Split(*reports, ","))
			if err != nil {
				fatal(err)
			}
			rs = ingest.MergeRiskSets(rs, more)
		}
		st, err := c.Stats(rs)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(st)
			return
		}
		fmt.Printf("文档总数 %d | pending %d | confirmed %d | rejected %d | 审计流转 %d\n",
			st.Total, st.Pending, st.Confirmed, st.Rejected, st.Reviewed)
		fmt.Printf("风险面：日期推断 %d | 全文即一节 %d | 缺公布日 %d\n",
			st.Risk.InferredDate, st.Risk.WholeText, st.Risk.NoPublishDate)
		fmt.Println("按效力等级（pending/confirmed/rejected）：")
		for _, lc := range st.ByLevel {
			fmt.Printf("  %-12s %6d / %6d / %6d\n", lc.Level, lc.Pending, lc.Confirmed, lc.Rejected)
		}
	case "sample":
		if *out == "" {
			fatal(fmt.Errorf("sample 需要 -out 输出路径"))
		}
		rs, err := ingest.LoadRiskSetFromProvenance(*dataDir)
		if err != nil {
			fatal(err)
		}
		if *reports != "" {
			more, err := ingest.LoadRiskSet(strings.Split(*reports, ","))
			if err != nil {
				fatal(err)
			}
			rs = ingest.MergeRiskSets(rs, more)
		}
		n2, err := c.Sample(*out, *n, rs)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("复核清单 %d 条 → %s（填写 verdict/reviewer/note 列后用 apply 子命令落判）\n", n2, *out)
	case "apply":
		if *in == "" {
			fatal(fmt.Errorf("apply 需要 -in 裁决 CSV 路径"))
		}
		sum, err := c.Apply(*in)
		if err != nil {
			fatal(err)
		}
		printJSON(sum)
		if len(sum.Failures) > 0 {
			os.Exit(1)
		}
	case "bulk":
		if !*yes {
			m, err := c.BulkMatch(ingest.BulkFilter{SourceLike: *source, Level: *level}, *verdict)
			if err != nil {
				fatal(err)
			}
			fmt.Printf("dry-run：按 %s 起始状态命中 %d 条（加 -yes 执行；执行须 -reviewer 与 -reason）\n", *verdict, m)
			return
		}
		sum, err := c.Bulk(ingest.BulkFilter{SourceLike: *source, Level: *level}, *verdict, *reviewr, *reason)
		if err != nil {
			fatal(err)
		}
		printJSON(sum)
		if len(sum.Failures) > 0 {
			os.Exit(1)
		}
	default:
		usage()
	}
}

func printJSON(v any) {
	out, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(out))
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: openlaw-confirm stats|sample|apply|bulk [-data 目录] [子命令参数，-h 查看]")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(2)
}
