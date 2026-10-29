package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestRecentReportsLimit 前端按面板展示条数传参（app().RecentReports(n)）：
// 跨来源合并后按修改时间倒序取最近 n 份；非法 n 走默认/钳制；无 incoming 目录
// 时如实返回空列表（不报错、不渲染假记录）。
func TestRecentReportsLimit(t *testing.T) {
	dir := t.TempDir()
	csvBody := "url,doc_id,status,articles,reason\nhttp://a.gov.cn/1.html,govcn_a1,ok,12,\n"
	files := []struct {
		rel, body, mod string
	}{
		{"incoming/govgz/report_20261025T000000.csv", csvBody, "2026-10-25T00:00:00Z"},
		{"incoming/govgz/report_20261028T000000.csv", csvBody, "2026-10-28T00:00:00Z"},
		{"incoming/govcn/report_20261027T000000.csv", csvBody, "2026-10-27T00:00:00Z"},
		{"incoming/batch/batch_report.csv", "doc_id,law_id,name,date,status,articles,detail\ngovcn_b1,L9,法,2020-01-01,ok,3,\n", "2026-10-26T00:00:00Z"},
		{"incoming/govlist/govlist_report_list_agent_y.json", `{"start_url":"https://www.gov.cn","cat":"部门规章","pages":2,"entries":[{"url":"u1"}],"stopped_at":"done"}`, "2026-10-29T00:00:00Z"},
	}
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f.rel))
		writeFile(t, p, f.body)
		ts, err := time.Parse(time.RFC3339, f.mod)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	a := NewApp(dir)

	got := a.RecentReports(2)
	if len(got) != 2 {
		t.Fatalf("limit=2 应返回 2 份，实际 %d: %+v", len(got), got)
	}
	if got[0].ModifiedAt != "2026-10-29T00:00:00Z" || got[1].ModifiedAt != "2026-10-28T00:00:00Z" {
		t.Errorf("应取全局最近 2 份（跨来源按时间），实际 %q %q", got[0].ModifiedAt, got[1].ModifiedAt)
	}
	for _, n := range []int{0, -3} {
		if r := a.RecentReports(n); len(r) != 5 {
			t.Errorf("limit=%d 应回落默认 5，实际 %d", n, len(r))
		}
	}
	if r := a.RecentReports(1000); len(r) != 5 {
		t.Errorf("limit 应钳制到上限而非放大语料扫描，实际 %d", len(r))
	}
	if r := NewApp(t.TempDir()).RecentReports(5); len(r) != 0 {
		t.Errorf("无 incoming 目录应如实返回空，实际 %d", len(r))
	}
}

func keys(m map[string]ReportSummary) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
