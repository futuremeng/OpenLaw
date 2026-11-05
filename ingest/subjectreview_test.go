// subjectreview.go 测试：署名强制、词表 fail-closed、驳回后机器不顶回、
// 批量先干跑后执行且预览数不符即中止、空署名不算已复核（离线临时库）。
package ingest

import (
	"testing"
)

// applyMachineSubject 跑一轮机器写入（只写规则命中），返回批次号。
func applyMachineSubject(t *testing.T, m *SubjectMarker) string {
	t.Helper()
	cands, _, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	var high []SubjectCandidate
	for _, c := range cands {
		if c.Confidence == SubjectConfHigh {
			high = append(high, c)
		}
	}
	batch, _, err := m.Apply(high)
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func TestSubjectReviewStats(t *testing.T) {
	m := setupSubject(t)
	applyMachineSubject(t, m)
	st, err := m.ReviewStats()
	if err != nil {
		t.Fatal(err)
	}
	// 9 部：5 机器建议待复核 + 1 已人工确认（s_marked）+ 1 驳回留空（s_rej）+ 2 未标注
	if st.Total != 9 || st.SuggestPending != 5 || st.Confirmed != 1 ||
		st.LeftBlank != 1 || st.Unmarked != 2 {
		t.Errorf("统计不符: %+v", st)
	}
}

func TestSubjectReviewJudge(t *testing.T) {
	m := setupSubject(t)
	applyMachineSubject(t, m)
	// 缺署名/缺依据/未知动作：一律拒绝
	if _, err := m.Review([]string{"s_cri"}, "confirm", "CN-CRI", "", "依据"); err == nil {
		t.Error("缺复核人仍落判成功")
	}
	if _, err := m.Review([]string{"s_cri"}, "confirm", "CN-CRI", "张三", ""); err == nil {
		t.Error("缺结论依据仍落判成功")
	}
	if _, err := m.Review([]string{"s_cri"}, "approve", "CN-CRI", "张三", "依据"); err == nil {
		t.Error("未知动作仍落判成功")
	}
	// 词表外码 fail-closed：拒绝落判，不落自由文本
	if _, err := m.Review([]string{"s_cri"}, "confirm", "CN-OTHER", "张三", "依据"); err == nil {
		t.Error("词表外部门法码仍落判成功")
	}
	// 确认（沿用机器建议）
	sum, err := m.Review([]string{"s_cri"}, "confirm", "CN-CRI", "张三", "名称与体系一致")
	if err != nil || sum.Confirmed != 1 {
		t.Fatalf("确认失败: %v %+v", err, sum)
	}
	code, source, reviewer, _ := subjectOf(t, m, "s_cri")
	if code != "CN-CRI" || source != "manual" || reviewer != "张三" {
		t.Errorf("确认后列值不符: %s/%s/%s", code, source, reviewer)
	}
	logs, err := m.ReviewLog("s_cri")
	if err != nil || len(logs) < 2 {
		t.Fatalf("复核记录应含机器建议与确认: %v %v", logs, err)
	}
	// 改选部门法
	if _, err := m.Review([]string{"s_cri"}, "confirm", "CN-CIV", "李四", "实为民商"); err != nil {
		t.Fatal(err)
	}
	if code, _, _, _ = subjectOf(t, m, "s_cri"); code != "CN-CIV" {
		t.Errorf("改选未生效: %s", code)
	}
	// 驳回：留空但署名在，机器不得顶回
	if _, err := m.Review([]string{"s_amd"}, "reject", "", "李四", "名称前缀不可信"); err != nil {
		t.Fatal(err)
	}
	if code, _, reviewer, _ = subjectOf(t, m, "s_amd"); code != "" || reviewer != "李四" {
		t.Errorf("驳回后应留空且署名: %s/%s", code, reviewer)
	}
	applyMachineSubject(t, m) // 再跑一轮机器写入
	if code, _, reviewer, _ = subjectOf(t, m, "s_amd"); code != "" || reviewer != "李四" {
		t.Errorf("机器顶回了人工驳回结论: %s/%s", code, reviewer)
	}
	st, _ := m.ReviewStats()
	if st.LeftBlank != 2 {
		t.Errorf("LeftBlank=%d，应为 2", st.LeftBlank)
	}
	// 清除标注：留空并署名
	if _, err := m.Review([]string{"s_civ"}, "clear", "", "王五", "试清除"); err != nil {
		t.Fatal(err)
	}
	if code, _, reviewer, _ = subjectOf(t, m, "s_civ"); code != "" || reviewer != "王五" {
		t.Errorf("清除后应留空且署名: %s/%s", code, reviewer)
	}
	// 本来未标注的行：驳回/清除无意义，计入 Skipped 不写审计
	if sum, err := m.Review([]string{"s_none"}, "reject", "", "王五", "无建议可驳"); err != nil ||
		sum.Skipped != 1 || sum.Rejected != 0 {
		t.Errorf("未标注行驳回应计 Skipped: %+v %v", sum, err)
	}
}

func TestSubjectReviewBulk(t *testing.T) {
	m := setupSubject(t)
	applyMachineSubject(t, m)
	n, err := m.BulkReviewMatch(SubjectFilter{}, "confirm")
	if err != nil || n != 5 {
		t.Fatalf("干跑计数不符: n=%d err=%v", n, err)
	}
	if n, _ = m.BulkReviewMatch(SubjectFilter{Jurisdiction: "EU"}, "confirm"); n != 0 {
		t.Errorf("EU 法域无机器建议，应为 0，实际 %d", n)
	}
	// 预览数与实际不符：中止
	if _, err := m.BulkReview(SubjectFilter{}, "confirm", "张三", "批量确认", 99); err == nil {
		t.Error("预览数不符仍执行")
	}
	// 缺署名：拒绝
	if _, err := m.BulkReview(SubjectFilter{}, "confirm", "", "依据", 5); err == nil {
		t.Error("批量缺复核人仍执行")
	}
	// 按法域缩小范围：确认 CN 的机器建议（各行沿用自己的建议码）
	if _, err := m.BulkReview(SubjectFilter{Jurisdiction: "CN"}, "confirm", "张三", "批量确认", 5); err != nil {
		t.Fatalf("按法域批量失败: %v", err)
	}
	if code, source, reviewer, _ := subjectOf(t, m, "s_cri"); code != "CN-CRI" ||
		source != "manual" || reviewer != "张三" {
		t.Errorf("批量确认结果不符: %s/%s/%s", code, source, reviewer)
	}
	if code, _, _, _ := subjectOf(t, m, "s_civ"); code != "CN-CIV" {
		t.Errorf("批量确认不得跨行套同一码: s_civ=%s", code)
	}
	// 已复核行不再进入批量范围
	if n, _ = m.BulkReviewMatch(SubjectFilter{}, "confirm"); n != 0 {
		t.Errorf("已复核行仍被批量匹配: %d", n)
	}
	// 驳回批量：剩余机器建议留空但署名
	if n, _ = m.BulkReviewMatch(SubjectFilter{}, "reject"); n != 0 {
		t.Errorf("confirm 后不应再有可驳回行: %d", n)
	}
}

// 空署名不得显示成已复核：subject_reviewer 空串与 NULL 同义，统计口径必须把它
// 留在「机器建议 · 待复核」，不得计入已人工确认（诚实展示纪律）。
func TestSubjectReviewBlankReviewerNotReviewed(t *testing.T) {
	m := setupSubject(t)
	if _, err := m.DB.Exec(`UPDATE law SET subject_code='CN-ADM', subject_source='auto',
		subject_reviewer='' WHERE law_id='s_marked'`); err != nil {
		t.Fatal(err)
	}
	st, err := m.ReviewStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Confirmed != 0 {
		t.Errorf("空署名行不得计入已人工确认，实际 %d", st.Confirmed)
	}
	if st.SuggestPending != 1 {
		t.Errorf("空署名行应留在待复核，实际 SuggestPending=%d", st.SuggestPending)
	}
}
