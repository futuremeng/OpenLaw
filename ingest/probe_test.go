package ingest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

// fakeProbe 可配状态码的探测假端点：robots.txt 与 expression 各自独立可控。
func fakeProbe(t *testing.T, robotsStatus, exprStatus int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		if robotsStatus != 200 {
			w.WriteHeader(robotsStatus)
			return
		}
		fmt.Fprint(w, "User-agent: *\nAllow: /\n")
	})
	mux.HandleFunc("/celex/32016R0679.ENG", func(w http.ResponseWriter, r *http.Request) {
		if exprStatus != 200 {
			w.WriteHeader(exprStatus)
			return
		}
		fmt.Fprint(w, `<rdf:RDF xmlns:rdf="http://www.w3.org/1999-02-22-rdf-syntax-ns#">`+
			`<rdf:Description rdf:about="http://publications.europa.eu/resource/oj/X.ENG">`+
			`<cdm:expression_manifested_by_manifestation xmlns:cdm="http://publications.europa.eu/ontology/cdm#" rdf:resource="http://publications.europa.eu/resource/oj/X.ENG.fmx4"/>`+
			`</rdf:Description></rdf:RDF>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func probeCellar(t *testing.T, srv *httptest.Server) *Cellar {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Cellar{DataDir: t.TempDir(), DB: db, Endpoint: srv.URL, MinInterval: 0}
}

// TestProbeOpen 两层全 200 → 窗口开。
func TestProbeOpen(t *testing.T) {
	c := probeCellar(t, fakeProbe(t, 200, 200))
	pr, err := c.Probe(context.Background(), "32016R0679", "eng")
	if err != nil {
		t.Fatal(err)
	}
	if !pr.Open || !pr.ExprOK || pr.RobotsStatus != 200 || pr.ExprStatus != 200 {
		t.Errorf("窗口开判定失败: %+v", pr)
	}
	var n int
	if err := c.DB.QueryRow(`SELECT COUNT(*) FROM import_log WHERE reason LIKE '[cellar-probe]%'`).Scan(&n); err != nil || n < 1 {
		t.Errorf("探测结论未留痕: n=%d err=%v", n, err)
	}
}

// TestProbeRobotsRateLimited robots 429 → 限流判定，且 expression 不发起（合规省配额）。
func TestProbeRobotsRateLimited(t *testing.T) {
	c := probeCellar(t, fakeProbe(t, 429, 200))
	pr, err := c.Probe(context.Background(), "32016R0679", "eng")
	if err != nil {
		t.Fatal(err)
	}
	if !pr.RateLimited || pr.Open || pr.RobotsStatus != 429 || pr.ExprStatus != 0 {
		t.Errorf("robots 429 判定失败: %+v", pr)
	}
	if !strings.Contains(pr.Suggest, "30") {
		t.Errorf("限流应给出退避建议: %q", pr.Suggest)
	}
}

// TestProbeExprRateLimited robots 通过但 expression 429 → 限流判定（nginx 资源层）。
func TestProbeExprRateLimited(t *testing.T) {
	c := probeCellar(t, fakeProbe(t, 200, 429))
	pr, err := c.Probe(context.Background(), "32016R0679", "eng")
	if err != nil {
		t.Fatal(err)
	}
	if !pr.RateLimited || pr.Open || pr.ExprStatus != 429 {
		t.Errorf("expression 429 判定失败: %+v", pr)
	}
}

// TestProbeNotFound 404 不是限流：输入问题，不应给出退避等待判定。
func TestProbeNotFound(t *testing.T) {
	c := probeCellar(t, fakeProbe(t, 200, 404))
	pr, err := c.Probe(context.Background(), "32016R0679", "eng")
	if err != nil {
		t.Fatal(err)
	}
	if pr.RateLimited || pr.Open {
		t.Errorf("404 不应判为限流或窗口开: %+v", pr)
	}
}

// TestProbeRejectsBadInput 非法 CELEX/语言必须报错（不发起任何请求）。
func TestProbeRejectsBadInput(t *testing.T) {
	c := probeCellar(t, fakeProbe(t, 200, 200))
	for _, bad := range [][2]string{{"bad", "eng"}, {"32016R0679", "english"}} {
		if _, err := c.Probe(context.Background(), bad[0], bad[1]); err == nil {
			t.Errorf("非法输入 %v 应报错", bad)
		}
	}
}
