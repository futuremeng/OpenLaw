package main

import (
	"sort"
	"strings"
	"testing"
)

// newRegionApp 临时语料 + 地域相关种子法规（省名开头 / 市名开头 / 无地域 /
// 已落码但标题无地区名 / 未落码但标题有地区名 / 人工落判 / 码名不一致 / 码不在表内）。
func newRegionApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	seed := []string{
		`INSERT INTO law(law_id,name,effect_level) VALUES('LR1','河北省工伤保险条例','地方性法规')`,
		`INSERT INTO law(law_id,name,effect_level) VALUES('LR2','石家庄市物业管理办法','地方政府规章')`,
		`INSERT INTO law(law_id,name,effect_level) VALUES('LR3','中华人民共和国劳动法','法律')`,
		`INSERT INTO law(law_id,name,effect_level) VALUES('LR4','义乌市某项规定','地方性法规')`, // 县级市：判不出地域
		// 已落码但标题不含地区名：只有按区划码过滤才命中（第 5 步切码的意义所在）。
		`INSERT INTO law(law_id,name,effect_level,region_code,region_source) VALUES('LR5','徐州开发区若干规定','地方性法规','320300','auto')`,
		// 未落码但标题写明地区：必须靠标题前缀回退命中（真实语料仅 83% 落码，不能只按码过滤）。
		`INSERT INTO law(law_id,name,effect_level) VALUES('LR6','河北省某项未标注办法','地方性法规')`,
		// 人工落判为省级：按市过滤不再命中——码优先于名称，代价如实记录在测试里。
		`INSERT INTO law(law_id,name,effect_level,region_code,region_source,region_reviewer) VALUES('LR7','石家庄市园区管理办法','地方性法规','130000','manual','复核人甲')`,
		// 码与名称指向不同省市：以码为准，名称不得推翻已落码的结论。
		`INSERT INTO law(law_id,name,effect_level,region_code,region_source) VALUES('LR8','上海市某项办法','地方性法规','110000','auto')`,
		// 码不在内置区划表：显示为数据异常（unknown），不静默回退名称。
		`INSERT INTO law(law_id,name,effect_level,region_code,region_source) VALUES('LR9','某项异常码办法','地方性法规','999999','auto')`,
	}
	for _, s := range seed {
		if _, err := a.db.Exec(s); err != nil {
			t.Fatalf("种子失败: %v\n%.60s", err, s)
		}
	}
	return a
}

func TestRegionOptionsAndSet(t *testing.T) {
	a := newRegionApp(t)
	opts := a.RegionOptions()
	if len(opts) < 300 {
		t.Fatalf("地区下拉项过少: %d", len(opts))
	}
	seen := map[string]int{}
	var hasCity, dupBeijing bool
	for _, o := range opts {
		seen[o.Code]++
		if o.Code == "130100" && o.Parent != "130000" {
			t.Errorf("石家庄市 parent=%q，应为 130000", o.Parent)
		}
		if o.Code == "130100" {
			hasCity = true
		}
		if o.Code == "110100" {
			dupBeijing = true // 直辖市的地级项与省级同名，下拉中应省略
		}
	}
	for code, n := range seen {
		if n != 1 {
			t.Errorf("区划码 %s 在下拉中出现 %d 次", code, n)
		}
	}
	if !hasCity {
		t.Error("下拉缺少地级项（石家庄市）")
	}
	if dupBeijing {
		t.Error("直辖市的同名地级项不应重复出现在下拉中")
	}

	if got := a.CurrentRegion(); got.Code != "" {
		t.Errorf("初始应未设置地区，实际 %+v", got)
	}
	if msg := a.SetRegion("999999"); msg == "" {
		t.Error("无效区划码应被拒绝")
	}
	if msg := a.SetRegion("130100"); msg != "" {
		t.Fatalf("设置地区失败: %s", msg)
	}
	st := a.CurrentRegion()
	if st.Code != "130100" || st.Name != "石家庄市" || st.Path != "河北省 · 石家庄市" || st.Source != "manual" {
		t.Errorf("地区偏好读回错: %+v", st)
	}
	// 偏好必须持久化：同一数据目录重新打开后仍在。
	b := NewApp(a.dataDir)
	if err := b.Open(); err != nil {
		t.Fatal(err)
	}
	defer b.db.Close()
	if got := b.CurrentRegion(); got.Code != "130100" {
		t.Errorf("重开后地区偏好丢失: %+v", got)
	}
	if msg := a.SetRegion(""); msg != "" {
		t.Fatalf("清除地区失败: %s", msg)
	}
	if got := a.CurrentRegion(); got.Code != "" || !strings.Contains(got.Note, "尚未设置") {
		t.Errorf("清除后应如实说明未设置，实际 %+v", got)
	}
}

