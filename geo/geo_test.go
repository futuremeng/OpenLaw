package geo

import (
	"math"
	"strings"
	"testing"
)

// TestMatchContained 机关名里的行政区命中：不要求位于开头、取最长命中、别名参与；
// 判不出必须返回 false（机关名推导属中置信，不得猜）。
func TestMatchContained(t *testing.T) {
	if r, ok := MatchContained("河北省人民政府"); !ok || r.Code != "130000" {
		t.Errorf("河北省人民政府 → %+v ok=%v", r, ok)
	}
	if r, ok := MatchContained("深圳市人民政府"); !ok || r.Code != "440300" {
		t.Errorf("深圳市人民政府 → %+v ok=%v", r, ok)
	}
	if r, ok := MatchContained("石家庄市人力资源和社会保障局"); !ok || r.Code != "130100" {
		t.Errorf("石家庄市人社局 → %+v ok=%v", r, ok)
	}
	// 同级并列取上级：与 MatchPrefix 同一口径（两处判定不得打架）。
	// 机关名属中置信、只进报告，取上级是保守做法，不影响写库。
	if r, ok := MatchContained("吉林省长春市人民政府"); !ok || r.Code != "220000" {
		t.Errorf("吉林省长春市人民政府应取上级 220000，实际 %+v ok=%v", r, ok)
	}
	if _, ok := MatchContained("全国人民代表大会常务委员会"); ok {
		t.Error("国家层面机关名不得命中行政区")
	}
	if _, ok := MatchContained(""); ok {
		t.Error("空串不得命中")
	}
	if r, ok := MatchContained("深圳经济特区管理委员会"); !ok || r.Code != "440300" {
		t.Errorf("别名应命中 440300，实际 %+v ok=%v", r, ok)
	}
}

// TestGeoDataIntegrity 内置数据的自检：码/名唯一、父级存在、坐标落在合理范围。
// 数据是人工整理的，这类门禁防止后续增删条目时把表写坏（错码、漏父级、坐标写反）。
func TestGeoDataIntegrity(t *testing.T) {
	codes := map[string]bool{}
	names := map[string]string{}
	for _, r := range regions {
		if codes[r.Code] {
			t.Errorf("行政区划码重复: %s (%s)", r.Code, r.Name)
		}
		codes[r.Code] = true
		if prev, dup := names[r.Name]; dup {
			// 直辖市会出现「省级与地级同名」（北京市 110000 / 北京市 110100），属有意为之；
			// 其余同名一律视为数据错误。
			if self, ok := byCode[r.Code]; !ok || prev != self.Parent {
				t.Errorf("行政区名重复: %s（%s 与 %s）", r.Name, prev, r.Code)
			}
		}
		names[r.Name] = r.Code
		switch r.Level {
		case LevelProvince:
			if r.Parent != "" {
				t.Errorf("%s 为省级但带父级 %s", r.Name, r.Parent)
			}
		case LevelCity:
			if _, ok := byCode[r.Parent]; !ok {
				t.Errorf("%s(%s) 的父级 %s 不存在", r.Name, r.Code, r.Parent)
			}
		default:
			t.Errorf("%s 层级非法: %d", r.Name, r.Level)
		}
		// 中国大陆及港澳台合理范围（含三沙等南海岛市，取 15°N 起）；写反经纬度会立刻暴露。
		if r.Lat < 15 || r.Lat > 54 || r.Lng < 72 || r.Lng > 135 {
			t.Errorf("%s(%s) 坐标超出合理范围: lat=%v lng=%v（是否写反？）", r.Name, r.Code, r.Lat, r.Lng)
		}
	}
	if len(provinces) != 34 {
		t.Errorf("省级应为 34 个，实际 %d", len(provinces))
	}
	if len(cities) < 300 {
		t.Errorf("地级条目过少（%d），就近判定会大面积误判", len(cities))
	}
}

func TestGeoLookup(t *testing.T) {
	if got, ok := ByCode("130100"); !ok || got.Name != "石家庄市" {
		t.Errorf("ByCode(130100)=%v,%v", got, ok)
	}
	if _, ok := ByCode("999999"); ok {
		t.Error("未知码应返回 false")
	}
	if !ValidCode("") {
		t.Error("空码 = 不限地区，应合法")
	}
	if ValidCode("999999") {
		t.Error("未知码应非法")
	}
	if got := PathName("130100"); got != "河北省 · 石家庄市" {
		t.Errorf("PathName(130100)=%q", got)
	}
	if got := PathName("130000"); got != "河北省" {
		t.Errorf("省级 PathName=%q", got)
	}
	// 直辖市：省市两级同名，不得显示成「北京市 · 北京市」（实测界面上出现过）
	for _, c := range []string{"110000", "110100", "120100", "310100", "500100"} {
		got := PathName(c)
		if strings.Contains(got, " · ") {
			t.Errorf("直辖市 PathName(%s) 不得重复拼接: %q", c, got)
		}
		if got == "" {
			t.Errorf("PathName(%s) 不应为空", c)
		}
	}
	if got := PathName("999999"); got != "" {
		t.Errorf("未知码 PathName 应为空，实际 %q", got)
	}
	if cs := Children("430000"); len(cs) < 13 {
		t.Errorf("湖南省下级过少: %d", len(cs))
	}
	for _, c := range Children("430000") {
		if c.Parent != "430000" {
			t.Errorf("Children 混入非下级: %+v", c)
		}
	}
}

