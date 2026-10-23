package element

import "strings"

// FactMatch 一个要件与案件事实的比对结果。
type FactMatch struct {
	Element  Element `json:"element"`
	Matched  bool    `json:"matched"`
	Fact     string  `json:"fact,omitempty"`
	MissHint string  `json:"miss_hint,omitempty"` // 缺失要件的补证/突破方向
}

// MatchFacts 将案件事实映射到构成要件（6.3）。
// 初版为确定性关键词映射：事实文本包含要件名称即视为有事实支撑。
// 无事实支撑的要件输出「可能翻盘点」，不输出胜诉结论。
func MatchFacts(elements []Element, facts []string) []FactMatch {
	out := make([]FactMatch, 0, len(elements))
	for _, e := range elements {
		m := FactMatch{Element: e}
		for _, f := range facts {
			if f == "" {
				continue
			}
			if strings.Contains(f, e.Name) || strings.Contains(e.Name, strings.TrimSpace(f)) {
				m.Matched = true
				m.Fact = f
				break
			}
		}
		if !m.Matched {
			m.MissHint = e.HowToAttack
			if m.MissHint == "" {
				m.MissHint = "无事实支撑，需补充证据或核实该要件是否成立"
			}
		}
		out = append(out, m)
	}
	return out
}
