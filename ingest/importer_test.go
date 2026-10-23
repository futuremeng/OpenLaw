package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

func setupImporter(t *testing.T) (*Importer, string) {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"pdf", "md", "db", "provenance"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Importer{DataDir: dir, DB: db}, dir
}

func sampleRequest() Request {
	return Request{
		DocID: "law_test_2018", LawID: "law_test", LawName: "测试法", ShortName: "测试法",
		Issuer: "全国人大", EffectLevel: "法律", VersionNo: "2018修正",
		EffectiveDate: "2018年10月26日", SourceType: "测试样例",
		PDFPath: filepath.Join("..", "testdata", "fake_law.pdf"),
		MDPath:  filepath.Join("..", "testdata", "law_company_2018.md"),
	}
}

func TestImportSuccess(t *testing.T) {
	im, dir := setupImporter(t)
	res, err := im.Import(sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if res.DocID != "law_test_2018" || len(res.ArticleIDs) != 2 {
		t.Fatalf("导入结果异常: %+v", res)
	}
	if res.SHA256 == "" {
		t.Fatal("必须记录 SHA-256")
	}

	// 文件落位
	for _, rel := range []string{"pdf/law_test_2018.pdf", "md/law_test_2018.md", "provenance/law_test_2018.json"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("缺少文件 %s: %v", rel, err)
		}
	}

	// 数据库计数
	var n int
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM article WHERE doc_id = 'law_test_2018'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("条文应入库 2 条，实际 %d (%v)", n, err)
	}
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result = 'success'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("应有一条 success 导入日志，实际 %d (%v)", n, err)
	}
	// 日期已标准化为 ISO（version_id = law_id + "_" + version_no）
	var eff string
	if err := im.DB.QueryRow(`SELECT effective_date FROM law_version WHERE version_id='law_test_2018修正'`).Scan(&eff); err != nil || eff != "2018-10-26" {
		t.Fatalf("生效日期应标准化为 ISO，实际 %q (%v)", eff, err)
	}
	// 溯源文件必填字段齐全
	if _, err := LoadProvenance(filepath.Join(dir, "provenance", "law_test_2018.json")); err != nil {
		t.Fatalf("溯源文件校验失败: %v", err)
	}
}

// TestImportNoOverwrite 原始层只读：重复 doc_id 必须拒绝，不得覆盖（7 章）。
func TestImportNoOverwrite(t *testing.T) {
	im, _ := setupImporter(t)
	if _, err := im.Import(sampleRequest()); err != nil {
		t.Fatal(err)
	}
	if _, err := im.Import(sampleRequest()); err == nil {
		t.Fatal("重复 doc_id 应被拒绝")
	}
	var n int
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM document`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("document 应仍为 1 条，实际 %d", n)
	}
}

// TestImportRollback 任一环节失败整批回滚，旧数据不受影响（6.1）。
func TestImportRollback(t *testing.T) {
	im, dir := setupImporter(t)

	// 正文无条文 → 解析失败，必须整批失败
	req := sampleRequest()
	badMD := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(badMD, []byte("# 说明\n没有条文标记。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req.MDPath = badMD
	if _, err := im.Import(req); err == nil {
		t.Fatal("无条文应失败")
	}
	var n int
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM document`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("失败后不得留下 document，实际 %d", n)
	}
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result = 'failed'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("应记录 failed 日志，实际 %d", n)
	}
}

// TestImportVersionOverlap 版本时效区间重叠必须被拦截（13.3）：
// 否则同一天会出现两个生效版本。
func TestImportVersionOverlap(t *testing.T) {
	im, _ := setupImporter(t)
	if _, err := im.Import(sampleRequest()); err != nil {
		t.Fatal(err)
	}
	req := sampleRequest()
	req.DocID = "law_test_2018_dup"
	if _, err := im.Import(req); err == nil {
		t.Fatal("区间重叠应被拦截")
	}
	var n int
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM law_version`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("版本表应仍为 1 条，实际 %d", n)
	}
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result = 'rolled_back'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("应记录 rolled_back 日志，实际 %d", n)
	}
}

// TestImportExpireBeforeEffective 失效日期必须晚于生效日期（左闭右开）。
func TestImportExpireBeforeEffective(t *testing.T) {
	im, _ := setupImporter(t)
	req := sampleRequest()
	req.ExpireDate = "2018-10-26" // 与生效同日，右开导致空区间
	if _, err := im.Import(req); err == nil {
		t.Fatal("失效日期不晚于生效日期应报错")
	}
}
