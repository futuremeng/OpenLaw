// confirm.go：待复核条目的落判流程（抽样 → 审核 → 落判 → 审计）。
// 原则：一切状态流转必须落 review_log（谁/何时/依据什么）；落判来源必须落
// document.confirm_source（agent=智能体自审核，human=人工）——诚实展示纪律要求界面
// 区分「智能体确认」与「人工确认」，不得混同（docs/design-agent-review.md）。
// 智能体自审核（agent 包）经本文件同一 runner 落判：桌面端与 CLI 都不是第二写路径。
// 抽样按风险优先：日期推断、全文即一节、缺公布日的文档先进队列。
package ingest

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// Confirmer 人工确认服务。
type Confirmer struct {
	DataDir string
	DB      *store.DB
}

// ---- 风险标记 ----

// RiskSet：doc_id → 风险标记列表（inferred_date / whole_text / no_publish_date）。
type RiskSet map[string][]string

// LoadRiskSet 从 batch_report CSV（openlaw-batch 产物）读取风险标记。
// 状态 ok_inferred/ok_whole/ok_whole_inferred 分别对应 inferred_date/whole_text/两者。
// DB 侧另有 whole_text 与 no_publish_date 判定（见 loadPending），报告缺失不致漏标。
func LoadRiskSet(reports []string) (RiskSet, error) {
	rs := RiskSet{}
	for _, p := range reports {
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("读取批量报告失败 %s: %w", p, err)
		}
		r := csv.NewReader(f)
		recs, err := r.ReadAll()
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("解析批量报告失败 %s: %w", p, err)
		}
		if len(recs) < 2 {
			continue
		}
		hdr := map[string]int{}
		for i, c := range recs[0] {
			hdr[c] = i
		}
		iDoc, iStatus := hdr["doc_id"], hdr["status"]
		for _, rec := range recs[1:] {
			doc := strings.TrimSpace(rec[iDoc])
			if doc == "" {
				continue
			}
			switch rec[iStatus] {
			case "ok_inferred":
				rs[doc] = append(rs[doc], "inferred_date")
			case "ok_whole":
				rs[doc] = append(rs[doc], "whole_text")
			case "ok_whole_inferred":
				rs[doc] = append(rs[doc], "inferred_date", "whole_text")
			}
		}
	}
	return rs, nil
}

// LoadRiskSetFromProvenance 从 provenance/<doc_id>.json 读取权威风险标记
// （effective_date_inferred）。批量报告会被重跑覆盖，溯源文件才是持久事实。
func LoadRiskSetFromProvenance(dataDir string) (RiskSet, error) {
	rs := RiskSet{}
	dir := filepath.Join(dataDir, "provenance")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return rs, nil
		}
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var p struct {
			DocID                 string `json:"doc_id"`
			EffectiveDateInferred bool   `json:"effective_date_inferred"`
		}
		if err := json.Unmarshal(raw, &p); err != nil || !p.EffectiveDateInferred {
			continue
		}
		doc := p.DocID
		if doc == "" {
			doc = strings.TrimSuffix(name, ".json")
		}
		rs[doc] = append(rs[doc], "inferred_date")
	}
	return rs, nil
}

// MergeRiskSets 合并多个标记集（同一 doc 的标记去重拼接）。
func MergeRiskSets(sets ...RiskSet) RiskSet {
	out := RiskSet{}
	for _, rs := range sets {
		for doc, flags := range rs {
			for _, f := range flags {
				if !contains(out[doc], f) {
					out[doc] = append(out[doc], f)
				}
			}
		}
	}
	return out
}

// ---- 统计 ----

// LevelCount 单一效力等级的确认状态分布。
type LevelCount struct {
	Level     string `json:"level"`
	Pending   int    `json:"pending"`
	Confirmed int    `json:"confirmed"`
	Rejected  int    `json:"rejected"`
}

// ConfirmStats 确认进度与风险面统计。
type ConfirmStats struct {
	Total     int          `json:"total"`
	Pending   int          `json:"pending"`
	Confirmed int          `json:"confirmed"`
	Rejected  int          `json:"rejected"`
	ByLevel   []LevelCount `json:"by_level"`
	Risk      struct {
		InferredDate  int `json:"inferred_date"`
		WholeText     int `json:"whole_text"`
		NoPublishDate int `json:"no_publish_date"`
	} `json:"risk"`
	Reviewed int `json:"reviewed"` // review_log 累计流转次数
}

