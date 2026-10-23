package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

func setupFetcher(t *testing.T, policy FetchPolicy) (*Fetcher, string) {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"db", "incoming"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Fetcher{DataDir: dir, DB: db, Policy: policy}, dir
}

func pdfServer(body []byte, contentType string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/law.pdf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Write(body)
	})
	mux.HandleFunc("/missing.pdf", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	return httptest.NewServer(mux)
}

func logCount(t *testing.T, db *store.DB, result string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result = ?`, result).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestFetchSuccess 抓取成功：文件进暂存区、哈希计算、import_log 记录
// source_url/http_status/content_type/抓取时间（4.2）。
func TestFetchSuccess(t *testing.T) {
	body := []byte("%PDF-1.4 official gazette sample\n")
	srv := pdfServer(body, "application/pdf")
	defer srv.Close()

	f, dir := setupFetcher(t, FetchPolicy{})
	res, err := f.Fetch(context.Background(), srv.URL+"/law.pdf", "doc_fetch_1")
	if err != nil {
		t.Fatal(err)
	}
	if res.HTTPStatus != 200 || res.SHA256 == "" || res.Size != int64(len(body)) {
		t.Fatalf("抓取结果异常: %+v", res)
	}
	staged := filepath.Join(dir, "incoming", "doc_fetch_1.pdf")
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("暂存文件缺失: %v", err)
	}
	var n int
	var srcURL, ctype string
	if err := f.DB.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result='success' AND http_status=200`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("应记录 http_status=200 的 success 日志，实际 %d (%v)", n, err)
	}
	if err := f.DB.QueryRow(`SELECT source_url, content_type FROM import_log ORDER BY import_id DESC LIMIT 1`).Scan(&srcURL, &ctype); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(srcURL, "/law.pdf") || ctype != "application/pdf" {
		t.Fatalf("日志必须记录来源与 Content-Type: %s %s", srcURL, ctype)
	}
}

// TestFetchRejectsNonPDF 门户页（text/html）必须拒绝，不得入库。
func TestFetchRejectsNonPDF(t *testing.T) {
	srv := pdfServer([]byte("<html>公报目录</html>"), "text/html; charset=utf-8")
	defer srv.Close()
	f, _ := setupFetcher(t, FetchPolicy{})
	if _, err := f.Fetch(context.Background(), srv.URL+"/law.pdf", "doc_x"); err == nil {
		t.Fatal("非 PDF 应被拒绝")
	}
	if logCount(t, f.DB, "failed") != 1 {
		t.Fatal("失败必须写 import_log")
	}
}

// TestFetchHTTPStatus 404 等异常状态码必须显式失败并留痕。
func TestFetchHTTPStatus(t *testing.T) {
	srv := pdfServer(nil, "application/pdf")
	defer srv.Close()
	f, _ := setupFetcher(t, FetchPolicy{})
	if _, err := f.Fetch(context.Background(), srv.URL+"/missing.pdf", "doc_y"); err == nil {
		t.Fatal("404 应失败")
	}
	var status int
	if err := f.DB.QueryRow(`SELECT http_status FROM import_log ORDER BY import_id DESC LIMIT 1`).Scan(&status); err != nil || status != 404 {
		t.Fatalf("日志必须记录 http_status=404，实际 %d (%v)", status, err)
	}
}

// TestFetchSizeLimit 超过大小上限必须失败且不留下半截文件。
func TestFetchSizeLimit(t *testing.T) {
	big := []byte("%PDF-1.4 " + strings.Repeat("A", 4096))
	srv := pdfServer(big, "application/pdf")
	defer srv.Close()
	f, dir := setupFetcher(t, FetchPolicy{MaxSize: 128})
	if _, err := f.Fetch(context.Background(), srv.URL+"/law.pdf", "doc_big"); err == nil {
		t.Fatal("超限应失败")
	}
	if _, err := os.Stat(filepath.Join(dir, "incoming", "doc_big.pdf")); err == nil {
		t.Fatal("超限不得留下文件")
	}
}

// TestFetchHostAllowlist 域名白名单拦截（18 章：优先官方渠道）。
func TestFetchHostAllowlist(t *testing.T) {
	srv := pdfServer([]byte("%PDF-1.4\n"), "application/pdf")
	defer srv.Close()
	f, _ := setupFetcher(t, FetchPolicy{AllowHosts: []string{"www.npc.gov.cn"}})
	if _, err := f.Fetch(context.Background(), srv.URL+"/law.pdf", "doc_h"); err == nil {
		t.Fatal("白名单外域名应被拒绝")
	}
}

// TestFetchRejectsPlainHTTP 明文 http 默认拒绝（回环测试地址除外）。
func TestFetchRejectsPlainHTTP(t *testing.T) {
	f, _ := setupFetcher(t, FetchPolicy{AllowHosts: nil})
	if _, err := f.Fetch(context.Background(), "http://example.com/law.pdf", "doc_p"); err == nil {
		t.Fatal("明文 http 应被拒绝")
	}
	if _, err := f.Fetch(context.Background(), "ftp://example.com/law.pdf", "doc_q"); err == nil {
		t.Fatal("非 http(s) 协议应被拒绝")
	}
}
