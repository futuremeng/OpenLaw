// subjectreview.go 部门法标引的人工落判层（docs/design-taxonomy.md §5，桌面「部门法标引」面板）。
//
// 纪律（与 regionreview.go 同一约定）：
//   - 机器不裁决：确认/驳回/清除必须由人工署名（复核人）+ 结论依据，全程写 subject_log；
//   - 人工复核过的行（subject_reviewer 非空）机器不得再写。驳回后部门法留空但署名仍在，
//     后续自动写入不得把人工结论顶回去（subject.go 各写入口的 WHERE 同样拦，双保险）；
//   - 确认取值必须命中词表 dict_subject——词表外码 fail-closed 拒判，不落自由文本；
//   - 批量流转必须先干跑计数（BulkReviewMatch），执行时把预览数带回来核对：
//     预览与执行之间队列变了就不许闭眼写；
//   - 文档形态不参与人工落判（拍板项 2：全自动机器判定），本层只动 subject_* 五列。
package ingest

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 部门法落判动作（写入 subject_log.action；与机器写入的 suggest/undo 并列）。
const (
	SubjectActionConfirm = "confirm" // 确认（可与机器建议不同 = 改选部门法）
	SubjectActionReject  = "reject"  // 驳回机器建议：留空，但署名与依据照写
	SubjectActionClear   = "clear"   // 清除已有标注：留空并署名
)

// SubjectBulkLimit 批量落判单次上限（同 RegionBulkLimit：首版只做逐条 + 小批）。
const SubjectBulkLimit = 500

// SubjectReviewSummary 落判结果计数。
type SubjectReviewSummary struct {
	Confirmed int `json:"confirmed"`
	Rejected  int `json:"rejected"`
	Cleared   int `json:"cleared"`
	Skipped   int `json:"skipped"` // 条目不存在 / 动作对该行无意义
}

// SubjectReviewStats 部门法标引复核面统计（面板据此如实报覆盖与待复核量）。
type SubjectReviewStats struct {
	Total          int `json:"total"`
	SuggestPending int `json:"suggest_pending"` // 机器建议 · 待复核
	Confirmed      int `json:"confirmed"`       // 已人工确认（含改选/人工标注）
	LeftBlank      int `json:"left_blank"`      // 已人工驳回或清除：留空但已复核
	Unmarked       int `json:"unmarked"`        // 未标注且未复核（待办积压，非终态）
}

// SubjectFilter 批量落判条件。
type SubjectFilter struct {
	Jurisdiction string // CN|EU|US；空 = 全部法域
	OnlySuggest  bool   // 只挑机器建议（subject_source='auto' 且未署名）
	Limit        int    // <=0 用 SubjectBulkLimit
}

// SQLSubjectReviewerSet / SQLSubjectReviewerBlank 署名状态的统一判定片段。
//
// NULL 与空串同义：没有署名的行绝不能算「已人工复核」（诚实展示纪律）。读侧
// （统计、队列、批量范围）一律用这两条，防止空署名被渲染成已复核；写侧的守卫
// 仍按「署名非空即人工结论」从严，不覆盖任何可能有结论的行。
const (
	SQLSubjectReviewerSet   = "COALESCE(subject_reviewer,'')<>''"
	SQLSubjectReviewerBlank = "COALESCE(subject_reviewer,'')=''"
)

// bulkWhere 组装批量条件（参数化，不拼字符串）。
func (f SubjectFilter) bulkWhere(action string) (string, []any) {
	var conds []string
	var args []any
	switch action {
	case SubjectActionConfirm, SubjectActionReject:
		conds = append(conds, "subject_code IS NOT NULL AND subject_code<>''",
			SQLSubjectReviewerBlank, "subject_source='auto'")
	case SubjectActionClear:
		conds = append(conds, "subject_code IS NOT NULL AND subject_code<>''", SQLSubjectReviewerBlank)
		if f.OnlySuggest {
			conds = append(conds, "subject_source='auto'")
		}
	default:
		return "", nil
	}
	if j := strings.TrimSpace(f.Jurisdiction); j != "" {
		conds = append(conds, "jurisdiction=?")
		args = append(args, j)
	}
	return strings.Join(conds, " AND "), args
}

