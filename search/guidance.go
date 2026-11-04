// guidance.go 检索的诚实出路（产品评审 P1-4）。
//
// 边界不变：本包**不重排序、不做语义扩展**（语料层必须可复现，见 AGENTS.md）。
// 但「没找到」不能是死路——用户需要知道为什么没找到、以及现在能做什么。
// 这里给出的每一条建议都必须是**可照做的动作**或**库内可核对的事实**
// （别名表里真有这个简称才提它），绝不编造「您是不是想搜……」。
package search

import (
	"strings"

	"github.com/futuremeng/OpenLaw/store"
)

// Guidance 一次检索的诚实说明。
type Guidance struct {
	Query       string   `json:"query"`
	Hits        int      `json:"hits"`
	Capped      bool     `json:"capped"`      // 命中数已达本次上限（不得让用户以为只有这些）
	ZeroResult  bool     `json:"zero_result"` // 零命中
	Reasons     []string `json:"reasons"`     // 为什么可能没找到（如实）
	Suggestions []string `json:"suggestions"` // 现在能做什么（可照做）
	AliasOf     []string `json:"alias_of"`    // 该词作为简称/别名在库内指向的法规全称
}

// SearchGuided 检索 + 诚实出路。检索本身走 Search 原路径（FTS5 优先、LIKE 兜底）。
func SearchGuided(db *store.DB, query string, topK int) ([]Hit, *Guidance, error) {
	if topK <= 0 {
		topK = 10
	}
	hits, err := Search(db, query, topK)
	g := &Guidance{Query: query, Hits: len(hits)}
	if err != nil {
		return hits, g, err
	}
	if len(hits) > 0 {
		if len(hits) == topK {
			g.Capped = true
			g.Suggestions = append(g.Suggestions,
				"本次只显示前 "+itoa(topK)+" 条命中，库里可能还有更多——加条件收窄或分批查看。")
		}
		return hits, g, nil
	}

	g.ZeroResult = true
	g.Reasons = append(g.Reasons,
		"条文原文里没有这段文字。本库按原文逐字检索，不做同义改写，也不重排序。")
	if len([]rune(query)) > 8 {
		g.Reasons = append(g.Reasons,
			"检索词较长：中文长句在全文索引里常被整段切词，整句一致才容易命中。")
	}
	g.Suggestions = append(g.Suggestions,
		"换成更短的词（如「违约金」而不是整句），或去掉标点后再试。")
	if names := aliasTargets(db, query); len(names) > 0 {
		g.AliasOf = names
		g.Suggestions = append(g.Suggestions,
			"「"+query+"」在库内是下列法规的简称/别名，可直接按该法规浏览条文："+strings.Join(names, "、"))
	} else if hasAliasLayer(db) {
		g.Suggestions = append(g.Suggestions,
			"如果这是一个简称，可在「按条件筛选」按名称片段查找，或先构建法名别名表（openlaw-alias）。")
	} else {
		g.Suggestions = append(g.Suggestions,
			"本机尚未构建法名别名表，简称无法自动对应；可用法规全称重试。")
	}
	g.Suggestions = append(g.Suggestions,
		"该主题可能尚未导入本机：在「按条件筛选」确认范围，或到「获取最新法规」从官方渠道获取。")
	return hits, g, nil
}

// aliasTargets 检索词作为简称/别名在库内指向哪些法规全称（库内事实，不是猜测）。
func aliasTargets(db *store.DB, q string) []string {
	q = strings.NewReplacer("《", "", "》", "", " ", "").Replace(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	collect := func(query string, args ...any) {
		rows, err := db.Query(query, args...)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err == nil && name != "" && !seen[name] && len(out) < 5 {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	collect(`SELECT DISTINCT l.name FROM law l WHERE l.short_name = ?`, q)
	if hasAliasLayer(db) {
		collect(`SELECT DISTINCT l.name FROM law_alias a JOIN law l ON l.law_id=a.law_id WHERE a.alias = ?`, q)
	}
	return out
}

func hasAliasLayer(db *store.DB) bool {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='law_alias'`).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
