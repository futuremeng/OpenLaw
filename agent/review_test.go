// review_test.go 双智能体自审核测试（离线：桩 Chat + 临时目录语料）。
// 覆盖：高置信自动确认（confirm_source=agent + review_log + agent_review 留痕）、
// 低置信转人工、坏 JSON fail-closed、ctx 取消即停且如实报数。
package agent

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

type fakeChat struct {
	resp []string
	i    int
}

func (f *fakeChat) Complete(ctx context.Context, system, user string) (string, error) {
	if f.i >= len(f.resp) {
		return "", sql.ErrNoRows
	}
	r := f.resp[f.i]
	f.i++
	return r, nil
}

// setupCorpus 落一个最小语料库：n 部 pending 法规（含条文文本，供首尾摘要取列）。
func setupCorpus(t *testing.T, names ...string) (*store.DB, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, name := range names {
		docID := "doc_" + name
		if _, err := db.Exec(`INSERT INTO law(law_id,name,issuer,effect_level) VALUES(?,?,?,?)`,
			"law_"+name, name, "国务院", "行政法规"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at,publish_date,source_type)
			VALUES(?,?,?,?,?,?,?,?)`, docID, docID+".pdf", "pdf/"+docID+".pdf", 1, "aa",
			"2026-01-01T00:00:00Z", "2020-01-01", "govcn"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO law_version(version_id,law_id,version_no,effective_date,doc_id)
			VALUES(?,?,?,?,?)`, "v_"+name, "law_"+name, "2020版", "2020-01-01", docID); err != nil {
			t.Fatal(err)
		}
		for _, a := range []struct{ no, content string }{
			{"第一条", "第一条　为了规范测试活动，制定本条例。"},
			{"第二条", "第二条　本条例自2020年1月1日起施行。"},
		} {
			if _, err := db.Exec(`INSERT INTO article(article_id,version_id,article_no,content,md_path,doc_id,parser_version)
				VALUES(?,?,?,?,?,?,?)`, "a_"+name+"_"+a.no, "v_"+name, a.no, a.content,
				"articles/"+name+".md", docID, "test-v1"); err != nil {
				t.Fatal(err)
			}
		}
	}
	return db, dir
}

const okClassify = `{"effect_level":"行政法规","issuer":"国务院","publish_date":"2020-01-01","region_code":"","confidence":0.95,"reason":"名称与原文标题一致"}`
const okAudit = `{"verdict":"confirm","confidence":0.93,"reason":"原文与元数据一致，无风险标记"}`
const lowAudit = `{"verdict":"confirm","confidence":0.5,"reason":"大体一致但个别日期存疑"}`

func statusOf(t *testing.T, db *store.DB, docID string) (string, string) {
	t.Helper()
	var st, src string
	if err := db.QueryRow(`SELECT confirm_status, COALESCE(confirm_source,'') FROM document WHERE doc_id=?`, docID).
		Scan(&st, &src); err != nil {
		t.Fatal(err)
	}
	return st, src
}

func TestRunReviewAutoConfirm(t *testing.T) {
	db, dir := setupCorpus(t, "甲条例")
	chat := &fakeChat{resp: []string{"```json\n" + okClassify + "\n```", okAudit}}
	sum, err := RunReview(context.Background(), dir, db, chat, "test-model", nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Confirmed != 1 || sum.Escalated != 0 {
		t.Fatalf("应自动确认 1 条，实际 %+v", sum)
	}
	st, src := statusOf(t, db, "doc_甲条例")
	if st != "confirmed" || src != "agent" {
		t.Errorf("应 confirmed/agent，实际 %s/%s", st, src)
	}
	var reviewer, reason string
	if err := db.QueryRow(`SELECT reviewer, COALESCE(reason,'') FROM review_log WHERE doc_id='doc_甲条例'
		ORDER BY review_id DESC LIMIT 1`).Scan(&reviewer, &reason); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reviewer, "test-model") || !strings.Contains(reason, "双智能体") {
		t.Errorf("审计必须带模型署名与双智能体依据，实际 %q / %q", reviewer, reason)
	}
	var verdict, model string
	var conf float64
	if err := db.QueryRow(`SELECT verdict, confidence, model FROM agent_review WHERE doc_id='doc_甲条例'`).
		Scan(&verdict, &conf, &model); err != nil {
		t.Fatal(err)
	}
	if verdict != "confirm" || model != "test-model" {
		t.Errorf("agent_review 留痕异常: %s/%s", verdict, model)
	}
}

