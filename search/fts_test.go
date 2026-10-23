package search

import (
	"path/filepath"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

func setupDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, `INSERT INTO law(law_id, name) VALUES('l1', '测试法')`)
	mustExec(t, db, `INSERT INTO document(doc_id, file_name, file_path, file_size, sha256, hash_algo, hash_computed_at)
		VALUES('d1','d1.pdf','pdf/d1.pdf',1,'x','SHA-256','2026-01-01T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO law_version(version_id, law_id, version_no, effective_date, doc_id)
		VALUES('l1_v1','l1','v1','2020-01-01','d1')`)
	mustExec(t, db, `INSERT INTO article(article_id, version_id, article_no, content, md_path, doc_id, parser_version)
		VALUES('l1_v1_第一条','l1_v1','第一条','公司股东滥用公司法人独立地位和股东有限责任，逃避债务，严重损害公司债权人利益的，应当对公司债务承担连带责任。','md/d1.md','d1','test')`)
	return db
}

func mustExec(t *testing.T, db *store.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
}

// TestSearchChineseFallback 中文检索：unicode61 整段切词导致 FTS 短语零结果时，
// LIKE 兜底必须保证基本召回（13.2 已知限制）。
func TestSearchChineseFallback(t *testing.T) {
	db := setupDB(t)
	hits, err := Search(db, "连带责任", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ArticleID != "l1_v1_第一条" {
		t.Fatalf("LIKE 兜底应命中 1 条，实际 %+v", hits)
	}
	if hits[0].DocID != "d1" {
		t.Fatal("命中必须携带 doc_id（7 章）")
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	db := setupDB(t)
	if _, err := Search(db, "", 5); err == nil {
		t.Fatal("空检索词应报错")
	}
}
