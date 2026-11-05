// subject.go 部门法标引与文档形态的库层执行路径（docs/design-taxonomy.md §5）。
// CLI（openlaw-subject）与桌面端必须共用这里——桌面端不是第二条写路径。
//
// 纪律：
//   - 机器只出建议，且只写 subject_code 为空（未标注）的行；人工落判过的行
//     （subject_reviewer 非空）机器一律不再写——驳回后留空但署名仍在，不得顶回人工结论；
//   - 机器写入 subject_reviewer 一律留空（= 机器建议 · 待复核，界面必须如实显示）；
//   - 每一笔写 subject_log（law 级审计），批量带 batch_id，可整批撤销；
//   - 文档形态全自动（拍板项 2）：doc_form 为空即写机器判定，判不出留空计入待办；
//     其审计行以 rule 前缀 title-suffix 区分，撤销时按前缀分流清列；
//   - 写入取值必须命中词表（dict_subject/dict_doc_form），词表缺失的码一律拒写——
//     宁缺勿错，规则与词表脱节属实现缺陷，必须显式失败而非静默留空。
package ingest

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
)

// 建议置信口径（报告与界面共用，避免各处自造说法）。
const (
	SubjectConfMarked   = "marked"   // 已有部门法标注（本次不动）
	SubjectConfHigh     = "high"     // 规则命中 → 可写（机器建议 · 待复核）
	SubjectConfRejected = "rejected" // 人工已驳回/清除：留空但已复核，机器不得再写
	SubjectConfNone     = "none"     // 无法判定 → 留空（绝不猜，转智能体/人工队列）
)

const (
	subjectActorMachine = "openlaw-subject" // 机器署名（非人工，不得冒充复核人）
	// subjectDocFormPrefix 审计行 rule 前缀：文档形态写入与部门法写入同表异轨，
	// 按前缀分流撤销（title-suffix→清 doc_form，其余→清 subject_*）。
	subjectDocFormPrefix = "title-suffix"
)

// SubjectMarker 部门法标引执行器（库层）。
type SubjectMarker struct {
	DB *store.DB
}

// SubjectCandidate 一条法规的部门法与文档形态建议（报告一行）。
type SubjectCandidate struct {
	LawID        string
	Name         string
	Jurisdiction string
	EffectLevel  string
	SubjectCode  string // 建议部门法码；空 = 无建议
	SubjectRule  string // 依据与规则号，如 title-keyword(S2)
	DocForm      string // 建议文档形态码；空 = 判不出
	DocFormRule  string // 依据与规则号，如 title-suffix(F3)
	Confidence   string // SubjectConf* 之一
	Note         string // 白话说明（报告用）
}

// SubjectCounts 试采统计。分类计数，不给单一「覆盖率」了事。
type SubjectCounts struct {
	Total            int
	AlreadyMarked    int // 已有部门法标注
	Rejected         int // 人工结论留空（驳回/清除），机器不再写
	High             int // 规则命中 → 可写
	Undecided        int // 部门法无法判定（待办积压，非终态）
	DocFormSuggested int // 文档形态有建议（含补判）
	DocFormUndecided int // 文档形态判不出（待办）
}

