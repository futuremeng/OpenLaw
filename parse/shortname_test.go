package parse

import "testing"

// TestExtractLawShortNames 「以下简称」条款抽取：只认成对确切写法，抽不到就如实不抽。
func TestExtractLawShortNames(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []ShortNamePair
	}{
		{"标准全称+书名号简称",
			"《中华人民共和国民法典》（以下简称《民法典》）",
			[]ShortNamePair{{FullName: "中华人民共和国民法典", ShortName: "民法典"}}},
		{"无书名号简称",
			"《中华人民共和国个人信息保护法》（以下简称个人信息保护法）",
			[]ShortNamePair{{FullName: "中华人民共和国个人信息保护法", ShortName: "个人信息保护法"}}},
		{"逗号式",
			"《中华人民共和国公司法》，以下简称《公司法》",
			[]ShortNamePair{{FullName: "中华人民共和国公司法", ShortName: "公司法"}}},
		{"术语定义式不匹配（全称不在书名号内）",
			"集中式饮用水水源保护区（以下简称保护区）",
			nil},
		{"无简称线索不匹配",
			"本法自 2021 年 11 月 1 日起施行。",
			nil},
		{"同名不构成别名",
			"《中华人民共和国公司法》（以下简称《中华人民共和国公司法》）",
			nil},
		{"一段多对",
			"《中华人民共和国数据安全法》（以下简称《数据安全法》）……《中华人民共和国网络安全法》（以下简称《网安法》）",
			[]ShortNamePair{
				{FullName: "中华人民共和国数据安全法", ShortName: "数据安全法"},
				{FullName: "中华人民共和国网络安全法", ShortName: "网安法"},
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExtractLawShortNames(c.content)
			if len(got) != len(c.want) {
				t.Fatalf("抽取 %d 对，期望 %d：%+v", len(got), len(c.want), got)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("第 %d 对 = %+v，期望 %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}
