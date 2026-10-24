package ingest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// TestEUWorkMapping CDM 字段 → 我们的模型：标识、效力等级、生效/适用双日期、失效占位。
func TestEUWorkMapping(t *testing.T) {
	w := &EUWork{
		Celex: "32016R0679", ELI: "http://data.europa.eu/eli/reg/2016/679/oj",
		TypeLetter: "R", Year: "2016", Number: "679",
		EntryIntoForce: []string{"2016-05-24", "2018-05-25"},
		EndOfValidity:  "9999-12-31",
	}
	if got := w.LawID(); got != "eu_reg_2016_679" {
		t.Fatalf("LawID=%s", got)
	}
	if got := w.EffectLevel(); got != "欧盟条例" {
		t.Fatalf("EffectLevel=%s", got)
	}
	if w.EffectiveDate() != "2016-05-24" || w.ApplicationDate() != "2018-05-25" {
		t.Fatalf("生效/适用日解析错: %v", w.EntryIntoForce)
	}
	if w.ExpireDate() != "" {
		t.Fatalf("9999 占位应转空串，得 %q", w.ExpireDate())
	}
	if w.DefaultName() != "Regulation (EU) 2016/679" {
		t.Fatalf("DefaultName=%s", w.DefaultName())
	}

	// 指令 + 无 ELI → CELEX 回退标识；真实失效日透传
	d := &EUWork{TypeLetter: "L", Year: "2019", Number: "790", EndOfValidity: "2024-06-06"}
	if got := d.LawID(); got != "eu_dir_2019_790" {
		t.Fatalf("指令 LawID=%s", got)
	}
	if d.EffectLevel() != "欧盟指令" || d.ExpireDate() != "2024-06-06" {
		t.Fatal("指令映射错")
	}

	// CELEX 格式校验
	for _, bad := range []string{"GDPR", "32016", "32016r0679x2", "00000X000000"} {
		if celexPattern.MatchString(bad) {
			t.Fatalf("非法 CELEX %q 不应通过", bad)
		}
	}
	if !celexPattern.MatchString("32016R0679") || !celexPattern.MatchString("31995L0046") {
		t.Fatal("合法 CELEX 应通过格式校验")
	}
}

