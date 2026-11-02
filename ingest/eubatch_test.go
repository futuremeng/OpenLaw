package ingest

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// fakeCellarBatch 批量假 Cellar：SPARQL（仅 GDPR 有 work graph）+ representation
// 四步链；fail429 开关模拟配额窗口关闭（expression 层整体 429）。
func fakeCellarBatch(t *testing.T) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	fail := &atomic.Bool{}
	body := []byte(`<ACT><ARTICLE IDENTIFIER="001"><TI.ART>Article 1</TI.ART>
<PARAG IDENTIFIER="001.001"><NO.PARAG>1.</NO.PARAG><ALINEA>Text one.</ALINEA></PARAG></ARTICLE></ACT>`)
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "User-agent: *\nAllow: /\n")
	})
	mux.HandleFunc("/sparql", func(w http.ResponseWriter, r *http.Request) {
		q, _ := url.QueryUnescape(r.URL.Query().Get("query"))
		w.Header().Set("Content-Type", "application/sparql-results+json")
		switch {
		case strings.Contains(q, "sameAs> <"+cellarResBase+"/celex/32016R0679"):
			fmt.Fprint(w, sparqlJSON([]string{"g"}, map[string]string{"g": fakeGraph}))
		case strings.Contains(q, "FILTER(str(?s)="):
			cdm := "http://publications.europa.eu/ontology/cdm#"
			fmt.Fprint(w, sparqlJSON([]string{"p", "o"},
				map[string]string{"p": cdm + "resource_legal_type", "o": "R"},
				map[string]string{"p": cdm + "resource_legal_date_entry-into-force", "o": "2016-05-24"},
			))
		default:
			fmt.Fprint(w, `{"results": {"bindings": []}}`)
		}
	})
	mux.HandleFunc("/sparql/celex/", func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if !strings.Contains(r.URL.Path, "32016R0679") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `<rdf:RDF xmlns:rdf="http://www.w3.org/1999-02-22-rdf-syntax-ns#">
<rdf:Description rdf:about="http://publications.europa.eu/resource/oj/JOL_2016_119_R_0001.ENG">
<cdm:expression_manifested_by_manifestation xmlns:cdm="http://publications.europa.eu/ontology/cdm#" rdf:resource="http://publications.europa.eu/resource/oj/JOL_2016_119_R_0001.ENG.fmx4"/>
<cdm:title xmlns:cdm="http://publications.europa.eu/ontology/cdm#">Regulation sample</cdm:title>
</rdf:Description></rdf:RDF>`)
	})
	mux.HandleFunc("/sparql/oj/JOL_2016_119_R_0001.ENG.fmx4/rdf/object/full", func(w http.ResponseWriter, r *http.Request) {
		cellman := "http://publications.europa.eu/resource/cellar/uuid1.0006.02"
		fmt.Fprintf(w, `<rdf:RDF xmlns:rdf="http://www.w3.org/1999-02-22-rdf-syntax-ns#">
<rdf:Description rdf:about="%s/DOC_1"><item_identifier xmlns="http://publications.europa.eu/ontology/cdm#">DOC_1</item_identifier></rdf:Description>
</rdf:RDF>`, cellman)
	})
	mux.HandleFunc("/sparql/cellar/uuid1.0006.02/DOC_1", func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, fail
}