// Scan 只读扫描全库，给出每条法规的部门法/文档形态建议与分类计数。不改库。
func (m *SubjectMarker) Scan() ([]SubjectCandidate, SubjectCounts, error) {
	rows, err := m.DB.Query(`SELECT law_id, name, COALESCE(jurisdiction,''), COALESCE(effect_level,''),
		COALESCE(subject_code,''), COALESCE(subject_reviewer,''), COALESCE(doc_form,'')
		FROM law ORDER BY law_id`)
	if err != nil {
		return nil, SubjectCounts{}, fmt.Errorf("读取法规清单失败: %w", err)
	}
	defer rows.Close()
	var out []SubjectCandidate
	var cnt SubjectCounts
	for rows.Next() {
		var lawID, name, jur, level, existing, reviewer, docForm string
		if err := rows.Scan(&lawID, &name, &jur, &level, &existing, &reviewer, &docForm); err != nil {
			return nil, SubjectCounts{}, err
		}
		c := SubjectCandidate{LawID: lawID, Name: name, EffectLevel: level}
		if jur == "" {
			jur = DeriveJurisdiction(level) // 法域未标注时按效力等级保守推导
		}
		c.Jurisdiction = jur
		cnt.Total++
		switch classifySubject(&c, existing, reviewer, docForm) {
		case "marked":
			cnt.AlreadyMarked++
		case "rejected":
			cnt.Rejected++
		case "high":
			cnt.High++
		default:
			cnt.Undecided++
		}
		suggestDocForm(&c, docForm)
		if c.DocForm != "" {
			cnt.DocFormSuggested++
		} else {
			cnt.DocFormUndecided++
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, SubjectCounts{}, err
	}
	return out, cnt, nil
}

// classifySubject 单条法规的部门法判定。Scan（全量）与 SuggestOne（单条）共用这一份
// 实现，保证面板与批量扫描口径一致。返回桶名：marked / rejected / high / none。
func classifySubject(c *SubjectCandidate, existing, reviewer, docForm string) string {
	switch {
	case existing != "":
		c.Confidence, c.SubjectCode = SubjectConfMarked, existing
		c.Note = "已标注"
		if reviewer != "" {
			c.Note = "已标注（人工已复核：" + reviewer + "）"
		}
		return "marked"
	case reviewer != "":
		c.Confidence = SubjectConfRejected
		c.Note = "人工结论留空（驳回/清除），机器不再写"
		return "rejected"
	}
	code, rule := parse.InferSubject(c.Name, c.Jurisdiction)
	if code != "" {
		c.Confidence, c.SubjectCode = SubjectConfHigh, code
		c.SubjectRule = fmt.Sprintf("title-keyword(S%d)", rule)
		c.Note = "规则命中：" + c.SubjectRule
		return "high"
	}
	c.Confidence = SubjectConfNone
	c.Note = "名称规则无法判定，转智能体/人工队列"
	return "none"
}

// suggestDocForm 文档形态判定（全自动，拍板项 2）：已标注列原样带出不覆盖；判不出留空。
func suggestDocForm(c *SubjectCandidate, docForm string) {
	if docForm != "" {
		c.DocForm = docForm // 已有值原样带出（不重写）
		return
	}
	code, rule := parse.InferDocForm(c.Name, c.Jurisdiction)
	if code != "" {
		c.DocForm = code
		c.DocFormRule = fmt.Sprintf("%s(F%d)", subjectDocFormPrefix, rule)
	}
}

// Apply 写入机器建议（调用方负责先给用户看过报告）。返回 batch_id 与实际写入条数。
// 部门法：只写高置信且 subject_code 为空、未经人工落判的行；
// 文档形态：只补 doc_form 为空的行（全自动，无人工队列）。
func (m *SubjectMarker) Apply(cands []SubjectCandidate) (string, int, error) {
	batchID := fmt.Sprintf("subject-%s", time.Now().UTC().Format("20060102T150405Z"))
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := m.DB.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	n := 0
	for _, c := range cands {
		if c.SubjectCode != "" && c.SubjectRule != "" && c.Confidence == SubjectConfHigh {
			if err := checkDictSubject(tx, c.SubjectCode); err != nil {
				return "", 0, fmt.Errorf("%s: %w", c.LawID, err)
			}
			res, err := tx.Exec(`UPDATE law SET subject_code=?, subject_source='auto', subject_rule=?,
				subject_reviewer=NULL, subject_reviewed_at=NULL
				WHERE law_id=? AND (subject_code IS NULL OR subject_code='') AND subject_reviewer IS NULL`,
				c.SubjectCode, c.SubjectRule, c.LawID)
			if err != nil {
				return "", 0, fmt.Errorf("写入部门法标引失败（%s）: %w", c.LawID, err)
			}
			if aff, _ := res.RowsAffected(); aff == 1 {
				n++
				if _, err := tx.Exec(`INSERT INTO subject_log(law_id,action,to_code,source,rule,actor,batch_id,created_at)
					VALUES(?,'suggest',?,'auto',?,?,?,?)`,
					c.LawID, c.SubjectCode, c.SubjectRule, subjectActorMachine, batchID, now); err != nil {
					return "", 0, fmt.Errorf("写部门法标引审计失败（%s）: %w", c.LawID, err)
				}
			}
		}
		if c.DocForm != "" && c.DocFormRule != "" {
			if err := checkDictDocForm(tx, c.DocForm); err != nil {
				return "", 0, fmt.Errorf("%s: %w", c.LawID, err)
			}
			res, err := tx.Exec(`UPDATE law SET doc_form=? WHERE law_id=?
				AND (doc_form IS NULL OR doc_form='')`, c.DocForm, c.LawID)
			if err != nil {
				return "", 0, fmt.Errorf("写入文档形态失败（%s）: %w", c.LawID, err)
			}
			if aff, _ := res.RowsAffected(); aff == 1 {
				n++
				if _, err := tx.Exec(`INSERT INTO subject_log(law_id,action,to_code,source,rule,actor,batch_id,created_at)
					VALUES(?,'suggest',?,'auto',?,?,?,?)`,
					c.LawID, c.DocForm, c.DocFormRule, subjectActorMachine, batchID, now); err != nil {
					return "", 0, fmt.Errorf("写文档形态审计失败（%s）: %w", c.LawID, err)
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return batchID, n, nil
}

// Undo 撤销某批次的机器写入。apply=false 只计数（干跑），与批量流转「先干跑再执行」一致。
// 只撤销仍是该批原样的行：部门法要求 subject_reviewer 为空且码仍等于该批写入值；
// 文档形态按 rule 前缀 title-suffix 分流、码仍等于该批写入值——人工落判的绝不清除。
func (m *SubjectMarker) Undo(batchID string, apply bool) (int, error) {
	rows, err := m.DB.Query(`SELECT s.law_id, s.to_code,
		CASE WHEN s.rule LIKE 'title-suffix%' OR s.rule LIKE '%doc-form%' THEN 1 ELSE 0 END is_df,
		CASE WHEN s.rule LIKE 'title-suffix%' OR s.rule LIKE '%doc-form%'
			THEN COALESCE(l.doc_form,'') ELSE COALESCE(l.subject_code,'') END cur,
		COALESCE(l.subject_reviewer,'')
		FROM subject_log s JOIN law l ON l.law_id = s.law_id
		WHERE s.batch_id = ? AND s.action = 'suggest'`, batchID)
	if err != nil {
		return 0, fmt.Errorf("查询批次失败: %w", err)
	}
	type target struct {
		lawID, code string
		isDocForm   bool
	}
	var targets []target
	for rows.Next() {
		var lawID, code, cur, reviewer string
		var df int
		if err := rows.Scan(&lawID, &code, &df, &cur, &reviewer); err != nil {
			rows.Close()
			return 0, err
		}
		if cur != code {
			continue // 已被后续人工或其他批次改写 → 不动
		}
		if df == 0 && reviewer != "" {
			continue // 人工落判过的行绝不回滚
		}
		targets = append(targets, target{lawID: lawID, code: code, isDocForm: df == 1})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if !apply {
		return len(targets), nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := m.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n := 0
	for _, x := range targets {
		var res sql.Result
		if x.isDocForm {
			res, err = tx.Exec(`UPDATE law SET doc_form=NULL WHERE law_id=? AND doc_form=?`, x.lawID, x.code)
		} else {
			res, err = tx.Exec(`UPDATE law SET subject_code=NULL, subject_source=NULL, subject_rule=NULL,
				subject_reviewer=NULL, subject_reviewed_at=NULL
				WHERE law_id=? AND subject_reviewer IS NULL AND subject_code=?`, x.lawID, x.code)
		}
		if err != nil {
			return 0, fmt.Errorf("撤销失败（%s）: %w", x.lawID, err)
		}
		if aff, _ := res.RowsAffected(); aff == 1 {
			n++
			if _, err := tx.Exec(`INSERT INTO subject_log(law_id,action,from_code,source,actor,batch_id,reason,created_at)
				VALUES(?,'undo',?,'auto',?,?,?,?)`,
				x.lawID, x.code, subjectActorMachine, batchID, "撤销批次 "+batchID, now); err != nil {
				return 0, fmt.Errorf("撤销审计失败（%s）: %w", x.lawID, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// SuggestOne 单条法规的建议（面板/例外队列用，与 Scan 同一份 classify 实现）。
func (m *SubjectMarker) SuggestOne(lawID string) (SubjectCandidate, error) {
	var c SubjectCandidate
	var existing, reviewer, docForm string
	err := m.DB.QueryRow(`SELECT name, COALESCE(jurisdiction,''), COALESCE(effect_level,''),
		COALESCE(subject_code,''), COALESCE(subject_reviewer,''), COALESCE(doc_form,'')
		FROM law WHERE law_id=?`, lawID).Scan(&c.Name, &c.Jurisdiction, &c.EffectLevel,
		&existing, &reviewer, &docForm)
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("法规 %s 不存在", lawID)
	}
	if err != nil {
		return c, err
	}
	c.LawID = lawID
	if c.Jurisdiction == "" {
		c.Jurisdiction = DeriveJurisdiction(c.EffectLevel)
	}
	classifySubject(&c, existing, reviewer, docForm)
	suggestDocForm(&c, docForm)
	return c, nil
}

// checkDictSubject/checkDictDocForm 写入前词表命中校验：码不在词表 = 拒写（整批中止）。
// 出现缺码说明规则与词表脱节，属实现缺陷，必须显式失败而非静默留空（诚实纪律）。
func checkDictSubject(tx *sql.Tx, code string) error {
	var one int
	err := tx.QueryRow(`SELECT 1 FROM dict_subject WHERE code=?`, code).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("部门法码 %s 不在词表（规则与词表脱节，请核对 parse/subjectinfer 与 dict_subject）", code)
	}
	return err
}

func checkDictDocForm(tx *sql.Tx, code string) error {
	var one int
	err := tx.QueryRow(`SELECT 1 FROM dict_doc_form WHERE code=?`, code).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("文档形态码 %s 不在词表（规则与词表脱节，请核对 parse/subjectinfer 与 dict_doc_form）", code)
	}
	return err
}

// WriteAgentSuggestion 智能体 classify 结论的部门法/文档形态落库（docs/design-taxonomy.md §5）。
// 与 confirm/escalate 裁决无关的独立建议通道：达门槛（subject_confidence ≥ minConf，
// 与自动确认同一门槛）才写，且只写空行——人工落判（subject_reviewer 非空）与已有
// 标注一律不顶回；署名留空 = 机器建议 · 待复核，永不代签人工。
// 码不在词表 = 不写入并在 note 中如实说明（fail-closed，不中断整轮审核）。
// 审计 actor 署名 agent:<model>，batch_id agent-<model>，可经 openlaw-subject -undo 整批撤销。
func WriteAgentSuggestion(db *store.DB, lawID, subjectCode string, subjectConf float64,
	docForm string, minConf float64, model string) (subjectWritten, docFormWritten bool, note string, err error) {
	var curSubject, curDocForm, reviewer string
	qerr := db.QueryRow(`SELECT COALESCE(subject_code,''), COALESCE(doc_form,''),
		COALESCE(subject_reviewer,'') FROM law WHERE law_id=?`, lawID).
		Scan(&curSubject, &curDocForm, &reviewer)
	if errors.Is(qerr, sql.ErrNoRows) {
		return false, false, "", nil // 条目未关联法规：无落点，结论已在 agent_review 留痕
	}
	if qerr != nil {
		return false, false, "", qerr
	}
	var notes []string
	tx, err := db.Begin()
	if err != nil {
		return false, false, "", err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	batchID, actor := "agent-"+model, "agent:"+model
	logAudit := func(toCode, rule string) error {
		_, err := tx.Exec(`INSERT INTO subject_log(law_id,action,to_code,source,rule,actor,batch_id,created_at)
			VALUES(?,'suggest',?,'auto',?,?,?,?)`, lawID, toCode, rule, actor, batchID, now)
		return err
	}
	if subjectCode != "" {
		switch {
		case subjectConf < minConf:
			notes = append(notes, fmt.Sprintf("部门法建议 %s 置信 %.2f 未达门槛，未写入",
				subjectCode, subjectConf))
		case reviewer != "":
			notes = append(notes, "部门法已有人工结论，机器不再写")
		case curSubject != "":
			notes = append(notes, "部门法已有标注，不覆盖")
		default:
			var one int
			derr := tx.QueryRow(`SELECT 1 FROM dict_subject WHERE code=?`, subjectCode).Scan(&one)
			if errors.Is(derr, sql.ErrNoRows) {
				notes = append(notes, "部门法码 "+subjectCode+" 不在词表，未写入（需核对词表与智能体口径）")
			} else if derr != nil {
				return false, false, "", derr
			} else if _, err := tx.Exec(`UPDATE law SET subject_code=?, subject_source='auto',
				subject_rule='agent-classify', subject_reviewer=NULL, subject_reviewed_at=NULL
				WHERE law_id=? AND (subject_code IS NULL OR subject_code='') AND subject_reviewer IS NULL`,
				subjectCode, lawID); err != nil {
				return false, false, "", fmt.Errorf("写入部门法标引失败（%s）: %w", lawID, err)
			} else {
				subjectWritten = true
				if err := logAudit(subjectCode, "agent-classify"); err != nil {
					return false, false, "", err
				}
				notes = append(notes, "部门法 "+subjectCode+" 已写入（机器建议 · 待复核）")
			}
		}
	}
	if docForm != "" {
		if curDocForm != "" {
			notes = append(notes, "文档形态已有值，不覆盖")
		} else {
			var one int
			derr := tx.QueryRow(`SELECT 1 FROM dict_doc_form WHERE code=?`, docForm).Scan(&one)
			if errors.Is(derr, sql.ErrNoRows) {
				notes = append(notes, "文档形态码 "+docForm+" 不在词表，未写入（需核对词表与智能体口径）")
			} else if derr != nil {
				return false, false, "", derr
			} else if _, err := tx.Exec(`UPDATE law SET doc_form=? WHERE law_id=?
				AND (doc_form IS NULL OR doc_form='')`, docForm, lawID); err != nil {
				return false, false, "", fmt.Errorf("写入文档形态失败（%s）: %w", lawID, err)
			} else {
				docFormWritten = true
				if err := logAudit(docForm, agentDocFormRule); err != nil {
					return false, false, "", err
				}
				notes = append(notes, "文档形态 "+docForm+" 已写入")
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return false, false, "", err
	}
	for _, s := range notes {
		if note == "" {
			note = s
		} else {
			note += "；" + s
		}
	}
	return subjectWritten, docFormWritten, note, nil
}

// agentDocFormRule 智能体文档形态审计的 rule 标记：撤销按 doc-form 关键字分流
// （与 title-suffix 规则写入同轨——清 doc_form 列；部门法写入另一轨——清 subject_*）。
const agentDocFormRule = "agent-classify(doc-form)"
