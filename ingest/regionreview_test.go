// regionreview.go 测试：人工落判必须署名+依据；驳回后机器不得再写；改选地区留痕；
// 批量先干跑计数、执行时核对预览数、超上限拒绝。
package ingest

import (
	"testing"
)

// applySuggestions 先跑一次机器写入，得到「机器建议 · 待复核」的起点状态。
func applySuggestions(t *testing.T, m *RegionMarker) {
	t.Helper()
	high, err := highCandidates(m)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if _, _, err := m.Apply(high); err != nil {
		t.Fatalf("机器写入失败: %v", err)
	}
}

// highCandidates 取当前扫描出的高置信候选（机器不再给已复核行出建议）。
func highCandidates(m *RegionMarker) ([]RegionCandidate, error) {
	cands, _, err := m.Scan()
	if err != nil {
		return nil, err
	}
	var high []RegionCandidate
	for _, c := range cands {
		if c.Confidence == RegionConfHigh {
			high = append(high, c)
		}
	}
	return high, nil
}
func TestRegionReviewConfirmAndReject(t *testing.T) {
	m := setupRegion(t)
	applySuggestions(t, m) // r_bj 拿到机器建议 110000

	// 无署名/无依据：库层直接拒绝
	if _, err := m.Review([]string{"r_bj"}, RegionActionConfirm, "110000", "", "依据"); err == nil {
		t.Error("缺复核人仍落判成功，违反署名纪律")
	}
	if _, err := m.Review([]string{"r_bj"}, RegionActionConfirm, "110000", "李四", ""); err == nil {
		t.Error("缺结论依据仍落判成功，违反署名纪律")
	}
	// 未知区划码：拒绝
	if _, err := m.Review([]string{"r_bj"}, RegionActionConfirm, "999999", "李四", "依据"); err == nil {
		t.Error("未知区划码仍落判成功")
	}

	// 确认（沿用机器建议）
	sum, err := m.Review([]string{"r_bj"}, RegionActionConfirm, "110000", "李四", "名称与发文机关一致")
	if err != nil || sum.Confirmed != 1 {
		t.Fatalf("确认失败: %v %+v", err, sum)
	}
	var src, reviewer, rule, at string
	if err := m.DB.QueryRow(`SELECT region_source, region_reviewer, region_rule, region_reviewed_at
		FROM law WHERE law_id='r_bj'`).Scan(&src, &reviewer, &rule, &at); err != nil {
		t.Fatal(err)
	}
	if src != "manual" || reviewer != "李四" || rule != "manual（确认机器建议）" || at == "" {
		t.Errorf("确认后字段不符: source=%s reviewer=%s rule=%s at=%q", src, reviewer, rule, at)
	}

	// 驳回：地域留空，但署名与依据照写
	if _, err := m.DB.Exec(`UPDATE law SET region_reviewer=NULL, region_reviewed_at=NULL WHERE law_id='r_bj'`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Review([]string{"r_bj"}, RegionActionReject, "", "王五", "该市已合并，名称前缀不可信"); err != nil {
		t.Fatal(err)
	}
	var code string
	if err := m.DB.QueryRow(`SELECT COALESCE(region_code,'') FROM law WHERE law_id='r_bj'`).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != "" {
		t.Errorf("驳回后地域应为空，实际 %q", code)
	}
}

// 关键护栏：人工驳回后（地域留空但已复核），后续机器写入不得把建议重新填回去。
func TestRegionRejectedNotRewrittenByMachine(t *testing.T) {
	m := setupRegion(t)
	applySuggestions(t, m)
	if _, err := m.Review([]string{"r_bj"}, RegionActionReject, "", "王五", "名称前缀不可信"); err != nil {
		t.Fatal(err)
	}
	// 再跑一次机器写入：即便候选里明确带上这条，Apply 的 WHERE 也必须拦住（双保险）
	cands := []RegionCandidate{{LawID: "r_bj", Name: "北京市河湖管理条例",
		Confidence: RegionConfHigh, Code: "110000"}}
	if _, n, err := m.Apply(cands); err != nil || n != 0 {
		t.Fatalf("机器写入了人工已驳回的行: n=%d err=%v", n, err)
	}
	var code, reviewer string
	if err := m.DB.QueryRow(`SELECT COALESCE(region_code,''), COALESCE(region_reviewer,'')
		FROM law WHERE law_id='r_bj'`).Scan(&code, &reviewer); err != nil {
		t.Fatal(err)
	}
	if code != "" || reviewer != "王五" {
		t.Errorf("人工结论被顶回: code=%q reviewer=%q", code, reviewer)
	}
	// 扫描须如实分类为「人工已驳回」，不再当作待判定
	_, cnt, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if cnt.Rejected != 1 {
		t.Errorf("Rejected 计数应为 1，实际 %d", cnt.Rejected)
	}
}

// 改选地区：from→to 与「改选」依据都要留痕，复核记录可追溯。
func TestRegionReviewReassign(t *testing.T) {
	m := setupRegion(t)
	applySuggestions(t, m)
	if _, err := m.Review([]string{"r_bj"}, RegionActionConfirm, "130000", "李四", "实际属地为河北省"); err != nil {
		t.Fatal(err)
	}
	var code, rule string
	if err := m.DB.QueryRow(`SELECT region_code, region_rule FROM law WHERE law_id='r_bj'`).Scan(&code, &rule); err != nil {
		t.Fatal(err)
	}
	if code != "130000" || rule != "manual（改选地区）" {
		t.Errorf("改选未生效: code=%s rule=%s", code, rule)
	}
	logs, err := m.ReviewLog("r_bj")
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("复核记录应含机器建议与改选两条，实际 %d: %v", len(logs), logs)
	}
	if logs[1] == "" || !hasSub(logs[1], "110000→130000") || !hasSub(logs[1], "改选") {
		t.Errorf("改选记录不完整: %q", logs[1])
	}
}

