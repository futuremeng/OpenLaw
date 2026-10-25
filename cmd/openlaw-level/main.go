// openlaw-level：flk 存量效力等级补标（parse.InferEffectLevel 保守名称规则）。
// 默认 dry-run：只产出 CSV 报告（law_id,name,level,rule）统计覆盖率，绝不写库。
// -apply：把高置信推断写入 law.effect_level / is_local，并在 law.remark 留痕规则号
// （机器建议，等待人工 confirm 复核纠正；无法判定的保持留空）。
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	var (
		dataDir = flag.String("data", "data", "数据目录")
		report  = flag.String("report", "level_report.csv", "CSV 报告输出路径")
		apply   = flag.Bool("apply", false, "写入数据库（缺省仅 dry-run 报告）")
	)
	flag.Parse()

	db, err := store.Open(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT law_id, name FROM law
		WHERE (effect_level IS NULL OR effect_level='') ORDER BY law_id`)
	if err != nil {
		fatal(err)
	}
	type cand struct {
		lawID, name, level string
		rule               int
	}
	var cands []cand
	byLevel := map[string]int{}
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.lawID, &c.name); err != nil {
			rows.Close()
			fatal(err)
		}
		c.level, c.rule = parse.InferEffectLevel(c.name)
		if c.rule == 0 {
			continue
		}
		cands = append(cands, c)
		byLevel[c.level]++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		fatal(err)
	}
	rows.Close()

	f, err := os.Create(*report)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"law_id", "name", "effect_level", "rule"})
	for _, c := range cands {
		_ = w.Write([]string{c.lawID, c.name, c.level, fmt.Sprint(c.rule)})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fatal(err)
	}

	fmt.Printf("待补标法律（effect_level 为空）命中 %d，无法判定保持留空；报告: %s\n", len(cands), *report)
	for lv, n := range byLevel {
		fmt.Printf("  %-8s %d\n", lv, n)
	}
	if !*apply {
		fmt.Println("dry-run：未写库。确认报告后加 -apply 执行")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		fatal(err)
	}
	defer tx.Rollback()
	n := 0
	for _, c := range cands {
		res, err := tx.Exec(`UPDATE law SET effect_level=?, is_local=?,
			remark=COALESCE(remark||'; ','')||?
			WHERE law_id=? AND (effect_level IS NULL OR effect_level='')`,
			c.level, boolToInt(parse.IsLocalLevel(c.level)),
			fmt.Sprintf("effect_level 名称推断(rule=%d, openlaw-level)", c.rule), c.lawID)
		if err != nil {
			fatal(err)
		}
		if aff, _ := res.RowsAffected(); aff == 1 {
			n++
		}
	}
	if err := tx.Commit(); err != nil {
		fatal(err)
	}
	fmt.Printf("已写入 %d 条（remark 留痕规则号，人工 confirm 可纠正）\n", n)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(2)
}
