// 「部门法标引」复核面板绑定测试：队列排序、状态口径、署名强制、改选、驳回留痕、
// 词表 fail-closed、批量先干跑后执行且预览数不符即中止。全部离线临时语料。
package main

import (
	"testing"

	"github.com/futuremeng/OpenLaw/ingest"
)

// newSubjectReviewApp 临时库 + 各类部门法判定情形，并先跑一次机器写入。
func newSubjectReviewApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	seed := []string{
		`INSERT INTO law(law_id,name,issuer,effect_level,jurisdiction) VALUES('SR1','中华人民共和国劳动法','全国人民代表大会常务委员会','法律','CN')`,
		`INSERT INTO law(law_id,name,issuer,effect_level,jurisdiction) VALUES('SR2','中华人民共和国刑法','全国人民代表大会','法律','CN')`,
		`INSERT INTO law(law_id,name,issuer,effect_level,jurisdiction) VALUES('SR3','反不正当竞争法','全国人大常委会','法律','CN')`, // 名称规则判不出
		`INSERT INTO law(law_id,name,issuer,effect_level,jurisdiction) VALUES('SR4','通用数据保护条例','欧洲议会','欧盟条例','EU')`, // EU 无中文规则 → 判不出
	}
	for _, s := range seed {
		if _, err := a.db.Exec(s); err != nil {
			t.Fatalf("种子失败: %v", err)
		}
	}
	m := &ingest.SubjectMarker{DB: a.db}
	cands, _, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	var high []ingest.SubjectCandidate
	for _, c := range cands {
		if c.Confidence == ingest.SubjectConfHigh {
			high = append(high, c)
		}
	}
	if _, n, err := m.Apply(high); err != nil || n == 0 {
		t.Fatalf("机器写入异常: n=%d err=%v", n, err)
	}
	return a
}

func TestSubjectReviewQueue(t *testing.T) {
	a := newSubjectReviewApp(t)
	opts, err := a.SubjectOptions()
	if err != nil || len(opts) < 7 {
		t.Fatalf("词表下拉应非空: %d %v", len(opts), err)
	}
	st, err := a.SubjectReviewStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.SuggestPending != 2 || st.Confirmed != 0 || st.LeftBlank != 0 || st.Unmarked != 3 {
		t.Errorf("统计不符: %+v", st) // Unmarked 含 newTestApp 种子 L1（判不出）
	}
	rows, err := a.SubjectReviewList("", "suggest", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("机器建议队列应 2 条，实际 %d", len(rows))
	}
	for _, r := range rows {
		if r.State != "suggest" || r.Code == "" || r.SubjectName == "" {
			t.Errorf("机器建议行状态/部门法名不符: %+v", r)
		}
	}
	// 默认队列把机器建议排在未标注之前
	all, err := a.SubjectReviewList("", "", 50, 0)
	if err != nil || len(all) == 0 {
		t.Fatalf("队列读取失败: %v", err)
	}
	if all[0].State != "suggest" {
		t.Errorf("首行应为机器建议待复核，实际 %q", all[0].State)
	}
	// 未标注行必须如实说明「为什么没写」
	d, err := a.SubjectReviewDetail("SR3")
	if err != nil {
		t.Fatal(err)
	}
	if d.Row.State != "unmarked" || d.Row.Code != "" || d.Suggest.Confidence != "none" || d.Suggest.Note == "" {
		t.Errorf("判不出未标注行说明缺失: %+v", d)
	}
	if len(d.Log) != 0 {
		t.Errorf("尚无复核记录时不应有记录: %v", d.Log)
	}
}