func TestRunReviewLowConfidenceEscalates(t *testing.T) {
	db, dir := setupCorpus(t, "乙条例")
	chat := &fakeChat{resp: []string{okClassify, lowAudit}}
	sum, err := RunReview(context.Background(), dir, db, chat, "test-model", nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Escalated != 1 || sum.Confirmed != 0 {
		t.Fatalf("低置信应转人工，实际 %+v", sum)
	}
	st, src := statusOf(t, db, "doc_乙条例")
	if st != "pending" || src != "" {
		t.Errorf("转人工条目必须保持待复核且无来源，实际 %s/%s", st, src)
	}
	var verdict, reason string
	if err := db.QueryRow(`SELECT verdict, audit FROM agent_review WHERE doc_id='doc_乙条例'`).
		Scan(&verdict, &reason); err != nil {
		t.Fatal(err)
	}
	if verdict != "escalate" || !strings.Contains(reason, "未达门槛") {
		t.Errorf("留痕应记 escalate 并注明门槛，实际 %s", verdict)
	}
}

func TestRunReviewBadOutputFailClosed(t *testing.T) {
	db, dir := setupCorpus(t, "丙条例")
	chat := &fakeChat{resp: []string{"抱歉，我无法判断。"}}
	sum, err := RunReview(context.Background(), dir, db, chat, "test-model", nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Escalated != 1 {
		t.Fatalf("坏输出应转人工，实际 %+v", sum)
	}
	if st, _ := statusOf(t, db, "doc_丙条例"); st != "pending" {
		t.Errorf("坏输出绝不得确认，实际 %s", st)
	}
}

func TestRunReviewCancelKeepsProgress(t *testing.T) {
	db, dir := setupCorpus(t, "丁条例", "戊条例")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sum, err := RunReview(ctx, dir, db, &fakeChat{resp: []string{okClassify, okAudit, okClassify, okAudit}},
		"test-model", nil, Options{})
	if err == nil {
		t.Fatal("ctx 取消必须返回错误")
	}
	if sum.StoppedAt != 0 || sum.Confirmed != 0 {
		t.Errorf("取消后不得有任何落判，实际 %+v", sum)
	}
	for _, n := range []string{"丁条例", "戊条例"} {
		if st, _ := statusOf(t, db, "doc_"+n); st != "pending" {
			t.Errorf("取消后 %s 应仍待复核", n)
		}
	}
}

func TestRunReviewRejectsNilChat(t *testing.T) {
	db, dir := setupCorpus(t, "甲条例")
	if _, err := RunReview(context.Background(), dir, db, nil, "m", nil, Options{}); err == nil {
		t.Error("未配置通道必须拒绝（默认离线纪律）")
	}
}

// —— 部门法/文档形态建议（docs/design-taxonomy.md §5 智能体自审扩展）——

const clsSubjHigh = `{"effect_level":"行政法规","issuer":"国务院","publish_date":"2020-01-01","region_code":"","subject_code":"CN-ADM","subject_confidence":0.9,"doc_form":"CN-REG","confidence":0.95,"reason":"名称与原文标题一致"}`
const clsSubjLow = `{"effect_level":"行政法规","issuer":"国务院","publish_date":"2020-01-01","region_code":"","subject_code":"CN-ADM","subject_confidence":0.5,"doc_form":"CN-REG","confidence":0.95,"reason":"部门法拿不准"}`
const clsSubjBad = `{"effect_level":"行政法规","issuer":"国务院","publish_date":"2020-01-01","region_code":"","subject_code":"CN-XXX","subject_confidence":0.9,"doc_form":"CN-REG","confidence":0.95,"reason":"词表外的码"}`

func subjectOf(t *testing.T, db *store.DB, lawID string) (code, source, reviewer, docForm string) {
	t.Helper()
	if err := db.QueryRow(`SELECT COALESCE(subject_code,''), COALESCE(subject_source,''),
		COALESCE(subject_reviewer,''), COALESCE(doc_form,'') FROM law WHERE law_id=?`, lawID).
		Scan(&code, &source, &reviewer, &docForm); err != nil {
		t.Fatal(err)
	}
	return
}

// TestRunReviewSubjectSuggestion 达门槛写 auto 建议（署名留空 = 待复核）、低置信不写、
// 人工结论不顶回；审计署名 agent:<model>，可经 SubjectMarker 按批撤销。
func TestRunReviewSubjectSuggestion(t *testing.T) {
	db, dir := setupCorpus(t, "甲条例", "乙条例", "丙条例")
	if _, err := db.Exec(`UPDATE law SET subject_reviewer='张三',
		subject_reviewed_at='2026-01-01T00:00:00Z' WHERE law_id='law_丙条例'`); err != nil {
		t.Fatal(err)
	}
	chat := &fakeChat{resp: []string{clsSubjHigh, okAudit, clsSubjLow, okAudit, clsSubjHigh, lowAudit}}
	sum, err := RunReview(context.Background(), dir, db, chat, "test-model", nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.SubjectAuto != 1 || sum.DocFormAuto != 3 {
		t.Fatalf("部门法只应写甲 1 处；文档形态全自动（拍板项 2，无置信门槛）应写 3 处，实际 %+v", sum)
	}
	if code, src, rev, df := subjectOf(t, db, "law_甲条例"); code != "CN-ADM" || src != "auto" || rev != "" || df != "CN-REG" {
		t.Errorf("甲应为 auto 建议且署名留空: %s/%s/%s/%s", code, src, rev, df)
	}
	if code, _, _, df := subjectOf(t, db, "law_乙条例"); code != "" || df != "CN-REG" {
		t.Errorf("乙低置信部门法不得写入、形态照判: %s/%s", code, df)
	}
	if code, _, rev, df := subjectOf(t, db, "law_丙条例"); code != "" || rev != "张三" || df != "CN-REG" {
		t.Errorf("丙人工结论必须原样保留、形态照判: %s/%s/%s", code, rev, df)
	}
	var logs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subject_log WHERE actor='agent:test-model'
		AND batch_id='agent-test-model'`).Scan(&logs); err != nil || logs != 4 {
		t.Fatalf("审计应为 4 笔 agent 写入（1 部门法 + 3 形态）: %d (%v)", logs, err)
	}
	// 整批撤销（含 doc-form 审计分流），人工结论不回滚。
	m := &ingest.SubjectMarker{DB: db}
	if k, err := m.Undo("agent-test-model", true); err != nil || k != 4 {
		t.Fatalf("撤销应执行 4 处: %d (%v)", k, err)
	}
	if code, _, _, df := subjectOf(t, db, "law_甲条例"); code != "" || df != "" {
		t.Errorf("撤销后应回到未标注: %s/%s", code, df)
	}
	if code, _, rev, df := subjectOf(t, db, "law_丙条例"); code != "" || rev != "张三" || df != "" {
		t.Errorf("撤销不得回滚人工落判、形态已随批撤销: %s/%s/%s", code, rev, df)
	}
}

// TestRunReviewEscalateStillSuggests 部门法建议与 confirm/escalate 裁决无关：
// 审核转人工的条目，达门槛的部门法建议照样写入（待复核，不是落判）。
func TestRunReviewEscalateStillSuggests(t *testing.T) {
	db, dir := setupCorpus(t, "戊条例")
	chat := &fakeChat{resp: []string{clsSubjHigh, lowAudit}}
	sum, err := RunReview(context.Background(), dir, db, chat, "test-model", nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Escalated != 1 || sum.SubjectAuto != 1 {
		t.Fatalf("应转人工但写入部门法建议，实际 %+v", sum)
	}
	if code, src, rev, _ := subjectOf(t, db, "law_戊条例"); code != "CN-ADM" || src != "auto" || rev != "" {
		t.Errorf("escalate 条目的部门法建议仍应是待复核 auto: %s/%s/%s", code, src, rev)
	}
	if st, _ := statusOf(t, db, "doc_戊条例"); st != "pending" {
		t.Errorf("部门法建议不得代替落判，条目必须仍待复核: %s", st)
	}
}

// TestRunReviewSubjectBadCodeFailClosed 词表外的码：不写入、不报错中断整轮，如实计数为 0。
func TestRunReviewSubjectBadCodeFailClosed(t *testing.T) {
	db, dir := setupCorpus(t, "丁条例")
	chat := &fakeChat{resp: []string{clsSubjBad, okAudit}}
	sum, err := RunReview(context.Background(), dir, db, chat, "test-model", nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.SubjectAuto != 0 {
		t.Fatalf("词表外的码绝不得写入: %+v", sum)
	}
	if code, _, _, _ := subjectOf(t, db, "law_丁条例"); code != "" {
		t.Errorf("坏码必须留空: %s", code)
	}
}
