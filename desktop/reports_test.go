package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeFailReason(t *testing.T) {
	cases := map[string]string{
		"正文容器缺失：未找到 zfxxgk_content":        "正文容器缺失",
		"HTTP 404 (not found)":             "HTTP 404",
		"robots 拒绝":                        "robots 拒绝",
		"":                                 "（无原因）",
		"UNIQUE constraint failed (multi)": "UNIQUE constraint failed",
	}
	for in, want := range cases {
		if got := normalizeFailReason(in); got != want {
			t.Errorf("normalizeFailReason(%q)=%q，期望 %q", in, got, want)
		}
	}
}

func TestScanReports(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "incoming/govgz/report_20261028T000000.csv"),
		`url,doc_id,status,articles,reason
http://a.gov.cn/1.html,govcn_a1,ok,12,
http://a.gov.cn/2.html,govcn_a2,ok_inferred,8,
http://a.gov.cn/3.html,,failed,0,正文容器缺失：未找到 content
http://a.gov.cn/4.html,,failed,0,robots 拒绝（Disallow: /）
http://b.example/5.html,,skip_bad_url,,形态不符
`)
	writeFile(t, filepath.Join(dir, "incoming/govgz/govlist_report_list_agent_x.json"),
		`{"start_url":"https://www.gov.cn","cat":"部门规章","pages":3,"entries":[{"url":"u1"},{"url":"u2"}],"stopped_at":"max_pages"}`)
	writeFile(t, filepath.Join(dir, "incoming/govcn/report_bad.csv"),
		`foo,bar
1,2
`)
	writeFile(t, filepath.Join(dir, "incoming/scanonly/other.csv"), `doc_id,status
x,ok
`) // 不匹配命名规则，应被忽略

	reps := ScanReports(dir, 2)
	byName := map[string]ReportSummary{}
	for _, r := range reps {
		byName[filepath.Base(r.Path)] = r
	}
	if len(reps) != 3 {
		t.Fatalf("应发现 3 份报告（other.csv 忽略），实际 %d: %+v", len(reps), keys(byName))
	}
	main := byName["report_20261028T000000.csv"]
	if main.Total != 5 || main.OK != 2 || main.Failed != 2 || main.Skipped != 1 {
		t.Errorf("分诊计数错: %+v", main)
	}
	if len(main.TopFailures) != 2 {
		t.Fatalf("失败直方图应 2 项: %+v", main.TopFailures)
	}
	if main.Source != "govgz" {
		t.Errorf("source=%q", main.Source)
	}
	gl := byName["govlist_report_list_agent_x.json"]
	if gl.Pages != 3 || gl.Total != 2 || gl.StoppedAt != "max_pages" {
		t.Errorf("govlist 报告解析错: %+v", gl)
	}
	bad := byName["report_bad.csv"]
	if bad.Note == "" {
		t.Error("缺 status 列应标注 malformed note")
	}
}

func keys(m map[string]ReportSummary) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
