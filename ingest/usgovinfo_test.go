package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// usSitemapBody 一份最小可用的 govinfo 型 sitemapindex（两条子清单）。
const usSitemapBody = `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://www.govinfo.gov/sitemap/bulkdata/ECFR/sitemap-0.xml</loc><lastmod>2026-10-30</lastmod></sitemap>
  <sitemap><loc>https://www.govinfo.gov/sitemap/bulkdata/ECFR/sitemap-1.xml</loc><lastmod>2026-10-30</lastmod></sitemap>
</sitemapindex>`

func testUSGovinfo(t *testing.T, robots string) *USGovinfo {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	files := map[string]string{"/sitemap/bulkdata/ECFR/sitemapindex.xml": usSitemapBody}
	srv := mockSite(t, robots, 200, files)
	return &USGovinfo{DataDir: dir, DB: db,
		Policy:      FetchPolicy{AllowHosts: []string{"127.0.0.1"}},
		MinInterval: 10 * time.Millisecond, SitemapBase: srv.URL + "/sitemap/bulkdata"}
}

func importLogRows(t *testing.T, db *store.DB) []struct{ result, reason string } {
	t.Helper()
	rows, err := db.Query(`SELECT result, reason FROM import_log WHERE source_url LIKE '%sitemapindex.xml%' ORDER BY import_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []struct{ result, reason string }
	for rows.Next() {
		var r struct{ result, reason string }
		if err := rows.Scan(&r.result, &r.reason); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// TestUSGovinfoSitemap 骨架端到端（httptest 离线）：robots 开放 → 清单解析、
// 原始留证落 incoming/、import_log 留成功痕。
func TestUSGovinfoSitemap(t *testing.T) {
	u := testUSGovinfo(t, "User-agent: *\nAllow: /\n")
	idx, err := u.FetchSitemapIndex(context.Background(), "ecfr")
	if err != nil {
		t.Fatalf("清单拉取应成功: %v", err)
	}
	if idx.Collection != "ECFR" || len(idx.Locs) != 2 {
		t.Errorf("清单解析错: %+v", idx)
	}
	raw := filepath.Join(u.DataDir, idx.RawPath)
	bodyB, err := os.ReadFile(raw)
	if err != nil {
		t.Fatalf("留证文件缺失: %v", err)
	}
	body := string(bodyB)
	if !strings.Contains(body, "sitemap-1.xml") {
		t.Error("留证必须保存原始响应本体")
	}
	if idx.SHA256 == "" {
		t.Error("留证必须带 SHA-256")
	}
	logs := importLogRows(t, u.DB)
	if len(logs) != 1 || logs[0].result != "success" {
		t.Errorf("import_log 应留一条成功痕: %+v", logs)
	}
}

// TestUSGovinfoRobotsFailClosed 站点 Disallow: / → 拒绝并留 failed 痕（不留假成功）。
func TestUSGovinfoRobotsFailClosed(t *testing.T) {
	u := testUSGovinfo(t, "User-agent: *\nDisallow: /\n")
	if _, err := u.FetchSitemapIndex(context.Background(), "ECFR"); err == nil ||
		!strings.Contains(err.Error(), "robots 策略拒绝") {
		t.Fatalf("robots 禁止必须 fail-closed: %v", err)
	}
	logs := importLogRows(t, u.DB)
	if len(logs) != 1 || logs[0].result != "failed" {
		t.Errorf("拒绝也应留痕: %+v", logs)
	}
}

// TestUSGovinfoGuards 名单外集合拒绝；空清单如实失败；限速下限不可低于 2s。
func TestUSGovinfoGuards(t *testing.T) {
	u := testUSGovinfo(t, "User-agent: *\nAllow: /\n")
	if _, err := u.BulkSitemapURL("USCODE"); err == nil {
		t.Error("USCODE 无批量包，名单外集合必须拒绝")
	}
	if u.interval() < usMinIntervalFloor {
		t.Errorf("限速下限不可被配置绕过（当前 %v）", u.interval())
	}
	// 空清单：200 但无 <loc> → 失败而非假成功。
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := mockSite(t, "User-agent: *\nAllow: /\n", 200,
		map[string]string{"/sitemap/bulkdata/CFR/sitemapindex.xml": `<?xml version="1.0"?><sitemapindex></sitemapindex>`})
	u2 := &USGovinfo{DataDir: dir, DB: db, SitemapBase: srv.URL + "/sitemap/bulkdata",
		Policy: FetchPolicy{AllowHosts: []string{"127.0.0.1"}}, MinInterval: 10 * time.Millisecond}
	if _, err := u2.FetchSitemapIndex(context.Background(), "CFR"); err == nil ||
		!strings.Contains(err.Error(), "清单为空") {
		t.Fatalf("空清单必须如实失败: %v", err)
	}
}