func TestLocateFromCoords(t *testing.T) {
	a := newRegionApp(t)
	res := a.LocateFromCoords(38.04, 114.51) // 石家庄中心
	if !res.OK || res.Region.Code != "130100" || res.Region.Source != "auto" {
		t.Fatalf("就近判定失败: %+v", res)
	}
	if !strings.Contains(res.Msg, "km") || res.Region.DistanceKm < 0 {
		t.Errorf("必须如实回传误差距离: %+v", res)
	}
	if got := a.CurrentRegion(); got.Code != "130100" || got.Source != "auto" {
		t.Errorf("定位后未保存偏好: %+v", got)
	}
	// 非法坐标：如实拒绝且不改动已有地区。
	bad := a.LocateFromCoords(999, 0)
	if bad.OK || bad.Msg == "" {
		t.Errorf("非法坐标应如实拒绝: %+v", bad)
	}
	if got := a.CurrentRegion(); got.Code != "130100" {
		t.Errorf("非法坐标不得改动地区: %+v", got)
	}
	// 远离任何行政中心的点：仍给出结果，但距离必须如实很大（UI 据此提示不可靠）。
	far := a.LocateFromCoords(20.0, 130.0)
	if !far.OK || far.Region.DistanceKm < 100 {
		t.Errorf("远离区划中心的点应给出大距离提示: %+v", far)
	}
}

// TestLocateFromRegion 试判定：以选定地点的中心点跑同一条判定链路。
// 关键纪律：来源必须是 trial，说明文字必须写明「非真实位置」——不得冒充真实定位。
func TestLocateFromRegion(t *testing.T) {
	a := newRegionApp(t)

	res := a.LocateFromRegion("130100") // 石家庄市
	if !res.OK || res.Region.Code != "130100" {
		t.Fatalf("试判定失败: %+v", res)
	}
	if res.Region.Source != regionSourceTrial {
		t.Errorf("来源必须标记为试判定，实际 %q", res.Region.Source)
	}
	if !strings.Contains(res.Msg, "试判定") || !strings.Contains(res.Msg, "非真实位置") {
		t.Errorf("必须如实说明非真实定位: %q", res.Msg)
	}
	if got := a.CurrentRegion(); got.Source != regionSourceTrial || got.Code != "130100" {
		t.Errorf("试判定后偏好应可回读且来源可区分: %+v", got)
	}

	// 选省级：中心点是省会，判定链路应落到地级条目（证明走的是同一套 Nearest）。
	pr := a.LocateFromRegion("130000")
	if !pr.OK || pr.Region.Code != "130100" {
		t.Errorf("省级试判定应落到下辖地级: %+v", pr)
	}

	// 未选地点：如实拒绝且不得改动已有地区。
	bad := a.LocateFromRegion("")
	if bad.OK || bad.Msg == "" {
		t.Errorf("未选地点应如实拒绝: %+v", bad)
	}
	if got := a.CurrentRegion(); got.Code != "130100" {
		t.Errorf("失败的试判定不得改动地区: %+v", got)
	}
	if bad2 := a.LocateFromRegion("999999"); bad2.OK {
		t.Errorf("未知区划码应被拒绝: %+v", bad2)
	}

	// 试判定与真实定位必须可区分：auto 与 trial 不得混同。
	if a.LocateFromCoords(38.04, 114.51).Region.Source != "auto" {
		t.Error("真实坐标判定应记为 auto")
	}
}

