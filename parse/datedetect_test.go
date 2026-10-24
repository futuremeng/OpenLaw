package parse

import "testing"

func TestInferDates(t *testing.T) {
	cases := []struct {
		name, text, wantPub, wantEff string
		wantOK                       bool
	}{
		{"双日期", "第一条　为保护……制定本法。\n附则\n第四十条　本法自2023年1月1日起施行。\n（2022年10月30日第十三届全国人民代表大会常务委员会第三十七次会议通过）",
			"2022-10-30", "2023-01-01", true},
		{"仅落款通过", "关于修改《中华人民共和国XX法》的决定\n（2015年4月24日第十二届全国人民代表大会常务委员会第十四次会议修订）\n本决定自公布之日起施行。",
			"2015-04-24", "2015-04-24", true},
		{"公布字样在前", "中华人民共和国主席令\n第一百零四号\n《中华人民共和国固体废物污染环境防治法》已由中华人民共和国第十三届全国人民代表大会常务委员会第十七次会议于2020年4月29日修订通过，现予公布，自2020年9月1日起施行。\n2020年4月29日",
			"2020-04-29", "2020-09-01", true},
		{"无日期", "第一条　没有日期标记的决定。", "", "", false},
		{"非法日期", "本法自9999年13月40日起施行。（2015年4月24日通过）", "2015-04-24", "2015-04-24", true},
		{"起施行变体", "本条例自2018年7月1日起生效。（2018年3月19日国务院第三次常务会议通过）", "2018-03-19", "2018-07-01", true},
		{"裸日期无关键词不成公布日", "2019年5月1日，会议开幕。讨论若干事项。", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pub, eff, ok := InferDates(c.text)
			if ok != c.wantOK || pub != c.wantPub || eff != c.wantEff {
				t.Fatalf("got pub=%q eff=%q ok=%v, want pub=%q eff=%q ok=%v", pub, eff, ok, c.wantPub, c.wantEff, c.wantOK)
			}
		})
	}
}
