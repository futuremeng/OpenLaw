// CLI 落判壳测试：批量必须先计数再执行、执行必须带回计数、人工落判的行不得被批量推翻、
// 署名与依据缺失即拒绝。全部离线临时库，不依赖网络或真实语料。
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

// setupReviewCLI 建一个含机器建议 / 人工落判 / 未标注三种情形的临时库。
func setupReviewCLI(t *testing.T) *ingest.RegionMarker {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db", "openlaw.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mk := func(lawID, name, code, source, reviewer string) {
		t.Helper()
		var rv any // 未复核必须写 NULL：读侧把空串与 NULL 同义，但种子照真实写入形状
		if reviewer != "" {
			rv = reviewer
		}
		if _, err := db.Exec(`INSERT INTO law(law_id,name,region_code,region_source,region_reviewer)
			VALUES(?,?,?,?,?)`, lawID, name, code, source, rv); err != nil {
			t.Fatal(err)
		}
	}
	mk("c_sug1", "北京市河湖管理条例", "110000", "auto", "")
	mk("c_sug2", "河北省地质灾害防治条例", "130000", "auto", "")
	mk("c_human", "石家庄市水污染防治条例", "130100", "manual", "张三") // 批量绝不得碰
	mk("c_none", "中华人民共和国劳动法", "", "", "")
	return &ingest.RegionMarker{DB: db}
}

// rowState 读回一行的标注状态。
func rowState(t *testing.T, m *ingest.RegionMarker, lawID string) (code, source, reviewer string) {
	t.Helper()
	err := m.DB.QueryRow(`SELECT COALESCE(region_code,''), COALESCE(region_source,''),
		COALESCE(region_reviewer,'') FROM law WHERE law_id=?`, lawID).Scan(&code, &source, &reviewer)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestCLISplitIDs(t *testing.T) {
	got := splitIDs("a, b;c\n d ,")
	if len(got) != 4 || got[0] != "a" || got[3] != "d" {
		t.Errorf("条目编号解析异常: %q", got)
	}
	if len(splitIDs("  ")) != 0 {
		t.Error("空白输入应解析为空")
	}
}

// 批量执行必须带回计数：不带 -expected 就拒绝，绝不拿旧计数当授权。
func TestCLIBulkRequiresCount(t *testing.T) {
	m := setupReviewCLI(t)
	err := runBulkReview(m, reviewOpts{verdict: ingest.RegionActionConfirm,
		reviewer: "李四", reason: "名称含地区", apply: true, expected: -1})
	if err == nil || !strings.Contains(err.Error(), "-expected") {
		t.Fatalf("批量执行必须要求带回计数，实际: %v", err)
	}
	if _, src, rv := rowState(t, m, "c_sug1"); src != "auto" || rv != "" {
		t.Errorf("被拒绝的批量执行不得改动数据: %s/%s", src, rv)
	}
}

// 缺省只计数：不写库，并回显可直接执行的命令（含原范围）。
func TestCLIBulkDryRunNoWrite(t *testing.T) {
	m := setupReviewCLI(t)
	if err := runBulkReview(m, reviewOpts{verdict: ingest.RegionActionConfirm, province: "13"}); err != nil {
		t.Fatal(err)
	}
	if _, src, rv := rowState(t, m, "c_sug2"); src != "auto" || rv != "" {
		t.Errorf("计数一步不得写库: %s/%s", src, rv)
	}
}

// 执行按计数放行；每条沿用各自的机器建议；人工落判的行绝不入列。
func TestCLIBulkApplyKeepsOwnSuggestion(t *testing.T) {
	m := setupReviewCLI(t)
	if err := runBulkReview(m, reviewOpts{verdict: ingest.RegionActionConfirm,
		reviewer: "李四", reason: "名称含地区", apply: true, expected: 2}); err != nil {
		t.Fatal(err)
	}
	if code, src, rv := rowState(t, m, "c_sug1"); code != "110000" || src != "manual" || rv != "李四" {
		t.Errorf("确认后应沿用原建议码并署名，实际: %s/%s/%s", code, src, rv)
	}
	if _, _, rv := rowState(t, m, "c_human"); rv != "张三" {
		t.Error("人工落判过的行不得被批量推翻")
	}
	// 队列已变：再拿旧计数执行必须中止。
	if err := runBulkReview(m, reviewOpts{verdict: ingest.RegionActionConfirm,
		reviewer: "李四", reason: "名称含地区", apply: true, expected: 2}); err == nil {
		t.Error("计数与实际不符时必须中止")
	}
}

// 逐条驳回：留空但已复核；缺复核人即拒绝。
func TestCLIReviewReject(t *testing.T) {
	m := setupReviewCLI(t)
	if err := runReview(m, []string{"c_sug2"}, reviewOpts{verdict: ingest.RegionActionReject,
		reason: "名称不含地区"}); err == nil {
		t.Error("缺复核人必须拒绝落判")
	}
	if err := runReview(m, []string{"c_sug2"}, reviewOpts{verdict: ingest.RegionActionReject,
		reviewer: "李四", reason: "名称不含地区"}); err != nil {
		t.Fatal(err)
	}
	if code, src, rv := rowState(t, m, "c_sug2"); code != "" || src != "manual" || rv != "李四" {
		t.Errorf("驳回应留空但已复核，实际: %s/%s/%s", code, src, rv)
	}
	logs, err := m.ReviewLog("c_sug2")
	if err != nil || len(logs) == 0 {
		t.Fatalf("落判必须留复核记录: %v %v", logs, err)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "李四") {
		t.Errorf("复核记录必须含复核人: %q", logs)
	}
}

// 未指定条目不得静默成功。
func TestCLIReviewNeedsIDs(t *testing.T) {
	m := setupReviewCLI(t)
	if err := runReview(m, nil, reviewOpts{verdict: ingest.RegionActionConfirm, code: "110000",
		reviewer: "李四", reason: "x"}); err == nil {
		t.Error("未指定条目必须报错")
	}
}

// 统计与面板同口径：待复核、已确认、已复核留空、未标注各自分开。
func TestCLIReviewStats(t *testing.T) {
	m := setupReviewCLI(t)
	if err := runReviewStats(m, false); err != nil {
		t.Fatal(err)
	}
	st, err := m.ReviewStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 4 || st.SuggestPending != 2 || st.Confirmed != 1 || st.Unmarked != 1 {
		t.Errorf("统计口径异常: %+v", st)
	}
}
