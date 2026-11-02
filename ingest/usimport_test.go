package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

const usTestUSLM = `<?xml version="1.0" encoding="UTF-8"?>
<uslm>
  <ident><citation>CFR Title 12</citation></ident>
  <body><title type="TITLE" N="12"><head>TITLE 12—BANKS AND BANKING</head>
    <div type="PART" N="1"><num>Part 1</num><head>PART 1—AUTHORITY</head>
      <div type="SECTION" N="1.1"><num>§ 1.1</num><head>Authority.</head>
        <p>Comptroller administers the national banking system.</p></div>
      <div type="SECTION" N="1.2"><num>§ 1.2</num><head>Scope.</head>
        <p>Applies to national banks.</p></div>
    </div>
  </title></body>
</uslm>`

func setupUSTest(t *testing.T) (string, *store.DB, string) {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"pdf", "md", "db", "provenance", "xml", "incoming"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	xmlPath := filepath.Join(dir, "incoming", "title-12.xml")
	if err := os.WriteFile(xmlPath, []byte(usTestUSLM), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, db, xmlPath
}

// TestRunUSLMImport 美国全文导入真实链路：dry-run 不写库；正式导入 law.jurisdiction=US
// （由效力等级真实推导）、document.confirm_status=pending（人工闸口）、lang=eng、
// 条文带 USLM 解析器版本、原始 XML 归档 xml/ 层；缺生效日期必须拒绝。
func TestRunUSLMImport(t *testing.T) {
	dir, db, xmlPath := setupUSTest(t)
	base := USLMImportOptions{XMLPath: xmlPath, DocID: "us_ecfr_title12_2025",
		Name: "12 CFR Part 1 (test)", Issuer: "Comptroller of the Currency",
		VersionNo: "2025 annual edition", EditionDate: "2025-01-01",
		PublishDate: "2024-12-16", SourceURL: "https://www.govinfo.gov/content/pkg/ECFR/title-12"}

	res, err := RunUSLMImport(dir, db, func() USLMImportOptions {
		o := base
		o.DryRun = true
		return o
	}())
	if err != nil {
		t.Fatal(err)
	}
	if res.Articles != 2 || !res.DryRun {
		t.Errorf("dry-run 结果: %+v", res)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM law`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("dry-run 不得写库")
	}

	// 缺生效日期——拒绝（不得从包体猜日期）。
	if o := base; true {
		o.EditionDate = ""
		if _, err := RunUSLMImport(dir, db, o); err == nil {
			t.Fatal("缺生效日期必须拒绝")
		}
	}

	res, err = RunUSLMImport(dir, db, base)
	if err != nil {
		t.Fatal(err)
	}
	if res.SHA256 == "" || res.VersionID == "" {
		t.Errorf("导入结果不完整: %+v", res)
	}
	var jur, confirm, lang string
	if err := db.QueryRow(`SELECT jurisdiction FROM law WHERE law_id='us_ecfr_title12_2025'`).Scan(&jur); err != nil {
		t.Fatal(err)
	}
	if jur != "US" {
		t.Errorf("法域应推导为 US，实际 %q", jur)
	}
	if err := db.QueryRow(`SELECT confirm_status, lang FROM document WHERE doc_id='us_ecfr_title12_2025'`).
		Scan(&confirm, &lang); err != nil {
		t.Fatal(err)
	}
	if confirm != "pending" || lang != "eng" {
		t.Errorf("人工闸口/语言: %s %s", confirm, lang)
	}
	if err := db.QueryRow(`SELECT COUNT(DISTINCT parser_version) FROM article
		WHERE parser_version = 'openlaw-parse/0.1.0+uslm-us'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("USLM 条文应带解析器版本 openlaw-parse/0.1.0+uslm-us，实际查询 %d", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "xml", "us_ecfr_title12_2025.xml")); err != nil {
		t.Errorf("原始 XML 应归档 xml/ 层: %v", err)
	}
	md, err := os.ReadFile(filepath.Join(dir, "md", "us_ecfr_title12_2025.md"))
	if err != nil || !strings.Contains(string(md), "national banking system") {
		t.Errorf("md 层缺失或不含正文: %v", err)
	}

	// 同 doc_id 重导——原始层禁止覆盖（写路径从严）。
	if _, err := RunUSLMImport(dir, db, base); err == nil {
		t.Error("同 doc_id 重导必须拒绝，不得覆盖原始层")
	}
}
