// regionreview.go 地域标引的人工落判层（F3 第 4 步，docs/design-region-write.md §5、§7）。
//
// 纪律（与 ingest/confirm.go 同一约定）：
//   - 机器不裁决：确认/驳回/清除必须由人工署名（复核人）+ 结论依据，全程写 region_log；
//   - 人工复核过的行（region_reviewer 非空）机器不得再写。驳回后地域留空但署名仍在，
//     后续自动写入不得把人工结论顶回去（Apply 的 WHERE 同样拦，双保险）；
//   - 批量流转必须先干跑计数（BulkReviewMatch），执行时把预览数带回来核对：
//     预览与执行之间队列变了就不许闭眼写；
//   - 首版只做逐条 + 小批（RegionBulkLimit 上限），不做全库批量。
package ingest

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/geo"
)

// 地域落判动作（写入 region_log.action；与自动写入的 suggest/undo 并列）。
const (
	RegionActionConfirm = "confirm" // 确认（可与机器建议不同 = 改选地区）
	RegionActionReject  = "reject"  // 驳回机器建议：地域留空，但留人工痕迹
	RegionActionClear   = "clear"   // 清除已有标注：留空并署名
)

// RegionBulkLimit 批量落判单次上限（§9.3：首版只做逐条 + 小批）。
const RegionBulkLimit = 500

// RegionReviewSummary 落判结果计数。
type RegionReviewSummary struct {
	Confirmed int `json:"confirmed"`
	Rejected  int `json:"rejected"`
	Cleared   int `json:"cleared"`
	Skipped   int `json:"skipped"` // 条目不存在 / 动作对该行无意义
}

// RegionReviewStats 地域标引复核面统计（面板据此如实报覆盖与待复核量）。
type RegionReviewStats struct {
	Total          int `json:"total"`
	SuggestPending int `json:"suggest_pending"` // 机器建议 · 待复核
	Confirmed      int `json:"confirmed"`       // 已人工确认（含改选/人工标注）
	LeftBlank      int `json:"left_blank"`      // 已人工驳回或清除：留空但已复核
	Unmarked       int `json:"unmarked"`        // 未标注且未复核
}

// RegionFilter 批量落判条件。
type RegionFilter struct {
	Province    string // 省码前缀（如 13 / 130000）；空 = 全部
	OnlySuggest bool   // 只挑机器建议（region_source='auto' 且未署名）
	Limit       int    // <=0 用 RegionBulkLimit
}

// SQLReviewerSet / SQLReviewerBlank 署名状态的统一判定片段。
//
// NULL 与空串同义：没有署名的行绝不能算「已人工复核」（诚实展示纪律）。读侧
// （统计、队列、批量范围）一律用这两条，防止空署名被渲染成已复核；写侧的守卫
// 仍按「署名非空即人工结论」从严，不覆盖任何可能有结论的行。
const (
	SQLReviewerSet   = "COALESCE(region_reviewer,'')<>''"
	SQLReviewerBlank = "COALESCE(region_reviewer,'')=''"
)

// bulkWhere 组装批量条件（参数化，不拼字符串）。
func (f RegionFilter) bulkWhere(action string) (string, []any) {
	var conds []string
	var args []any
	switch action {
	case RegionActionConfirm, RegionActionReject:
		conds = append(conds, "region_code IS NOT NULL AND region_code<>''",
			SQLReviewerBlank, "region_source='auto'")
	case RegionActionClear:
		conds = append(conds, "region_code IS NOT NULL AND region_code<>''", SQLReviewerBlank)
		if f.OnlySuggest {
			conds = append(conds, "region_source='auto'")
		}
	default:
		return "", nil
	}
	if p := strings.TrimSpace(f.Province); p != "" {
		conds = append(conds, "region_code LIKE ?")
		args = append(args, p+"%")
	}
	return strings.Join(conds, " AND "), args
}

// ReviewStats 复核面统计。
func (m *RegionMarker) ReviewStats() (RegionReviewStats, error) {
	if m.DB == nil {
		return RegionReviewStats{}, errors.New("未连接数据库")
	}
	var s RegionReviewStats
	err := m.DB.QueryRow(`SELECT count(*),
		COALESCE(sum(CASE WHEN region_code IS NOT NULL AND region_code<>'' AND `+SQLReviewerBlank+` THEN 1 ELSE 0 END),0),
		COALESCE(sum(CASE WHEN region_code IS NOT NULL AND region_code<>'' AND `+SQLReviewerSet+` THEN 1 ELSE 0 END),0),
		COALESCE(sum(CASE WHEN (region_code IS NULL OR region_code='') AND `+SQLReviewerSet+` THEN 1 ELSE 0 END),0),
		COALESCE(sum(CASE WHEN (region_code IS NULL OR region_code='') AND `+SQLReviewerBlank+` THEN 1 ELSE 0 END),0)
		FROM law`).Scan(&s.Total, &s.SuggestPending, &s.Confirmed, &s.LeftBlank, &s.Unmarked)
	return s, err
}

