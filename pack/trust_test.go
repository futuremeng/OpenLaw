package pack

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

// TestImportRejectsUntrustedKey 签名自洽但公钥不在受信名单 → 导入必须拒绝，
// 且不得写入任何数据（完整性通过≠来源可信）。
func TestImportRejectsUntrustedKey(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-untrusted")
	if err := Build(db, dir, packDir, key, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := TrustOf(packDir); !errors.Is(err, ErrUntrustedKey) {
		t.Fatalf("未登记公钥应报 ErrUntrustedKey: %v", err)
	}
	if err := VerifyTrusted(packDir); !errors.Is(err, ErrUntrustedKey) {
		t.Fatalf("VerifyTrusted 应拒绝未受信包: %v", err)
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
	if _, err := Import(tdb, target, packDir); !errors.Is(err, ErrUntrustedKey) {
		t.Fatalf("Import 应拒绝未受信包: %v", err)
	}
	var n int
	if err := tdb.QueryRow(`SELECT COUNT(*) FROM document`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("拒绝导入不得写入任何数据，实际 %d 条", n)
	}
}

// TestImportTrustedAfterRegistration 登记公钥后同一包导入成功（模拟发版内置名单）。
func TestImportTrustedAfterRegistration(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-trusted")
	if err := Build(db, dir, packDir, key, "", ""); err != nil {
		t.Fatal(err)
	}
	trustPack(t, packDir)
	if id, err := TrustOf(packDir); err != nil || id == "" {
		t.Fatalf("登记后应命中受信名单: %v %q", err, id)
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
	if err != nil || len(results) != 1 {
		t.Fatalf("受信包导入应成功: %v %+v", err, results)
	}
}

// TestImportAllowUntrusted 显式本地通道：不查名单但仍强制完整性校验。
func TestImportAllowUntrusted(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-local")
	if err := Build(db, dir, packDir, key, "", ""); err != nil {
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
	results, err := ImportAllowUntrusted(tdb, target, packDir)
	if err != nil || len(results) != 1 {
		t.Fatalf("本地自验通道应成功: %v %+v", err, results)
	}
}

// TestRegisterTrustedPubkeyGuards 非法 hex 与重复 key ID 必须拒绝。
func TestRegisterTrustedPubkeyGuards(t *testing.T) {
	if err := RegisterTrustedPubkey("bad", "zz"); err == nil {
		t.Error("非法 hex 应拒绝")
	}
	if err := RegisterTrustedPubkey("dup", "0000000000000000000000000000000000000000000000000000000000000000"); err != nil {
		t.Fatal(err)
	}
	if err := RegisterTrustedPubkey("dup", "1100000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Error("重复 key ID 应拒绝（轮换须用新 ID）")
	}
}
