// 要件导入与人工落判（OL-2，docs/tasks-opencourt-bridge.md）。
// 纪律：机器不裁决——导入的要件一律 confirm_status=pending，人工确认后才 confirmed；
// 落判必须署名（reviewer）+ 结论依据（reason），全程写 review_log（复用 confirm 流程的
// 审计语义：doc_id 取要件所依条文的文档，reason 前缀标明要件与案由——要件是条文级判定，
// 与文档级复核并列于同一审计流水，不另立台账）。
// article_id 关联不到条文的行如实拒绝并报告，不静默丢弃；幂等：同 element_id 更新而非
// 新增，且人工已落判的 confirm_status 不被重导入顶掉。默认 dry-run，apply 才写库。
package element

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// Input 一条待导入要件（CSV/JSON 行）。
type Input struct {
	ElementID   string `json:"element_id"`
	ArticleID   string `json:"article_id"`
	CaseType    string `json:"case_type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	HowToAttack string `json:"how_to_attack"`
}

// RowError 一条被拒绝的输入行（行号 + 原因）。
type RowError struct {
	Line      int    `json:"line"`
	ElementID string `json:"element_id"`
	Reason    string `json:"reason"`
}

// ImportReport 导入结果。Applied=false 表示 dry-run（未写库，计数为「将会」口径）。
type ImportReport struct {
	Total    int        `json:"total"`
	Added    int        `json:"added"`
	Updated  int        `json:"updated"`
	Rejected []RowError `json:"rejected"`
	Applied  bool       `json:"applied"`
}

// ParseCSVFile 读要件 CSV（表头必须含 element_id,article_id,case_type,name，
// 可选 description,how_to_attack；陌生表头拒绝，不用默认值伪造缺列）。
func ParseCSVFile(path string) ([]Input, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV 解析失败: %w", err)
	}
	if len(rows) < 1 {
		return nil, fmt.Errorf("CSV 为空: %s", path)
	}
	idx := map[string]int{}
	for i, h := range rows[0] {
		idx[strings.TrimSpace(strings.ToLower(h))] = i
	}
	for _, need := range []string{"element_id", "article_id", "case_type", "name"} {
		if _, ok := idx[need]; !ok {
			return nil, fmt.Errorf("CSV 缺少必需列 %s（表头: %s）", need, strings.Join(rows[0], ","))
		}
	}
	for h := range idx {
		switch h {
		case "element_id", "article_id", "case_type", "name", "description", "how_to_attack":
		default:
			return nil, fmt.Errorf("CSV 陌生表头列 %s，拒绝导入（不用默认值伪造）", h)
		}
	}
	get := func(row []string, col string) string {
		if i, ok := idx[col]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	out := make([]Input, 0, len(rows)-1)
	for _, row := range rows[1:] {
		out = append(out, Input{
			ElementID: get(row, "element_id"), ArticleID: get(row, "article_id"),
			CaseType: get(row, "case_type"), Name: get(row, "name"),
			Description: get(row, "description"), HowToAttack: get(row, "how_to_attack"),
		})
	}
	return out, nil
}

// ParseJSONFile 读要件 JSON 数组。
func ParseJSONFile(path string) ([]Input, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Input
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}
	return out, nil
}

// Import 校验并 upsert 要件。apply=false 仅干跑计数（不写库）。
// 拒绝规则：必填缺失；article_id 在库中不存在（关联不到条文）。
// 幂等：element_id 已存在则更新正文列，confirm_status 保持库内现值——
// 人工已落判的结论不被重导入顶掉，已确认的也不会被静默打回 pending。
func Import(db *store.DB, inputs []Input, apply bool) (ImportReport, error) {
	rep := ImportReport{Total: len(inputs), Rejected: []RowError{}}
	tx, err := db.Begin()
	if err != nil {
		return rep, err
	}
	defer tx.Rollback()
	for i, in := range inputs {
		line := i + 1
		if in.ElementID == "" || in.ArticleID == "" || in.CaseType == "" || in.Name == "" {
			rep.Rejected = append(rep.Rejected, RowError{line, in.ElementID,
				"必填列缺失（element_id/article_id/case_type/name）"})
			continue
		}
		var docID string
		err := tx.QueryRow(`SELECT a.doc_id FROM article a WHERE a.article_id = ?`, in.ArticleID).Scan(&docID)
		if err == sql.ErrNoRows {
			rep.Rejected = append(rep.Rejected, RowError{line, in.ElementID,
				fmt.Sprintf("条文 %s 不在库中——要件必须可溯源，拒绝该行", in.ArticleID)})
			continue
		} else if err != nil {
			return rep, err
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM element WHERE element_id = ?`, in.ElementID).Scan(&exists); err != nil {
			return rep, err
		}
		if !apply {
			if exists > 0 {
				rep.Updated++
			} else {
				rep.Added++
			}
			continue
		}
		if exists > 0 {
			if _, err := tx.Exec(`UPDATE element SET article_id=?, case_type=?, name=?,
				description=?, how_to_attack=? WHERE element_id=?`,
				in.ArticleID, in.CaseType, in.Name, in.Description, in.HowToAttack, in.ElementID); err != nil {
				return rep, err
			}
			rep.Updated++
		} else {
			if _, err := tx.Exec(`INSERT INTO element(element_id, article_id, case_type, name,
				description, how_to_attack, confirm_status) VALUES(?,?,?,?,?,?, 'pending')`,
				in.ElementID, in.ArticleID, in.CaseType, in.Name, in.Description, in.HowToAttack); err != nil {
				return rep, err
			}
			rep.Added++
		}
	}
	if apply {
		if err := tx.Commit(); err != nil {
			return rep, err
		}
		rep.Applied = true
	}
	return rep, nil
}