// TestNearest 就近判定：已知城市坐标应判到该城市；非法坐标如实返回 -1。
// 判定必然带误差，因此上层必须把距离如实展示（见 desktop/region.go）。
func TestNearest(t *testing.T) {
	cases := []struct {
		name      string
		lat, lng  float64
		wantCode  string
		wantMaxKm float64
	}{
		{"石家庄", 38.04, 114.51, "130100", 1},
		{"上海", 31.23, 121.47, "310100", 1},
		{"吉首（湘西州）", 28.26, 109.74, "433100", 2},
		{"昆明", 25.04, 102.71, "530100", 1},
		{"喀什", 39.47, 75.99, "653100", 1},
		{"北京南郊（仍判北京市）", 39.72, 116.30, "110100", 30},
	}
	for _, c := range cases {
		got, d := Nearest(c.lat, c.lng)
		if got.Code != c.wantCode {
			t.Errorf("%s 就近判定为 %s(%s)，期望 %s", c.name, got.Name, got.Code, c.wantCode)
		}
		if d < 0 || d > c.wantMaxKm {
			t.Errorf("%s 距离 %v km 超出预期上限 %v", c.name, d, c.wantMaxKm)
		}
	}
	if got, d := Nearest(999, 0); d >= 0 || got.Code != "" {
		t.Errorf("非法坐标应如实拒绝（距离 -1），实际 %+v %v", got, d)
	}
	// 边界外（公海/境外）仍会给出最近行政区，但距离必须如实很大——UI 据此提示不确定。
	if _, d := Nearest(20.0, 130.0); d < 100 {
		t.Errorf("远离任何行政区的点应给出大距离提示，实际 %v km", d)
	}
}

// TestMatchPrefix 名称前缀匹配：最长匹配优先，同级并列取上级
// （「吉林省吉林市…」判省级，与 parse/levelinfer.go 先强后弱一致）。
func TestMatchPrefix(t *testing.T) {
	cases := []struct {
		name, wantCode string
		wantName       string
	}{
		{"河北省地质灾害防治条例", "130000", "河北省"},
		{"吉林省吉林市…, 应为省级", "220000", "吉林省"},
		{"玉溪市革命遗址保护条例", "530400", "玉溪市"},
		{"湘西土家族苗族自治州环境保护条例", "433100", "湘西土家族苗族自治州"},
		{"北京市民宿管理办法", "110000", "北京市"},
		{"中华人民共和国劳动法", "", ""},
		{"国务院关于…, 不判地域", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		got, ok := MatchPrefix(c.name)
		if c.wantCode == "" {
			if ok {
				t.Errorf("%q 不应匹配任何行政区，实际 %+v", c.name, got)
			}
			continue
		}
		if !ok || got.Code != c.wantCode {
			t.Errorf("%q 应匹配 %s，实际 %+v (ok=%v)", c.name, c.wantCode, got, ok)
		}
		if ok && got.Name != c.wantName {
			t.Errorf("%q 匹配名 %q，期望 %q", c.name, got.Name, c.wantName)
		}
	}
}

// TestMatchPrefixAlias 「××经济特区」这类命名必须归到其所属行政区，
// 且别名不得把青海「海南藏族自治州」误判成海南省。
func TestMatchPrefixAlias(t *testing.T) {
	cases := []struct{ name, wantCode, wantName string }{
		{"深圳经济特区红树林湿地保护若干规定", "440300", "深圳市"},
		{"厦门经济特区反不正当竞争…, 归厦门市", "350200", "厦门市"},
		{"海南经济特区企业国有资产条例", "460000", "海南省"},
		{"深圳市物业管理条例, 走市名", "440300", "深圳市"},
		{"海南藏族自治州…, 青海不是海南", "632500", "海南藏族自治州"},
	}
	for _, c := range cases {
		got, ok := MatchPrefix(c.name)
		if !ok || got.Code != c.wantCode {
			t.Errorf("%q 应判 %s(%s)，实际 %+v ok=%v", c.name, c.wantName, c.wantCode, got, ok)
		}
	}
	if got := AliasesOf("440300"); len(got) != 1 || got[0] != "深圳经济特区" {
		t.Errorf("AliasesOf(440300)=%v", got)
	}
	if got := AliasesOf("130100"); got != nil {
		t.Errorf("无别名的区划应返回空: %v", got)
	}
	for _, a := range regionAliases { // 别名必须指向已知区划，否则是死规则
		if _, ok := ByCode(a.code); !ok {
			t.Errorf("别名 %q 指向未知区划 %s", a.prefix, a.code)
		}
	}
}

// TestNearestSymmetry 距离计算自洽：任一点到自身中心距离为 0。
func TestNearestSymmetry(t *testing.T) {
	for _, code := range []string{"130100", "440100", "650100", "232700"} {
		r, ok := ByCode(code)
		if !ok {
			t.Fatalf("内置数据缺少 %s", code)
		}
		if got, d := Nearest(r.Lat, r.Lng); got.Code != r.Code || math.Abs(d) > 0.001 {
			t.Errorf("%s 中心点应判自身且距离 0，实际 %+v d=%v", r.Name, got, d)
		}
	}
}
