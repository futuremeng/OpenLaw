// openlaw-region：法规地域标引（parse.SuggestRegion → ingest.RegionMarker）。
// 默认 dry-run：只产出 CSV 报告（含已标注/高置信/中置信/冲突/无法判定 全量分类）与计数，
// 绝不写库。-apply：仅把**高置信**建议写入 law.region_*——region_reviewer 一律留空
// （= 机器建议 · 待复核，界面必须如实显示），并逐笔写 region_log 审计。
// 中置信（发布机关）与冲突一律不写库（docs/design-region-write.md §9.2 拍板）。
// -undo <batch_id>：按批次撤销机器建议；默认同样只计数，须加 -apply 才执行；
// 已被人工落判的行不会被回滚。
// -check：一致性自检（§6）——只报告矛盾项（地方性法规未标地域、国家级效力却标地域、
// 效力等级未标注却标地域、is_local 与地域矛盾），逐条出 CSV，绝不写库、绝不自动改数。
// 人工落判（与桌面「地域标引」面板同一库层入口 ingest.RegionMarker，不是第二条写路径）：
// -stats 复核面统计；-review <条目编号[,编号…]> 逐条落判（-verdict confirm/reject/clear，
// confirm 须 -code；-reviewer 与 -reason 必填，库层同样强制）；-bulk 批量落判
// （只挑「机器建议 · 待复核」，可按 -province 收窄）——缺省只计数，执行必须加
// -apply -expected <上一步计数>：计数变了即中止，不拿旧计数当授权。
package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	var (
		dataDir     = flag.String("data", "data", "数据目录")
		report      = flag.String("report", "region_report.csv", "CSV 报告输出路径")
		apply       = flag.Bool("apply", false, "写入数据库（缺省仅 dry-run 报告）")
		undo        = flag.String("undo", "", "撤销指定批次的机器建议（加 -apply 才真正执行）")
		check       = flag.Bool("check", false, "一致性自检：只报告矛盾项，不写库、不自动改")
		checkReport = flag.String("check-report", "region_audit.csv", "一致性自检 CSV 报告路径")
		stats       = flag.Bool("stats", false, "复核面统计（与桌面「地域标引」面板同口径）")
		review      = flag.String("review", "", "人工落判：条目编号（逗号分隔可小批）；须配 -verdict/-reviewer/-reason，confirm 还须 -code")
		bulk        = flag.Bool("bulk", false, "批量落判：只挑「机器建议 · 待复核」，可按 -province 收窄；缺省只计数，执行须 -apply -expected <条数>")
		province    = flag.String("province", "", "-bulk：省/市码前缀（如 13 或 1301）")
		verdict     = flag.String("verdict", ingest.RegionActionConfirm, "落判结论：confirm 确认 / reject 驳回机器建议 / clear 清除标注")
		code        = flag.String("code", "", "confirm 的区划码（改选地区时填新地区）")
		reviewer    = flag.String("reviewer", "", "复核人署名（落判必填）")
		reason      = flag.String("reason", "", "结论依据（落判必填）")
		expected    = flag.Int("expected", -1, "-bulk -apply：须等于上一步计数的条数，数变了即中止")
		asJSON      = flag.Bool("json", false, "统计以 JSON 输出")
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

	if *stats {
		if err := runReviewStats(m, *asJSON); err != nil {
			fatal(err)
		}
		return
	}

	if *review != "" {
		o := reviewOpts{verdict: *verdict, code: *code, reviewer: *reviewer, reason: *reason}
		if err := runReview(m, splitIDs(*review), o); err != nil {
			fatal(err)
		}
		return
	}

	if *bulk {
		o := reviewOpts{verdict: *verdict, reviewer: *reviewer, reason: *reason,
			province: *province, expected: *expected, apply: *apply}
		if err := runBulkReview(m, o); err != nil {
			fatal(err)
		}
		return
	}

	if *check {
		audit, err := m.Audit()
		if err != nil {
			fatal(err)
		}
		if err := writeAuditReport(*checkReport, audit); err != nil {
			fatal(err)
		}
		if *asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"total":                    audit.Total,
				"local_no_region":          audit.LocalNoRegion,
				"national_with_region":     audit.NationalWithRegion,
				"level_blank_with_region":  audit.LevelBlankWithRegion,
				"flag_local_no_region":     audit.FlagLocalNoRegion,
				"flag_level_local_no_flag": audit.FlagLevelLocalNoFlag,
				"flag_code_no_basis":       audit.FlagCodeNoBasis,
				"rows":                     len(audit.Rows),
				"report":                   *checkReport,
			})
		} else {
			fmt.Printf("一致性自检（只报告、不自动改）：法规 %d 部\n", audit.Total)
			fmt.Printf("  地方性法规/地方政府规章未标地域 %d | 国家级效力却标地域 %d | 效力等级未标注却标地域 %d\n",
				audit.LocalNoRegion, audit.NationalWithRegion, audit.LevelBlankWithRegion)
			fmt.Printf("  标记为地方效力但未标地域 %d | 效力等级属地方级但 is_local 未标记 %d | 已标地域但无地方效力依据 %d\n",
				audit.FlagLocalNoRegion, audit.FlagLevelLocalNoFlag, audit.FlagCodeNoBasis)
			fmt.Printf("报告: %s（矛盾项 %d 条逐条列出）\n", *checkReport, len(audit.Rows))
			fmt.Println("提示：is_local 在语料中不可靠，只作线索不作结论；矛盾项一律交人工在桌面「地域标引」面板落判，机器不自动改。")
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

// reviewOpts 落判参数。CLI 与测试走同一条路径，避免测试绕开真实分支。
type reviewOpts struct {
	verdict  string
	code     string
	reviewer string
	reason   string
	province string
	expected int
	apply    bool
}

// splitIDs 逗号/空白分隔的条目编号（CLI 小批落判用）。
func splitIDs(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	}) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// runReviewStats 复核面统计：与桌面「地域标引」面板同一口径（ingest.RegionMarker.ReviewStats）。
func runReviewStats(m *ingest.RegionMarker, asJSON bool) error {
	st, err := m.ReviewStats()
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(st)
	}
	fmt.Printf("法规 %d：机器建议 · 待复核 %d | 已人工确认 %d | 已复核留空（驳回/清除）%d | 未标注且未复核 %d\n",
		st.Total, st.SuggestPending, st.Confirmed, st.LeftBlank, st.Unmarked)
	fmt.Println("确认/驳回/清除一律由人工落判：CLI 用 -review/-bulk，桌面用「地域标引」面板，同一库层入口。")
	return nil
}

