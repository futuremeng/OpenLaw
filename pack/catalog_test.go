package pack

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArchiveRoundtrip 归档往返：zip 解到暂存区后 Verify 仍通过；重复解包拒绝覆盖。
func TestArchiveRoundtrip(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-round")
	if err := BuildWithOptions(db, dir, packDir, key, "", "",
		BuildOptions{PackID: "base-full-1", Kind: "base"}); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "out", "base-full-1.zip")
	if err := Archive(packDir, zipPath); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(t.TempDir(), "staged")
	if err := Unarchive(zipPath, staging); err != nil {
		t.Fatal(err)
	}
	if err := Verify(staging); err != nil {
		t.Fatalf("解包后应校验通过: %v", err)
	}
	if err := Unarchive(zipPath, staging); err == nil {
		t.Error("暂存区已有同名文件应拒绝覆盖（如实清理后重试）")
	}
	// 缺 manifest 的目录不得归档
	if err := Archive(t.TempDir(), filepath.Join(t.TempDir(), "x.zip")); !errors.Is(err, ErrManifestMissing) {
		t.Errorf("空目录归档应报缺 manifest: %v", err)
	}
}

// TestUnarchiveRejectsSlip 归档越界路径（zip-slip）必须被拒。
func TestUnarchiveRejectsSlip(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "evil.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../escaped.txt")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("x"))
	zw.Close()
	f.Close()
	if err := Unarchive(zipPath, filepath.Join(t.TempDir(), "staged")); err == nil {
		t.Error("zip-slip 路径必须被拒绝")
	}
}

// TestCatalogRoundtrip 目录往返：切分包归档 → ReadPackMeta（身份取自包内签名 manifest）
// → BuildCatalog → 登记公钥 → ParseCatalog 通过。
func TestCatalogRoundtrip(t *testing.T) {
	dir, db, key := setupMultiSource(t)
	packDir := filepath.Join(t.TempDir(), "base-cn")
	if err := BuildWithOptions(db, dir, packDir, key, "", "",
		BuildOptions{PackID: "base-cn-2026", Kind: "base", Regions: []string{"CN"}}); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "base-cn-2026.zip")
	if err := Archive(packDir, zipPath); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadPackMeta(zipPath, "https://example.test/releases/base-cn-2026.zip")
	if err != nil {
		t.Fatal(err)
	}
	if meta.PackID != "base-cn-2026" || meta.Kind != "base" || meta.DocCount != 1 || meta.Size == 0 || len(meta.SHA256) != 64 {
		t.Errorf("归档元信息不符: %+v", meta)
	}
	indexDir := filepath.Join(t.TempDir(), "index")
	if err := BuildCatalog([]CatalogPack{meta}, indexDir, key); err != nil {
		t.Fatal(err)
	}
	// 未登记公钥：目录必须拒（完整性通过≠来源可信）
	if _, err := ParseCatalog(indexDir); !errors.Is(err, ErrUntrustedKey) {
		t.Fatalf("未受信目录应拒绝: %v", err)
	}
	trustPack(t, packDir) // 同一密钥：登记包公钥即登记目录公钥
	c, err := ParseCatalog(indexDir)
	if err != nil {
		t.Fatalf("受信目录应通过: %v", err)
	}
	if len(c.Packs) != 1 || c.Packs[0].PackID != "base-cn-2026" {
		t.Errorf("目录条目不符: %+v", c.Packs)
	}
}

// TestCatalogGuards 目录门禁：篡改 index 即验签失败；旧整包（无 pack_id）不得进目录；
// 重复 pack_id 拒绝出目录。
func TestCatalogGuards(t *testing.T) {
	dir, db, key := setupMultiSource(t)
	packDir := filepath.Join(t.TempDir(), "base-gd")
	if err := BuildWithOptions(db, dir, packDir, key, "", "",
		BuildOptions{PackID: "base-44-2026", Kind: "base", Regions: []string{"44"}}); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "base-44-2026.zip")
	if err := Archive(packDir, zipPath); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadPackMeta(zipPath, "https://example.test/base-44-2026.zip")
	if err != nil {
		t.Fatal(err)
	}
	indexDir := filepath.Join(t.TempDir(), "index")
	if err := BuildCatalog([]CatalogPack{meta, meta}, indexDir, key); !errors.Is(err, ErrDuplicatePack) {
		t.Errorf("重复 pack_id 应拒绝: %v", err)
	}
	if err := BuildCatalog([]CatalogPack{meta}, indexDir, key); err != nil {
		t.Fatal(err)
	}
	// 篡改 sha256（改 index.json 正文）→ 验签失败
	body, err := os.ReadFile(filepath.Join(indexDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c Catalog
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	c.Packs[0].SHA256 = strings.Repeat("0", 64)
	tampered, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "index.json"), tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCatalog(indexDir); !errors.Is(err, ErrSignatureFail) {
		t.Errorf("篡改目录正文应验签失败: %v", err)
	}

	// 旧整包（无 pack_id）不得进目录
	legacyDir := filepath.Join(t.TempDir(), "legacy")
	if err := Build(db, dir, legacyDir, key, "", ""); err != nil {
		t.Fatal(err)
	}
	legacyZip := filepath.Join(t.TempDir(), "legacy.zip")
	if err := Archive(legacyDir, legacyZip); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPackMeta(legacyZip, "https://example.test/legacy.zip"); !errors.Is(err, ErrBadCatalog) {
		t.Errorf("旧整包进目录应被拒: %v", err)
	}
}
