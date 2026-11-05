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

// seedFacetLaws 追加若干法规，覆盖：不同效力等级、不同复核状态、不同来源、
// 不同公布年代（含公布日期缺失）、已标区划码与未标注。
func seedFacetLaws(t *testing.T, a *App) {
	t.Helper()
	seed := []string{
		`INSERT INTO law(law_id,name,effect_level,region_code,region_source) VALUES('L2','安全生产违法行为行政处罚办法','部门规章','','')`,
		`INSERT INTO law(law_id,name,effect_level,region_code,region_source) VALUES('L3','北京市生活垃圾管理条例','地方性法规','110000','auto')`,
		`INSERT INTO law(law_id,name,effect_level,region_code,region_source) VALUES('L4','某项未标注地区的决定','其他','','')`,
		`INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at,source_type,publish_date,confirm_status)
		 VALUES('govcn_x2','x2.md','md/govcn_x2.md',10,'dd','2026-10-01T00:00:00Z','gov.cn 国家规章库','2015-07-10','confirmed')`,
		`INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at,source_type,publish_date,confirm_status)
		 VALUES('govcn_x3','x3.md','md/govcn_x3.md',10,'ee','2026-10-01T00:00:00Z','地方人大公布','1996-03-01','pending')`,
		`INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at,source_type,confirm_status)
		 VALUES('govcn_x4','x4.md','md/govcn_x4.md',10,'ff','2026-10-01T00:00:00Z','','pending')`,
		`INSERT INTO law_version(version_id,law_id,version_no,effective_date,doc_id) VALUES('V2','L2','现行','2015-10-01','govcn_x2')`,
		`INSERT INTO law_version(version_id,law_id,version_no,effective_date,doc_id) VALUES('V3','L3','现行','1996-06-01','govcn_x3')`,
		`INSERT INTO law_version(version_id,law_id,version_no,effective_date,doc_id) VALUES('V4','L4','现行','2020-01-01','govcn_x4')`,
	}
	for _, s := range seed {
		if _, err := a.db.Exec(s); err != nil {
			t.Fatalf("种子失败: %v\n%.60s", err, s)
		}
	}
}

func facetCount(opts []FacetOption, value string) (int, bool) {
	for _, o := range opts {
		if o.Value == value {
			return o.Count, true
		}
	}
	return 0, false
}

