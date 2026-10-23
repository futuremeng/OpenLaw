package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

// mockSite 可控 robots 与文件路由的测试站点。
func mockSite(t *testing.T, robotsBody string, robotsStatus int, files map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	if robotsBody != "" {
		mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(robotsStatus)
			_, _ = w.Write([]byte(robotsBody))
		})
	} else {
		mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}
	for p, body := range files {
		p, body := p, body
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			if body == "" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte(body))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func testFetcher(t *testing.T, srvURL string) *Fetcher {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Fetcher{DataDir: dir, DB: db, Policy: FetchPolicy{AllowHosts: []string{"127.0.0.1"}}}
}

// TestRobotsDisallowAll flk 型站点（Disallow: /）→ 一律拒绝。
func TestRobotsDisallowAll(t *testing.T) {
	srv := mockSite(t, "User-agent: *\nDisallow: /\n", 200, map[string]string{"/a.pdf": "%PDF-1.4 fake"})
	f := testFetcher(t, srv.URL)
	if ok, why := f.Robots().Allowed(context.Background(), srv.URL+"/a.pdf"); ok {
		t.Fatalf("全站 Disallow 应拒绝: %s", why)
	}
	if _, err := f.Fetch(context.Background(), srv.URL+"/a.pdf", "doc_a"); err == nil ||
		!strings.Contains(err.Error(), "robots") {
		t.Fatalf("Fetch 应被 robots 拒绝: %v", err)
	}
}

// TestRobotsPathRules 路径规则：最长前缀匹配，Allow 覆盖同级 Disallow。
func TestRobotsPathRules(t *testing.T) {
	srv := mockSite(t, "User-agent: *\nDisallow: /private/\nAllow: /private/public/\n", 200, nil)
	f := testFetcher(t, srv.URL)
	rc := f.Robots()
	ctx := context.Background()
	if ok, _ := rc.Allowed(ctx, srv.URL+"/pub/a.pdf"); !ok {
		t.Fatal("/pub/ 应允许")
	}
	if ok, _ := rc.Allowed(ctx, srv.URL+"/private/x.pdf"); ok {
		t.Fatal("/private/ 应拒绝")
	}
	if ok, _ := rc.Allowed(ctx, srv.URL+"/private/public/y.pdf"); !ok {
		t.Fatal("更长的 Allow 应覆盖 Disallow")
	}
}

// TestRobotsMissing 无 robots（404）→ 无限制（惯例）。
func TestRobotsMissing(t *testing.T) {
	srv := mockSite(t, "", 0, nil)
	f := testFetcher(t, srv.URL)
	if ok, _ := f.Robots().Allowed(context.Background(), srv.URL+"/a.pdf"); !ok {
		t.Fatal("无 robots 应允许")
	}
}

// TestRobotsFailClosed robots 获取失败（5xx）→ 拒绝（宁可不抓也不违规）。
func TestRobotsFailClosed(t *testing.T) {
	srv := mockSite(t, "err", 500, nil)
	f := testFetcher(t, srv.URL)
	if ok, _ := f.Robots().Allowed(context.Background(), srv.URL+"/a.pdf"); ok {
		t.Fatal("robots 5xx 应 fail-closed 拒绝")
	}
}

// TestBlockedHostsFlk flk.npc.gov.cn 明确禁止自动化采集 → 硬拒绝，先于任何网络请求。
func TestBlockedHostsFlk(t *testing.T) {
	f := &Fetcher{DataDir: t.TempDir()}
	_, err := f.Fetch(context.Background(), "https://flk.npc.gov.cn/some.pdf", "x")
	if err == nil || !strings.Contains(err.Error(), "禁止自动化采集") {
		t.Fatalf("flk 应被硬禁止: %v", err)
	}
}

// TestRobotsOtherGroupIgnored 非本 token 的组规则不影响判定。
func TestRobotsOtherGroupIgnored(t *testing.T) {
	srv := mockSite(t, "User-agent: BadBot\nDisallow: /\n", 200, nil)
	f := testFetcher(t, srv.URL)
	if ok, _ := f.Robots().Allowed(context.Background(), srv.URL+"/a.pdf"); !ok {
		t.Fatal("仅约束 BadBot 的组不应影响 openlaw")
	}
}
