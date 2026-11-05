// subjectreview.go 「部门法标引」复核面板绑定（docs/design-taxonomy.md §5 人工落判）。
//
// 纪律：
//   - 一律经 ingest.SubjectMarker：桌面端不是第二条写路径，CLI 与桌面共用同一 runner；
//   - 确认/驳回/清除必须由人工署名（复核人）+ 结论依据，库层同样强制（双保险）；
//   - 批量先干跑计数（SubjectBulkPreview），执行时把预览数带回来（expected），
//     数变了就中止——不拿旧计数当授权；
//   - 队列排序：机器建议 · 待复核 → 未标注 → 已复核，让复核人先看到机器已经拿出的东西；
//   - 未标注就是「部门法未标注」，机器建议就是「机器建议 · 待复核」，不得渲染成已确认；
//   - 文档形态全自动（拍板项 2）：只读展示，不提供人工落判入口。
package main

import (
	"errors"
	"strings"

	"github.com/futuremeng/OpenLaw/ingest"
)

// SubjectReviewRow 队列一行。State：suggest（机器建议 · 待复核）/
// unmarked（部门法未标注）/ reviewed（已人工复核）。
type SubjectReviewRow struct {
	LawID        string `json:"law_id"`
	Name         string `json:"name"`
	Issuer       string `json:"issuer"`
	EffectLevel  string `json:"effect_level"`
	Jurisdiction string `json:"jurisdiction"`
	Code         string `json:"subject_code"`
	SubjectName  string `json:"subject_name"`
	Source       string `json:"subject_source"`
	Rule         string `json:"subject_rule"`
	Reviewer     string `json:"subject_reviewer"`
	DocForm      string `json:"doc_form"`
	DocFormName  string `json:"doc_form_name"`
	State        string `json:"state"`
}

// SubjectSuggestView 单条的机器判定视图（含「为什么没写」的如实说明）。
type SubjectSuggestView struct {
	Code        string `json:"subject_code"`
	SubjectName string `json:"subject_name"`
	Confidence  string `json:"confidence"`
	Rule        string `json:"rule"`
	Note        string `json:"note"`
	DocForm     string `json:"doc_form"`
	DocFormName string `json:"doc_form_name"`
}

// SubjectReviewDetail 预览面板所需：当前标注 + 机器判定 + 复核记录。
type SubjectReviewDetail struct {
	Row     SubjectReviewRow   `json:"row"`
	Suggest SubjectSuggestView `json:"suggest"`
	Log     []string           `json:"log"`
}

// SubjectOption 部门法词表一项（下拉按法域分组，顺序即 dict_subject.ordinal）。
type SubjectOption struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	Jurisdiction string `json:"jurisdiction"`
}

func (a *App) subjectMarker() (*ingest.SubjectMarker, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	return &ingest.SubjectMarker{DB: a.db}, nil
}

// subjectState 与列表/预览共用一套状态口径，避免两处自造说法。
func subjectState(code, reviewer string) string {
	switch {
	case reviewer != "":
		return "reviewed"
	case code != "":
		return "suggest"
	}
	return "unmarked"
}

// dictNames 词表码→展示名（dict_* 只有数十行，每次调用整表读取代价可忽略，
// 且词表回填后无需改代码即自动生效）。
func (a *App) dictNames(table string) map[string]string {
	out := map[string]string{}
	if a.db == nil {
		return out
	}
	rows, err := a.db.Query(`SELECT code, name FROM ` + table)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c, n string
		if err := rows.Scan(&c, &n); err == nil {
			out[c] = n
		}
	}
	return out
}

