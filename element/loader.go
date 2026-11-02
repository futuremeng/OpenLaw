// Package element 构成要件加载、事实比对与脱敏（开发文档 3.5、6.3、FR-08）。
// 要件比对输出风险假设清单（缺失要件、证据问题），不输出胜诉结论。
package element

import (
	"github.com/futuremeng/OpenLaw/store"
)

// Element 构成要件（4.2 element 表）。
// ConfirmStatus：v8 起随库持久化（pending|confirmed|rejected）；读 v7 及更早只读语料
// 时该列不存在，一律按 "pending" 如实处理（未确认 ≠ 已确认，诚实展示纪律）。
type Element struct {
	ElementID     string `json:"element_id"`
	ArticleID     string `json:"article_id"`
	CaseType      string `json:"case_type"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	HowToAttack   string `json:"how_to_attack"`
	ConfirmStatus string `json:"confirm_status"`
}

// LoadElements 按案由加载构成要件（6.3 第一步）。
func LoadElements(db *store.DB, caseType string) ([]Element, error) {
	withStatus, err := hasConfirmColumn(db)
	if err != nil {
		return nil, err
	}
	q := `SELECT element_id, article_id, case_type, name,
		COALESCE(description, ''), COALESCE(how_to_attack, '')`
	if withStatus {
		q += `, COALESCE(confirm_status, 'pending')`
	} else {
		q += `, 'pending'` // 旧 schema 无该列：未确认，不装懂
	}
	q += ` FROM element WHERE case_type = ? ORDER BY element_id`
	rows, err := db.Query(q, caseType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Element
	for rows.Next() {
		var e Element
		if err := rows.Scan(&e.ElementID, &e.ArticleID, &e.CaseType, &e.Name, &e.Description, &e.HowToAttack, &e.ConfirmStatus); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// hasConfirmColumn element 表是否已有 confirm_status 列（v8+；只读语料可能更旧）。
func hasConfirmColumn(db *store.DB) (bool, error) {
	rows, err := db.Query(`SELECT 1 FROM pragma_table_info('element') WHERE name = 'confirm_status'`)
	if err != nil {
		return false, nil // 表不存在等 → 视为无列
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}