// ---- 抽样 ----

// PendingRow 复核队列一行（导出：agent 包与桌面端经 LoadPending 共用同一取列口径）。
type PendingRow struct {
	DocID, LawID, LawName, Level, Issuer, VersionNo, Effective, Publish string
	SourceType, SHA256, FilePath                                        string
	Articles, Whole                                                     int
	Head, Tail                                                          string
	Flags                                                               []string
}

// pendingQuery 拉取全部 pending 文档及复核材料（文本首尾摘要供比对）。
const pendingQuery = `SELECT d.doc_id, COALESCE(l.law_id,''), COALESCE(l.name,''),
	COALESCE(l.effect_level,'（未标注）'), COALESCE(l.issuer,''), COALESCE(v.version_no,''),
	COALESCE(v.effective_date,''), COALESCE(d.publish_date,''), COALESCE(d.source_type,''),
	d.sha256, d.file_path,
	(SELECT COUNT(*) FROM article a WHERE a.doc_id = d.doc_id),
	(SELECT COUNT(*) FROM article a WHERE a.doc_id = d.doc_id AND a.article_no = '全文'),
	COALESCE((SELECT a.content FROM article a WHERE a.doc_id = d.doc_id ORDER BY a.article_id LIMIT 1),''),
	COALESCE((SELECT a.content FROM article a WHERE a.doc_id = d.doc_id ORDER BY a.article_id DESC LIMIT 1),'')
	FROM document d
	LEFT JOIN law_version v ON v.doc_id = d.doc_id
	LEFT JOIN law l ON l.law_id = v.law_id
	WHERE d.confirm_status = 'pending'
	ORDER BY d.doc_id`

// LoadPending 导出待审队列（agent 包与桌面端共用同一取列口径，桌面端不是第二读路径）。
func (c *Confirmer) LoadPending(rs RiskSet) ([]PendingRow, error) {
	return c.loadPending(rs)
}

