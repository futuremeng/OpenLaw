package element

import (
	"regexp"
	"strconv"
	"strings"
)

// 脱敏（FR-08、验收 8.2）：脱敏后文本不得残留姓名、案号、身份证号、企业名称。
// 默认全部本地完成；返回本地还原映射供界面还原（不写入日志）。

var (
	idCardRe  = regexp.MustCompile(`\d{17}[0-9Xx]|\d{15}`)
	caseNoRe  = regexp.MustCompile(`[（(]\d{4}[)）][0-9\p{Han}]{2,15}号`)
	companyRe = regexp.MustCompile(`[\p{Han}A-Za-z0-9（）()·]{2,30}?(?:股份有限公司|有限责任公司|集团有限公司|有限公司|集团)`)
	// 角色上下文后的 2-4 字中文视为人名（原告/被告/法定代表人等之后的首个词）
	personCtxRe = regexp.MustCompile(`((?:原告|被告|第三人|申请人|被申请人|法定代表人|委托诉讼代理人|证人|审判长|审判员)[：: ]*)([\p{Han}]{2,4})`)
	// 企业名匹配可能从更靠左的位置起算，剥离这些引导词避免过度替换
	companyLeadWords = []string{"原告", "被告", "第三人", "申请人", "被申请人", "委托诉讼代理人",
		"法定代表人", "证人", "审判长", "审判员", "诉", "与", "和", "等", "的", "或", "但", "是",
		"在", "了", "向", "对", "由", "从", "经", "并", "系", "及"}
)

// Desensitize 对文本执行四类脱敏，返回脱敏文本与「替换标记 → 原文」还原映射。
// extraNames 为调用方显式提供的名单（案件当事人等），优先精确替换。
func Desensitize(text string, extraNames []string) (string, map[string]string) {
	redacted := map[string]string{}
	counter := map[string]int{}
	next := func(kind, original string) string {
		counter[kind]++
		mark := "【" + kind + strconv.Itoa(counter[kind]) + "】"
		redacted[mark] = original
		return mark
	}

	// 1) 显式名单（姓名）
	names := append([]string{}, extraNames...)
	// 去重并按长度降序，避免短名吞掉长名
	sortNamesDescUnique(names)
	for _, n := range names {
		if n == "" || !strings.Contains(text, n) {
			continue
		}
		text = strings.ReplaceAll(text, n, next("姓名", n))
	}

	// 2) 身份证号
	text = idCardRe.ReplaceAllStringFunc(text, func(s string) string { return next("身份证号", s) })

	// 3) 案号
	text = caseNoRe.ReplaceAllStringFunc(text, func(s string) string { return next("案号", s) })

	// 4) 企业名称
	text = companyRe.ReplaceAllStringFunc(text, func(s string) string { return next("企业名称", s) })

	// 5) 角色上下文人名
	text = personCtxRe.ReplaceAllStringFunc(text, func(s string) string {
		m := personCtxRe.FindStringSubmatch(s)
		return m[1] + next("姓名", m[2])
	})

	return text, redacted
}

func sortNamesDescUnique(names []string) {
	seen := map[string]bool{}
	uniq := names[:0]
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			uniq = append(uniq, n)
		}
	}
	for i := 0; i < len(uniq); i++ {
		for j := i + 1; j < len(uniq); j++ {
			if len(uniq[j]) > len(uniq[i]) {
				uniq[i], uniq[j] = uniq[j], uniq[i]
			}
		}
	}
}
