// coverage_test.go 复核兑现进度：命中口径（全称/简称/别名）、缺失与未复核必须分开、
// 清单截断必须声明、空库与零复核必须直说。
package ingest

import (
	"strings"
	"testing"
)

func seedLaws(t *testing.T, im *Importer, rows []struct{ lawID, name, short string }) {
	t.Helper()
	seedLawsConfirmed(t, im, rows, nil)
}

// seedLawsConfirmed 直插法规/版本/文本（绕开导入器，专注复核口径）；
// confirmed 集合内的 law_id 其文本置为已复核确认，其余保持待复核。
func seedLawsConfirmed(t *testing.T, im *Importer, rows []struct{ lawID, name, short string }, confirmed map[string]bool) {
	t.Helper()
	for _, r := range rows {
		docID := "d_" + r.lawID
		if _, err := im.DB.Exec(`INSERT INTO law(law_id,name,short_name) VALUES(?,?,?)`, r.lawID, r.name, r.short); err != nil {
			t.Fatal(err)
		}
		status := "pending"
		if confirmed[r.lawID] {
			status = "confirmed"
		}
		if _, err := im.DB.Exec(`INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at,confirm_status)
			VALUES(?,?,?,1,'x','2026-01-01T00:00:00Z',?)`, docID, "f.pdf", "pdf/"+docID+".pdf", status); err != nil {
			t.Fatal(err)
		}
		if _, err := im.DB.Exec(`INSERT INTO law_version(version_id,law_id,version_no,effective_date,doc_id)
			VALUES(?,?, '现行','2020-01-01',?)`, "v_"+r.lawID, r.lawID, docID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReviewCoverage(t *testing.T) {
	im, _ := setupImporter(t)
	seed := []struct{ lawID, name, short string }{
		{"l_mfd", "中华人民共和国民法典", "民法典"},
		{"l_xf", "中华人民共和国宪法", ""},
		{"l_hsf", "中华人民共和国海商法", ""}, // 清单内、未复核
	}
	seedLawsConfirmed(t, im, seed, map[string]bool{"l_mfd": true, "l_xf": true})

	c, err := ReviewCoverage(im.DB, 10)
	if err != nil {
		t.Fatal(err)
	}
	if c.PriorityTotal != len(PriorityLaws) {
		t.Errorf("清单总数错: %d", c.PriorityTotal)
	}
	if c.PriorityInCorpus != 3 {
		t.Errorf("已收录计数错: %d", c.PriorityInCorpus)
	}
	if c.PriorityConfirmed != 2 || c.PriorityUnreviewed != 1 {
		t.Errorf("已复核/未复核错: %d %d（未复核清单 %v）", c.PriorityConfirmed, c.PriorityUnreviewed, c.UnreviewedNames)
	}
	if len(c.UnreviewedNames) != 1 || c.UnreviewedNames[0] != "中华人民共和国海商法" {
		t.Errorf("未复核清单内容错: %v", c.UnreviewedNames)
	}
	if c.PriorityMissing != len(PriorityLaws)-3 {
		t.Errorf("未收录计数错: %d", c.PriorityMissing)
	}
	if !c.Truncated || len(c.MissingNames) != 10 {
		t.Errorf("清单截断必须声明并给出 limit 条: truncated=%v len=%d", c.Truncated, len(c.MissingNames))
	}
	if c.CorpusLaws != 3 || c.CorpusDocs != 3 || c.CorpusConfirmed != 2 {
		t.Errorf("全库计数错: %+v", c)
	}
	if !strings.Contains(c.Headline(), "已复核确认 2 / 3") {
		t.Errorf("白话行不如实: %q", c.Headline())
	}
	if !strings.Contains(c.ListNote, "不是官方认定") {
		t.Errorf("清单性质声明缺失: %q", c.ListNote)
	}
}

// TestReviewCoverageAliasHit 别名表命中算已收录——简称在库内真有其法就是库内事实。
func TestReviewCoverageAliasHit(t *testing.T) {
	im, _ := setupImporter(t)
	seedLawsConfirmed(t, im, []struct{ lawID, name, short string }{{"l_x", "某测试法规全称", ""}},
		map[string]bool{"l_x": true})
	if _, err := im.DB.Exec(`INSERT INTO law_alias(alias,law_id,source,built_at)
		VALUES('中华人民共和国民法典','l_x','corpus-text','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	c, err := ReviewCoverage(im.DB, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c.PriorityConfirmed < 1 {
		t.Errorf("别名命中的已复核法规应计入，got %+v", c)
	}
	if len(c.MissingNames) != 0 || len(c.UnreviewedNames) != 0 {
		t.Errorf("limit=0 不得列明细: %+v", c)
	}
}

// TestReviewCoverageShortNameWins 同一简称指向两部法时，只要有一部已复核就算已复核
// （不得因为索引写入顺序把已复核的法规报成未复核）。
func TestReviewCoverageShortNameWins(t *testing.T) {
	im, _ := setupImporter(t)
	seedLawsConfirmed(t, im, []struct{ lawID, name, short string }{
		{"l_a", "甲测试法规全称", "中华人民共和国公司法"}, // 已复核
		{"l_b", "乙测试法规全称", "中华人民共和国公司法"}, // 待复核；简称同名
	}, map[string]bool{"l_a": true})
	c, err := ReviewCoverage(im.DB, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c.PriorityInCorpus < 1 || c.PriorityConfirmed < 1 {
		t.Errorf("简称指向已复核法规时必须计入: %+v", c)
	}
}

func TestReviewCoverageHonestEmpty(t *testing.T) {
	im, _ := setupImporter(t)
	c, err := ReviewCoverage(im.DB, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Headline(), "尚无文本") {
		t.Errorf("空库必须直说没有数据: %q", c.Headline())
	}
	if c.PriorityMissing != len(PriorityLaws) {
		t.Errorf("空库全部法规应报未收录: %d", c.PriorityMissing)
	}

	seedLaws(t, im, []struct{ lawID, name, short string }{{"l_p", "中华人民共和国民法典", ""}})
	if _, err := im.DB.Exec(`UPDATE document SET confirm_status='pending'`); err != nil {
		t.Fatal(err)
	}
	c, err = ReviewCoverage(im.DB, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Headline(), "尚未经人工复核确认") {
		t.Errorf("零复核必须直说未经确认: %q", c.Headline())
	}
}

func TestReviewCoverageNoDB(t *testing.T) {
	if _, err := ReviewCoverage(nil, 5); err == nil {
		t.Error("库未打开必须报错，不得返回空结果")
	}
}

func TestPriorityLawNames(t *testing.T) {
	names := PriorityLawNames()
	if len(names) != len(PriorityLaws) {
		t.Fatalf("名称清单与清单条数不一致: %d %d", len(names), len(PriorityLaws))
	}
	for _, n := range names {
		if n == "" || strings.ContainsAny(n, "《》 ") {
			t.Errorf("清单名称必须是法规全称（不带书名号与空白）: %q", n)
		}
	}
}
