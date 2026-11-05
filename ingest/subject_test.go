// subject.go 测试：试采只读、只写规则命中、文档形态独立补判、审计留痕、
// 按批次撤销、绝不覆盖人工落判（离线临时库，不依赖网络或真实语料）。
package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

func setupSubject(t *testing.T) *SubjectMarker {
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
	mk := func(lawID, name, jur, level string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO law(law_id,name,jurisdiction,effect_level) VALUES(?,?,?,?)`,
			lawID, name, jur, level); err != nil {
			t.Fatal(err)
		}
	}
	mk("s_cri", "中华人民共和国刑法", "CN", "法律")
	mk("s_amd", "中华人民共和国刑法修正案（十二）", "CN", "法律")
	mk("s_civ", "中华人民共和国公司法", "CN", "法律")
	mk("s_labor", "劳动合同法", "CN", "法律")
	mk("s_arb", "劳动争议调解仲裁法", "CN", "法律") // 社会法优先于程序法（规则顺序）
	mk("s_none", "反不正当竞争法", "CN", "法律")  // 名称规则判不出 → 待办积压
	mk("s_eu", "通用数据保护条例", "EU", "欧盟条例") // EU 无中文关键词规则 → 判不出
	mk("s_marked", "北京市河道管理条例", "CN", "地方性法规")
	if _, err := db.Exec(`UPDATE law SET subject_code='CN-ADM', subject_source='manual',
		subject_reviewer='张三', subject_reviewed_at='2026-01-01T00:00:00Z' WHERE law_id='s_marked'`); err != nil {
		t.Fatal(err)
	}
	mk("s_rej", "中华人民共和国治安管理处罚法", "CN", "法律")
	// 人工驳回后留空但署名仍在：机器不得再写部门法（文档形态是客观事实，照判）
	if _, err := db.Exec(`UPDATE law SET subject_reviewer='李四',
		subject_reviewed_at='2026-01-01T00:00:00Z' WHERE law_id='s_rej'`); err != nil {
		t.Fatal(err)
	}
	return &SubjectMarker{DB: db}
}

func subjectOf(t *testing.T, m *SubjectMarker, lawID string) (code, source, reviewer, docForm string) {
	t.Helper()
	err := m.DB.QueryRow(`SELECT COALESCE(subject_code,''), COALESCE(subject_source,''),
		COALESCE(subject_reviewer,''), COALESCE(doc_form,'') FROM law WHERE law_id=?`, lawID).
		Scan(&code, &source, &reviewer, &docForm)
	if err != nil {
		t.Fatal(err)
	}
	return
}

// TestSubjectScan 分类计数如实分开，扫描不改库。
func TestSubjectScan(t *testing.T) {
	m := setupSubject(t)
	cands, cnt, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 9 || cnt.Total != 9 {
		t.Fatalf("应扫出 9 条，实得 %d (%+v)", len(cands), cnt)
	}
	if cnt.High != 5 || cnt.AlreadyMarked != 1 || cnt.Rejected != 1 || cnt.Undecided != 2 {
		t.Errorf("分类计数失真: %+v", cnt)
	}
	if cnt.DocFormSuggested != 9 || cnt.DocFormUndecided != 0 {
		t.Errorf("文档形态应全部有判定: %+v", cnt)
	}
	for _, c := range cands {
		switch c.LawID {
		case "s_arb":
			if c.SubjectCode != "CN-SOC" { // 劳动(S4) 先于 仲裁(S6)
				t.Errorf("劳动争议调解仲裁法应判社会法，实得 %s", c.SubjectCode)
			}
		case "s_amd":
			if c.SubjectCode != "CN-CRI" || c.DocForm != "CN-AMD" {
				t.Errorf("刑法修正案应判 CN-CRI/CN-AMD，实得 %s/%s", c.SubjectCode, c.DocForm)
			}
		case "s_eu":
			if c.SubjectCode != "" || c.DocForm != "EU-REG" {
				t.Errorf("EU 条目部门法不得猜，形态应判 EU-REG，实得 %q/%s", c.SubjectCode, c.DocForm)
			}
		case "s_marked":
			if c.Confidence != SubjectConfMarked || c.DocForm != "CN-REG" {
				t.Errorf("已标注行应 marked 且文档形态独立补判: %+v", c)
			}
		case "s_rej":
			if c.Confidence != SubjectConfRejected {
				t.Errorf("人工驳回行应为 rejected，实得 %s", c.Confidence)
			}
		}
	}
}

// TestSubjectApplyUndo 只写规则命中与缺值形态；人工结论不被顶回；审计留痕；整批可撤销。
func TestSubjectApplyUndo(t *testing.T) {
	m := setupSubject(t)
	cands, _, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	batchID, n, err := m.Apply(cands)
	if err != nil {
		t.Fatal(err)
	}
	if want := 5 + 9; n != want { // 5 处部门法 + 9 处文档形态
		t.Fatalf("写入条数应为 %d，实得 %d", want, n)
	}
	if code, src, rev, _ := subjectOf(t, m, "s_cri"); code != "CN-CRI" || src != "auto" || rev != "" {
		t.Errorf("机器写入应 auto 且 reviewer 留空: %s/%s/%s", code, src, rev)
	}
	if _, _, _, df := subjectOf(t, m, "s_none"); df != "CN-LAW" {
		t.Errorf("部门法判不出的条目文档形态仍应自动补判: %s", df)
	}
	if code, src, rev, df := subjectOf(t, m, "s_marked"); code != "CN-ADM" || src != "manual" || rev != "张三" || df != "CN-REG" {
		t.Errorf("人工落判的部门法不得被顶回，形态应补判: %s/%s/%s/%s", code, src, rev, df)
	}
	if code, _, rev, df := subjectOf(t, m, "s_rej"); code != "" || rev != "李四" || df != "CN-LAW" {
		t.Errorf("驳回行部门法应保持留空且署名在，形态照判: %s/%s/%s", code, rev, df)
	}
	var logs int
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM subject_log WHERE batch_id=? AND action='suggest'
		AND actor='openlaw-subject'`, batchID).Scan(&logs); err != nil || logs != 14 {
		t.Fatalf("审计应为 14 笔机器写入: %d (%v)", logs, err)
	}
	// 干跑计数 → -apply 撤销 → 回到原样（人工结论仍未动）。
	if k, err := m.Undo(batchID, false); err != nil || k != 14 {
		t.Fatalf("干跑应计 14: %d (%v)", k, err)
	}
	if k, err := m.Undo(batchID, true); err != nil || k != 14 {
		t.Fatalf("撤销应执行 14: %d (%v)", k, err)
	}
	if code, _, _, df := subjectOf(t, m, "s_cri"); code != "" || df != "" {
		t.Errorf("撤销后应回到未标注: %s/%s", code, df)
	}
	if code, src, rev, _ := subjectOf(t, m, "s_marked"); code != "CN-ADM" || src != "manual" || rev != "张三" {
		t.Errorf("撤销不得回滚人工落判: %s/%s/%s", code, src, rev)
	}
	if code, _, rev, _ := subjectOf(t, m, "s_rej"); code != "" || rev != "李四" {
		t.Errorf("撤销不得动驳回行: %s/%s", code, rev)
	}
}

// TestSubjectSuggestOneSamePath 单条建议与全量扫描同一份实现（面板与批量口径一致）。
func TestSubjectSuggestOneSamePath(t *testing.T) {
	m := setupSubject(t)
	c, err := m.SuggestOne("s_arb")
	if err != nil {
		t.Fatal(err)
	}
	if c.SubjectCode != "CN-SOC" || c.Confidence != SubjectConfHigh || c.DocForm != "CN-LAW" {
		t.Errorf("单条建议与扫描口径不一致: %+v", c)
	}
	if _, err := m.SuggestOne("不存在"); err == nil {
		t.Errorf("不存在的条目必须报错，不得返回空建议")
	}
}