// ReviewLog 某条法规的地域复核记录（复核记录必须可追溯，含机器建议与回滚）。
func (m *RegionMarker) ReviewLog(lawID string) ([]string, error) {
	if m.DB == nil {
		return nil, errors.New("未连接数据库")
	}
	rows, err := m.DB.Query(`SELECT action, COALESCE(from_code,''), COALESCE(to_code,''),
		COALESCE(rule,''), actor, COALESCE(reason,''), COALESCE(batch_id,''), created_at
		FROM region_log WHERE law_id=? ORDER BY log_id`, lawID)
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
		line := regionActionText(action) + "：" + orNone(from) + "→" + orNone(to) +
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

func regionActionText(a string) string {
	switch a {
	case "suggest":
		return "机器建议"
	case "undo":
		return "撤销"
	case RegionActionConfirm:
		return "确认"
	case RegionActionReject:
		return "驳回"
	case RegionActionClear:
		return "清除标注"
	}
	return a
}

func orNone(s string) string {
	if s == "" {
		return "无"
	}
	return s
}

// Review 逐条/小批落判。
//
//	action=confirm：code 必填（与机器建议不同即「改选地区」；原本未标注则为人工标注）；
//	action=reject ：驳回机器建议——地域留空，但署名与依据照写，机器此后不得再写该行；
//	action=clear  ：清除已有标注——留空并署名。
//
// 复核人与结论依据必填（库层强制，不靠界面自觉）。
func (m *RegionMarker) Review(lawIDs []string, action, code, reviewer, reason string) (*RegionReviewSummary, error) {
	if m.DB == nil {
		return nil, errors.New("未连接数据库")
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(reason) == "" {
		return nil, errors.New("地域落判必须填写复核人与结论依据")
	}
	if action != RegionActionConfirm && action != RegionActionReject && action != RegionActionClear {
		return nil, fmt.Errorf("未知落判动作 %q（confirm/reject/clear）", action)
	}
	code = strings.TrimSpace(code)
	if action == RegionActionConfirm {
		if code == "" {
			return nil, errors.New("确认落判必须给出区划码（改选地区时选新地区）")
		}
		if geo.NameOf(code) == "" {
			return nil, fmt.Errorf("区划码 %s 不在内置区划表内，拒绝落判", code)
		}
	}
	sum := &RegionReviewSummary{}
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
		err := tx.QueryRow(`SELECT COALESCE(region_code,'') FROM law WHERE law_id=?`, id).Scan(&old)
		if errors.Is(err, sql.ErrNoRows) {
			sum.Skipped++
			continue
		}
		if err != nil {
			return nil, err
		}
		var to, rule string
		switch action {
		case RegionActionConfirm:
			to = code
			switch {
			case old == "":
				rule = "manual（人工标注）"
			case old == code:
				rule = "manual（确认机器建议）"
			default:
				rule = "manual（改选地区）"
			}
		case RegionActionReject:
			if old == "" {
				sum.Skipped++ // 本来就没标注，无建议可驳回
				continue
			}
			rule = "manual（驳回机器建议）"
		case RegionActionClear:
			if old == "" {
				sum.Skipped++
				continue
			}
			rule = "manual（清除标注）"
		}
		if _, err := tx.Exec(`UPDATE law SET region_code=?, region_source='manual', region_rule=?,
			region_reviewer=?, region_reviewed_at=? WHERE law_id=?`,
			to, rule, reviewer, now, id); err != nil {
			return nil, fmt.Errorf("落判 %s 失败: %w", id, err)
		}
		if _, err := tx.Exec(`INSERT INTO region_log(law_id,action,from_code,to_code,source,rule,actor,reason,created_at)
			VALUES(?,?,?,?,?,?,?,?,?)`, id, action, old, to, "manual", rule, reviewer, reason, now); err != nil {
			return nil, fmt.Errorf("写入 region_log 失败: %w", err)
		}
		switch action {
		case RegionActionConfirm:
			sum.Confirmed++
		case RegionActionReject:
			sum.Rejected++
		case RegionActionClear:
			sum.Cleared++
		}
	}
	return sum, tx.Commit()
}

// BulkReviewMatch 批量落判干跑：只计数不写库。
func (m *RegionMarker) BulkReviewMatch(f RegionFilter, action string) (int, error) {
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
// 不拿旧计数当授权。上限 RegionBulkLimit（首版不做全库批量）。
func (m *RegionMarker) BulkReview(f RegionFilter, action, reviewer, reason string, expected int) (*RegionReviewSummary, error) {
	n, err := m.BulkReviewMatch(f, action)
	if err != nil {
		return nil, err
	}
	if expected >= 0 && expected != n {
		return nil, fmt.Errorf("影响范围已变化（预览 %d 条，实际 %d 条），请重新预览后再执行", expected, n)
	}
	limit := f.Limit
	if limit <= 0 || limit > RegionBulkLimit {
		limit = RegionBulkLimit
	}
	if n > limit {
		return nil, fmt.Errorf("匹配 %d 条，超过单次批量上限 %d 条：请缩小范围（按省分批）", n, limit)
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
	// confirm 批量：沿用各行自己的机器建议，不跨行套同一个区划码
	if action == RegionActionConfirm {
		sum := &RegionReviewSummary{}
		for _, id := range ids {
			var old string
			if err := m.DB.QueryRow(`SELECT region_code FROM law WHERE law_id=?`, id).Scan(&old); err != nil {
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
