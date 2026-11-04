// product_test.go 产品评审落地绑定的契约：发行版分档、粘贴即核验、检索诚实出路、
// 复核兑现与本机度量；台账只记种类与结论，绝不记内容。
package main

import (
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

func usageCount(t *testing.T, a *App, kind, outcome string) int {
	t.Helper()
	var n int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM usage_event WHERE kind=? AND outcome=?`, kind, outcome).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEditionSplit(t *testing.T) {
	a := newTestApp(t)
	code, note := a.editionInfo()
	if code != EditionFull || !strings.Contains(note, "数据维护版") {
		t.Errorf("默认必须是数据维护版: %q %q", code, note)
	}
	a.SetEdition("reader")
	code, note = a.editionInfo()
	if code != EditionReader {
		t.Fatalf("reader 档位未生效: %q", code)
	}
	if !strings.Contains(note, "只读查询版") || !strings.Contains(note, "不提供人工复核") {
		t.Errorf("档位说明必须讲清不给写路径入口: %q", note)
	}
	if got := a.AppEdition()["code"]; got != EditionReader {
		t.Errorf("AppEdition 绑定错: %v", got)
	}
	// 无法识别的档位：回落到维护版并如实留痕，不得静默。
	a.SetEdition("pro")
	code, note = a.editionInfo()
	if code != EditionFull {
		t.Errorf("未知档位应回落数据维护版: %q", code)
	}
	if !strings.Contains(note, "无法识别") {
		t.Errorf("未知档位必须在档位说明里留痕: %q", note)
	}
}

func TestVerifyCitationTextBinding(t *testing.T) {
	a := newTestApp(t)
	res, err := a.VerifyCitationText("《工伤保险条例》第一条 为了保障工伤职工，制定本条例。", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict == "" || len(res.NextSteps) == 0 {
		t.Fatalf("必须给出结论与下一步: %+v", res)
	}
	if res.Parsed.LawName != "工伤保险条例" {
		t.Errorf("法规名应由库内认定: %+v", res.Parsed)
	}
	if n := usageCount(t, a, store.EventVerify, res.Verdict); n != 1 {
		t.Errorf("核验结论必须入本机台账（只记结论）: %d", n)
	}
	// 显式时点优先于文中认出的时点，且必须声明来源。
	res, err = a.VerifyCitationText("《工伤保险条例》第一条 2003-01-01", "2025-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(res.NextSteps, "|"), "已按您指定的引用时点（2025-01-01）") {
		t.Errorf("显式时点必须声明: %+v", res.NextSteps)
	}
}

func TestSearchGuidedOutcomeLogged(t *testing.T) {
	a := newTestApp(t)
	v, err := a.SearchGuided("工伤保险", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Rows) != 1 || v.Guidance == nil || v.Guidance.Hits != 1 {
		t.Fatalf("结果与说明必须同源: %+v", v)
	}
	if v.Guidance.ZeroResult {
		t.Error("有命中不得标注零命中")
	}
	if n := usageCount(t, a, store.EventSearch, "ok"); n != 1 {
		t.Errorf("检索命中应记 ok: %d", n)
	}
	// 零命中：如实记 zero（用于聚合零结果率），且必须给出下一步。
	out, err := a.SearchGuided("完全不存在的词组", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Guidance.Suggestions) == 0 {
		t.Error("零命中必须给出可照做的下一步")
	}
	if n := usageCount(t, a, store.EventSearch, "zero"); n != 1 {
		t.Errorf("零命中应记 zero: %d", n)
	}
	var detail string
	if err := a.db.QueryRow(`SELECT detail FROM usage_event WHERE kind='search' LIMIT 1`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(detail, "工伤") {
		t.Error("台账不得记检索词")
	}
}

// TestSearchGuidedCappedOutcome 命中数达上限记 capped——「封顶」与「全部」必须可区分。
func TestSearchGuidedCappedOutcome(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.db.Exec(`INSERT INTO article(article_id,version_id,article_no,content,md_path,doc_id,parser_version)
		VALUES('A2','V1','第二条','工伤职工应当享受工伤保险待遇','md/govcn_x1.md','govcn_x1','t1')`); err != nil {
		t.Fatal(err)
	}
	v, err := a.SearchGuided("工伤", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Guidance.Capped {
		t.Fatalf("命中数达 topK 必须标注封顶: %+v", v.Guidance)
	}
	if n := usageCount(t, a, store.EventSearch, "capped"); n != 1 {
		t.Errorf("封顶应记 capped: %d", n)
	}
}

func TestReviewCoverageAndUsageReportBindings(t *testing.T) {
	a := newTestApp(t)
	c, err := a.ReviewCoverage(0)
	if err != nil {
		t.Fatal(err)
	}
	if c.PriorityTotal != len(ingest.PriorityLaws) || c.CorpusDocs == 0 {
		t.Errorf("复核兑现绑定错: %+v", c)
	}
	if !strings.Contains(c.Headline(), "已复核确认") || !strings.Contains(c.Headline(), "常用法规参考清单") {
		t.Errorf("复核兑现白话缺失: %q", c.Headline())
	}
	r, err := a.UsageReport()
	if err != nil {
		t.Fatal(err)
	}
	if !r.Available || r.Window <= 0 {
		t.Errorf("度量绑定必须给出窗口与可用性: %+v", r)
	}
	a.SetupRouteChosen("collect")
	if n := usageCount(t, a, store.EventSetupRoute, "collect"); n != 1 {
		t.Errorf("获取路线应入台账: %d", n)
	}
	a.SetupRouteChosen("随便写的") // 白名单外一律不记
	if n := usageCount(t, a, store.EventSetupRoute, "随便写的"); n != 0 {
		t.Errorf("白名单外的路线名不得入账: %d", n)
	}
}

// TestStatusCarriesProductFields 库总览必须自带档位与复核兑现白话——
// 面板不得各自再拼口径（口径不一是诚实展示缺陷）。
func TestStatusCarriesProductFields(t *testing.T) {
	a := newTestApp(t)
	a.SetEdition("reader")
	v, err := a.Status()
	if err != nil {
		t.Fatal(err)
	}
	if v.Edition != EditionReader || !strings.Contains(v.EditionNote, "只读查询版") {
		t.Errorf("状态未带档位: %+v", v)
	}
	if v.CoverageHeadline == "" {
		t.Error("状态未带复核兑现白话")
	}
}

// TestTaskEventMapping 任务终态入账的映射与状态口径：取消=partial，失败=fail。
func TestTaskEventMapping(t *testing.T) {
	a := newTestApp(t)
	a.logTaskEvent("govcn-import", "done")
	a.logTaskEvent("pack-install", "cancelled")
	a.logTaskEvent("agent-review", "failed")
	a.logTaskEvent("pack-build", "done") // 构建包不属于用户取数动作，不入账
	for _, c := range []struct {
		kind, outcome string
		want          int
	}{
		{store.EventFetch, "ok", 1},
		{store.EventPackInstall, "partial", 1},
		{store.EventReviewRun, "fail", 1},
	} {
		if n := usageCount(t, a, c.kind, c.outcome); n != c.want {
			t.Errorf("%s/%s 应记 %d 次，实际 %d", c.kind, c.outcome, c.want, n)
		}
	}
	var n int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM usage_event`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 4 { // app_open 1 + 上述 3
		t.Errorf("台账条数错（不得多记）: %d", n)
	}
}

// TestVerifyVerdictConstants 结论取值必须是库层六态，不得自造。
func TestVerifyVerdictConstants(t *testing.T) {
	a := newTestApp(t)
	res, err := a.VerifyCitationText("《不存在的法规》第一条", "")
	if err != nil {
		t.Fatal(err)
	}
	switch res.Verdict {
	case validate.VerdictNotFound, validate.VerdictIncomplete, validate.VerdictAmbiguous:
	default:
		t.Errorf("未收录法规的结论不在六态内: %q", res.Verdict)
	}
}

func TestVerifyCitationTextNoDB(t *testing.T) {
	a := &App{dataDir: t.TempDir(), openErr: "法规库未打开"}
	if _, err := a.VerifyCitationText("《测试法》第一条", ""); err == nil {
		t.Error("库未打开必须报错，不得返回空结论")
	}
	if _, err := a.SearchGuided("测试", 5); err == nil {
		t.Error("库未打开时检索必须报错")
	}
	if _, err := a.ReviewCoverage(5); err == nil {
		t.Error("库未打开时复核进度必须报错")
	}
	if _, err := a.UsageReport(); err == nil {
		t.Error("库未打开时度量必须报错")
	}
	a.logUsage(store.EventSearch, "ok", "") // 库未打开时写台账必须静默跳过，不得 panic
}