func (c *Confirmer) loadPending(rs RiskSet) ([]PendingRow, error) {
	rows, err := c.DB.Query(pendingQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingRow
	for rows.Next() {
		var p PendingRow
		if err := rows.Scan(&p.DocID, &p.LawID, &p.LawName, &p.Level, &p.Issuer, &p.VersionNo,
			&p.Effective, &p.Publish, &p.SourceType, &p.SHA256, &p.FilePath,
			&p.Articles, &p.Whole, &p.Head, &p.Tail); err != nil {
			return nil, err
		}
		p.Flags = append(p.Flags, rs[p.DocID]...)
		if p.Whole > 0 && !contains(p.Flags, "whole_text") {
			p.Flags = append(p.Flags, "whole_text")
		}
		if p.Publish == "" {
			p.Flags = append(p.Flags, "no_publish_date")
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Sample 生成复核清单 CSV：高风险（有标记）全部入列，剩余配额按效力等级轮转补齐。
// n<=0 表示仅高风险（无上限）；返回入列条数。
func (c *Confirmer) Sample(outPath string, n int, rs RiskSet) (int, error) {
	pending, err := c.loadPending(rs)
	if err != nil {
		return 0, err
	}
	var risky []PendingRow
	byLevel := map[string][]PendingRow{}
	for _, p := range pending {
		if len(p.Flags) > 0 {
			risky = append(risky, p)
		} else {
			byLevel[p.Level] = append(byLevel[p.Level], p)
		}
	}
	sort.SliceStable(risky, func(i, j int) bool { return len(risky[i].Flags) > len(risky[j].Flags) })
	picked := risky
	if n > 0 && n < len(picked) {
		picked = picked[:n]
	} else if n > 0 {
		// 轮转补齐：各效力等级轮流出样本，避免大类淹没小类
		levels := make([]string, 0, len(byLevel))
		for lv := range byLevel {
			levels = append(levels, lv)
		}
		sort.Strings(levels)
		idx := map[string]int{}
		for len(picked) < n {
			progress := false
			for _, lv := range levels {
				if len(picked) >= n {
					break
				}
				if idx[lv] < len(byLevel[lv]) {
					picked = append(picked, byLevel[lv][idx[lv]])
					idx[lv]++
					progress = true
				}
			}
			if !progress {
				break // pending 用尽
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	hdr := []string{"doc_id", "law_id", "law_name", "effect_level", "issuer", "version_no",
		"effective_date", "publish_date", "source_type", "sha256", "file_path", "articles",
		"risk_flags", "text_head", "text_tail", "verdict", "reviewer", "note"}
	if err := w.Write(hdr); err != nil {
		return 0, err
	}
	for _, p := range picked {
		if err := w.Write([]string{p.DocID, p.LawID, p.LawName, p.Level, p.Issuer, p.VersionNo,
			p.Effective, p.Publish, p.SourceType, p.SHA256, p.FilePath, fmt.Sprint(p.Articles),
			strings.Join(p.Flags, "|"), truncRunes(p.Head, 160), truncRunes(p.Tail, 160), "", "", ""}); err != nil {
			return 0, err
		}
	}
	w.Flush()
	return len(picked), w.Error()
}

// Stats 确认进度与风险面统计（rs 可为 nil：inferred_date 取 import_log 留痕与报告标记的并集）。
func (c *Confirmer) Stats(rs RiskSet) (*ConfirmStats, error) {
	st := &ConfirmStats{}
	rows, err := c.DB.Query(`SELECT confirm_status, COUNT(*) FROM document GROUP BY confirm_status`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			rows.Close()
			return nil, err
		}
		st.Total += n
		switch s {
		case "pending":
			st.Pending = n
		case "confirmed":
			st.Confirmed = n
		case "rejected":
			st.Rejected = n
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	lrows, err := c.DB.Query(`SELECT COALESCE(l.effect_level,'（未标注）'),
		SUM(d.confirm_status='pending'), SUM(d.confirm_status='confirmed'), SUM(d.confirm_status='rejected')
		FROM document d
		LEFT JOIN law_version v ON v.doc_id = d.doc_id
		LEFT JOIN law l ON l.law_id = v.law_id
		GROUP BY 1 ORDER BY 3 DESC`)
	if err != nil {
		return nil, err
	}
	for lrows.Next() {
		var lc LevelCount
		if err := lrows.Scan(&lc.Level, &lc.Pending, &lc.Confirmed, &lc.Rejected); err != nil {
			lrows.Close()
			return nil, err
		}
		st.ByLevel = append(st.ByLevel, lc)
	}
	if err := lrows.Err(); err != nil {
		lrows.Close()
		return nil, err
	}
	lrows.Close()

	if err := c.DB.QueryRow(`SELECT
		(SELECT COUNT(DISTINCT doc_id) FROM article WHERE article_no='全文'),
		(SELECT COUNT(*) FROM document WHERE (publish_date IS NULL OR publish_date='') AND confirm_status='pending'),
		(SELECT COUNT(*) FROM review_log)`).
		Scan(&st.Risk.WholeText, &st.Risk.NoPublishDate, &st.Reviewed); err != nil {
		return nil, err
	}
	// inferred_date：import_log 留痕（新导入）∪ 批量报告标记（存量回填前的权威来源）
	seen := map[string]bool{}
	irows, err := c.DB.Query(`SELECT DISTINCT doc_id FROM import_log
		WHERE result='success' AND reason LIKE '%effective_date_inferred%' AND doc_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	for irows.Next() {
		var d string
		if err := irows.Scan(&d); err != nil {
			irows.Close()
			return nil, err
		}
		seen[d] = true
	}
	if err := irows.Err(); err != nil {
		irows.Close()
		return nil, err
	}
	irows.Close()
	if rs != nil {
		for doc, flags := range rs {
			if contains(flags, "inferred_date") {
				seen[doc] = true
			}
		}
	}
	st.Risk.InferredDate = len(seen)
	return st, nil
}

// ---- 落判 ----

// ApplySummary 落判结果统计。
type ApplySummary struct {
	Confirmed int      `json:"confirmed"`
	Rejected  int      `json:"rejected"`
	Reset     int      `json:"reset"`
	Skipped   int      `json:"skipped"`
	Failures  []string `json:"failures,omitempty"`
}

var verdictToStatus = map[string]string{"confirm": "confirmed", "reject": "rejected", "reset": "pending"}

// errSkip 内部哨兵：目标状态与当前一致，幂等跳过。
var errSkip = fmt.Errorf("skip")

// Apply 读裁决 CSV（列：doc_id,verdict,reviewer,note；Sample 清单可直接填写后回读），
// 逐行落判并写 review_log。未填 verdict 的行跳过；单行失败不中断。
func (c *Confirmer) Apply(inPath string) (*ApplySummary, error) {
	f, err := os.Open(inPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	recs, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("解析裁决文件失败: %w", err)
	}
	if len(recs) < 2 {
		return nil, fmt.Errorf("裁决文件无数据行")
	}
	hdr := map[string]int{}
	for i, col := range recs[0] {
		hdr[col] = i
	}
	for _, col := range []string{"doc_id", "verdict", "reviewer"} {
		if _, ok := hdr[col]; !ok {
			return nil, fmt.Errorf("裁决文件缺少 %q 列", col)
		}
	}
	sum := &ApplySummary{}
	now := time.Now().UTC().Format(time.RFC3339)
	for i, rec := range recs[1:] {
		doc := strings.TrimSpace(rec[hdr["doc_id"]])
		verdict := strings.TrimSpace(rec[hdr["verdict"]])
		if doc == "" || verdict == "" {
			sum.Skipped++ // 清单里尚未裁决的行
			continue
		}
		to, ok := verdictToStatus[verdict]
		if !ok {
			sum.Failures = append(sum.Failures, fmt.Sprintf("第 %d 行: verdict 须为 confirm/reject/reset，实际 %q", i+2, verdict))
			continue
		}
		reviewer := strings.TrimSpace(rec[hdr["reviewer"]])
		if reviewer == "" {
			sum.Failures = append(sum.Failures, fmt.Sprintf("第 %d 行: reviewer 必填（确认必须署名）", i+2))
			continue
		}
		note := ""
		if j, ok := hdr["note"]; ok && j < len(rec) {
			note = strings.TrimSpace(rec[j])
		}
		if err := c.transition(doc, verdict, to, reviewer, note, now, "human"); err != nil {
			if err == errSkip {
				sum.Skipped++
				continue
			}
			sum.Failures = append(sum.Failures, fmt.Sprintf("第 %d 行 %s: %v", i+2, doc, err))
			continue
		}
		switch verdict {
		case "confirm":
			sum.Confirmed++
		case "reject":
			sum.Rejected++
		default:
			sum.Reset++
		}
	}
	return sum, nil
}

// transition 单文档状态流转 + 审计 + 落判来源，一个事务完成，绝不出现改状态无留痕。
// source：agent=智能体自审核落判，human=人工落判；回到 pending（reset）时清空来源——
// 未复核状态不得携带任何「已确认」痕迹（诚实展示纪律）。
func (c *Confirmer) transition(docID, action, to, reviewer, reason, now, source string) error {
	tx, err := c.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var from string
	err = tx.QueryRow(`SELECT confirm_status FROM document WHERE doc_id=?`, docID).Scan(&from)
	if err == sql.ErrNoRows {
		return fmt.Errorf("doc_id 不存在")
	} else if err != nil {
		return err
	}
	if from == to {
		return errSkip
	}
	src := any(source)
	if to == "pending" {
		src = nil
	}
	if _, err := tx.Exec(`UPDATE document SET confirm_status=?, confirm_source=? WHERE doc_id=?`, to, src, docID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO review_log(doc_id, action, from_status, to_status, reviewer, reason, created_at)
		VALUES(?,?,?,?,?,?,?)`, docID, action, from, to, reviewer, reason, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// 溯源 JSON 与库保持同一事实：confirm_status 同步（缺文件的历史数据不阻断确认）
	return SyncProvenanceStatus(c.DataDir, docID, to)
}

// SyncProvenanceStatus 更新 provenance/<doc_id>.json 的 confirm_status 字段。
func SyncProvenanceStatus(dataDir, docID, status string) error {
	path := filepath.Join(dataDir, "provenance", docID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("溯源 JSON 解析失败 %s: %w", path, err)
	}
	m["confirm_status"] = status
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// Judge 单条落判（桌面端人工纠正与智能体自审核共用同一写路径）。
// verdict：confirm/reject/reset；source：agent|human（必填——落判来源必须可区分，
// 这是诚实展示的库层保证）；必须署名（reviewer）并写明依据（reason），全程写 review_log。
// 已是目标状态时按幂等处理（返回 nil，不产生空转审计）。
func (c *Confirmer) Judge(docID, verdict, reviewer, reason, source string) error {
	to, ok := verdictToStatus[verdict]
	if !ok {
		return fmt.Errorf("verdict 须为 confirm/reject/reset")
	}
	if source != "agent" && source != "human" {
		return fmt.Errorf("source 须为 agent 或 human（落判来源必须可区分）")
	}
	if reviewer == "" || reason == "" {
		return fmt.Errorf("落判必须署名（reviewer）并写明依据（reason）")
	}
	err := c.transition(docID, verdict, to, reviewer, reason, time.Now().UTC().Format(time.RFC3339), source)
	if err == errSkip {
		return nil
	}
	return err
}

// ---- 条件批量 ----

// BulkFilter 批量流转的过滤条件（结构化参数，非裸 SQL）。
type BulkFilter struct {
	SourceLike string // document.source_type LIKE（CLI 用，允许通配符）
	Source     string // document.source_type 精确匹配（界面下拉交来的是真实来源名，不让用户写通配符）
	Level      string // law.effect_level 精确匹配
}

// describe 把过滤条件写成一句人话，进复核记录（审计可读）。
func (f BulkFilter) describe() string {
	var parts []string
	if f.Source != "" {
		parts = append(parts, fmt.Sprintf("来源=%q", f.Source))
	}
	if f.SourceLike != "" {
		parts = append(parts, fmt.Sprintf("来源按模式 %q", f.SourceLike))
	}
	if f.Level != "" {
		parts = append(parts, fmt.Sprintf("效力等级=%q", f.Level))
	}
	if len(parts) == 0 {
		return "全部条目"
	}
	return strings.Join(parts, "，")
}

// verdictToFrom 各裁决的合法起始状态（reset 只能回流已确认/已驳回，不能空转）。
var verdictToFrom = map[string][]string{
	"confirm": {"pending"},
	"reject":  {"pending"},
	"reset":   {"confirmed", "rejected"},
}

const bulkBase = `FROM document d
	LEFT JOIN law_version v ON v.doc_id = d.doc_id
	LEFT JOIN law l ON l.law_id = v.law_id`

func (f BulkFilter) where(from []string) (string, []any) {
	ph := strings.Repeat("?,", len(from))
	cond := []string{"d.confirm_status IN (" + ph[:len(ph)-1] + ")"}
	args := make([]any, 0, len(from)+2)
	for _, s := range from {
		args = append(args, s)
	}
	if f.Source != "" {
		cond = append(cond, "COALESCE(d.source_type,'') = ?")
		args = append(args, f.Source)
	}
	if f.SourceLike != "" {
		cond = append(cond, "COALESCE(d.source_type,'') LIKE ?")
		args = append(args, f.SourceLike)
	}
	if f.Level != "" {
		cond = append(cond, "COALESCE(l.effect_level,'') = ?")
		args = append(args, f.Level)
	}
	return strings.Join(cond, " AND "), args
}

// BulkMatch 统计命中条数（dry-run 用，按裁决的起始状态过滤）。
func (c *Confirmer) BulkMatch(f BulkFilter, verdict string) (int, error) {
	from, ok := verdictToFrom[verdict]
	if !ok {
		return 0, fmt.Errorf("verdict 须为 confirm/reject/reset")
	}
	where, args := f.where(from)
	var n int
	err := c.DB.QueryRow(`SELECT COUNT(*) `+bulkBase+` WHERE `+where, args...).Scan(&n)
	return n, err
}

// Bulk 按条件批量流转状态，全部写 review_log（reason 必含过滤条件与依据）。
// 必须署名并写明依据——绝不允许无审计痕迹的整库确认。
func (c *Confirmer) Bulk(f BulkFilter, verdict, reviewer, reason string) (*ApplySummary, error) {
	to, ok := verdictToStatus[verdict]
	if !ok {
		return nil, fmt.Errorf("verdict 须为 confirm/reject/reset")
	}
	if reviewer == "" || reason == "" {
		return nil, fmt.Errorf("批量流转必须署名（-reviewer）并写明依据（-reason）")
	}
	where, args := f.where(verdictToFrom[verdict])
	rows, err := c.DB.Query(`SELECT d.doc_id `+bulkBase+` WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	var docs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		docs = append(docs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	note := fmt.Sprintf("批量 %s | 条件: %s | 依据: %s", verdict, f.describe(), reason)
	now := time.Now().UTC().Format(time.RFC3339)
	sum := &ApplySummary{}
	for _, id := range docs {
		if err := c.transition(id, verdict, to, reviewer, note, now, "human"); err != nil {
			if err == errSkip {
				sum.Skipped++
				continue
			}
			sum.Failures = append(sum.Failures, id+": "+err.Error())
			continue
		}
		switch verdict {
		case "confirm":
			sum.Confirmed++
		case "reject":
			sum.Rejected++
		default:
			sum.Reset++
		}
	}
	return sum, nil
}
