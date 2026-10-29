package main

import "testing"

func TestBrowseLawsAndArticles(t *testing.T) {
	a := newTestApp(t)
	rows, err := a.BrowseLaws("", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("应列出 1 部法规: %+v", rows)
	}
	r := rows[0]
	if r.Name != "工伤保险条例" || r.Level != "行政法规" || r.Status != "pending" {
		t.Errorf("法规行错: %+v", r)
	}
	if r.VersionNo != "现行" || r.Effective != "2004-01-01" || r.Expire != "" {
		t.Errorf("代表版本应取最新施行版: %+v", r)
	}
	if got, _ := a.BrowseLaws("部门规章", "", "", 50, 0); len(got) != 0 {
		t.Errorf("等级过滤失效: %+v", got)
	}
	if got, _ := a.BrowseLaws("", "工伤", "", 50, 0); len(got) != 1 {
		t.Errorf("名称过滤失效: %+v", got)
	}
	arts, err := a.BrowseArticles("L1")
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 || arts[0].ArticleNo != "第一条" || arts[0].DocID != "govcn_x1" {
		t.Errorf("条款列表错: %+v", arts)
	}
}

func TestLawVersionsTimeline(t *testing.T) {
	a := newTestApp(t)
	vs, err := a.LawVersions("L1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 || vs[0].VersionID != "V0" || vs[0].Expire != "2004-01-01" || vs[1].Expire != "" {
		t.Fatalf("沿革时间线错（应按施行日升序、现行无失效日）: %+v", vs)
	}
}

func TestPreviewLegalFields(t *testing.T) {
	a := newTestApp(t)
	p, err := a.Preview("govcn_x1")
	if err != nil {
		t.Fatal(err)
	}
	if p.LawName != "工伤保险条例" || p.Level != "行政法规" || p.Effective != "2004-01-01" || p.VersionNo != "现行" {
		t.Errorf("预览法规要素缺失: %+v", p)
	}
	rows, _ := a.SearchArticles("工伤保险", 10)
	if len(rows) != 1 || rows[0].Status != "pending" || rows[0].Effective != "2004-01-01" {
		t.Errorf("检索信任层字段缺失: %+v", rows)
	}
}