// SubjectOptions 部门法词表（供前端下拉；空表 = 语料版本不含词表，
// 前端如实显示「词表缺失」，不得渲染空下拉当已覆盖）。
func (a *App) SubjectOptions() ([]SubjectOption, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	rows, err := a.db.Query(`SELECT code, name, jurisdiction FROM dict_subject
		ORDER BY jurisdiction, ordinal, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubjectOption
	for rows.Next() {
		var o SubjectOption
		if err := rows.Scan(&o.Code, &o.Name, &o.Jurisdiction); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SubjectReviewStats 复核面统计（覆盖与待复核量，如实报数）。
func (a *App) SubjectReviewStats() (*ingest.SubjectReviewStats, error) {
	m, err := a.subjectMarker()
	if err != nil {
		return nil, err
	}
	st, err := m.ReviewStats()
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// SubjectReviewList 复核队列。state："" 全部 / suggest 机器建议 / unmarked 未标注 /
// reviewed 已复核。排序把机器建议排在最前。
func (a *App) SubjectReviewList(filter, state string, limit, offset int) ([]SubjectReviewRow, error) {
	m, err := a.subjectMarker()
	if err != nil {
		return nil, err
	}
	const marked = "subject_code IS NOT NULL AND subject_code<>''"
	conds, args := []string{"1=1"}, []any{}
	if f := strings.TrimSpace(filter); f != "" {
		conds = append(conds, "(law_id LIKE ? OR name LIKE ? OR COALESCE(issuer,'') LIKE ?)")
		args = append(args, "%"+f+"%", "%"+f+"%", "%"+f+"%")
	}
	switch strings.TrimSpace(state) {
	case "suggest":
		conds = append(conds, marked, ingest.SQLSubjectReviewerBlank)
	case "unmarked":
		conds = append(conds, "("+marked+")=0", ingest.SQLSubjectReviewerBlank)
	case "reviewed":
		conds = append(conds, ingest.SQLSubjectReviewerSet)
	}
	if limit <= 0 || limit > 300 {
		limit = 300
	}
	if offset < 0 {
		offset = 0
	}
	subjects := a.dictNames("dict_subject")
	docForms := a.dictNames("dict_doc_form")
	rows, err := m.DB.Query(`SELECT law_id, name, COALESCE(issuer,''), COALESCE(effect_level,''),
		COALESCE(jurisdiction,''), COALESCE(subject_code,''), COALESCE(subject_source,''),
		COALESCE(subject_rule,''), COALESCE(subject_reviewer,''), COALESCE(doc_form,'')
		FROM law WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY CASE
			WHEN `+marked+` AND `+ingest.SQLSubjectReviewerBlank+` THEN 0
			WHEN `+ingest.SQLSubjectReviewerSet+` THEN 2
			ELSE 1 END, law_id
		LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubjectReviewRow
	for rows.Next() {
		var r SubjectReviewRow
		if err := rows.Scan(&r.LawID, &r.Name, &r.Issuer, &r.EffectLevel, &r.Jurisdiction,
			&r.Code, &r.Source, &r.Rule, &r.Reviewer, &r.DocForm); err != nil {
			return nil, err
		}

		r.State = subjectState(r.Code, r.Reviewer)
		r.SubjectName = subjects[r.Code]
		r.DocFormName = docForms[r.DocForm]
		out = append(out, r)
	}
	return out, rows.Err()
}

// SubjectReviewDetail 单条预览：当前标注、机器判定（含未写入原因）、复核记录。
func (a *App) SubjectReviewDetail(lawID string) (*SubjectReviewDetail, error) {
	m, err := a.subjectMarker()
	if err != nil {
		return nil, err
	}
	c, err := m.SuggestOne(strings.TrimSpace(lawID))
	if err != nil {
		return nil, err
	}
	logs, err := m.ReviewLog(c.LawID)
	if err != nil {
		return nil, err
	}
	subjects := a.dictNames("dict_subject")
	docForms := a.dictNames("dict_doc_form")
	d := &SubjectReviewDetail{
		Row: SubjectReviewRow{LawID: c.LawID, Name: c.Name,
			EffectLevel: c.EffectLevel, Jurisdiction: c.Jurisdiction},
		Suggest: SubjectSuggestView{Code: c.SubjectCode, SubjectName: subjects[c.SubjectCode],
			Confidence: c.Confidence, Rule: c.SubjectRule, Note: c.Note,
			DocForm: c.DocForm, DocFormName: docForms[c.DocForm]},
		Log: logs,
	}
	// Row.Code 必须取库里的实际标注，不能用机器建议码——否则未标注行会被显示成已标注
	if err := m.DB.QueryRow(`SELECT COALESCE(issuer,''), COALESCE(subject_code,''), COALESCE(subject_source,''),
		COALESCE(subject_rule,''), COALESCE(subject_reviewer,''), COALESCE(doc_form,'')
		FROM law WHERE law_id=?`, c.LawID).
		Scan(&d.Row.Issuer, &d.Row.Code, &d.Row.Source, &d.Row.Rule, &d.Row.Reviewer, &d.Row.DocForm); err != nil {
		return nil, err
	}
	d.Row.State = subjectState(d.Row.Code, d.Row.Reviewer)
	d.Row.SubjectName = subjects[d.Row.Code]
	d.Row.DocFormName = docForms[d.Row.DocForm]
	return d, nil
}

// JudgeSubject 逐条/小批落判。verdict：confirm（确认，code 必填；与现值不同即改选部门法）/
// reject（驳回，留空但署名）/ clear（清除标注）。复核人与依据必填。
// 文档形态不参与人工落判（全自动机器判定，拍板项 2）。
func (a *App) JudgeSubject(lawIDs []string, code, verdict, reviewer, reason string) (*ingest.SubjectReviewSummary, error) {
	m, err := a.subjectMarker()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(reason) == "" {
		return nil, errors.New("部门法落判须填写复核人与结论依据")
	}
	if len(lawIDs) == 0 {
		return nil, errors.New("未选择条目")
	}
	return m.Review(lawIDs, strings.TrimSpace(verdict), code, reviewer, reason)
}

// SubjectBulkPreview 批量落判干跑：只计数不写库。
func (a *App) SubjectBulkPreview(jurisdiction, verdict string) (int, error) {
	m, err := a.subjectMarker()
	if err != nil {
		return 0, err
	}
	return m.BulkReviewMatch(ingest.SubjectFilter{Jurisdiction: jurisdiction, OnlySuggest: true}, verdict)
}

// SubjectBulkApply 批量落判执行：expected 必须是刚才预览的条数，不符即中止。
func (a *App) SubjectBulkApply(jurisdiction, verdict, reviewer, reason string, expected int) (*ingest.SubjectReviewSummary, error) {
	m, err := a.subjectMarker()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(reason) == "" {
		return nil, errors.New("批量落判须填写复核人与结论依据")
	}
	return m.BulkReview(ingest.SubjectFilter{Jurisdiction: jurisdiction, OnlySuggest: true}, verdict, reviewer, reason, expected)
}