func setupEUBatch(t *testing.T) (string, *store.DB, *Cellar, string, *atomic.Bool) {
	t.Helper()
	srv, fail := fakeCellarBatch(t)
	dir := t.TempDir()
	for _, sub := range []string{"pdf", "md", "db", "provenance", "incoming"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := &Cellar{DataDir: dir, DB: db, Endpoint: srv.URL + "/sparql",
		MinInterval: time.Millisecond, BackoffBase: time.Millisecond}
	queue := filepath.Join(dir, "queue.json")
	return dir, db, c, queue, fail
}

func seedEUQueue(t *testing.T, db *store.DB, queue string, celexes ...string) {
	t.Helper()
	items := []map[string]string{}
	for i, cx := range celexes {
		lawID := fmt.Sprintf("law_%d", i)
		if _, err := db.Exec(`INSERT INTO law(law_id, name, celex, effect_level, jurisdiction)
			VALUES(?,?,?,'欧盟条例','EU')`, lawID, "EU law "+cx, cx); err != nil {
			t.Fatal(err)
		}
		items = append(items, map[string]string{"celex": cx})
	}
	b, _ := json.Marshal(items)
	if err := os.WriteFile(queue, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestEUBatchCircuitBreak 连续限流 ≥ 阈值：整批熔断中止，剩余如实留 pending，
// 队列与报告都在——不得假装完成。
func TestEUBatchCircuitBreak(t *testing.T) {
	dir, db, c, queue, fail := setupEUBatch(t)
	fail.Store(true)
	seedEUQueue(t, db, queue, "32016R0679", "32015R1234", "32014R0001", "32013R0002", "32012R0003", "32011R0004")
	res, err := RunEUBatch(context.Background(), dir, db, c, EUBatchOptions{QueuePath: queue, CircuitBreak: 3}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Aborted || res.Consecutive429 != 3 || res.Done != 0 || res.Pending != 6 {
		t.Errorf("熔断结果: %+v", res)
	}
	items, _ := LoadEUQueue(queue)
	for _, it := range items {
		if it.Status != "pending" {
			t.Fatalf("熔断后队列必须如实 pending: %+v", items)
		}
	}
}

// TestEUBatchResume 中断-恢复演练：第一轮限流中止（pending），配额恢复后重跑
// 同一队列——成功入库、失败如实 failed，done 不重复抓取。
func TestEUBatchResume(t *testing.T) {
	dir, db, c, queue, fail := setupEUBatch(t)
	fail.Store(true)
	seedEUQueue(t, db, queue, "32016R0679", "32015R1234")
	if _, err := RunEUBatch(context.Background(), dir, db, c, EUBatchOptions{QueuePath: queue, CircuitBreak: 2}, nil); err != nil {
		t.Fatal(err)
	}
	fail.Store(false)
	res, err := RunEUBatch(context.Background(), dir, db, c, EUBatchOptions{QueuePath: queue, CircuitBreak: 5}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Done != 1 || res.Failed != 1 || res.Aborted {
		t.Errorf("恢复轮结果: %+v", res)
	}
	items, _ := LoadEUQueue(queue)
	if items[0].Status != "done" || items[0].DocID != "eu_32016r0679_eng" || items[1].Status != "failed" {
		t.Errorf("队列回写: %+v", items)
	}
	var confirm, lang string
	if err := db.QueryRow(`SELECT confirm_status, lang FROM document WHERE doc_id='eu_32016r0679_eng'`).
		Scan(&confirm, &lang); err != nil || confirm != "pending" || lang != "eng" {
		t.Errorf("入库闸口/语言: %v %s %s", err, confirm, lang)
	}
	f, err := os.Open(res.ReportPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil || len(recs) != 3 {
		t.Fatalf("报告应表头+2 行: %v %v", err, recs)
	}
	if recs[1][0] != "32016R0679" || recs[1][2] != "done" || recs[2][2] != "failed" {
		t.Errorf("报告行: %v", recs)
	}
	before := hitCount(t, db)
	res, err = RunEUBatch(context.Background(), dir, db, c, EUBatchOptions{QueuePath: queue}, nil)
	if err != nil || res.Done != 1 || res.Failed != 1 {
		t.Errorf("终轮: %+v %v", res, err)
	}
	if got := hitCount(t, db); got != before {
		t.Errorf("done 条目不得再发请求: %d → %d", before, got)
	}
}

func hitCount(t *testing.T, db *store.DB) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM import_log`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
