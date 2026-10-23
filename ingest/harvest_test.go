package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestListAndBatchFetch 两阶段批量（19 章）：目录页 → 候选清单 → 限速下载。
// 白名单外主机被过滤；下载失败记录跳过不中断；import_log 全量留痕。
func TestListAndBatchFetch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/gongbao/2026/issue_1/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body>
			<a href="./material/a.pdf">整期PDF</a>
			<a href="material/b.pdf">另一PDF</a>
			<a href="https://example.org/c.pdf">外站</a>
			<a href="./note.html">非PDF</a>
		</body></html>`))
	})
	mux.HandleFunc("/gongbao/2026/issue_1/material/a.pdf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4 A"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	f := testFetcher(t, srv.URL)
	ctx := context.Background()

	cands, err := f.ListCandidates(ctx, srv.URL+"/gongbao/2026/issue_1/")
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 {
		t.Fatalf("应得 2 个候选（外站与非 PDF 被过滤）: %+v", cands)
	}
	if cands[0].Name != "a" || cands[1].Name != "b" {
		t.Fatalf("doc_id 派生异常: %+v", cands)
	}

	// CSV 往返（供人工审阅编辑）
	csvPath := filepath.Join(t.TempDir(), "candidates.csv")
	if err := WriteCandidatesCSV(csvPath, cands); err != nil {
		t.Fatal(err)
	}
	back, err := ReadCandidatesCSV(csvPath)
	if err != nil || len(back) != 2 {
		t.Fatalf("CSV 往返失败: %v %+v", err, back)
	}

	items := f.BatchFetch(ctx, back, 5*time.Millisecond)
	if len(items) != 2 {
		t.Fatalf("应处理 2 条: %+v", items)
	}
	if items[0].Status != "success" || items[0].SHA256 == "" {
		t.Fatalf("a.pdf 应成功: %+v", items[0])
	}
	if items[1].Status != "failed" {
		t.Fatalf("b.pdf（404）应失败但不中断: %+v", items[1])
	}
	if _, err := os.Stat(filepath.Join(f.DataDir, "incoming", "a.pdf")); err != nil {
		t.Fatal("成功条目应落暂存区")
	}

	var okN, failN int
	if err := f.DB.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result='success' AND reason LIKE '[fetch%'`).Scan(&okN); err != nil || okN != 1 {
		t.Fatalf("import_log 应有 1 条 fetch success（%v）", err)
	}
	if err := f.DB.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result='failed' AND reason LIKE '[fetch%'`).Scan(&failN); err != nil || failN != 1 {
		t.Fatalf("import_log 应有 1 条 fetch failed（%v）", err)
	}
}

// TestBatchFetchRespectsBlockedHost 清单里混入禁止名单主机 → 该条失败，其余正常。
func TestBatchFetchRespectsBlockedHost(t *testing.T) {
	f := testFetcher(t, "http://127.0.0.1")
	items := f.BatchFetch(context.Background(), []Candidate{
		{URL: "https://flk.npc.gov.cn/x.pdf", Name: "flk_x"},
	}, time.Millisecond)
	if len(items) != 1 || items[0].Status != "failed" {
		t.Fatalf("flk 条目必须失败: %+v", items)
	}
}
