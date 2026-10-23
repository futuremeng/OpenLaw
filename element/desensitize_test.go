package element

import (
	"strings"
	"testing"
)

// TestDesensitizeCoverage 脱敏覆盖率（验收 8.2）：
// 脱敏后文本不得残留姓名、案号、身份证号、企业名称。
func TestDesensitizeCoverage(t *testing.T) {
	original := "原告张伟与被告北京未来法律科技有限公司股东损害公司债权人利益责任纠纷一案，" +
		"案号（2018）京0105民初12345号。张伟身份证号 110101199003071234，" +
		"法定代表人李秀英。另涉上海某某集团有限公司。"
	names := []string{"张伟"}

	got, mapping := Desensitize(original, names)

	residues := []string{"张伟", "李秀英", "110101199003071234", "（2018）京0105民初12345号",
		"北京未来法律科技有限公司", "上海某某集团有限公司"}
	for _, r := range residues {
		if strings.Contains(got, r) {
			t.Fatalf("脱敏后残留 %q\n脱敏文本: %s", r, got)
		}
	}
	// 还原映射必须覆盖所有替换标记（本地还原，不写日志）
	for mark, orig := range mapping {
		if !strings.Contains(original, orig) {
			t.Fatalf("还原映射项 %s→%q 不在原文中", mark, orig)
		}
		if !strings.Contains(got, mark) {
			t.Fatalf("标记 %s 未出现在脱敏文本中", mark)
		}
	}
}

func TestDesensitizeNoFalseTrigger(t *testing.T) {
	// 普通条文文本不应被误吞
	text := "公司股东滥用公司法人独立地位和股东有限责任，逃避债务，严重损害公司债权人利益的，应当对公司债务承担连带责任。"
	got, mapping := Desensitize(text, nil)
	if len(mapping) > 0 {
		t.Fatalf("普通条文不应产生脱敏替换: %v -> %s", mapping, got)
	}
}
