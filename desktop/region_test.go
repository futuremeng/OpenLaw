package main

import (
	"strings"
	"testing"
)

// newRegionApp 临时语料 + 地域相关种子法规（省名开头 / 市名开头 / 无地域）。
func newRegionApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	seed := []string{
		`INSERT INTO law(law_id,name,effect_level) VALUES('LR1','河北省工伤保险条例','地方性法规')`,
		`INSERT INTO law(law_id,name,effect_level) VALUES('LR2','石家庄市物业管理办法','地方政府规章')`,
		`INSERT INTO law(law_id,name,effect_level) VALUES('LR3','中华人民共和国劳动法','法律')`,
		`INSERT INTO law(law_id,name,effect_level) VALUES('LR4','义乌市某项规定','地方性法规')`, // 县级市：判不出地域
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

	ids := func(rows []LawRow) string {
		var s []string
		for _, r := range rows {
			s = append(s, r.LawID)
		}
		return strings.Join(s, ",")
	}
	all, err := a.BrowseLaws("", "", "", 500, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ids(all), "LR1") || !strings.Contains(ids(all), "LR2") || !strings.Contains(ids(all), "LR3") {
		t.Errorf("不限地区应全部命中: %s", ids(all))
	}
	hb, err := a.BrowseLaws("", "", "130000", 500, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ids(hb) != "LR1,LR2" { // 按 effect_level,name 排序（二进制序：地方性法规 先于 地方政府规章）
		t.Errorf("河北省应命中省与市两部，实际: %s", ids(hb))
	}
	sjz, err := a.BrowseLaws("", "", "130100", 500, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ids(sjz) != "LR2" {
		t.Errorf("石家庄市只应命中本市法规，实际: %s", ids(sjz))
	}
	// Region 字段：推得出的给出地域名，推不出的留空由前端显示「地域未标注」。
	reg := map[string]string{}
	for _, r := range all {
		reg[r.LawID] = r.Region
	}
	if reg["LR1"] != "河北省" || reg["LR2"] != "石家庄市" {
		t.Errorf("推导地域错: %+v", reg)
	}
	if reg["LR3"] != "" || reg["LR4"] != "" {
		t.Errorf("国家层面/县级市不得假装推导出地域: %+v", reg)
	}
}

func TestBrowseStatsReportsUnlabeled(t *testing.T) {
	a := newRegionApp(t)
	total, unlabeled, err := a.BrowseStats("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if total < 5 { // 种子 L1 + LR1..LR4
		t.Errorf("总数异常: %d", total)
	}
	// 未标注数必须如实反映（LR3/LR4 与种子工伤保险条例均无地域）。
	if unlabeled < 3 {
		t.Errorf("未标注地域的数量应如实统计，实际 %d", unlabeled)
	}
	if _, _, err := a.BrowseStats("", "", "999999"); err != nil {
		t.Errorf("未知区划码按不过滤处理，不应报错: %v", err)
	}
}
