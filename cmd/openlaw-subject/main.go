// openlaw-subject：部门法标引与文档形态补标（parse 规则 → ingest.SubjectMarker）。
// 默认 dry-run：只产出 CSV 报告（已标注/人工留空/规则命中/无法判定 全量分类）与计数，
// 绝不写库。-apply：部门法只写规则命中的高置信建议（subject_reviewer 留空 =
// 机器建议 · 待复核，界面必须如实显示）；文档形态全自动补判（无人工队列）。
// 逐笔写 subject_log 审计，批次号 subject-<UTC>；-undo <batch_id> 整批撤销
// （默认同样只计数，须加 -apply 才执行；人工落判过的行不会被回滚）。
// 「无法判定」是待办积压而非终态（拍板项 1 全覆盖目标）：残余条目转智能体建议
// 与人工落判队列，报告如实列出分布。
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	var (
		dataDir = flag.String("data", "data", "数据目录")
		report  = flag.String("report", "subject_report.csv", "CSV 报告输出路径")
		apply   = flag.Bool("apply", false, "写入数据库（缺省仅 dry-run 报告）")
		undo    = flag.String("undo", "", "撤销指定批次的机器写入（加 -apply 才真正执行）")
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
	m := &ingest.SubjectMarker{DB: db}

	if *undo != "" {
		n, err := m.Undo(*undo, *apply)
		if err != nil {
			fatal(err)
		}
		if *apply {
			fmt.Printf("已撤销批次 %s：%d 处写入回到未标注（人工落判过的行未被回滚）\n", *undo, n)
		} else {
			fmt.Printf("批次 %s 可撤销 %d 处。dry-run：未改库，确认后加 -apply 执行\n", *undo, n)
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
	fmt.Printf("共 %d 条：已标注 %d | 人工结论留空 %d | 规则命中可写 %d | 无法判定 %d；"+
		"文档形态有判定 %d | 判不出 %d\n",
		cnt.Total, cnt.AlreadyMarked, cnt.Rejected, cnt.High, cnt.Undecided,
		cnt.DocFormSuggested, cnt.DocFormUndecided)
	fmt.Printf("报告：%s（含已标注条目，便于核对覆盖面而不是只看命中数）\n", *report)
	if !*apply {
		fmt.Println("dry-run：未写库。核对报告后加 -apply 执行（部门法只写规则命中；文档形态全自动补判）")
		return
	}
	batchID, n, err := m.Apply(cands)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("已写入 %d 处（批次 %s）。机器写入一律「机器建议 · 待复核」，人工确认请走桌面面板。\n", n, batchID)
	fmt.Printf("如需撤销：openlaw-subject -undo %s -apply\n", batchID)
}

// writeReport 全量分类报告：已标注的也列出，便于核对覆盖面而不是只看命中数。
func writeReport(path string, cands []ingest.SubjectCandidate) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{
		"law_id", "name", "jurisdiction", "effect_level", "confidence",
		"subject_code", "subject_rule", "doc_form", "doc_form_rule", "note",
	}); err != nil {
		return err
	}
	for _, c := range cands {
		if err := w.Write([]string{
			c.LawID, c.Name, c.Jurisdiction, c.EffectLevel, c.Confidence,
			c.SubjectCode, c.SubjectRule, c.DocForm, c.DocFormRule, c.Note,
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