// runReview 逐条/小批落判。复核人与结论依据由库层强制，CLI 只如实转达结果。
func runReview(m *ingest.RegionMarker, ids []string, o reviewOpts) error {
	if len(ids) == 0 {
		return errors.New("未指定条目：-review <条目编号>（逗号分隔可小批）")
	}
	sum, err := m.Review(ids, o.verdict, o.code, o.reviewer, o.reason)
	if err != nil {
		return err
	}
	fmt.Printf("落判结论 %s：确认 %d | 驳回 %d | 清除标注 %d | 跳过 %d（条目不存在或该行无需此结论）\n",
		o.verdict, sum.Confirmed, sum.Rejected, sum.Cleared, sum.Skipped)
	fmt.Println("复核人与结论依据已逐笔写入复核记录，可在桌面「地域标引」面板查看。")
	return nil
}

// runBulkReview 批量落判：缺省只计数（不写入）；执行必须带回上一步的计数——
// 计数变了说明队列已变，不拿旧计数当授权（库层 BulkReview 同样强制）。
// 范围固定为「机器建议 · 待复核」：人工已落判的行绝不批量推翻。
func runBulkReview(m *ingest.RegionMarker, o reviewOpts) error {
	f := ingest.RegionFilter{Province: o.province, OnlySuggest: true}
	if !o.apply {
		n, err := m.BulkReviewMatch(f, o.verdict)
		if err != nil {
			return err
		}
		scope := "全部省份"
		if p := strings.TrimSpace(o.province); p != "" {
			scope = "区划码前缀 " + p
		}
		fmt.Printf("批量落判结论 %s（只挑「机器建议 · 待复核」，%s）：%d 条待流转\n", o.verdict, scope, n)
		fmt.Printf("未写入。确认后执行：openlaw-region -bulk -apply -expected %d -verdict %s "+
			"-reviewer <复核人> -reason <结论依据>%s\n",
			n, o.verdict, provinceHint(o.province))
		return nil
	}
	if o.expected < 0 {
		return errors.New("批量落判必须先计数再执行：加 -apply 时必须带 -expected <上一步计数的条数>")
	}
	sum, err := m.BulkReview(f, o.verdict, o.reviewer, o.reason, o.expected)
	if err != nil {
		return err
	}
	fmt.Printf("已批量落判 %s：确认 %d | 驳回 %d | 清除标注 %d | 跳过 %d\n",
		o.verdict, sum.Confirmed, sum.Rejected, sum.Cleared, sum.Skipped)
	fmt.Println("每条沿用各自的机器建议（确认不跨行套同一个区划码）；复核人与依据逐笔写入复核记录。")
	return nil
}

// provinceHint 回显 -bulk 计数命令时带上原范围，避免用户照抄命令却漏掉范围。
func provinceHint(p string) string {
	if s := strings.TrimSpace(p); s != "" {
		return " -province " + s
	}
	return ""
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

// writeAuditReport 一致性自检报告：逐条列出矛盾项与问题描述。只出报告，不改数据。
func writeAuditReport(path string, sum *ingest.RegionAuditSummary) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"law_id", "name", "effect_level", "is_local", "region_code", "issues"}); err != nil {
		return err
	}
	for _, it := range sum.Rows {
		if err := w.Write([]string{
			it.LawID, it.Name, it.EffectLevel, strconv.Itoa(it.IsLocal),
			it.RegionCode, strings.Join(it.Issues, "; "),
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
