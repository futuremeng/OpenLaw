package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

// setupChainFixture 构造最小溯源链：law → version → document → 文件。
// （不导入 ingest 包以避免循环依赖，直接 SQL 布景。）
func setupChainFixture(t *testing.T) (string, *store.DB, string) {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"pdf", "db"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	content := []byte("%PDF-1.4 original\n")
	pdfPath := filepath.Join(dir, "pdf", "d1.pdf")
	if err := os.WriteFile(pdfPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)

	if _, err := db.Exec(`INSERT INTO law(law_id, name) VALUES('l1','测试法')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO document(doc_id, file_name, file_path, file_size, sha256, hash_algo, hash_computed_at, confirm_status)
		VALUES('d1','d1.pdf','pdf/d1.pdf', ?, ?, 'SHA-256', '2026-01-01T00:00:00Z', 'confirmed')`,
		len(content), hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO law_version(version_id, law_id, version_no, effective_date, doc_id)
		VALUES('l1_v1','l1','v1','2020-01-01','d1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO article(article_id, version_id, article_no, content, md_path, doc_id, parser_version)
		VALUES('l1_v1_第一条','l1_v1','第一条','条文内容','md/d1.md','d1','test')`); err != nil {
		t.Fatal(err)
	}
	return dir, db, "d1"
}

func keyPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sign.key")
	if err := GenerateKey(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestCheckDocumentPass 完整链：文件、哈希、manifest、签名全部完好。
func TestCheckDocumentPass(t *testing.T) {
	dir, db, docID := setupChainFixture(t)
	if issues := CheckDocument(db, dir, docID); len(issues) == 0 {
		t.Fatal("manifest 缺失时应报断链（未纳入每日防篡改校验）")
	}
	if err := BuildManifest(dir, keyPath(t)); err != nil {
		t.Fatal(err)
	}
	if issues := CheckDocument(db, dir, docID); len(issues) != 0 {
		t.Fatalf("完整链应无断点，实际: %+v", issues)
	}
}

// TestCheckDocumentHashTampered PDF 被替换 → OL-1007（验收 8.2：哈希失败必须告警）。
func TestCheckDocumentHashTampered(t *testing.T) {
	dir, db, docID := setupChainFixture(t)
	if err := BuildManifest(dir, keyPath(t)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pdf", "d1.pdf"), []byte("%PDF-1.4 TAMPERED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := CheckDocument(db, dir, docID)
	if len(issues) == 0 || issues[0].Step != StepHash || issues[0].ErrorCode != CodeHashFail {
		t.Fatalf("应命中哈希断链 OL-1007，实际: %+v", issues)
	}
}

// TestCheckDocumentFileMissing PDF 文件缺失 → OL-1006。
func TestCheckDocumentFileMissing(t *testing.T) {
	dir, db, docID := setupChainFixture(t)
	if err := os.Remove(filepath.Join(dir, "pdf", "d1.pdf")); err != nil {
		t.Fatal(err)
	}
	issues := CheckDocument(db, dir, docID)
	if len(issues) == 0 || issues[0].Step != StepFile || issues[0].ErrorCode != CodeBrokenChain {
		t.Fatalf("应命中文件缺失断链 OL-1006，实际: %+v", issues)
	}
}

// TestManifestTampered manifest 被篡改 → 签名验证失败 OL-1008（回归 8.3）。
func TestManifestTampered(t *testing.T) {
	dir, db, docID := setupChainFixture(t)
	key := keyPath(t)
	if err := BuildManifest(dir, key); err != nil {
		t.Fatal(err)
	}
	// 篡改 manifest.json（不改签名）
	mp := filepath.Join(dir, "manifest", "manifest.json")
	b, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mp, append(b, ' '), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyManifest(dir); err == nil {
		t.Fatal("manifest 篡改应验签失败")
	}
	issues := CheckDocument(db, dir, docID)
	if len(issues) == 0 || issues[0].ErrorCode != CodeSignatureFail {
		t.Fatalf("应命中签名断链 OL-1008，实际: %+v", issues)
	}
}

// TestCheckArticleVersionBreak 版本记录缺失 → version 环节断链。
// 正常导入下外键保证不会出现孤儿行；此场景模拟数据目录被外部破坏（如手工删表数据），
// 故临时关闭外键制造孤儿状态，验证检测逻辑本身。
func TestCheckArticleVersionBreak(t *testing.T) {
	dir, db, _ := setupChainFixture(t)
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM law_version`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	issues := CheckArticle(db, dir, "l1_v1_第一条")
	if len(issues) == 0 || issues[0].Step != StepVersion {
		t.Fatalf("应命中版本断链，实际: %+v", issues)
	}
}

// TestValidateLogWritten 断链必须写 validate_log（2.2 可靠性：不得静默）。
func TestValidateLogWritten(t *testing.T) {
	dir, db, docID := setupChainFixture(t)
	if err := os.Remove(filepath.Join(dir, "pdf", "d1.pdf")); err != nil {
		t.Fatal(err)
	}
	CheckDocument(db, dir, docID)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM validate_log WHERE result='fail' AND validate_type='chain:file'`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("断链必须写 validate_log，实际 %d 条 (%v)", n, err)
	}
}
