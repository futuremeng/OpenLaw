// Package element 构成要件加载、事实比对与脱敏（开发文档 3.5、6.3、FR-08）。
// 要件比对输出风险假设清单（缺失要件、证据问题），不输出胜诉结论。
package element

import (
	"github.com/futuremeng/OpenLaw/store"
)

// Element 构成要件（4.2 element 表）。
type Element struct {
	ElementID   string `json:"element_id"`
	ArticleID   string `json:"article_id"`
	CaseType    string `json:"case_type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	HowToAttack string `json:"how_to_attack"` // 对方可能的突破方向
}

// LoadElements 按案由加载构成要件（6.3 第一步）。
func LoadElements(db *store.DB, caseType string) ([]Element, error) {
	rows, err := db.Query(`SELECT element_id, article_id, case_type, name,
		COALESCE(description, ''), COALESCE(how_to_attack, '')
		FROM element WHERE case_type = ? ORDER BY element_id`, caseType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Element
	for rows.Next() {
		var e Element
		if err := rows.Scan(&e.ElementID, &e.ArticleID, &e.CaseType, &e.Name, &e.Description, &e.HowToAttack); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
