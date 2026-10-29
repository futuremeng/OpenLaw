// openlaw-region：法规地域标引（parse.SuggestRegion → ingest.RegionMarker）。
// 默认 dry-run：只产出 CSV 报告（含已标注/高置信/中置信/冲突/无法判定 全量分类）与计数，
// 绝不写库。-apply：仅把**高置信**建议写入 law.region_*——region_reviewer 一律留空
// （= 机器建议 · 待复核，界面必须如实显示），并逐笔写 region_log 审计。
// 中置信（发布机关）与冲突一律不写库（docs/design-region-write.md §9.2 拍板）。
// -undo <batch_id>：按批次撤销机器建议；默认同样只计数，须加 -apply 才执行；
// 已被人工落判的行不会被回滚。
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	var (
		dataDir = flag.String("data", "data", "数据目录")
		report  = flag.String("report", "region_report.csv", "CSV 报告输出路径")
		apply   = flag.Bool("apply", false, "写入数据库（缺省仅 dry-run 报告）")
		undo    = flag.String("undo", "", "撤销指定批次的机器建议（加 -apply 才真正执行）")
		asJSON  = flag.Bool("json", false, "统计以 JSON 输出")
	)
	flag.Parse()

	if err := os.MkdirAll(filepath.Join(*dataDir, "db"), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	m := &ingest.RegionMarker{DB: db}

	if *undo != "" {
		n, err := m.Undo(*undo, *apply)
		if err != nil {
			fatal(err)
		}
		if *apply {
			fmt.Printf("已撤销批次 %s：%d 条回到未标注（人工落判过的行未被回滚）\n", *undo, n)
		} else {
			fmt.Printf("批次 %s 可撤销 %d 条。dry-run：未改库，确认后加 -apply 执行\n", *undo, n)
		}
		return
	}

	cands, cnt, err := m.Scan()
	if err != nil {
		fatal(err)
	}
	if err := writeReport(*report, cands); err != nil {
		fatal(err)
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"total": cnt.Total, "already_marked": cnt.AlreadyMarked,
			"high": cnt.High, "medium": cnt.Medium,
			"conflict": cnt.Conflict, "national": cnt.National,
			"undecided": cnt.Undecided,
			"report":    *report, "applied": *apply,
		})
	} else {
		fmt.Printf("法规 %d：已标注 %d | 高置信建议 %d | 中置信（仅报告，不写库）%d | 冲突转人工 %d | 国家级不按名称标 %d | 无法判定 %d\n",
			cnt.Total, cnt.AlreadyMarked, cnt.High, cnt.Medium, cnt.Conflict, cnt.National, cnt.Undecided)
		fmt.Printf("报告: %s\n", *report)
	}
	if !*apply {
		fmt.Println("dry-run：未写库。核对报告后加 -apply 执行（只写高置信）")
		return
	}

	batchID, n, err := m.Apply(cands)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("已写入 %d 条：region_source=auto、复核人留空（界面显示「机器建议 · 待复核」）\n", n)
	fmt.Printf("批次号 %s；如需撤销：openlaw-region -undo %s -apply\n", batchID, batchID)
}

// writeReport 全量分类报告：已标注的也列出，便于核对覆盖率而不是只看命中数。
func writeReport(path string, cands []ingest.RegionCandidate) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{
		"law_id", "name", "issuer", "effect_level", "confidence",
		"region_code", "region_path", "rule", "note",
	}); err != nil {
		return err
	}
	for _, c := range cands {
		if err := w.Write([]string{
			c.LawID, c.Name, c.Issuer, c.EffectLevel, c.Confidence,
			c.Code, c.RegionPath, strconv.Itoa(c.Rule), c.Note,
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(2)
}
