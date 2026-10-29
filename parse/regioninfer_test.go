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