// Confirm 人工落判要件（verdict=confirm|reject）。必须署名 + 结论依据（AGENTS.md 人工
// 复核纪律）；状态流转与审计原子写入：element.confirm_status + review_log
// （doc_id=要件所依条文的文档，reason 前缀 element:<id> case:<案由>）。
// 库中不存在的 element_id 如实报错返回，不静默跳过。
func Confirm(db *store.DB, elementIDs []string, verdict, reviewer, reason string) (int, error) {
	if reviewer == "" || reason == "" {
		return 0, fmt.Errorf("落判必须署名（reviewer）并写明结论依据（reason）")
	}
	var action, to string
	switch verdict {
	case "confirm":
		action, to = "confirm", "confirmed"
	case "reject":
		action, to = "reject", "rejected"
	default:
		return 0, fmt.Errorf("verdict 必须为 confirm|reject，收到 %q", verdict)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	n := 0
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, id := range elementIDs {
		var from, docID, caseType string
		err := tx.QueryRow(`SELECT e.confirm_status, a.doc_id, e.case_type
			FROM element e JOIN article a ON a.article_id = e.article_id
			WHERE e.element_id = ?`, id).Scan(&from, &docID, &caseType)
		if err == sql.ErrNoRows {
			return n, fmt.Errorf("要件 %s 不在库中", id)
		} else if err != nil {
			return n, err
		}
		if _, err := tx.Exec(`UPDATE element SET confirm_status=? WHERE element_id=?`, to, id); err != nil {
			return n, err
		}
		if _, err := tx.Exec(`INSERT INTO review_log(doc_id, action, from_status, to_status, reviewer, reason, created_at)
			VALUES(?,?,?,?,?,?,?)`,
			docID, action, from, to, reviewer,
			fmt.Sprintf("element:%s case:%s %s", id, caseType, reason), now); err != nil {
			return n, err
		}
		n++
	}
	if err := tx.Commit(); err != nil {
		return n, err
	}
	return n, nil
}