func TestSubjectReviewJudge(t *testing.T) {
	a := newSubjectReviewApp(t)
	// 无署名/无依据/未选条目：拒绝
	if _, err := a.JudgeSubject([]string{"SR1"}, "CN-SOC", "confirm", "", "依据"); err == nil {
		t.Error("缺复核人仍落判成功")
	}
	if _, err := a.JudgeSubject([]string{"SR1"}, "CN-SOC", "confirm", "李四", ""); err == nil {
		t.Error("缺依据仍落判成功")
	}
	if _, err := a.JudgeSubject(nil, "CN-SOC", "confirm", "李四", "依据"); err == nil {
		t.Error("未选条目仍落判成功")
	}
	// 词表外码：拒绝（fail-closed）
	if _, err := a.JudgeSubject([]string{"SR1"}, "CN-OTHER", "confirm", "李四", "依据"); err == nil {
		t.Error("词表外部门法码仍落判成功")
	}
	// 确认（沿用机器建议）
	sum, err := a.JudgeSubject([]string{"SR1"}, "CN-SOC", "confirm", "李四", "名称与体系一致")
	if err != nil || sum.Confirmed != 1 {
		t.Fatalf("确认失败: %v %+v", err, sum)
	}
	d, err := a.SubjectReviewDetail("SR1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Row.State != "reviewed" || d.Row.Reviewer != "李四" || d.Row.SubjectName != "社会法" {
		t.Errorf("确认后状态不符: %+v", d.Row)
	}
	if len(d.Log) < 2 {
		t.Errorf("复核记录应含机器建议与确认，实际 %d 条", len(d.Log))
	}
	// 改选部门法
	if _, err := a.JudgeSubject([]string{"SR2"}, "CN-CIV", "confirm", "王五", "实为民商"); err != nil {
		t.Fatal(err)
	}
	if d, _ = a.SubjectReviewDetail("SR2"); d.Row.Code != "CN-CIV" {
		t.Errorf("改选未生效: %q", d.Row.Code)
	}
	// 驳回：留空但已复核
	if _, err := a.JudgeSubject([]string{"SR1"}, "", "reject", "王五", "规则不可信"); err != nil {
		t.Fatal(err)
	}
	if d, _ = a.SubjectReviewDetail("SR1"); d.Row.Code != "" || d.Row.State != "reviewed" {
		t.Errorf("驳回后应留空且已复核: %+v", d.Row)
	}
	st, _ := a.SubjectReviewStats()
	if st.LeftBlank != 1 {
		t.Errorf("LeftBlank=%d，应为 1", st.LeftBlank)
	}
}

func TestSubjectReviewBulk(t *testing.T) {
	a := newSubjectReviewApp(t)
	n, err := a.SubjectBulkPreview("", "confirm")
	if err != nil || n != 2 {
		t.Fatalf("干跑计数不符: n=%d err=%v", n, err)
	}
	if n, _ = a.SubjectBulkPreview("US", "confirm"); n != 0 {
		t.Errorf("US 法域无机器建议，应为 0，实际 %d", n)
	}
	// 预览数与实际不符：中止
	if _, err := a.SubjectBulkApply("", "confirm", "李四", "批量确认", 99); err == nil {
		t.Error("预览数不符仍执行")
	}
	// 无署名：拒绝
	if _, err := a.SubjectBulkApply("", "confirm", "", "依据", 2); err == nil {
		t.Error("批量缺复核人仍执行")
	}
	// 按法域缩小范围：各行沿用自己的机器建议码
	if _, err := a.SubjectBulkApply("CN", "confirm", "李四", "批量确认", 2); err != nil {
		t.Fatalf("按法域批量失败: %v", err)
	}
	var code, reviewer string
	if err := a.db.QueryRow(`SELECT subject_code, subject_reviewer FROM law WHERE law_id='SR2'`).
		Scan(&code, &reviewer); err != nil {
		t.Fatal(err)
	}
	if code != "CN-CRI" || reviewer != "李四" {
		t.Errorf("批量确认结果不符: code=%s reviewer=%s", code, reviewer)
	}
	// 已复核行不再进入批量范围；清除批量此时无可处理条目（如实 0，不硬凑数字）
	if n, _ = a.SubjectBulkPreview("CN", "confirm"); n != 0 {
		t.Errorf("已复核行仍被批量匹配: %d", n)
	}
	if n, _ = a.SubjectBulkPreview("", "clear"); n != 0 {
		t.Errorf("全部机器建议已复核后 clear 应为 0，实际 %d", n)
	}
}

// 空署名不得显示成已复核：subject_reviewer 空串与 NULL 同义，队列与状态口径都必须
// 把它留在「机器建议 · 待复核」，不得渲染成已人工复核。
func TestSubjectReviewBlankReviewerNotReviewed(t *testing.T) {
	a := newSubjectReviewApp(t)
	if _, err := a.db.Exec(`INSERT INTO law(law_id,name,jurisdiction,subject_code,subject_source,subject_reviewer)
		VALUES('SR5','社会保险法','CN','CN-SOC','auto','')`); err != nil {
		t.Fatal(err)
	}
	rows, err := a.SubjectReviewList("", "suggest", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range rows {
		if r.LawID != "SR5" {
			continue
		}
		found = true
		if r.State != "suggest" {
			t.Errorf("空署名行状态应为 suggest，实际 %q", r.State)
		}
	}
	if !found {
		t.Error("空署名行必须留在待复核队列")
	}
	reviewed, err := a.SubjectReviewList("", "reviewed", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reviewed {
		if r.LawID == "SR5" {
			t.Error("空署名行不得出现在已复核队列")
		}
	}
	st, err := a.SubjectReviewStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Confirmed != 0 {
		t.Errorf("空署名行不得计入已人工确认，实际 %d", st.Confirmed)
	}
}
