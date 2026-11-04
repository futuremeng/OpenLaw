// metrics_test.go 本机度量聚合：未记录与记录为 0 必须区分；比率只在有记录时给出。
package metrics

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustExec(t *testing.T, db *store.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
}

func TestReportFreshDBHonest(t *testing.T) {
	db := openDB(t)
	r, err := ReportFor(db)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Available {
		t.Fatal("v10 库应有台账表")
	}
	if len(r.Funnel) < 5 {
		t.Fatalf("首启漏斗步骤缺失: %+v", r.Funnel)
	}
	if r.SearchTotal != 0 || r.ZeroRate != 0 {
		t.Errorf("空台账不得凭空产出比率: %+v", r)
	}
	if !strings.Contains(r.Headline(), "还没有检索与复核记录") {
		t.Errorf("空台账必须直说没有记录，got %q", r.Headline())
	}
	if !strings.Contains(r.Note, "不上传") {
		t.Errorf("口径说明必须声明不上传: %q", r.Note)
	}
}

func TestReportAggregates(t *testing.T) {
	db := openDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	mustExec(t, db, `INSERT INTO law(law_id,name) VALUES('L1','测试法')`)
	mustExec(t, db, `INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at,confirm_status)
		VALUES('d1','d1.pdf','pdf/d1.pdf',1,'aa','`+now+`','pending')`)
	for _, o := range []string{"ok", "ok", "zero"} {
		if err := store.UsageLog(db, store.EventSearch, o, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UsageLog(db, store.EventVerify, "verified", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.UsageLog(db, store.EventFetch, "partial", ""); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO review_log(doc_id,action,from_status,to_status,reviewer,reason,created_at)
		VALUES('d1','confirm','pending','confirmed','张三','与官网原文比对','`+now+`')`)
	mustExec(t, db, `INSERT INTO agent_review(doc_id,verdict,confidence,classify,audit,model,created_at)
		VALUES('d1','escalate',0.6,'{}','{}','test-model','`+now+`')`)
	mustExec(t, db, `INSERT INTO validate_log(target_type,target_id,validate_type,result,reason,created_at)
		VALUES('document','d1','chain','fail','OL-1006 断链','`+now+`')`)

	r, err := ReportFor(db)
	if err != nil {
		t.Fatal(err)
	}
	if r.SearchTotal != 3 || r.SearchZero != 1 {
		t.Errorf("检索聚合错: %d/%d", r.SearchTotal, r.SearchZero)
	}
	if r.ZeroRate < 33 || r.ZeroRate > 34 {
		t.Errorf("零命中比率应约 33.3%%，got %v", r.ZeroRate)
	}
	if r.VerifyTotal != 1 || r.FetchRuns != 1 {
		t.Errorf("核验/获取计数错: %d %d", r.VerifyTotal, r.FetchRuns)
	}
	if r.HumanReviews != 1 || r.HumanPerDay <= 0 {
		t.Errorf("人工复核吞吐错: %v %v", r.HumanReviews, r.HumanPerDay)
	}
	if r.ExceptionQ != 1 {
		t.Errorf("例外队列积压应为 1（escalate 且仍 pending），got %d", r.ExceptionQ)
	}
	if r.ChainTotal != 1 || r.ChainFail != 1 || r.FailRate != 100 {
		t.Errorf("断链率聚合错: %+v", r)
	}
	for _, step := range r.Funnel {
		if step.Kind == store.EventAppOpen && step.Done {
			t.Error("未记录 app_open 不得显示为已完成")
		}
	}
	if !strings.Contains(r.Headline(), "检索 3 次") {
		t.Errorf("白话行未如实给出检索次数: %q", r.Headline())
	}
}

// TestReportEscalateResolved 已人工处理的例外不算积压——把已纠正的算进积压会虚增工作量。
func TestReportEscalateResolved(t *testing.T) {
	db := openDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	mustExec(t, db, `INSERT INTO law(law_id,name) VALUES('L1','测试法')`)
	mustExec(t, db, `INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at,confirm_status)
		VALUES('d1','d1.pdf','pdf/d1.pdf',1,'aa','`+now+`','confirmed')`)
	mustExec(t, db, `INSERT INTO agent_review(doc_id,verdict,confidence,classify,audit,model,created_at)
		VALUES('d1','escalate',0.6,'{}','{}','test-model','`+now+`')`)
	r, err := ReportFor(db)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExceptionQ != 0 {
		t.Errorf("已确认条目不得计入积压，got %d", r.ExceptionQ)
	}
}

// TestReportOldCorpus 旧语料（无台账表）：Available=false，白话必须说「尚未记录」而不是 0。
func TestReportOldCorpus(t *testing.T) {
	db := openDB(t)
	mustExec(t, db, `DROP TABLE usage_event`)
	r, err := ReportFor(db)
	if err != nil {
		t.Fatal(err)
	}
	if r.Available {
		t.Fatal("无台账表必须报未记录")
	}
	if !strings.Contains(r.Headline(), "尚未记录") {
		t.Errorf("got %q", r.Headline())
	}
	for _, s := range r.Funnel {
		if s.Count != 0 || s.Done {
			t.Errorf("未记录时漏斗不得产出计数: %+v", s)
		}
	}
}