func TestRegionPatternsAndBrowseFilter(t *testing.T) {
	a := newRegionApp(t)

	// 选省级必须连同下辖地级一起命中，否则「石家庄市…」会被漏掉。
	hebei := regionPatterns("130000")
	if len(hebei) < 12 {
		t.Errorf("河北省前缀过少: %d", len(hebei))
	}
	var hasProv, hasCity bool
	for _, p := range hebei {
		if p == "河北省" {
			hasProv = true
		}
		if p == "石家庄市" {
			hasCity = true
		}
	}
	if !hasProv || !hasCity {
		t.Errorf("省级前缀集合缺少省或下辖市: %v", hebei)
	}
	if got := regionPatterns("130100"); len(got) != 1 || got[0] != "石家庄市" {
		t.Errorf("地级前缀应只有自身: %v", got)
	}
	if got := regionPatterns("999999"); got != nil {
		t.Errorf("未知区划码应无前缀: %v", got)
	}

	// 别名必须同时进入推导与过滤口径，否则「显示有地域、过滤查不到」会自相矛盾。
	if sz := regionPatterns("440300"); len(sz) != 2 || sz[0] != "深圳市" || sz[1] != "深圳经济特区" {
		t.Errorf("深圳市前缀应含别名: %v", sz)
	}
	var aliasInGD bool
	for _, p := range regionPatterns("440000") {
		if p == "深圳经济特区" {
			aliasInGD = true
		}
	}
	if !aliasInGD {
		t.Error("广东省过滤应包含下辖特区别名")
	}

	set := func(rows []LawRow) string {
		var s []string
		for _, r := range rows {
			s = append(s, r.LawID)
		}
		sort.Strings(s)
		return strings.Join(s, ",")
	}
	browse := func(code string) string {
		t.Helper()
		rows, err := a.BrowseLaws("", "", code, 500, 0)
		if err != nil {
			t.Fatalf("浏览失败(%s): %v", code, err)
		}
		return set(rows)
	}
	all := browse("")
	for _, id := range []string{"LR1", "LR2", "LR5", "LR6", "LR7", "LR8"} {
		if !strings.Contains(all, id) {
			t.Errorf("不限地区应全部命中，缺 %s: %s", id, all)
		}
	}
	// 省级过滤：已落码的按码命中（LR7），未落码的按标题回退（LR1/LR2/LR6）。
	if got := browse("130000"); got != "LR1,LR2,LR6,LR7" {
		t.Errorf("河北省应命中省名回退三条 + 已落码一条，实际: %s", got)
	}
	// 地级过滤：只命中本市。LR7 标题写「石家庄市」但人工落判为省级 130000，
	// 按市不再命中——码优先于名称，这是有意为之的代价（见 regionFilter 注释）。
	if got := browse("130100"); got != "LR2" {
		t.Errorf("石家庄市只应命中本市法规，实际: %s", got)
	}
	// 已落码但标题不含地区名：按市与按省都必须命中（切码的意义）。
	if got := browse("320300"); got != "LR5" {
		t.Errorf("徐州市应按区划码命中 LR5，实际: %s", got)
	}
	if got := browse("320000"); got != "LR5" {
		t.Errorf("江苏省应按码前缀命中下辖地级 LR5，实际: %s", got)
	}
	// 码与名称不一致：以码为准。LR8 标题写「上海市」但码是 110000（北京）。
	if got := browse("110000"); got != "LR8" {
		t.Errorf("北京市应按码命中 LR8，实际: %s", got)
	}
	if strings.Contains(browse("310000"), "LR8") {
		t.Errorf("上海市不得按名称推翻已落码的结论: %s", browse("310000"))
	}
	// 码不在内置表：任何有效地区过滤都不命中（不静默回退名称，界面按 unknown 显示）。
	for _, code := range []string{"130000", "110000", "310000", "320000"} {
		if got := browse(code); strings.Contains(got, "LR9") {
			t.Errorf("异常码条目不应被地区过滤命中(%s): %s", code, got)
		}
	}

	// Region/RegionSource：已标注取库中码（auto/manual 可区分，机器建议不得显示成已复核）；
	// 未标注回退名称推定并标 none；码不在表内标 unknown。
	rows, err := a.BrowseLaws("", "", "", 500, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]LawRow{}
	for _, r := range rows {
		got[r.LawID] = r
	}
	if r := got["LR5"]; r.Region != "徐州市" || r.RegionSource != "auto" || r.RegionCode != "320300" {
		t.Errorf("已落码机器建议应显示码与 auto: %+v", r)
	}
	if r := got["LR7"]; r.Region != "河北省" || r.RegionSource != "manual" {
		t.Errorf("人工落判必须显示码对应地名与 manual（不得显示成名称推定）: %+v", r)
	}
	if r := got["LR6"]; r.Region != "河北省" || r.RegionSource != "none" || r.RegionCode != "" {
		t.Errorf("未落码必须标 none（前端据此显示名称推定），不得冒充已标注: %+v", r)
	}
	if r := got["LR1"]; r.Region != "河北省" || r.RegionSource != "none" {
		t.Errorf("未落码的省名条目同样必须标 none: %+v", r)
	}
	if r := got["LR3"]; r.Region != "" || r.RegionSource != "none" {
		t.Errorf("国家层面条目不得假装推导出地域: %+v", r)
	}
	if r := got["LR4"]; r.Region != "" || r.RegionSource != "none" {
		t.Errorf("县级市不得假装推导出地域: %+v", r)
	}
	if r := got["LR9"]; r.RegionSource != "unknown" {
		t.Errorf("码不在内置区划表必须标 unknown: %+v", r)
	}
}

