package main

import (
	"path/filepath"
	"testing"
	"time"
)

// newTestApp 临时语料 + 种子数据（与 schema 约束对齐）。
func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	a := NewApp(dir)
	if err := a.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.db.Close() })
	seed := []string{
		`INSERT INTO law(law_id,name,effect_level) VALUES('L1','工伤保险条例','行政法规')`,
		`INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at,source_type,publish_date,confirm_status)
		 VALUES('govcn_x1','x1.md','md/govcn_x1.md',10,'aa','2026-10-01T00:00:00Z','gov.cn 国家规章库','2003-04-27','pending')`,
		`INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at,source_type,confirm_status)
		 VALUES('flk_y1','y1.pdf','pdf/flk_y1.pdf',10,'bb','2026-10-01T00:00:00Z','flk','confirmed')`,
		`INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at,source_type,publish_date,confirm_status)
		 VALUES('govcn_x0','x0.md','md/govcn_x0.md',10,'cc','2026-10-01T00:00:00Z','gov.cn 国家行政法规库','1996-04-12','confirmed')`,
		`INSERT INTO law_version(version_id,law_id,version_no,effective_date,doc_id) VALUES('V1','L1','现行','2004-01-01','govcn_x1')`,
		`INSERT INTO law_version(version_id,law_id,version_no,effective_date,expire_date,doc_id) VALUES('V0','L1','1996 原文','1996-01-01','2004-01-01','govcn_x0')`,
		`INSERT INTO article(article_id,version_id,article_no,content,md_path,doc_id,parser_version)
		 VALUES('A1','V1','第一条','工伤保险条例第一条 为了保障工伤职工,制定本条例','md/govcn_x1.md','govcn_x1','t1')`,
		`INSERT INTO review_log(doc_id,action,from_status,to_status,reviewer,reason,created_at)
		 VALUES('flk_y1','confirm','pending','confirmed','张三','官方原文比对','2026-10-20T00:00:00Z')`,
	}
	for _, s := range seed {
		if _, err := a.db.Exec(s); err != nil {
			t.Fatalf("种子失败: %v\n%.60s", err, s)
		}
	}
	writeFile(t, filepath.Join(dir, "provenance/govcn_x1.json"),
		`{"doc_id":"govcn_x1","effective_date_inferred":true}`)
	return a
}

func TestSearchArticles(t *testing.T) {
	a := newTestApp(t)
	rows, err := a.SearchArticles("工伤保险", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("应命中 1 条: %+v", rows)
	}
	r := rows[0]
	if r.DocID != "govcn_x1" || r.LawName != "工伤保险条例" || r.Level != "行政法规" || r.ArticleNo != "第一条" {
		t.Errorf("结果补全错: %+v", r)
	}
}

func TestPendingListRiskFlags(t *testing.T) {
	a := newTestApp(t)
	list, err := a.PendingList("", 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].DocID != "govcn_x1" {
		t.Fatalf("队列错: %+v", list)
	}
	found := false
	for _, f := range list[0].Flags {
		if f == "inferred_date" {
			found = true
		}
	}
	if !found {
		t.Errorf("风险标记缺失: %+v", list[0].Flags)
	}
}

func TestQueueStatsAndBulkPreview(t *testing.T) {
	a := newTestApp(t)
	st, err := a.QueueStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Pending != 1 || st.Confirmed != 2 || st.Risk.InferredDate != 1 {
		t.Errorf("统计错: %+v", st)
	}
	n, err := a.BulkPreview("gov.cn%", "", "confirm")
	if err != nil || n != 1 {
		t.Errorf("dry-run 计数错: %v %v", n, err)
	}
	if _, err := a.BulkApply("gov.cn%", "", "confirm", "", ""); err == nil {
		t.Error("批量落判必须署名+依据")
	}
}

func TestRecentReviewsAndJudgeAudit(t *testing.T) {
	a := newTestApp(t)
	rev, err := a.RecentReviews(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev) != 1 || rev[0].Reviewer != "张三" {
		t.Fatalf("审计流水错: %+v", rev)
	}
	sum, err := a.Judge([]string{"govcn_x1"}, "confirm", "李四", "官方对照")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Confirmed != 1 {
		t.Errorf("落判错: %+v", sum)
	}
	rev, _ = a.RecentReviews(10)
	if len(rev) != 2 || rev[0].Reviewer != "李四" {
		t.Errorf("落判未留审计: %+v", rev)
	}
}

func TestCheckCitationPanel(t *testing.T) {
	a := newTestApp(t)
	today := time.Now().Format("2006-01-02")
	c, err := a.CheckCitation("工伤保险条例", "第一条", "工伤保险条例第一条 为了保障工伤职工,制定本条例", today)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Hit || c.DocID != "govcn_x1" {
		t.Errorf("引用核验错: %+v", c)
	}
	c2, err := a.CheckCitation("工伤保险条例", "第一百条", "不存在", today)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Hit {
		t.Errorf("不存在的条款不应命中: %+v", c2)
	}
}