// TestBrowseFacetsJurisdiction v7 多法域：法域分面按 CN/EU/US/未标注如实分组
// （未标注单独成桶不并入任何法域），筛选与计数同源联动，标签用法言法语。
func TestBrowseFacetsJurisdiction(t *testing.T) {
	a := newTestApp(t)
	seedFacetLaws(t, a)
	seed := []string{
		`INSERT INTO law(law_id,name,effect_level,jurisdiction) VALUES('L_eu','通用数据保护条例','欧盟条例','EU')`,
		`INSERT INTO law(law_id,name,effect_level,jurisdiction) VALUES('L_us','清洁空气法','联邦法律','US')`,
		`INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at,source_type,publish_date,confirm_status)
		 VALUES('eu_x1','x1.md','md/eu_x1.md',10,'aa','2026-10-01T00:00:00Z','欧盟出版局 Cellar','2016-04-27','pending')`,
		`INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at,source_type,publish_date,confirm_status)
		 VALUES('us_x1','x1.md','md/us_x1.md',10,'bb','2026-10-01T00:00:00Z','govinfo','1970-12-31','pending')`,
		`INSERT INTO law_version(version_id,law_id,version_no,effective_date,doc_id) VALUES('V_eu','L_eu','现行','2018-05-25','eu_x1')`,
		`INSERT INTO law_version(version_id,law_id,version_no,effective_date,doc_id) VALUES('V_us','L_us','现行','1970-12-31','us_x1')`,
	}
	for _, s := range seed {
		if _, err := a.db.Exec(s); err != nil {
			t.Fatalf("种子失败: %v\n%.60s", err, s)
		}
	}
	v, err := a.BrowseFacets(LawFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := facetCount(v.Juris, "EU"); !ok || c != 1 {
		t.Errorf("欧盟法域分面错: %+v", v.Juris)
	}
	if c, ok := facetCount(v.Juris, "US"); !ok || c != 1 {
		t.Errorf("美国法域分面错: %+v", v.Juris)
	}
	if c, ok := facetCount(v.Juris, "none"); !ok || c != 4 {
		t.Errorf("法域未标注必须单独成桶（种子 4 部未落法域）: %+v", v.Juris)
	}
	for _, o := range v.Juris {
		if o.Value == "EU" && o.Label != "欧盟" {
			t.Errorf("法域标签应为法言法语: %+v", o)
		}
		if o.Value == "none" && o.Label != "法域未标注" {
			t.Errorf("法域缺失标签必须如实: %+v", o)
		}
	}
	// 筛选与计数同源：选欧盟只剩 1 部，且该维度自身仍列全部可选值（可改选）。
	rows, err := a.BrowseLawList(LawFilter{Juris: "EU", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].LawID != "L_eu" {
		t.Errorf("法域筛选应只剩欧盟条目: %+v", rows)
	}
	v2, err := a.BrowseFacets(LawFilter{Juris: "EU"})
	if err != nil {
		t.Fatal(err)
	}
	if v2.Total != 1 {
		t.Errorf("法域筛选后总数应联动为 1: %+v", v2)
	}
	if c, ok := facetCount(v2.Juris, "US"); !ok || c != 1 {
		t.Errorf("选中欧盟后法域维度仍应列出美国可改选: %+v", v2.Juris)
	}
}

// TestBrowseFacetsLinkage 分面计数必须与其他维度实时联动，且与本维度自身选择无关
// （选中某等级后，等级维度仍显示全部可选值，用户才能改选）。
func TestBrowseFacetsLinkage(t *testing.T) {
	a := newTestApp(t)
	seedFacetLaws(t, a)

	v, err := a.BrowseFacets(LawFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if v.Total != 4 { // 基础种子 1 部 + 本例追加 3 部
		t.Fatalf("总数应为 4: %+v", v)
	}
	if v.Unlabeled != 3 { // L1/L2/L4 未落区划码
		t.Errorf("未标注区划码数应如实为 3: %+v", v)
	}
	if c, ok := facetCount(v.Levels, "部门规章"); !ok || c != 1 {
		t.Errorf("等级分面错: %+v", v.Levels)
	}
	if c, ok := facetCount(v.Confirms, "pending"); !ok || c != 3 {
		t.Errorf("复核状态分面错: %+v", v.Confirms)
	}
	if c, ok := facetCount(v.Years, "2000"); !ok || c != 1 {
		t.Errorf("公布年代分面错: %+v", v.Years)
	}
	if c, ok := facetCount(v.Years, "none"); !ok || c != 1 {
		t.Errorf("公布日期缺失必须单独成桶: %+v", v.Years)
	}
	if c, ok := facetCount(v.Regions, "11"); !ok || c != 1 {
		t.Errorf("地区分面错: %+v", v.Regions)
	}
	if c, ok := facetCount(v.Regions, ""); !ok || c != 3 {
		t.Errorf("未标注地区应单列: %+v", v.Regions)
	}

	// 选了「部门规章」后：其他维度按该子集计数，本维度仍给全量。
	v2, err := a.BrowseFacets(LawFilter{Level: "部门规章"})
	if err != nil {
		t.Fatal(err)
	}
	if v2.Total != 1 {
		t.Fatalf("等级过滤后总数应为 1: %+v", v2)
	}
	if c, ok := facetCount(v2.Confirms, "confirmed"); !ok || c != 1 {
		t.Errorf("复核状态应随等级联动: %+v", v2.Confirms)
	}
	if c, ok := facetCount(v2.Confirms, "pending"); ok && c != 0 {
		t.Errorf("不适用的可选值不应出现计数: %+v", v2.Confirms)
	}
	if c, ok := facetCount(v2.Levels, "行政法规"); !ok || c != 1 {
		t.Errorf("等级维度不应被自身选择收窄（否则无法改选）: %+v", v2.Levels)
	}

	// 列表与计数必须同源：同一条件下条数 == Total。
	rows, err := a.BrowseLawList(LawFilter{Level: "部门规章", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != v2.Total {
		t.Errorf("列表与计数口径不一致: 列表 %d 条, Total %d", len(rows), v2.Total)
	}
}

// TestBrowseFacetsRegionAndSource 地区与来源维度：区划码前缀过滤（省级含下辖）
// 与来源精确值过滤都要反映在计数上；地区标签取内置区划表名称。
func TestBrowseFacetsRegionAndSource(t *testing.T) {
	a := newTestApp(t)
	seedFacetLaws(t, a)

	v, err := a.BrowseFacets(LawFilter{Region: "110000"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Total != 1 {
		t.Fatalf("按北京市过滤应只剩 1 部: %+v", v)
	}
	var label string
	for _, o := range v.Regions {
		if o.Value == "11" {
			label = o.Label
		}
	}
	if label != "北京市" {
		t.Errorf("地区标签应为内置区划表名称: %q", label)
	}

	src, err := a.BrowseFacets(LawFilter{Source: "gov.cn 国家规章库"})
	if err != nil {
		t.Fatal(err)
	}
	if src.Total != 2 {
		t.Fatalf("来源精确过滤应 2 部: %+v", src)
	}
	// 来源维度自身不被自身选择收窄（否则无法改选），但其他维度必须联动。
	if c, ok := facetCount(src.Sources, "gov.cn 国家规章库"); !ok || c != 2 {
		t.Errorf("来源维度应保留全量可选值: %+v", src.Sources)
	}
	if c, ok := facetCount(src.Confirms, "confirmed"); !ok || c != 1 {
		t.Errorf("复核状态应随来源联动: %+v", src.Confirms)
	}

	// 名称关键词 + 年代组合：跨维度收窄可用。
	kw, err := a.BrowseFacets(LawFilter{Kw: "条例", Year: "1990"})
	if err != nil {
		t.Fatal(err)
	}
	if kw.Total != 1 {
		t.Fatalf("关键词+年代组合应 1 部: %+v", kw)
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

// TestBrowseLawListOrdersUnlabeledLast 未标注效力等级的条目必须排在已标注之后。
// SQLite 把 NULL 排最前，实测本机语料（28,952 部）前 300 条因此全部未标注等级——
// 第一眼全是缺失属显示缺陷，不是诚实：缺失仍在行内如实标出（前端合并成「未标注 N 项」），
// 只是不占用第一眼。该规则同时写进界面脚注，不得悄悄发生。
func TestBrowseLawListOrdersUnlabeledLast(t *testing.T) {
	a := newTestApp(t)
	seedFacetLaws(t, a)
	if _, err := a.db.Exec(`INSERT INTO law(law_id,name,effect_level,region_code,region_source) VALUES('L5','应排在最后的未标注条目',NULL,'','')`); err != nil {
		t.Fatalf("种子失败: %v", err)
	}
	rows, err := a.BrowseLawList(LawFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("应列出 5 部: %d", len(rows))
	}
	if last := rows[len(rows)-1]; last.LawID != "L5" || last.Level != "效力等级未标注" {
		t.Errorf("未标注等级的条目应排在最后且保留诚实默认值，实测末位=%+v", last)
	}
	for _, r := range rows[:len(rows)-1] {
		if r.Level == "效力等级未标注" {
			t.Errorf("已标注条目与未标注条目顺序混淆: %+v", rows)
			break
		}
	}
	// 筛选到具体等级时，排序规则不得影响过滤结果。
	if got, _ := a.BrowseLawList(LawFilter{Level: "部门规章", Limit: 50}); len(got) != 1 || got[0].LawID != "L2" {
		t.Errorf("等级过滤在新排序下失效: %+v", got)
	}
}

// TestBrowseSubjectTimeliness v11 浏览侧（docs/design-taxonomy.md §6）：部门法与时效
// 各为一维分面，标签以词表为准（未标注如实成桶），筛选与计数同源；时效唯一出口是
// law_status 视图（L1 有版本链 → 已修改；单版本现行 → 现行有效）。
func TestBrowseSubjectTimeliness(t *testing.T) {
	a := newTestApp(t)
	seedFacetLaws(t, a)
	if _, err := a.db.Exec(`UPDATE law SET subject_code='CN-SOC', subject_source='auto' WHERE law_id='L1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE law SET subject_code='CN-ADM', subject_source='manual',
		subject_reviewer='张三' WHERE law_id='L2'`); err != nil {
		t.Fatal(err)
	}
	v, err := a.BrowseFacets(LawFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if !v.HasTimeliness {
		t.Fatal("v11 语料应有时效视图")
	}
	if n, ok := facetCount(v.Subjects, "CN-SOC"); !ok || n != 1 {
		t.Errorf("部门法分面缺 CN-SOC: %+v", v.Subjects)
	}
	for _, o := range v.Subjects {
		if o.Value == "CN-SOC" && o.Label != "社会法" {
			t.Errorf("部门法标签应以词表为准: %+v", o)
		}
		if o.Value == "none" && o.Label != "部门法未标注" {
			t.Errorf("未标注应如实成桶: %+v", o)
		}
	}
	if n, ok := facetCount(v.Timeliness, "已修改"); !ok || n != 1 {
		t.Errorf("时效分面缺「已修改」（L1 有版本链）: %+v", v.Timeliness)
	}
	if n, ok := facetCount(v.Timeliness, "现行有效"); !ok || n != 3 {
		t.Errorf("时效分面「现行有效」应为 3（L2/L3/L4 单版本现行）: %+v", v.Timeliness)
	}
	// 筛选与计数同源：部门法、时效各自收窄后总数跟着变
	rows, err := a.BrowseLawList(LawFilter{Subject: "CN-SOC"})
	if err != nil || len(rows) != 1 || rows[0].LawID != "L1" || rows[0].SubjectName != "社会法" {
		t.Errorf("部门法过滤不符: %v %+v", err, rows)
	}
	if rows[0].SubjectRev != "" {
		t.Errorf("机器建议行署名应为空（待复核）: %+v", rows[0])
	}
	rows, err = a.BrowseLawList(LawFilter{Timeliness: "已修改"})
	if err != nil || len(rows) != 1 || rows[0].Timeliness != "已修改" {
		t.Errorf("时效过滤不符: %v %+v", err, rows)
	}
	// 信息卡（§6 字段序数据源）：时效、部门法状态、形态、来源如实给出
	m, err := a.LawMeta("L1")
	if err != nil {
		t.Fatal(err)
	}
	if m.Timeliness != "已修改" || !m.HasTimeliness || m.SubjectName != "社会法" || m.SubjectState != "suggest" {
		t.Errorf("信息卡不符: %+v", m)
	}
	if m, _ = a.LawMeta("L2"); m.SubjectState != "reviewed" || m.Timeliness != "现行有效" {
		t.Errorf("已人工复核行信息卡不符: %+v", m)
	}
}

// TestBrowseTimelinessViewMissing 只读消费 v10 语料（无 law_status 视图）：列表照常、
// 时效分面如实缺席（HasTimeliness=false，不渲染全「未知」的假分面）、行内时效为空；
// 显式按时效筛选必须显式失败而非静默给全量（诚实展示纪律）。
func TestBrowseTimelinessViewMissing(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.db.Exec(`DROP VIEW law_status`); err != nil {
		t.Fatal(err)
	}
	v, err := a.BrowseFacets(LawFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if v.HasTimeliness || len(v.Timeliness) != 0 {
		t.Errorf("无视图时时效分面应缺席: %+v", v.Timeliness)
	}
	if len(v.Subjects) == 0 {
		t.Error("部门法分面不受视图缺失影响")
	}
	rows, err := a.BrowseLawList(LawFilter{})
	if err != nil || len(rows) == 0 || rows[0].Timeliness != "" {
		t.Errorf("无视图列表应照常且时效为空: %v %+v", err, rows)
	}
	if _, err := a.BrowseLawList(LawFilter{Timeliness: "现行有效"}); err == nil {
		t.Error("无视图仍按时效筛选静默成功——必须显式失败")
	}
	m, err := a.LawMeta("L1")
	if err != nil || m.HasTimeliness || m.Timeliness != "" {
		t.Errorf("信息卡应如实报视图缺失: %v %+v", err, m)
	}
}
