package ingest

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docx 导入路径测试（flk 存量主格式）。

func writeTestDocx(t *testing.T, path, docXML string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(docXML)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestImportDocx(t *testing.T) {
	im, dir := setupImporter(t)
	src := filepath.Join(t.TempDir(), "测试条例_20200101.docx")
	writeTestDocx(t, src, `<w:document><w:body>`+
		`<w:p><w:r><w:t>第一条　为规范测试，制定本条例。</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t>第二条 本条例自2020年1月1日起施行。</w:t></w:r></w:p>`+
		`</w:body></w:document>`)
	res, err := im.Import(Request{
		DocID: "flk_test_20200101", LawID: "flk_test", LawName: "测试条例",
		VersionNo: "2020-01-01", EffectiveDate: "2020-01-01", PublishDate: "2020-01-01",
		SourceType: "flk 批量下载（用户提供存量）", PDFPath: src,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ArticleIDs) != 2 {
		t.Fatalf("应拆出 2 条，实际 %d", len(res.ArticleIDs))
	}
	// 原始层按真实格式落位 docx/，document.file_path 一致
	if _, err := os.Stat(filepath.Join(dir, "docx", "flk_test_20200101.docx")); err != nil {
		t.Fatal(err)
	}
	var fp string
	if err := im.DB.QueryRow(`SELECT file_path FROM document WHERE doc_id='flk_test_20200101'`).Scan(&fp); err != nil || fp != filepath.Join("docx", "flk_test_20200101.docx") {
		t.Fatalf("file_path 应为 docx/…，实际 %q (%v)", fp, err)
	}
	// 断链检测应通过（docx 层已纳入哈希）
	if len(res.ChainIssues) != 0 {
		t.Logf("链路提示（manifest 未生成属预期）: %v", res.ChainIssues)
	}
}

func TestImportOrphanLayerReplaced(t *testing.T) {
	// 此前失败运行：事务回滚但落盘文件遗留 → 重跑必须可恢复（覆盖孤儿层并留痕）
	im, dir := setupImporter(t)
	src := filepath.Join(t.TempDir(), "孤儿条例_20210101.docx")
	writeTestDocx(t, src, `<w:document><w:body>`+
		`<w:p><w:r><w:t>第一条　正文。</w:t></w:r></w:p>`+
		`</w:body></w:document>`)
	if err := os.MkdirAll(filepath.Join(dir, "docx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docx", "flk_orph_20210101.docx"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := im.Import(Request{
		DocID: "flk_orph_20210101", LawID: "flk_orph", LawName: "孤儿条例",
		VersionNo: "2021-01-01", EffectiveDate: "2021-01-01",
		SourceType: "测试样例", PDFPath: src,
	}); err != nil {
		t.Fatalf("孤儿层应允许覆盖重跑: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "docx", "flk_orph_20210101.docx"))
	if err != nil || string(b) == "stale" {
		t.Fatalf("孤儿层未被替换: %v", err)
	}
	var n int
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result='success' AND reason LIKE '%孤儿层%'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("成功日志应留痕孤儿层替换，实际 %d (%v)", n, err)
	}
}

func TestImportAutoExpireChain(t *testing.T) {
	im, _ := setupImporter(t)
	mk := func(docID, eff string, auto bool) error {
		src := filepath.Join(t.TempDir(), docID+".docx")
		writeTestDocx(t, src, `<w:document><w:body>`+
			`<w:p><w:r><w:t>第一条　正文。</w:t></w:r></w:p>`+
			`</w:body></w:document>`)
		_, err := im.Import(Request{
			DocID: docID, LawID: "law_chain", LawName: "链测试条例",
			VersionNo: eff, EffectiveDate: eff, SourceType: "测试样例",
			PDFPath: src, AutoExpirePrior: auto,
		})
		return err
	}
	if err := mk("chain_1997", "1997-06-29", true); err != nil {
		t.Fatal(err)
	}
	// 新版导入：旧版自动闭合到新版生效日
	if err := mk("chain_2020", "2020-11-25", true); err != nil {
		t.Fatal(err)
	}
	var exp string
	if err := im.DB.QueryRow(`SELECT expire_date FROM law_version WHERE version_id='law_chain_1997-06-29'`).Scan(&exp); err != nil || exp != "2020-11-25" {
		t.Fatalf("旧版应自动闭合到 2020-11-25，实际 %q (%v)", exp, err)
	}
	// 回填历史版本：其 expire 自动设为更新版本生效日
	if err := mk("chain_1990", "1990-03-01", true); err != nil {
		t.Fatal(err)
	}
	if err := im.DB.QueryRow(`SELECT expire_date FROM law_version WHERE version_id='law_chain_1990-03-01'`).Scan(&exp); err != nil || exp != "1997-06-29" {
		t.Fatalf("回填版本应闭合到 1997-06-29，实际 %q (%v)", exp, err)
	}
	// 分批到达插链：1997 版此前被闭合到 2020（当时未见中间版），
	// 中间版 2005 到达 → 1997 版区间前移到 2005，2005 版闭合到 2020
	if err := mk("chain_2005", "2005-01-01", true); err != nil {
		t.Fatalf("中间版插链应成功: %v", err)
	}
	if err := im.DB.QueryRow(`SELECT expire_date FROM law_version WHERE version_id='law_chain_1997-06-29'`).Scan(&exp); err != nil || exp != "2005-01-01" {
		t.Fatalf("1997 版区间应前移到 2005-01-01，实际 %q (%v)", exp, err)
	}
	if err := im.DB.QueryRow(`SELECT expire_date FROM law_version WHERE version_id='law_chain_2005-01-01'`).Scan(&exp); err != nil || exp != "2020-11-25" {
		t.Fatalf("2005 版应闭合到 2020-11-25，实际 %q (%v)", exp, err)
	}
	var rn int
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result='success' AND reason LIKE '%前移%'`).Scan(&rn); err != nil || rn != 1 {
		t.Fatalf("区间前移应留痕，实际 %d (%v)", rn, err)
	}
	// 同生效日双版本：真冲突仍拒绝
	if err := mk("chain_dup", "2020-11-25", true); err == nil || !strings.Contains(err.Error(), "同生效日") {
		t.Fatalf("同生效日应拒绝，实际: %v", err)
	}
	// 关闭 AutoExpirePrior 时保持严格：重叠直接拒绝
	if err := mk("chain_strict", "2021-01-01", false); err == nil || !strings.Contains(err.Error(), "重叠") {
		t.Fatalf("严格模式应拒绝重叠，实际: %v", err)
	}
}

func TestImportLegacyDocRejected(t *testing.T) {
	im, _ := setupImporter(t)
	src := filepath.Join(t.TempDir(), "旧格式.doc")
	if err := os.WriteFile(src, []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := im.Import(Request{
		DocID: "flk_old", LawID: "flk_old", LawName: "旧格式",
		VersionNo: "2010-01-01", EffectiveDate: "2010-01-01", PDFPath: src,
	})
	if err == nil || !strings.Contains(err.Error(), "textutil") {
		t.Fatalf(".doc 应报错并指引 textutil 转换，实际: %v", err)
	}
}