// ReviewStats 复核面统计。
func (m *SubjectMarker) ReviewStats() (SubjectReviewStats, error) {
	if m.DB == nil {
		return SubjectReviewStats{}, errors.New("未连接数据库")
	}
	var s SubjectReviewStats
	err := m.DB.QueryRow(`SELECT count(*),
		COALESCE(sum(CASE WHEN subject_code IS NOT NULL AND subject_code<>'' AND `+SQLSubjectReviewerBlank+` THEN 1 ELSE 0 END),0),
		COALESCE(sum(CASE WHEN subject_code IS NOT NULL AND subject_code<>'' AND `+SQLSubjectReviewerSet+` THEN 1 ELSE 0 END),0),
		COALESCE(sum(CASE WHEN (subject_code IS NULL OR subject_code='') AND `+SQLSubjectReviewerSet+` THEN 1 ELSE 0 END),0),
		COALESCE(sum(CASE WHEN (subject_code IS NULL OR subject_code='') AND `+SQLSubjectReviewerBlank+` THEN 1 ELSE 0 END),0)
		FROM law`).Scan(&s.Total, &s.SuggestPending, &s.Confirmed, &s.LeftBlank, &s.Unmarked)
	return s, err
}

// ReviewLog 某条法规的部门法复核记录（复核记录必须可追溯，含机器建议与回滚）。
func (m *SubjectMarker) ReviewLog(lawID string) ([]string, error) {
	if m.DB == nil {
		return nil, errors.New("未连接数据库")
	}
	rows, err := m.DB.Query(`SELECT action, COALESCE(from_code,''), COALESCE(to_code,''),
		COALESCE(rule,''), actor, COALESCE(reason,''), COALESCE(batch_id,''), created_at
		FROM subject_log WHERE law_id=? ORDER BY log_id`, lawID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var action, from, to, rule, reviewer, reason, batch, at string
		if err := rows.Scan(&action, &from, &to, &rule, &reviewer, &reason, &batch, &at); err != nil {
			return nil, err
		}
		line := subjectActionText(action) + "：" + orNone(from) + "→" + orNone(to) +
			" by " + reviewer + "：" + reason + " @" + at
		if rule != "" {
			line += "（依据 " + rule + "）"
		}
		if batch != "" {
			line += "（批次 " + batch + "）"
		}
		out = append(out, line)
	}
	return out, rows.Err()
}

func subjectActionText(a string) string {
	switch a {
	case "suggest":
		return "机器建议"
	case "undo":
		return "撤销"
	case SubjectActionConfirm:
		return "确认"
	case SubjectActionReject:
		return "驳回"
	case SubjectActionClear:
		return "清除标注"
	}
	return a
}

