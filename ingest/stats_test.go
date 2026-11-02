package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

// TestEUQuotaStats 合成 import_log：按日聚合（请求/成败/429/字节）与 CELEX 归因；
// 非 europa.eu 来源不混入；since 过滤生效。
func TestEUQuotaStats(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log := func(url, result string, status int, size int, at string) {
		if _, err := db.Exec(`INSERT INTO import_log(doc_id, source_url, file_path, file_size, sha256,
			http_status, content_type, result, reason, created_at)
			VALUES(NULL,?,NULL,?,NULL,?,NULL,?,?,?)`, url, size, status, result, "", at); err != nil {
			t.Fatal(err)
		}
	}
	// 2026-10-25 429 爆发段 + 2026-10-26 正常段；一条中国通道不混入。
	log("https://publications.europa.eu/resource/celex/32016R0679.ENG", "failed", 429, 120, "2026-10-25T08:00:00Z")
	log("https://publications.europa.eu/resource/celex/32016R0679.ENG", "failed", 429, 120, "2026-10-25T08:10:00Z")
	log("https://op.europa.eu/resource/cellar/u1.0006.02/DOC_2", "failed", 429, 120, "2026-10-25T08:20:00Z")
	log("https://publications.europa.eu/resource/celex/32016R0679.ENG", "success", 200, 90000, "2026-10-26T02:00:00Z")
	log("https://publications.europa.eu/resource/oj/JOL_2016_119_R_0001.ENG.fmx4/rdf/object/full", "success", 200, 4000, "2026-10-26T02:00:05Z")
	log("https://a14-gov-app.flk.naeps.org/x", "success", 200, 5000, "2026-10-26T03:00:00Z")

	rep, err := EUQuotaStats(db, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Days) != 2 {
		t.Fatalf("应按日两行: %+v", rep.Days)
	}
	d0, d1 := rep.Days[0], rep.Days[1]
	if d0.Requests != 3 || d0.Limited429 != 3 || d0.OK != 0 || d0.Failed != 3 {
		t.Errorf("爆发段: %+v", d0)
	}
	if d1.Requests != 2 || d1.OK != 2 || d1.Bytes != 94000 {
		t.Errorf("正常段: %+v", d1)
	}
	if len(rep.Celexes) != 1 || rep.Celexes[0].Celex != "32016R0679" || rep.Celexes[0].Requests != 3 || rep.Celexes[0].OK != 1 {
		t.Errorf("CELEX 归因: %+v", rep.Celexes)
	}

	rep, err = EUQuotaStats(db, "2026-10-26")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Days) != 1 || rep.Days[0].Requests != 2 {
		t.Errorf("since 过滤: %+v", rep.Days)
	}
}