// TestRegionCodePrefix 码前缀口径：省级取 2 位（含下辖全部地级），地级取 4 位；
// 无效码无前缀（与「未知区划码按不过滤处理」的既有口径一致）。
func TestRegionCodePrefix(t *testing.T) {
	if got := regionCodePrefix("130000"); got != "13" {
		t.Errorf("省级前缀应 2 位: %q", got)
	}
	if got := regionCodePrefix("130100"); got != "1301" {
		t.Errorf("地级前缀应 4 位: %q", got)
	}
	if got := regionCodePrefix("999999"); got != "" {
		t.Errorf("无效码应无前缀: %q", got)
	}
	if f := newRegionFilter("999999"); f.CodePrefix != "" || f.Patterns != nil {
		t.Errorf("无效码两条依据都应为空: %+v", f)
	}
	if clause, args := regionClause(newRegionFilter("")); clause != "" || args != nil {
		t.Errorf("未选地区不得构造过滤子句: %q", clause)
	}
}

func TestBrowseStatsReportsUnlabeled(t *testing.T) {
	a := newRegionApp(t)
	total, unlabeled, err := a.BrowseStats("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if total < 5 { // 种子 L1 + LR1..LR9
		t.Errorf("总数异常: %d", total)
	}
	// 未标注 = 库中无区划码。种子只有 LR5/LR7/LR8/LR9 落码，其余一律如实计入未标注——
	// 这个数决定「按区划码过滤」覆盖多少，不得隐藏，也不得用名称推定顶替。
	if unlabeled != total-4 {
		t.Errorf("未标注数应按区划码统计: total=%d unlabeled=%d", total, unlabeled)
	}
	// 按地区过滤时同样如实给出未标注数：河北省命中 4 条，其中 3 条未落码（按标题回退）。
	ftotal, funlabeled, err := a.BrowseStats("", "", "130000")
	if err != nil {
		t.Fatal(err)
	}
	if ftotal != 4 || funlabeled != 3 {
		t.Errorf("按地区过滤的未标注数应如实给出: total=%d unlabeled=%d", ftotal, funlabeled)
	}
	if _, _, err := a.BrowseStats("", "", "999999"); err != nil {
		t.Errorf("未知区划码按不过滤处理，不应报错: %v", err)
	}
}
