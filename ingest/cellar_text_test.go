package ingest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

// fakeCellarText 假 Cellar representation 层：expression → manifestation → items。
// 结构复刻 2026-10-25 GDPR 实测样本（oj/… 与 cellar/… 双 URI、fmx4 wrapper+正文）。
func fakeCellarText(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	body := []byte(`<ACT><PREAMBLE><PREAMBLE.INIT>THE COUNCIL,</PREAMBLE.INIT></PREAMBLE>
<ARTICLE IDENTIFIER="001"><TI.ART>Article 1</TI.ART><STI.ART>Scope</STI.ART>
<PARAG IDENTIFIER="001.001"><NO.PARAG>1.</NO.PARAG><ALINEA>Text one.</ALINEA></PARAG></ARTICLE>
<ARTICLE IDENTIFIER="002"><TI.ART>Article 2</TI.ART>
<PARAG IDENTIFIER="002.001"><NO.PARAG>1.</NO.PARAG><ALINEA>Text two.</ALINEA></PARAG></ARTICLE></ACT>`)

	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "User-agent: *\nAllow: /\n")
	})
	mani := "http://publications.europa.eu/resource/oj/JOL_2016_119_R_0001.ENG.fmx4"
	cellarMani := "http://publications.europa.eu/resource/cellar/uuid1.0006.02"
	mux.HandleFunc("/celex/32016R0679.ENG", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="http://publications.europa.eu/resource/oj/JOL_2016_119_R_0001.ENG">
<cdm:expression_manifested_by_manifestation xmlns:cdm="http://publications.europa.eu/ontology/cdm#" rdf:resource="%s"/>
<cdm:title xmlns:cdm="http://publications.europa.eu/ontology/cdm#">Regulation (EU) 2016/679 sample title</cdm:title>
</rdf:Description></rdf:RDF>`, mani)
	})
	mux.HandleFunc("/oj/JOL_2016_119_R_0001.ENG.fmx4/rdf/object/full", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="%s/DOC_1"><item_identifier xmlns="http://publications.europa.eu/ontology/cdm#">DOC_1</item_identifier></rdf:Description>
<rdf:Description rdf:about="%s/DOC_2"><item_identifier xmlns="http://publications.europa.eu/ontology/cdm#">DOC_2</item_identifier></rdf:Description>
</rdf:RDF>`, cellarMani, cellarMani)
	})
	mux.HandleFunc("/cellar/uuid1.0006.02/DOC_1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<DOC>tiny wrapper</DOC>") // 小体量：应被「取最大者」策略淘汰
	})
	mux.HandleFunc("/cellar/uuid1.0006.02/DOC_2", func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, string(body)
}

// TestFetchTextFourStep 四步链：expression→manifestation→items→取最大正文；
// 留证文件落 incoming；URI 要素齐全；import_log 留痕。
func TestFetchTextFourStep(t *testing.T) {
	srv, wantBody := fakeCellarText(t)
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := &Cellar{DataDir: dir, DB: db, Endpoint: srv.URL, MinInterval: 0}

	tx, err := c.FetchText(context.Background(), "32016R0679", "eng", "fmx4")
	if err != nil {
		t.Fatalf("FetchText: %v", err)
	}
	if string(tx.Bytes) != wantBody {
		t.Errorf("正文应为最大 item（DOC_2），实得 %.60q", tx.Bytes)
	}
	if tx.Title != "Regulation (EU) 2016/679 sample title" {
		t.Errorf("Title = %q", tx.Title)
	}
	if !strings.HasSuffix(tx.ItemURI, "/DOC_2") {
		t.Errorf("ItemURI = %s", tx.ItemURI)
	}
	if tx.SHA256 == "" || tx.Size == 0 {
		t.Errorf("sha256/size 缺失")
	}
	if b, err := os.ReadFile(filepath.Join(dir, tx.RawPath)); err != nil || string(b) != wantBody {
		t.Errorf("留证文件缺失或内容不符: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM import_log WHERE source_url IS NOT NULL`).Scan(&n); err != nil || n < 3 {
		t.Errorf("import_log 记录不足: n=%d err=%v", n, err)
	}
}

// TestFetchTextRejectsBadInput 非法 CELEX/语言/格式必须报错。
func TestFetchTextRejectsBadInput(t *testing.T) {
	srv, _ := fakeCellarText(t)
	db, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := &Cellar{DataDir: t.TempDir(), DB: db, Endpoint: srv.URL, MinInterval: 0}
	ctx := context.Background()
	for _, bad := range [][3]string{{"bad", "eng", "fmx4"}, {"32016R0679", "english", "fmx4"}, {"32016R0679", "eng", "pdf"}} {
		if _, err := c.FetchText(ctx, bad[0], bad[1], bad[2]); err == nil {
			t.Errorf("非法输入 %v 应报错", bad)
		}
	}
}
