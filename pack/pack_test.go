package pack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

// setupSource 构造一个已导入一份法规的数据目录（复用 ingest 闭环）。
func setupSource(t *testing.T) (string, *store.DB, string) {
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

	im := &ingest.Importer{DataDir: dir, DB: db}
	_, err = im.Import(ingest.Request{
		DocID: "law_pack_2018", LawID: "law_pack", LawName: "打包测试法", ShortName: "打包法",
		Issuer: "测试机关", EffectLevel: "法律", VersionNo: "2018版",
		EffectiveDate: "2018-01-01", SourceType: "测试样例",
		PDFPath: filepath.Join("..", "testdata", "fake_law.pdf"),
		MDPath:  filepath.Join("..", "testdata", "law_company_2018.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "pack.key")
	if err := validate.GenerateKey(key); err != nil {
		t.Fatal(err)
	}
	return dir, db, key
}

// TestBuildVerifyRoundtrip build → verify 往返：签名有效、哈希一致。
func TestBuildVerifyRoundtrip(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-20261023")
	if err := Build(db, dir, packDir, key); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"manifest.json", "manifest.sig", "pubkey.hex", "changes.csv",
		"pdf/law_pack_2018.pdf", "md/law_pack_2018.md", "provenance/law_pack_2018.json"} {
		if _, err := os.Stat(filepath.Join(packDir, f)); err != nil {
			t.Fatalf("包内缺少 %s", f)
		}
	}
	if err := Verify(packDir); err != nil {
		t.Fatalf("完整包应校验通过: %v", err)
	}
}

// TestVerifyDetectsTamper 包内文件被替换 / 塞入未收录文件 → 校验必须失败。
func TestVerifyDetectsTamper(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-tamper")
	if err := Build(db, dir, packDir, key); err != nil {
		t.Fatal(err)
	}

	// 替换 PDF
	if err := os.WriteFile(filepath.Join(packDir, "pdf", "law_pack_2018.pdf"),
		[]byte("TAMPERED"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(packDir); err == nil {
		t.Fatal("替换文件应校验失败")
	}

	// 重新打包后塞入多余文件
	packDir2 := filepath.Join(t.TempDir(), "pack-extra")
	if err := Build(db, dir, packDir2, key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir2, "pdf", "evil.pdf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(packDir2); err == nil {
		t.Fatal("未收录文件应校验失败")
	}
}

// TestImportPackFailNoOverwrite（17.2 追溯矩阵 test_pack_fail_no_overwrite）
// 校验失败的包必须整体拒绝，目标数据目录保持原样。
func TestImportPackFailNoOverwrite(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-bad")
	if err := Build(db, dir, packDir, key); err != nil {
		t.Fatal(err)
	}
	// 篡改 changes.csv（不改签名）
	cp := filepath.Join(packDir, "changes.csv")
	b, err := os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cp, append(b, "add,x,y,z\n"...), 0o644); err != nil {
		t.Fatal(err)
	}

	// 全新目标目录
	target := t.TempDir()
	tdb, err := store.Open(filepath.Join(target, "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer tdb.Close()

	if _, err := Import(tdb, target, packDir); err == nil {
		t.Fatal("篡改包应拒绝导入")
	}
	var n int
	if err := tdb.QueryRow(`SELECT COUNT(*) FROM document`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("失败导入不得写入任何数据，实际 %d 条", n)
	}
}

// TestImportPackSuccess 正常包导入到空目录：数据落位且走同一闭环（pending 转人工）。
func TestImportPackSuccess(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-ok")
	if err := Build(db, dir, packDir, key); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	tdb, err := store.Open(filepath.Join(target, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer tdb.Close()

	results, err := Import(tdb, target, packDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].DocID != "law_pack_2018" {
		t.Fatalf("应导入 1 条: %+v", results)
	}
	var n int
	if err := tdb.QueryRow(`SELECT COUNT(*) FROM article WHERE doc_id='law_pack_2018'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("条文应入库 2 条，实际 %d (%v)", n, err)
	}
	var cs string
	if err := tdb.QueryRow(`SELECT confirm_status FROM document WHERE doc_id='law_pack_2018'`).Scan(&cs); err != nil || cs != "pending" {
		t.Fatalf("导入包条目应为 pending 待人工确认，实际 %q", cs)
	}
}