// sparqlJSON 构造 application/sparql-results+json 响应。
func sparqlJSON(vars []string, rows ...map[string]string) string {
	var b []string
	for _, r := range rows {
		var parts []string
		for _, v := range vars {
			parts = append(parts, fmt.Sprintf("%q: {\"type\": \"uri\", \"value\": %q}", v, r[v]))
		}
		b = append(b, "{ "+strings.Join(parts, ", ")+" }")
	}
	return fmt.Sprintf(`{"head": {"vars": [%s]}, "results": {"bindings": [%s]}}`,
		strings.Join(quoteAll(vars), ","), strings.Join(b, ", "))
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

const (
	fakeGraph    = "http://publications.europa.eu/resource/cellar/3e485e15-11bd-11e6-ba9a-01aa75ed71a1"
	fakeRepealed = "http://publications.europa.eu/resource/cellar/102e48a9-478f-11e6-ba9a-01aa75ed71a1"
	fakeBasis    = "http://publications.europa.eu/resource/cellar/fc797fa2-af0e-11e9-01aa75ed71a1"
)

// fakeCellarGDPR 模拟 Cellar SPARQL：robots 允许 + GDPR work 三元组 + sameAs 反查。
func fakeCellarGDPR(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "User-agent: *\nAllow: /\n")
	})
	mux.HandleFunc("/webapi/rdf/sparql", func(w http.ResponseWriter, r *http.Request) {
		q, _ := url.QueryUnescape(r.URL.Query().Get("query"))
		w.Header().Set("Content-Type", "application/sparql-results+json")
		switch {
		case strings.Contains(q, "sameAs> <"+cellarResBase+"/celex/32016R0679"):
			fmt.Fprint(w, sparqlJSON([]string{"g"}, map[string]string{"g": fakeGraph}))
		case strings.Contains(q, "FILTER(str(?s)="):
			cdm := "http://publications.europa.eu/ontology/cdm#"
			triples := []map[string]string{
				{"p": cdm + "resource_legal_eli", "o": "http://data.europa.eu/eli/reg/2016/679/oj"},
				{"p": cdm + "resource_legal_id_celex", "o": "32016R0679"},
				{"p": cdm + "resource_legal_type", "o": "R"},
				{"p": cdm + "resource_legal_year", "o": "2016"},
				{"p": cdm + "resource_legal_number_natural", "o": "679"},
				{"p": cdm + "resource_legal_in-force", "o": "1"},
				{"p": cdm + "resource_legal_date_entry-into-force", "o": "2016-05-24"},
				{"p": cdm + "resource_legal_date_entry-into-force", "o": "2018-05-25"},
				{"p": cdm + "resource_legal_date_end-of-validity", "o": "9999-12-31"},
				{"p": cdm + "work_id_document", "o": "oj:JOL_2016_119_R_0001"},
				{"p": cdm + "resource_legal_repeals_resource_legal", "o": fakeRepealed},
				{"p": cdm + "resource_legal_based_on_resource_legal", "o": fakeBasis},
			}
			fmt.Fprint(w, sparqlJSON([]string{"p", "o"}, triples...))
		case strings.Contains(q, "<"+fakeRepealed+">"):
			fmt.Fprint(w, sparqlJSON([]string{"o"}, map[string]string{"o": cellarResBase + "/celex/31995L0046"}))
		case strings.Contains(q, "<"+fakeBasis+">"):
			fmt.Fprint(w, sparqlJSON([]string{"o"}, map[string]string{"o": cellarResBase + "/celex/32010R1024"}))
		default:
			fmt.Fprint(w, `{"results": {"bindings": []}}`)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestCellarEndToEnd 假 Cellar：元数据获取 → 留证文件 → law upsert → 关系仅挂已入库目标。
func TestCellarEndToEnd(t *testing.T) {
	srv := fakeCellarGDPR(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 预置废止目标法（已入库）；依据目标法故意不入库 → 应 skipped
	if _, err := db.Exec(`INSERT INTO law(law_id,name,effect_level,celex)
		VALUES('eu_dir_1995_46','Directive 95/46/EC','欧盟指令','31995L0046')`); err != nil {
		t.Fatal(err)
	}

	c := &Cellar{DataDir: dir, DB: db, MinInterval: time.Millisecond,
		Endpoint: srv.URL + "/webapi/rdf/sparql"}
	ctx := context.Background()

	w, err := c.FetchWork(ctx, "32016R0679")
	if err != nil {
		t.Fatal(err)
	}
	if w.ELI == "" || w.TypeLetter != "R" || !w.InForce ||
		len(w.EntryIntoForce) != 2 || w.OJRef != "JOL_2016_119_R_0001" ||
		len(w.Repeals) != 1 || len(w.BasedOnLegal) != 1 {
		t.Fatalf("元数据解析不全: %+v", w)
	}
	if _, err := os.Stat(filepath.Join(dir, "incoming", "eu_32016R0679_work.json")); err != nil {
		t.Fatalf("原始三元组未留证: %v", err)
	}

	lawID, err := c.UpsertLaw(w, "欧盟通用数据保护条例", "GDPR", "")
	if err != nil {
		t.Fatal(err)
	}
	if lawID != "eu_reg_2016_679" {
		t.Fatalf("lawID=%s", lawID)
	}
	var level, eli string
	if err := db.QueryRow(`SELECT effect_level, eli FROM law WHERE law_id=?`, lawID).Scan(&level, &eli); err != nil {
		t.Fatal(err)
	}
	if level != "欧盟条例" || !strings.HasPrefix(eli, "http://data.europa.eu/eli/") {
		t.Fatalf("law 写入错: %s %s", level, eli)
	}

	linked, skipped, err := c.LinkRelations(ctx, lawID, w)
	if err != nil {
		t.Fatal(err)
	}
	if linked != 1 || skipped != 1 {
		t.Fatalf("linked=%d skipped=%d，期望 1/1", linked, skipped)
	}
	var rType, status string
	if err := db.QueryRow(`SELECT relation_type, confirm_status FROM relation WHERE src_law_id=?`, lawID).
		Scan(&rType, &status); err != nil {
		t.Fatal(err)
	}
	if rType != "废止" || status != "pending" {
		t.Fatalf("关系应为 废止/pending，得 %s/%s", rType, status)
	}

	// 每次 SPARQL 访问都进了 import_log
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM import_log WHERE result='success' AND reason LIKE '[cellar]%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n < 4 {
		t.Fatalf("import_log 应记录全部 SPARQL 访问，得 %d", n)
	}

	// 幂等重跑：law upsert 不报错、relation INSERT OR IGNORE 不重复
	if _, err := c.UpsertLaw(w, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM relation WHERE src_law_id=?`, lawID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("关系应不重复: %d (%v)", n, err)
	}
}

// TestCellarRejectsBadCELEX 非法 CELEX 不得触网。
func TestCellarRejectsBadCELEX(t *testing.T) {
	c := &Cellar{DataDir: t.TempDir(), MinInterval: time.Millisecond}
	if _, err := c.FetchWork(context.Background(), "GDPR"); err == nil {
		t.Fatal("非法 CELEX 应被拒绝")
	}
}
