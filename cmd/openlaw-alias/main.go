// openlaw-alias：法名别名表（law_alias）构建与巡检（OL-4，docs/tasks-opencourt-bridge.md）。
// 默认 dry-run（试采，不写入）；-apply 才落库。别名只来自语料内可溯源信息
// （法规库简称登记 + 官方文本「以下简称」条款），本命令不生成任何别名。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

func main() {
	fs := flag.NewFlagSet("openlaw-alias", flag.ExitOnError)
	dataDir := fs.String("data", "data", "数据目录")
	apply := fs.Bool("apply", false, "写入别名表（缺省为试采，不写入）")
	stats := fs.Bool("stats", false, "只查看别名表规模与来源分布")
	lookup := fs.String("lookup", "", "按法名解析并打印候选（含命中依据）")
	csvPath := fs.String("csv", "", "把报告落成 CSV（供人工巡检）")
	asJSON := fs.Bool("json", false, "以 JSON 输出")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: openlaw-alias [-data 目录] [-apply] [-stats] [-lookup 法名] [-csv 报告.csv] [-json]")
		fs.PrintDefaults()
	}
	fs.Parse(os.Args[1:])

	db, err := store.Open(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	switch {
	case *lookup != "":
		doLookup(db, *lookup, *asJSON)
	case *stats:
		doStats(db, *asJSON)
	default:
		doBuild(db, *apply, *csvPath, *asJSON)
	}
}

func doBuild(db *store.DB, apply bool, csvPath string, asJSON bool) {
	rep, err := validate.BuildLawAliases(db, apply)
	if err != nil {
		fatal(err)
	}
	if csvPath != "" {
		if err := validate.WriteAliasReport(csvPath, rep); err != nil {
			fatal(err)
		}
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		fatal(enc.Encode(rep))
		return
	}
	mode := "试采（未写入）"
	if rep.Applied {
		mode = "已写入"
	}
	fmt.Printf("别名表构建：%s\n", mode)
	fmt.Printf("  扫描条文 %d 条；抽出「以下简称」简称 %d 处\n", rep.ArticlesScanned, rep.Extracted)
	fmt.Printf("  全称可反查 %d 处；全称查不到（如实不收录）%d 处\n", rep.Resolved, rep.Unresolved)
	fmt.Printf("  新增 %d 条；已存在将更新 %d 条；同批重复 %d 处（只落一行）\n",
		rep.Added, rep.Updated, rep.Duplicates)
	if csvPath != "" {
		fmt.Printf("  巡检报告：%s\n", csvPath)
	}
	if !rep.Applied {
		fmt.Println("  以上为「将会」计数；确认无误后加 -apply 写入。")
	}
	if rep.Unresolved > 0 {
		fmt.Println("  提示：全称查不到的简称说明法规库缺该法规或名称写法不一致，" +
			"需人工核对后补录法规条目，本命令不猜测法规身份。")
	}
}

func doStats(db *store.DB, asJSON bool) {
	st, err := validate.LawAliasStats(db)
	if err != nil {
		fatal(err)
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		fatal(enc.Encode(st))
		return
	}
	if !st.Exists {
		fmt.Println("别名表 law_alias 不存在：该语料数据版本早于 v9。")
		fmt.Println("补救：升级数据目录后执行 openlaw-alias -apply 构建别名表。")
		return
	}
	fmt.Printf("别名表共 %d 条\n", st.Total)
	for src, n := range st.Sources {
		fmt.Printf("  %-18s %d\n", src, n)
	}
	if len(st.Samples) > 0 {
		fmt.Println("样例（最多 10 条）：")
		for _, r := range st.Samples {
			fmt.Printf("  %s → 《%s》 [%s] %s\n", r.Alias, r.LawName, r.Source, r.ArticleID)
		}
	}
}

func doLookup(db *store.DB, name string, asJSON bool) {
	lr, err := validate.ResolveLawName(db, name)
	if err != nil {
		fatal(err)
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		fatal(enc.Encode(lr))
		return
	}
	if lr.AliasTableMissing {
		fmt.Println("提示：该语料无别名表（数据版本早于 v9），简称仅按法规库既有名称匹配。")
	}
	if len(lr.Candidates) == 0 {
		fmt.Printf("未命中：法规库中没有「%s」（规范化后 %q）。\n", lr.Query, lr.Normalized)
		fmt.Println("补救：核对名称写法；或在法规库补录该法规后重跑 openlaw-alias -apply。")
		return
	}
	if lr.Ambiguous {
		fmt.Printf("「%s」对应 %d 部法规，需人工选择（不自动采信）：\n", lr.Query, len(lr.Candidates))
		for _, c := range lr.Candidates {
			fmt.Printf("  %s  《%s》  %s  [依据 %s]\n", c.LawID, c.Name, c.Issuer, c.ResolvedFrom)
		}
		return
	}
	c := lr.Candidates[0]
	fmt.Printf("「%s」→《%s》（%s，依据 %s）\n", lr.Query, c.Name, c.LawID, c.ResolvedFrom)
	if c.BasedOnArticleID != "" {
		fmt.Printf("  别名出处条文：%s\n", c.BasedOnArticleID)
	}
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "openlaw-alias:", err)
		os.Exit(1)
	}
}