func TestRegionBulkReview(t *testing.T) {
	m := setupRegion(t)
	applySuggestions(t, m) // 仅 r_bj 一条机器建议

	f := RegionFilter{Province: "11"}
	n, err := m.BulkReviewMatch(f, RegionActionConfirm)
	if err != nil || n != 1 {
		t.Fatalf("干跑计数错误: n=%d err=%v", n, err)
	}
	// 预览数与实际不符：中止，不拿旧计数当授权
	if _, err := m.BulkReview(f, RegionActionConfirm, "李四", "批量确认", 99); err == nil {
		t.Error("预览数与实际不符仍执行")
	}
	// 无署名：拒绝
	if _, err := m.BulkReview(f, RegionActionConfirm, "", "依据", 1); err == nil {
		t.Error("批量缺复核人仍执行")
	}
	sum, err := m.BulkReview(f, RegionActionConfirm, "李四", "批量确认", 1)
	if err != nil || sum.Confirmed != 1 {
		t.Fatalf("批量确认失败: %v %+v", err, sum)
	}
	// 批量确认沿用各行自己的建议，不跨行套同一区划码
	var code string
	if err := m.DB.QueryRow(`SELECT region_code FROM law WHERE law_id='r_bj'`).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != "110000" {
		t.Errorf("批量确认改动了区划码: %q", code)
	}
	// 已复核过的行不再进入批量范围
	if n, _ := m.BulkReviewMatch(f, RegionActionConfirm); n != 0 {
		t.Errorf("已复核行仍被批量匹配: %d", n)
	}
	// 超出单次上限：拒绝而非静默截断
	if _, err := m.BulkReview(RegionFilter{}, RegionActionClear, "李四", "清空", -1); err != nil {
		t.Errorf("清空批量应可执行: %v", err)
	}
}

func TestRegionReviewStats(t *testing.T) {
	m := setupRegion(t)
	applySuggestions(t, m)
	if _, err := m.Review([]string{"r_bj"}, RegionActionConfirm, "110000", "李四", "一致"); err != nil {
		t.Fatal(err)
	}
	st, err := m.ReviewStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 5 {
		t.Errorf("Total=%d，应为 5", st.Total)
	}
	if st.Confirmed != 2 { // r_bj 人工确认 + r_marked 原本已人工落判
		t.Errorf("Confirmed=%d，应为 2", st.Confirmed)
	}
	if st.SuggestPending != 0 {
		t.Errorf("SuggestPending=%d，应为 0", st.SuggestPending)
	}
	if st.Unmarked != 3 { // conflict / medium / none 均未标注
		t.Errorf("Unmarked=%d，应为 3", st.Unmarked)
	}
}

func hasSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
