// agentreview.go 「智能审核」面板绑定：双智能体自审核（agent 包）+ 人工例外纠正。
//
// 纪律（docs/design-agent-review.md）：
//   - 默认离线：智能体通道未配置一律拒绝任务，并给出可照做的白话指引；
//   - 同一代码路径：审核经 agent.RunReview → ingest.Confirmer（与 CLI/库层同一 runner）；
//   - 人工只出现在例外队列：纠正自动署名「本机用户（人工纠正）」，结论依据必填，
//     全部落 review_log（库层强制）；
//   - 诚实展示：智能体确认与人工确认分开计数、分开徽章；例外条目如实给出
//     智能体的结论、置信度与依据（agent_review 留痕原文）。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/futuremeng/OpenLaw/agent"
	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

// AgentStatus 智能审核通道状态：未配置时 guidance 给出白话指引，
// technical 只在前端「技术细节」折叠里展示（变量名不得进主视图）。
func (a *App) AgentStatus() map[string]any {
	_, _, model, ok := agent.Config()
	out := map[string]any{
		"configured": ok,
		"model":      model,
		"guidance":   agent.Guidance,
		"technical":  "环境变量 " + agent.EnvEndpoint + " / " + agent.EnvKey + " / " + agent.EnvModel + "（OpenAI 兼容接口；本机模型亦可）",
	}
	return out
}

