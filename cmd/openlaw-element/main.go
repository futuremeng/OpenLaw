// openlaw-element：构成要件导入与人工落判（OL-2，docs/tasks-opencourt-bridge.md）。
// 原则：机器不裁决——导入一律 pending，人工确认（署名 + 依据）后才 confirmed；
// 全程 review_log 留痕；默认 dry-run，-apply 才写库；关联不到条文的行如实拒绝并报告。
// 子命令：
//
//	import  CSV/JSON 导入要件（-in <file>；-apply 写库）
//	confirm 人工落判（-ids <id[,id…]> -verdict confirm|reject -reviewer <署名> -reason <依据>）
//	stats   按案由统计要件数与确认状态
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/futuremeng/OpenLaw/element"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	fs := flag.NewFlagSet(sub, flag.ExitOnError)
	dataDir := fs.String("data", "data", "数据目录")
	var (
		in      = fs.String("in", "", "import：要件 CSV/JSON 文件路径（.json 结尾按 JSON 读）")
		apply   = fs.Bool("apply", false, "import：真正写库（缺省仅 dry-run 计数）")
		ids     = fs.String("ids", "", "confirm：要件编号，逗号分隔")
		verdict = fs.String("verdict", "confirm", "confirm：confirm|reject")
		reviewr = fs.String("reviewer", "", "confirm：署名（必填）")
		reason  = fs.String("reason", "", "confirm：结论依据（必填）")
		asJSON  = fs.Bool("json", false, "JSON 输出")
	)
	_ = fs.Parse(os.Args[2:])

	dbPath := filepath.Join(*dataDir, "db", "openlaw.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	switch sub {
	case "import":
		if *in == "" {
			fatal(fmt.Errorf("import 需要 -in <文件>"))
		}
		var inputs []element.Input
		if strings.HasSuffix(*in, ".json") {
			inputs, err = element.ParseJSONFile(*in)
		} else {
			inputs, err = element.ParseCSVFile(*in)
		}
		if err != nil {
			fatal(err)
		}
		rep, err := element.Import(db, inputs, *apply)
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			printJSON(rep)
			return
		}
		mode := "试跑（未写入）"
		if rep.Applied {
			mode = "已写入"
		}
		fmt.Printf("%s：共 %d 行 | 新增 %d | 更新 %d | 拒绝 %d\n",
			mode, rep.Total, rep.Added, rep.Updated, len(rep.Rejected))
		for _, re := range rep.Rejected {
			fmt.Printf("  拒绝 第%d行 %s：%s\n", re.Line, re.ElementID, re.Reason)
		}
		if !rep.Applied && rep.Total > len(rep.Rejected) {
			fmt.Println("确认无误后加 -apply 写入；写入后要件一律「待复核」，须人工确认。")
		}
	case "confirm":
		n, err := element.Confirm(db, splitIDs(*ids), *verdict, *reviewr, *reason)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("已落判 %d 项要件（%s，复核人：%s；全程复核记录留痕）\n", n, *verdict, *reviewr)
	case "stats":
		rows, err := db.Query(`SELECT case_type,
			COUNT(*), SUM(confirm_status='confirmed'), SUM(confirm_status='pending'),
			SUM(confirm_status='rejected')
			FROM element GROUP BY case_type ORDER BY case_type`)
		if err != nil {
			fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var ct string
			var total, c, p, r int
			if err := rows.Scan(&ct, &total, &c, &p, &r); err != nil {
				fatal(err)
			}
			fmt.Printf("%s：共 %d | 已复核确认 %d | 待复核 %d | 已驳回 %d\n", ct, total, c, p, r)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func splitIDs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: openlaw-element import|confirm|stats [-data data] …（-h 看参数）")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(1)
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
