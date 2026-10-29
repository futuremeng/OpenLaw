package parse

import "testing"

// TestInferRegion 名称 → 地域的保守推导：判不出必须留空，不得猜。
func TestInferRegion(t *testing.T) {
	cases := []struct {
		name, wantCode, wantName string
		wantRule                 int
	}{
		{"河北省地质灾害防治条例", "130000", "河北省", RegionRuleProvince},
		{"海南省机动车排气污染防治规定", "460000", "海南省", RegionRuleProvince},
		{"玉溪市革命遗址保护条例", "530400", "玉溪市", RegionRuleCity},
		{"湘西土家族苗族自治州环境…, 地级", "433100", "湘西土家族苗族自治州", RegionRuleCity},
		{"吉林省…, 省级优先", "220000", "吉林省", RegionRuleProvince},
		{"深圳经济特区红树林湿地保护若干规定", "440300", "深圳市", RegionRuleCity},
		{"海南经济特区企业国有资产条例", "460000", "海南省", RegionRuleProvince},
		{"中华人民共和国劳动法", "", "", 0},
		{"最高人民法院关于…, 不判地域", "", "", 0},
		{"义乌市…, 县级市不在地级表内，留空", "", "", 0},
		{"", "", "", 0},
	}
	for _, c := range cases {
		code, rn, rule := InferRegion(c.name)
		if code != c.wantCode || rn != c.wantName || rule != c.wantRule {
			t.Errorf("InferRegion(%q)=(%q,%q,%d)，期望 (%q,%q,%d)",
				c.name, code, rn, rule, c.wantCode, c.wantName, c.wantRule)
		}
	}
	if got := RegionOf("玉溪市集中式饮用水水源地保护条例"); got != "玉溪市" {
		t.Errorf("RegionOf=%q", got)
	}
	if got := RegionOf("中华人民共和国宪法"); got != "" {
		t.Errorf("国家层面名称应留空，实际 %q", got)
	}
}

// TestSuggestRegion 写侧建议层：高置信可写、中置信只进报告、互不隶属算冲突、判不出留空。
func TestSuggestRegion(t *testing.T) {
	cases := []struct {
		name, issuer string
		wantCode     string
		wantRule     int
		wantConflict bool
		wantHigh     bool
	}{
		{"石家庄市集中式饮用水水源地保护条例", "石家庄市人大常委会", "130100", RegionRuleCity, false, true},
		{"石家庄市水污染防治条例", "河北省人大常委会", "130100", RegionRuleCity, false, true}, // 同链不算冲突
		{"石家庄市水污染防治条例", "山西省人大常委会", "130100", RegionRuleCity, true, false}, // 互不隶属 = 冲突
		{"河北省地质灾害防治条例", "", "130000", RegionRuleProvince, false, true},
		{"深圳经济特区红树林湿地保护若干规定", "", "440300", RegionRuleCity, false, true},
		{"劳动和社会保障监察条例", "石家庄市人力资源和社会保障局", "130100", RegionRuleIssuer, false, false}, // 仅机关命中 = 中置信
		{"中华人民共和国劳动法", "全国人民代表大会常务委员会", "", 0, false, false},
		{"城市民族工作条例", "", "", 0, false, false},
		{"", "", "", 0, false, false},
	}
	for _, c := range cases {
		got := SuggestRegion(c.name, c.issuer)
		if got.Code != c.wantCode || got.Rule != c.wantRule || got.Conflict != c.wantConflict {
			t.Errorf("SuggestRegion(%q,%q)=(%q,R%d,冲突=%v)，期望 (%q,R%d,冲突=%v)",
				c.name, c.issuer, got.Code, got.Rule, got.Conflict, c.wantCode, c.wantRule, c.wantConflict)
		}
		if got.HighConfidence() != c.wantHigh {
			t.Errorf("HighConfidence(%q)=%v，期望 %v", c.name, got.HighConfidence(), c.wantHigh)
		}
		if got.Code != "" && got.Name == "" {
			t.Errorf("有码却无路径名: %q", got.Name)
		}
	}
	// 直辖市不得写成「北京市 · 北京市」；依据文案必须可读（人工复核要看）
	s := SuggestRegion("北京市河湖管理条例", "北京市人大常委会")
	if s.Name != "北京市" {
		t.Errorf("直辖市建议路径名=%q", s.Name)
	}
	if s.Note == "" {
		t.Error("建议必须带可读依据（写入 region_rule 供复核追溯）")
	}
}
