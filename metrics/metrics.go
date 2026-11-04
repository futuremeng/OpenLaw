// Package metrics 本机度量聚合（产品评审「度量体系」）。
//
// 三条不可让的边界：
//   - **只读**：本包绝不写库，也绝不联网；数据源就是本机既有留痕
//     （usage_event / review_log / agent_review / validate_log）。
//   - **区分「没记录」与「记录为 0」**：台账表不存在（v9 及更早语料、只读消费方）
//     时 Available=false，界面必须显示「本机尚未记录」，不得显示 0%。
//   - **不做推断**：只报计数与比率，不报「用户满意度」这类造出来的指标。
package metrics

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// WindowDays 统计窗口（天）。窗口外的记录不参与比率，避免历史一次性导入把结论带偏。
const WindowDays = 30

// Step 首启漏斗一步的完成情况。
type Step struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Count int    `json:"count"`
	Done  bool   `json:"done"`
}

// Report 本机使用情况（全部为本机计数，无任何上传）。
type Report struct {
	Available bool   `json:"available"`
	Since     string `json:"since"`
	Window    int    `json:"window_days"`

	Funnel []Step `json:"funnel"`

	SearchTotal int     `json:"search_total"`
	SearchZero  int     `json:"search_zero"`
	ZeroRate    float64 `json:"zero_rate"` // 百分比；SearchTotal=0 时无意义（界面须判 Available/Total）

	VerifyTotal int `json:"verify_total"`

	HumanReviews int     `json:"human_reviews"`     // 窗口内 review_log 流转次数
	HumanPerDay  float64 `json:"human_per_day"`     // 窗口内平均每天
	ExceptionQ   int     `json:"exception_backlog"` // 当前仍需人工纠正的条目数
	FetchRuns    int     `json:"fetch_runs"`

	ChainTotal int     `json:"chain_total"` // 窗口内溯源校验次数
	ChainFail  int     `json:"chain_fail"`
	FailRate   float64 `json:"fail_rate"`

	Note string `json:"note"`
}

const funnelNote = "以下数字全部来自本机留痕（不上传、不含检索词与案件内容）；" +
	"「没有记录」与「记录为 0」是两回事，未记录时不会显示比率。"

// HeadlineNote 界面必须原样带上的口径说明。
func HeadlineNote() string { return funnelNote }

// ReportFor 聚合最近 WindowDays 天的本机留痕。
func ReportFor(db *store.DB) (*Report, error) {
	if db == nil {
		return nil, fmt.Errorf("法规库未打开")
	}
	r := &Report{Available: store.UsageAvailable(db), Window: WindowDays,
		Note: funnelNote}
	r.Since = time.Now().UTC().Add(-WindowDays * 24 * time.Hour).Format(time.RFC3339)

	kinds := []struct{ kind, label string }{
		{store.EventAppOpen, "打开本应用"},
		{store.EventSetupRoute, "选择了获取路线"},
		{store.EventPackInstall, "装好数据包"},
		{store.EventFetch, "从官方渠道获取"},
		{store.EventSearch, "完成一次检索"},
		{store.EventVerify, "完成一次引用核验"},
	}
	for _, k := range kinds {
		s := Step{Kind: k.kind, Label: k.label}
		if r.Available {
			_ = db.QueryRow(`SELECT COUNT(*) FROM usage_event WHERE kind=? AND created_at>=?`,
				k.kind, r.Since).Scan(&s.Count)
		}
		s.Done = s.Count > 0
		r.Funnel = append(r.Funnel, s)
	}

	if r.Available {
		var zero sql.NullInt64
		_ = db.QueryRow(`SELECT COUNT(*), SUM(outcome='zero') FROM usage_event WHERE kind='search' AND created_at>=?`,
			r.Since).Scan(&r.SearchTotal, &zero)
		r.SearchZero = int(zero.Int64)
		if r.SearchTotal > 0 {
			r.ZeroRate = float64(r.SearchZero) * 100 / float64(r.SearchTotal)
		}
		var vt sql.NullInt64
		_ = db.QueryRow(`SELECT COUNT(*) FROM usage_event WHERE kind='verify' AND created_at>=?`, r.Since).Scan(&vt)
		r.VerifyTotal = int(vt.Int64)
		_ = db.QueryRow(`SELECT COUNT(*) FROM usage_event WHERE kind='fetch' AND created_at>=?`, r.Since).Scan(&r.FetchRuns)
	}

	_ = db.QueryRow(`SELECT COUNT(*) FROM review_log WHERE created_at>=?`, r.Since).Scan(&r.HumanReviews)
	r.HumanPerDay = float64(r.HumanReviews) / float64(WindowDays)
	// 例外队列积压：智能体判 escalate、且条目现在仍是 pending（已人工处理的不算积压）。
	_ = db.QueryRow(`SELECT COUNT(DISTINCT ar.doc_id) FROM agent_review ar JOIN document d ON d.doc_id=ar.doc_id
		WHERE ar.verdict='escalate' AND d.confirm_status='pending'`).Scan(&r.ExceptionQ)

	var pass, fail sql.NullInt64
	_ = db.QueryRow(`SELECT SUM(result='pass'), SUM(result='fail') FROM validate_log WHERE created_at>=?`,
		r.Since).Scan(&pass, &fail)
	r.ChainFail = int(fail.Int64)
	r.ChainTotal = int(pass.Int64) + r.ChainFail
	if r.ChainTotal > 0 {
		r.FailRate = float64(r.ChainFail) * 100 / float64(r.ChainTotal)
	}
	return r, nil
}

// Headline 一行白话（未记录必须直说）。
func (r *Report) Headline() string {
	if !r.Available {
		return "本机尚未记录使用情况（记录只在这台机器上累计，不上传）。"
	}
	if r.SearchTotal == 0 && r.HumanReviews == 0 {
		return fmt.Sprintf("最近 %d 天本机还没有检索与复核记录。", r.Window)
	}
	return fmt.Sprintf("最近 %d 天：检索 %d 次（其中 %d 次没有命中）、引用核验 %d 次、人工复核 %d 次（平均每天 %.1f 次）、待您纠正 %d 条。",
		r.Window, r.SearchTotal, r.SearchZero, r.VerifyTotal, r.HumanReviews, r.HumanPerDay, r.ExceptionQ)
}
