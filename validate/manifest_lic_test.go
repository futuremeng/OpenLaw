package validate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestLicense(t *testing.T) {
	// 语料 manifest 必须携带机器可读许可声明（EU 重使用署名随包分发），
	// 且新字段受签名覆盖：篡改 license 即验签失败。
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pdf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pdf", "a.pdf"), []byte("pdf-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "k.hex")
	if err := GenerateKey(key); err != nil {
		t.Fatal(err)
	}

	if err := BuildManifest(dir, key, "", ""); err != nil {
		t.Fatal(err)
	}
	m, _, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.License != DefaultLicense || m.Attribution != DefaultAttribution {
		t.Errorf("空参数应落默认许可声明: license=%q attribution=%q", m.License, m.Attribution)
	}
	if err := VerifyManifest(dir); err != nil {
		t.Fatalf("默认许可下验签应通过: %v", err)
	}

	// 自定义声明入签名
	if err := BuildManifest(dir, key, "CC0", "(c) EU"); err != nil {
		t.Fatal(err)
	}
	m, _, err = LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.License != "CC0" || m.Attribution != "(c) EU" {
		t.Errorf("自定义声明未入 manifest: %+v", m)
	}

	// 篡改 license 字段 → 签名失效（防「重新打包换许可」）
	jp := filepath.Join(dir, "manifest", "manifest.json")
	b, err := os.ReadFile(jp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jp, []byte(strings.Replace(string(b), `"license": "CC0"`, `"license": "public domain"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyManifest(dir); !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("篡改 license 应验签失败，得: %v", err)
	}
}
