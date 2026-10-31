package pack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newDistServer 起一个只读本地文件服务（测试用「远程」渠道），返回服务与根目录。
func newDistServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	srv := httptest.NewServer(http.FileServer(http.Dir(root)))
	t.Cleanup(srv.Close)
	return srv, root
}

// buildDist 在 dist 目录里产出两个切分包（国家级基础包 + 2020 窗口增量包）
// 与签名目录；返回目录结构。公钥登记进受信名单。
func buildDist(t *testing.T, srvURL, dist string) *Catalog {
	t.Helper()
	dir, db, key := setupMultiSource(t)
	specs := []BuildOptions{
		{PackID: "base-cn-2026", Kind: "base", Regions: []string{"CN"}},
		{PackID: "delta-20200101-20211231", Kind: "delta", Since: "2020-01-01", Until: "2021-12-31"},
	}
	var metas []CatalogPack
	for _, opts := range specs {
		packDir := filepath.Join(dist, opts.PackID)
		if err := BuildWithOptions(db, dir, packDir, key, "", "", opts); err != nil {
			t.Fatal(err)
		}
		trustPack(t, packDir)
		zipPath := filepath.Join(dist, opts.PackID+".zip")
		if err := Archive(packDir, zipPath); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(packDir); err != nil { // 渠道上只放归档
			t.Fatal(err)
		}
		meta, err := ReadPackMeta(zipPath, srvURL+"/"+opts.PackID+".zip")
		if err != nil {
			t.Fatal(err)
		}
		metas = append(metas, meta)
	}
	indexDir := filepath.Join(dist, "index")
	if err := BuildCatalog(metas, indexDir, key); err != nil {
		t.Fatal(err)
	}
	// 目录两件套也放到渠道根（模拟 Release 资产）
	for _, f := range []string{"index.json", "index.sig"} {
		b, err := os.ReadFile(filepath.Join(indexDir, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dist, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := ParseCatalog(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testFetcher() *Fetcher {
	f := NewFetcher()
	f.MinInterval = 0
	return f
}

// TestFetchCatalogAndInstallPacks 端到端（本地 httptest 渠道）：latest API → 目录 →
// 选包（乱序传入）→ 基础包先行串行安装 → 再跑全跳过（幂等）。
func TestFetchCatalogAndInstallPacks(t *testing.T) {
	srv, dist := newDistServer(t)
	buildDist(t, srv.URL, dist)

	// 模拟 GitHub latest API 响应
	apiDir := filepath.Join(dist, "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rel := map[string]any{"assets": []map[string]string{
		{"name": "index.json", "browser_download_url": srv.URL + "/index.json"},
		{"name": "index.sig", "browser_download_url": srv.URL + "/index.sig"},
		{"name": "base-cn-2026.zip", "browser_download_url": srv.URL + "/base-cn-2026.zip"},
	}}
	b, err := json.Marshal(rel)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "latest.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	f := testFetcher()
	ctx := context.Background()
	jsonURL, sigURL, err := LatestIndexURLs(ctx, f, srv.URL+"/api/latest.json")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := FetchCatalog(ctx, f, jsonURL, sigURL)
	if err != nil || len(cat.Packs) != 2 {
		t.Fatalf("目录拉取失败: %v %+v", err, cat)
	}

	target, tdb := openTarget(t)
	var seen []string
	progress := func(s string) { seen = append(seen, s) }
	rep, err := InstallPacks(ctx, tdb, target, f, cat,
		[]string{"delta-20200101-20211231", "base-cn-2026"}, progress)
	if err != nil {
		t.Fatalf("安装应成功: %v %+v", err, rep)
	}
	if len(rep.Reports) != 2 || rep.Reports[0].PackID != "base-cn-2026" {
		t.Errorf("基础包必须先装: %+v", rep.Reports)
	}
	if rep.Reports[0].Imported != 1 || rep.Reports[1].Imported != 1 {
		t.Errorf("两包各应导入 1 条: %+v", rep.Reports)
	}
	var n int
	if err := tdb.QueryRow(`SELECT COUNT(*) FROM document`).Scan(&n); err != nil || n != 2 {
		t.Errorf("安装后应有 2 条，实际 %d", n)
	}

	// 再跑一遍：全部整包跳过（台账幂等），不重复写数据。
	rep2, err := InstallPacks(ctx, tdb, target, f, cat,
		[]string{"base-cn-2026", "delta-20200101-20211231"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ir := range rep2.Reports {
		if !ir.AlreadyApplied {
			t.Errorf("已装包应整包跳过: %+v", ir)
		}
	}
	if err := tdb.QueryRow(`SELECT COUNT(*) FROM document`).Scan(&n); err != nil || n != 2 {
		t.Errorf("重跑不得重复写入，实际 %d", n)
	}
}

// TestDownloadPackTampered 渠道上的归档与目录锚定的 sha256 不符 → 拒收（传输损坏/
// 源被篡改同等对待），不留半成品。
func TestDownloadPackTampered(t *testing.T) {
	srv, dist := newDistServer(t)
	c := buildDist(t, srv.URL, dist)
	if err := os.WriteFile(filepath.Join(dist, "base-cn-2026.zip"), []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, _ := openTarget(t)
	_, err := DownloadPack(context.Background(), testFetcher(), c.Packs[0], filepath.Join(target, "packs"))
	if !errors.Is(err, ErrFileMismatch) {
		t.Errorf("哈希不符应报 ErrFileMismatch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "packs", "base-cn-2026.zip")); !os.IsNotExist(err) {
		t.Error("哈希不符的归档不得留下")
	}
}

// TestFetchCatalogRejectsUntrusted 目录签名自洽但公钥未受信 → 拒收整个目录。
func TestFetchCatalogRejectsUntrusted(t *testing.T) {
	srv, dist := newDistServer(t)
	dir, db, key := setupSource(t)
	packDir := filepath.Join(dist, "base-x")
	if err := BuildWithOptions(db, dir, packDir, key, "", "",
		BuildOptions{PackID: "base-x-1", Kind: "base"}); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dist, "base-x-1.zip")
	if err := Archive(packDir, zipPath); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadPackMeta(zipPath, srv.URL+"/base-x-1.zip")
	if err != nil {
		t.Fatal(err)
	}
	indexDir := filepath.Join(dist, "index")
	if err := BuildCatalog([]CatalogPack{meta}, indexDir, key); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.json", "index.sig"} {
		b, err := os.ReadFile(filepath.Join(indexDir, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dist, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err = FetchCatalog(context.Background(), testFetcher(), srv.URL+"/index.json", srv.URL+"/index.sig")
	if !errors.Is(err, ErrUntrustedKey) {
		t.Errorf("未受信目录应拒收: %v", err)
	}
}

// TestInstallPacksUnknownID 选了目录里没有的包 → 拒绝执行（不开始任何下载）。
func TestInstallPacksUnknownID(t *testing.T) {
	srv, dist := newDistServer(t)
	c := buildDist(t, srv.URL, dist)
	target, tdb := openTarget(t)
	_, err := InstallPacks(context.Background(), tdb, target, testFetcher(), c,
		[]string{"no-such-pack"}, nil)
	if !errors.Is(err, ErrBadCatalog) {
		t.Errorf("目录缺该包应拒绝: %v", err)
	}
}
