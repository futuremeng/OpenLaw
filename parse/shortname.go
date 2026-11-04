// 法名简称抽取（OL-4，docs/tasks-opencourt-bridge.md）：从法规文本里识别官方自己给出的
// 「《全称》……以下简称《简称》」条款。纪律：只认成对出现的确切写法，不做任何推断，
// 更不由模型生成别名——抽取不到就是抽取不到（诚实展示纪律）。
// 术语定义式简称（如「集中式饮用水水源保护区（以下简称保护区）」）前面没有书名号全称，
// 因此天然不匹配本规则；入库前还要用全称反查法规库，反查不到一律不收录。
package parse

import "regexp"

// ShortNamePair 文本给出的一对「全称 → 简称」。
type ShortNamePair struct {
	FullName  string `json:"full_name"`
	ShortName string `json:"short_name"`
}

// 书名号全称 + 可选顿号/逗号 + 可选左括号 + 以下简称 + 可选书名号简称。
// 简称允许一次括号注记（如「以下简称审计法（2021年修订）」不取，保守放弃）。
var lawShortNameRe = regexp.MustCompile(`《([^》（）()]{4,80})》[，,、]{0,2}[（(]?\s*以下简称\s*[《「]?([^》」。；;，,、（）()\s]{2,30})`)

// ExtractLawShortNames 抽取一段文本中全部「以下简称」式简称对（顺序保持原文顺序）。
func ExtractLawShortNames(content string) []ShortNamePair {
	ms := lawShortNameRe.FindAllStringSubmatch(content, -1)
	out := make([]ShortNamePair, 0, len(ms))
	for _, m := range ms {
		full, short := m[1], m[2]
		if full == "" || short == "" || full == short {
			continue // 同名或空值不构成别名关系，如实不取
		}
		out = append(out, ShortNamePair{FullName: full, ShortName: short})
	}
	return out
}