// Review 逐条/小批落判。
//
//	action=confirm：code 必填且必须命中词表（与机器建议不同即「改选部门法」；
//	                 原本未标注则为人工标注）；
//	action=reject ：驳回机器建议——部门法留空，但署名与依据照写，机器此后不得再写该行；
//	action=clear  ：清除已有标注——留空并署名。
//
// 复核人与结论依据必填（库层强制，不靠界面自觉）。
func (m *SubjectMarker) Review(lawIDs []string, action, code, reviewer, reason string) (*SubjectReviewSummary, error) {
	if m.DB == nil {
		return nil, errors.New("未连接数据库")
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(reason) == "" {
		return nil, errors.New("部门法落判必须填写复核人与结论依据")
	}
	if action != SubjectActionConfirm && action != SubjectActionReject && action != SubjectActionClear {
		return nil, fmt.Errorf("未知落判动作 %q（confirm/reject/clear）", action)
	}
	code = strings.TrimSpace(code)
	if action == SubjectActionConfirm {
		if code == "" {
			return nil, errors.New("确认落判必须给出部门法码（改选部门法时选新部门法）")
		}
		var one int
		err := m.DB.QueryRow(`SELECT 1 FROM dict_subject WHERE code=?`, code).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("部门法码 %s 不在词表内，拒绝落判（界面与导出不得出现自由文本分类）", code)
		}
		if err != nil {
			return nil, err
		}
	}
	sum := &SubjectReviewSummary{}
	tx, err := m.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range lawIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		var old string
		err := tx.QueryRow(`SELECT COALESCE(subject_code,'') FROM law WHERE law_id=?`, id).Scan(&old)
		if errors.Is(err, sql.ErrNoRows) {
			sum.Skipped++
			continue
		}
		if err != nil {
			return nil, err
		}
		var to, rule string
		switch action {
		case SubjectActionConfirm:
			to = code
			switch {
			case old == "":
				rule = "manual（人工标注）"
			case old == code:
				rule = "manual（确认机器建议）"
			default:
				rule = "manual（改选部门法）"
			}
		case SubjectActionReject:
			if old == "" {
				sum.Skipped++ // 本来就没标注，无建议可驳回
				continue
			}
			rule = "manual（驳回机器建议）"
		case SubjectActionClear:
			if old == "" {
				sum.Skipped++
				continue
			}
			rule = "manual（清除标注）"
		}
		if _, err := tx.Exec(`UPDATE law SET subject_code=?, subject_source='manual', subject_rule=?,
			subject_reviewer=?, subject_reviewed_at=? WHERE law_id=?`,
			to, rule, reviewer, now, id); err != nil {
			return nil, fmt.Errorf("落判 %s 失败: %w", id, err)
		}
		if _, err := tx.Exec(`INSERT INTO subject_log(law_id,action,from_code,to_code,source,rule,actor,reason,created_at)
			VALUES(?,?,?,?,?,?,?,?,?)`, id, action, old, to, "manual", rule, reviewer, reason, now); err != nil {
			return nil, fmt.Errorf("写入 subject_log 失败: %w", err)
		}
		switch action {
		case SubjectActionConfirm:
			sum.Confirmed++
		case SubjectActionReject:
			sum.Rejected++
		case SubjectActionClear:
			sum.Cleared++
		}
	}
	return sum, tx.Commit()
}

// BulkReviewMatch 批量落判干跑：只计数不写库。
func (m *SubjectMarker) BulkReviewMatch(f SubjectFilter, action string) (int, error) {
	if m.DB == nil {
		return 0, errors.New("未连接数据库")
	}
	where, args := f.bulkWhere(action)
	if where == "" {
		return 0, fmt.Errorf("未知落判动作 %q", action)
	}
	var n int
	if err := m.DB.QueryRow(`SELECT count(*) FROM law WHERE `+where, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// BulkReview 批量落判执行。expected 必须等于当前匹配数：预览与执行之间队列变了就中止，
// 不拿旧计数当授权。上限 SubjectBulkLimit（首版不做全库批量）。
func (m *SubjectMarker) BulkReview(f SubjectFilter, action, reviewer, reason string, expected int) (*SubjectReviewSummary, error) {
	n, err := m.BulkReviewMatch(f, action)
	if err != nil {
		return nil, err
	}
	if expected >= 0 && expected != n {
		return nil, fmt.Errorf("影响范围已变化（预览 %d 条，实际 %d 条），请重新预览后再执行", expected, n)
	}
	limit := f.Limit
	if limit <= 0 || limit > SubjectBulkLimit {
		limit = SubjectBulkLimit
	}
	if n > limit {
		return nil, fmt.Errorf("匹配 %d 条，超过单次批量上限 %d 条：请缩小范围（按法域分批）", n, limit)
	}
	where, args := f.bulkWhere(action)
	rows, err := m.DB.Query(`SELECT law_id FROM law WHERE `+where+` ORDER BY law_id LIMIT ?`,
		append(args, limit)...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// confirm 批量：沿用各行自己的机器建议，不跨行套同一个部门法码
	if action == SubjectActionConfirm {
		sum := &SubjectReviewSummary{}
		for _, id := range ids {
			var old string
			if err := m.DB.QueryRow(`SELECT subject_code FROM law WHERE law_id=?`, id).Scan(&old); err != nil {
				return nil, err
			}
			one, err := m.Review([]string{id}, action, old, reviewer, reason)
			if err != nil {
				return nil, err
			}
			sum.Confirmed += one.Confirmed
			sum.Skipped += one.Skipped
		}
		return sum, nil
	}
	return m.Review(ids, action, "", reviewer, reason)
}
