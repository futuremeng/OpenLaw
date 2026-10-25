package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
)

// TestImportEUArticles EU 通道入库：预置 Articles + Lang + ParserVersion →
// document.lang / law_version.lang / article.parser_version 正确；FTS 可搜英文；
// md 层落盘（pack 往返依赖）；中文文档 lang 默认 zho 不受影响（回归）。
func TestImportEUArticles(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	src := filepath.Join(dir, "src.xml")
	if err := os.WriteFile(src, []byte("<ACT>raw xml bytes</ACT>"), 0o644); err != nil {
		t.Fatal(err)
	}
	im := &Importer{DataDir: dir, DB: db}
	res, err := im.Import(Request{
		DocID: "eu_32016r0679_eng", LawID: "eu_reg_2016_679",
		LawName: "Regulation (EU) 2016/679", Issuer: "European Union",
		EffectLevel: "欧盟条例", VersionNo: "OJ原文",
		EffectiveDate: "2016-05-24",
		SourceURL:     "http://publications.europa.eu/resource/cellar/x/DOC_2",
		SourceType:    "cellar", ConfirmStatus: "pending",
		PDFPath: src, Lang: "eng",
		Articles: []parse.Article{
			{No: "Article 1", Content: "Article 1\nScope\n\n1. Text one."},
			{No: "Article 2", Content: "Article 2\n\n1. Text two."},
		},
		MDContent:     "## Article 1 Scope\n\n1. Text one.\n\n## Article 2\n\n1. Text two.",
		ParserVersion: parse.ParserVersionEU,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(res.ArticleIDs) != 2 {
		t.Fatalf("条文数 = %d", len(res.ArticleIDs))
	}
	var lang string
	if err := db.QueryRow(`SELECT lang FROM document WHERE doc_id='eu_32016r0679_eng'`).Scan(&lang); err != nil || lang != "eng" {
		t.Errorf("document.lang = %q err=%v", lang, err)
	}
	if err := db.QueryRow(`SELECT lang FROM law_version WHERE law_id='eu_reg_2016_679'`).Scan(&lang); err != nil || lang != "eng" {
		t.Errorf("law_version.lang = %q err=%v", lang, err)
	}
	var pv string
	if err := db.QueryRow(`SELECT DISTINCT parser_version FROM article WHERE doc_id='eu_32016r0679_eng'`).Scan(&pv); err != nil || pv != parse.ParserVersionEU {
		t.Errorf("parser_version = %q err=%v", pv, err)
	}
	var hits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM article_fts WHERE content MATCH '"Text one"'`).Scan(&hits); err != nil || hits == 0 {
		t.Errorf("FTS 应命中英文正文: hits=%d err=%v", hits, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "md", "eu_32016r0679_eng.md")); err != nil || !strings.Contains(string(b), "Article 1") {
		t.Errorf("md 层缺失: %v", err)
	}

	// 回归：中文文档 lang 默认 zho、切分器不受影响
	src2 := filepath.Join(dir, "src2.txt")
	if err := os.WriteFile(src2, []byte("第一条 测试\n正文。"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := im.Import(Request{DocID: "law_zho_test", LawID: "law_zho_test", LawName: "测试法",
		VersionNo: "原文", EffectiveDate: "2020-01-01", PDFPath: src2, ConfirmStatus: "pending",
		SourceType: "测试样例"}); err != nil {
		t.Fatalf("中文回归导入: %v", err)
	}
	if err := db.QueryRow(`SELECT lang FROM document WHERE doc_id='law_zho_test'`).Scan(&lang); err != nil || lang != "zho" {
		t.Errorf("中文文档 lang = %q, 期望 zho (err=%v)", lang, err)
	}
}