// StartReviewTask 提交一轮双智能体自审核（单槽任务，可取消；中断如实保留已审结果）。
func (a *App) StartReviewTask(limit int) (TaskInfo, error) {
	chat, ok := agent.NewFromEnv()
	if !ok {
		return TaskInfo{}, errors.New(agent.Guidance)
	}
	_, _, model, _ := agent.Config()
	if limit < 0 {
		limit = 0
	}
	params := fmt.Sprintf("limit=%d model=%s", limit, model)
	return a.tm.Submit("agent-review", params, func(ctx context.Context, progress func(string)) error {
		db, err := store.Open(filepath.Join(a.dataDir, "db", "openlaw.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		rs := a.riskSet()
		sum, err := agent.RunReview(ctx, a.dataDir, db, chat, model, rs, agent.Options{
			Limit: limit,
			Progress: func(done, total int, docID, verdict string) {
				label := "需要您纠正"
				if verdict == "confirm" {
					label = "已自动确认"
				}
				progress(fmt.Sprintf("%d/%d %s %s", done, total, label, docID))
			},
		})
		if sum != nil {
			progress(fmt.Sprintf("完成：自动确认 %d 条 · 需要您纠正 %d 条 · 失败 %d 条（模型 %s）",
				sum.Confirmed, sum.Escalated, sum.Failed, sum.Model))
		}
		return err
	})
}

// AgentStats 智能审核统计（诚实口径：智能体确认 / 人工确认 / 需要纠正 分开计数）。
type AgentStats struct {
	AgentConfirmed int    `json:"agent_confirmed"`
	HumanConfirmed int    `json:"human_confirmed"`
	LegacyConfirm  int    `json:"legacy_confirmed"` // 历史数据：已确认但来源未记录
	Escalated      int    `json:"escalated"`        // 智能体审过但转人工（需要您纠正）
	Unreviewed     int    `json:"unreviewed"`       // 尚未审核（无智能体留痕）
	LastRun        string `json:"last_run,omitempty"`
}

func (a *App) AgentStats() (*AgentStats, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	st := &AgentStats{}
	if err := a.db.QueryRow(`SELECT
		SUM(CASE WHEN confirm_status='confirmed' AND confirm_source='agent' THEN 1 ELSE 0 END),
		SUM(CASE WHEN confirm_status='confirmed' AND confirm_source='human' THEN 1 ELSE 0 END),
		SUM(CASE WHEN confirm_status='confirmed' AND COALESCE(confirm_source,'')='' THEN 1 ELSE 0 END),
		SUM(CASE WHEN confirm_status='pending' AND EXISTS (SELECT 1 FROM agent_review ar WHERE ar.doc_id=document.doc_id) THEN 1 ELSE 0 END),
		SUM(CASE WHEN confirm_status='pending' AND NOT EXISTS (SELECT 1 FROM agent_review ar WHERE ar.doc_id=document.doc_id) THEN 1 ELSE 0 END)
		FROM document`).Scan(&st.AgentConfirmed, &st.HumanConfirmed, &st.LegacyConfirm, &st.Escalated, &st.Unreviewed); err != nil {
		return nil, err
	}
	var last sql.NullString
	if err := a.db.QueryRow(`SELECT MAX(created_at) FROM agent_review`).Scan(&last); err == nil {
		st.LastRun = last.String
	}
	return st, nil
}

// ExceptionRow 「需要您纠正的条目」一行：智能体最近一次结论 + 依据（留痕原文）。
type ExceptionRow struct {
	DocID      string  `json:"doc_id"`
	LawName    string  `json:"law_name"`
	Level      string  `json:"level"`
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`  // 白话：为什么需要人工（从留痕 JSON 提取）
	Suggest    string  `json:"suggest"` // 分类智能体建议（等级/机关/日期/地域）白话
	Model      string  `json:"model"`
	ReviewedAt string  `json:"reviewed_at"`
}

// auditReasonText 从 audit 留痕（模型原始回复或有效结论 JSON）提取白话依据。
func auditReasonText(raw string) string {
	var v struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err == nil && strings.TrimSpace(v.Reason) != "" {
		return v.Reason
	}
	return "智能体未给出可读依据——请在右侧核对原文与留痕。"
}

// classifySuggestText 把分类建议写成一行白话（缺项不列，不刷屏）。
func classifySuggestText(raw string) string {
	var c agent.ClassifyResult
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return ""
	}
	var parts []string
	if c.EffectLevel != "" {
		parts = append(parts, "类别 "+c.EffectLevel)
	}
	if c.Issuer != "" {
		parts = append(parts, "机关 "+c.Issuer)
	}
	if c.PublishDate != "" {
		parts = append(parts, "公布 "+c.PublishDate)
	}
	if c.RegionCode != "" {
		parts = append(parts, "地域 "+c.RegionCode)
	}
	if c.SubjectCode != "" {
		parts = append(parts, "部门法 "+c.SubjectCode+"（置信 "+agent.ConfidenceLabel(c.SubjectConfidence)+"）")
	}
	if c.DocForm != "" {
		parts = append(parts, "形态 "+c.DocForm)
	}
	return strings.Join(parts, " · ")
}

// ExceptionList 例外队列（智能体审过但仍待复核的条目），最近结论优先。
func (a *App) ExceptionList(filter string, limit, offset int) ([]ExceptionRow, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	if limit <= 0 || limit > 300 {
		limit = 300
	}
	if offset < 0 {
		offset = 0
	}
	conds := []string{"d.confirm_status='pending'",
		"EXISTS (SELECT 1 FROM agent_review ar WHERE ar.doc_id=d.doc_id)"}
	args := []any{}
	if f := strings.TrimSpace(filter); f != "" {
		conds = append(conds, "(d.doc_id LIKE ? OR COALESCE(l.name,'') LIKE ?)")
		args = append(args, "%"+f+"%", "%"+f+"%")
	}
	rows, err := a.db.Query(`SELECT d.doc_id, COALESCE(l.name,'（未关联法规）'),
		COALESCE(l.effect_level,''), COALESCE(d.source_type,''),
		ar.confidence, ar.audit, ar.classify, ar.model, ar.created_at
		FROM document d
		JOIN agent_review ar ON ar.review_id =
			(SELECT review_id FROM agent_review x WHERE x.doc_id=d.doc_id ORDER BY review_id DESC LIMIT 1)
		LEFT JOIN law_version v ON v.doc_id=d.doc_id
		LEFT JOIN law l ON l.law_id=v.law_id
		WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY ar.review_id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExceptionRow
	for rows.Next() {
		var r ExceptionRow
		var auditRaw, classRaw string
		if err := rows.Scan(&r.DocID, &r.LawName, &r.Level, &r.Source,
			&r.Confidence, &auditRaw, &classRaw, &r.Model, &r.ReviewedAt); err != nil {
			return nil, err
		}
		r.Reason = auditReasonText(auditRaw)
		r.Suggest = classifySuggestText(classRaw)
		out = append(out, r)
	}
	return out, rows.Err()
}

// CorrectDoc 人工纠正（例外队列里唯一的人工动作）：确认或驳回该条目；
// 对已自动确认的条目用 reset 撤回重审。署名由界面固定为「本机用户（人工纠正）」，
// 结论依据必填——人工参与降到最低，但审计纪律一寸不让。
func (a *App) CorrectDoc(docID, verdict, reason string) error {
	if a.db == nil {
		return errors.New(a.openErr)
	}
	if strings.TrimSpace(docID) == "" {
		return errors.New("未选择条目")
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("请填写结论依据（一句话即可，进复核记录）")
	}
	c := &ingest.Confirmer{DataDir: a.dataDir, DB: a.db}
	return c.Judge(strings.TrimSpace(docID), strings.TrimSpace(verdict), "本机用户（人工纠正）", strings.TrimSpace(reason), "human")
}
