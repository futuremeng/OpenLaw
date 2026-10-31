// review.go 双智能体自审核流水线：分类 → 审核 → 门槛落判 / 例外队列。
// 自动确认的门槛（两个置信度都达标）之外的一切情况都留在待复核并如实留痕——
// 人工只被叫去看「需要纠正的个别条目」。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

// DefaultMinConfidence 自动确认的默认置信度门槛（分类与审核两个结论都要达标）。
const DefaultMinConfidence = 0.85

// ClassifyResult 分类智能体结论（confidence 0–1；缺把握就给低值，不得编造）。
type ClassifyResult struct {
	EffectLevel string  `json:"effect_level"`
	Issuer      string  `json:"issuer"`
	PublishDate string  `json:"publish_date"`
	RegionCode  string  `json:"region_code"`
	Confidence  float64 `json:"confidence"`
	Reason      string  `json:"reason"`
}

// AuditResult 审核智能体结论。Verdict：confirm=可自动确认 / escalate=转人工。
type AuditResult struct {
	Verdict    string  `json:"verdict"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

const classifySystem = `你是法律法规数据库的分类智能体。给定一部法规的现有元数据与官方原文摘要，你负责补全或核对以下著录项：效力等级、发布机关、公布日期、地域区划码。
效力等级只能从这些值里选：宪法、法律、行政法规、监察法规、地方性法规、部门规章、地方政府规章、司法解释、其他、欧盟条例、欧盟指令、欧盟决定；判不出就留空。
区划码用 6 位数字（GB/T 2260，如北京市 110000）；中央立法或判不出就留空。
只输出一个 JSON 对象，不要输出其他任何文字：
{"effect_level":"…","issuer":"…","publish_date":"YYYY-MM-DD 或空","region_code":"…","confidence":0 到 1 之间的小数,"reason":"一句话依据"}
没有把握的项目留空并调低 confidence；绝不编造。`

const auditSystem = `你是法律法规数据库的审核智能体，负责比对官方原文与著录元数据，裁决该条目能否自动确认。
你会看到：条目元数据、来源、风险标记、原文首尾摘要，以及分类智能体的建议。
裁决标准：原文标题与法规名称一致、施行/公布信息与原文相符、无未解释的风险标记时给 confirm；
只要存在任何不一致、原文信息缺失、风险标记存疑或你不确定，一律给 escalate（宁可转人工，绝不误确认）。
只输出一个 JSON 对象，不要输出其他任何文字：
{"verdict":"confirm 或 escalate","confidence":0 到 1 之间的小数,"reason":"一句话结论依据"}`

// extractJSON 从模型回复里抠出第一个完整 JSON 对象（容忍围栏与前后废话）。
func extractJSON(s string) (string, error) {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return "", fmt.Errorf("回复中没有 JSON 对象")
	}
	return s[i : j+1], nil
}

func clampConf(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// parseClassify 解析分类结论；任何不合法都按低置信处理（fail-closed），不中断流程。
func parseClassify(raw string) (ClassifyResult, error) {
	obj, err := extractJSON(raw)
	if err != nil {
		return ClassifyResult{Confidence: 0}, err
	}
	var c ClassifyResult
	if err := json.Unmarshal([]byte(obj), &c); err != nil {
		return ClassifyResult{Confidence: 0}, err
	}
	c.Confidence = clampConf(c.Confidence)
	return c, nil
}

// parseAudit 解析审核结论；裁决不合法一律 escalate（fail-closed）。
func parseAudit(raw string) (AuditResult, error) {
	obj, err := extractJSON(raw)
	if err != nil {
		return AuditResult{Verdict: "escalate", Reason: "审核智能体未给出可解析的结论"}, err
	}
	var a AuditResult
	if err := json.Unmarshal([]byte(obj), &a); err != nil {
		return AuditResult{Verdict: "escalate", Reason: "审核智能体结论无法解析"}, err
	}
	if a.Verdict != "confirm" {
		a.Verdict = "escalate"
	}
	a.Confidence = clampConf(a.Confidence)
	return a, nil
}

// pendingBrief 把待审条目写成给智能体的用户消息（元数据 + 原文首尾摘要 + 风险标记）。
func pendingBrief(p ingest.PendingRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "法规名称：%s\n", p.LawName)
	fmt.Fprintf(&b, "条目编号：%s\n", p.DocID)
	fmt.Fprintf(&b, "现有元数据：效力等级=%s；发布机关=%s；公布日期=%s；版本=%s；施行日期=%s\n",
		orUnknown(p.Level), orUnknown(p.Issuer), orUnknown(p.Publish), orUnknown(p.VersionNo), orUnknown(p.Effective))
	fmt.Fprintf(&b, "来源：%s；收录条文数：%d\n", orUnknown(p.SourceType), p.Articles)
	if len(p.Flags) > 0 {
		fmt.Fprintf(&b, "风险标记：%s\n", strings.Join(p.Flags, "、"))
	}
	fmt.Fprintf(&b, "原文开头：%s\n", p.Head)
	fmt.Fprintf(&b, "原文结尾：%s\n", p.Tail)
	return b.String()
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（缺失）"
	}
	return s
}

// Options 流水线参数。Limit<=0 表示处理全部待审条目；MinConfidence<=0 用默认门槛。
type Options struct {
	Limit         int
	MinConfidence float64
	Progress      func(done, total int, docID, verdict string)
}

// Summary 一轮自审核的如实汇总。
type Summary struct {
	Total     int    `json:"total"`
	Confirmed int    `json:"confirmed"`
	Escalated int    `json:"escalated"`
	Failed    int    `json:"failed"`
	Model     string `json:"model"`
	StoppedAt int    `json:"stopped_at"` // 中断时已处理数（如实保留，不谎称全量）
}

// recordReview 每次审核都留痕（含原始 JSON 结论）——例外队列与事后抽查的依据。
func recordReview(db *store.DB, docID, verdict string, conf float64, clsRaw, audRaw, model string) error {
	_, err := db.Exec(`INSERT INTO agent_review(doc_id,verdict,confidence,classify,audit,model,created_at)
		VALUES(?,?,?,?,?,?,?)`, docID, verdict, conf, clsRaw, audRaw, model,
		time.Now().UTC().Format(time.RFC3339))
	return err
}

// RunReview 跑一轮双智能体自审核。chat 必填（未配置时调用方必须拒绝任务）；
// ctx 取消即停并如实返回已处理结果。写路径全部经 ingest.Confirmer（同一 runner）。
func RunReview(ctx context.Context, dataDir string, db *store.DB, chat Chat, model string,
	rs ingest.RiskSet, opts Options) (*Summary, error) {
	if chat == nil {
		return nil, fmt.Errorf("智能审核通道未配置")
	}
	if opts.MinConfidence <= 0 {
		opts.MinConfidence = DefaultMinConfidence
	}
	c := &ingest.Confirmer{DataDir: dataDir, DB: db}
	rows, err := c.LoadPending(rs)
	if err != nil {
		return nil, err
	}
	if opts.Limit > 0 && opts.Limit < len(rows) {
		rows = rows[:opts.Limit]
	}
	sum := &Summary{Total: len(rows), Model: model}
	for i, p := range rows {
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		brief := pendingBrief(p)

		clsRaw, err := chat.Complete(ctx, classifySystem, brief)
		var cls ClassifyResult
		if err == nil {
			cls, err = parseClassify(clsRaw)
		}
		audRaw := ""
		aud := AuditResult{Verdict: "escalate", Reason: "智能体未产出可解析的结论，转人工"}
		if err == nil {
			audRaw, err = chat.Complete(ctx, auditSystem, brief+"\n分类智能体的建议："+clsRaw)
			if err == nil {
				aud, _ = parseAudit(audRaw)
			} else {
				audRaw = ""
			}
		} else {
			aud.Reason = "分类智能体未产出可解析的结论，转人工"
		}

		verdict := aud.Verdict
		downgraded := false
		if verdict == "confirm" && (cls.Confidence < opts.MinConfidence || aud.Confidence < opts.MinConfidence) {
			verdict = "escalate"
			downgraded = true
			aud.Reason = fmt.Sprintf("%s（置信度未达门槛：分类 %.2f / 审核 %.2f，门槛 %.2f）",
				aud.Reason, cls.Confidence, aud.Confidence, opts.MinConfidence)
		}
		// 留证：能确认的正常存模型原始回复；转人工的存「有效结论」（含门槛/解析失败说明），
		// 例外队列直接以 audit 展示「为什么需要您纠正」。
		auditStored := audRaw
		if downgraded || auditStored == "" {
			if b, merr := json.Marshal(aud); merr == nil {
				auditStored = string(b)
			}
		}

		if rerr := recordReview(db, p.DocID, verdict, aud.Confidence, clsRaw, auditStored, model); rerr != nil {
			return sum, fmt.Errorf("智能审核留痕写入失败（已停在该条目，之前的结论已保留）: %w", rerr)
		}

		if verdict == "confirm" {
			reason := fmt.Sprintf("双智能体自审核通过 | 分类：%s | 审核：%s | 置信度 分类 %.2f / 审核 %.2f",
				orUnknown(cls.Reason), orUnknown(aud.Reason), cls.Confidence, aud.Confidence)
			if jerr := c.Judge(p.DocID, "confirm", "智能体复核（"+model+"）", reason, "agent"); jerr != nil {
				sum.Failed++
			} else {
				sum.Confirmed++
			}
		} else {
			sum.Escalated++
		}
		sum.StoppedAt = i + 1
		if opts.Progress != nil {
			opts.Progress(i+1, len(rows), p.DocID, verdict)
		}
	}
	return sum, nil
}

// ConfidenceLabel 置信度的白话写法（界面展示用）。
func ConfidenceLabel(conf float64) string {
	return strconv.FormatFloat(clampConf(conf), 'f', 2, 64)
}
