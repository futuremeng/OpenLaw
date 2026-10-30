// regionreview.go 「地域标引」复核面板绑定（F3 第 4 步，docs/design-region-write.md §7）。
//
// 纪律：
//   - 一律经 ingest.RegionMarker：桌面端不是第二条写路径，CLI 与桌面共用同一 runner；
//   - 确认/驳回/清除必须由人工署名（复核人）+ 结论依据，库层同样强制（双保险）；
//   - 批量先干跑计数（RegionBulkPreview），执行时把预览数带回来（expected），
//     数变了就中止——不拿旧计数当授权；
//   - 队列排序：机器建议 · 待复核 → 未标注 → 已复核，让复核人先看到机器已经拿出的东西；
//   - 未标注就是「地域未标注」，机器建议就是「机器建议 · 待复核」，不得渲染成已确认。
package main

import (
	"errors"
	"strings"

	"github.com/futuremeng/OpenLaw/geo"
	"github.com/futuremeng/OpenLaw/ingest"
)

// RegionReviewRow 队列一行。State：suggest（机器建议 · 待复核）/
// unmarked（地域未标注）/ reviewed（已人工复核）。
type RegionReviewRow struct {
	LawID       string `json:"law_id"`
	Name        string `json:"name"`
	Issuer      string `json:"issuer"`
	EffectLevel string `json:"effect_level"`
	Code        string `json:"region_code"`
	RegionPath  string `json:"region_path"`
	Source      string `json:"region_source"`
	Rule        string `json:"region_rule"`
	Reviewer    string `json:"region_reviewer"`
	State       string `json:"state"`
}

// RegionSuggestView 单条的机器判定视图（含「为什么没写」的如实说明）。
type RegionSuggestView struct {
	Code       string `json:"region_code"`
	RegionPath string `json:"region_path"`
	Confidence string `json:"confidence"`
	Rule       int    `json:"rule"`
	Note       string `json:"note"`
}

// RegionReviewDetail 预览面板所需：当前标注 + 机器判定 + 复核记录。
type RegionReviewDetail struct {
	Row     RegionReviewRow   `json:"row"`
	Suggest RegionSuggestView `json:"suggest"`
	Log     []string          `json:"log"`
}

func (a *App) regionMarker() (*ingest.RegionMarker, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	return &ingest.RegionMarker{DB: a.db}, nil
}

// regionState 与列表/预览共用一套状态口径，避免两处自造说法。
func regionState(code, reviewer string) string {
	switch {
	case reviewer != "":
		return "reviewed"
	case code != "":
		return "suggest"
	}
	return "unmarked"
}

// RegionReviewStats 复核面统计（覆盖与待复核量，如实报数）。
func (a *App) RegionReviewStats() (*ingest.RegionReviewStats, error) {
	m, err := a.regionMarker()
	if err != nil {
		return nil, err
	}
	st, err := m.ReviewStats()
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// RegionReviewList 复核队列。state："" 全部 / suggest 机器建议 / unmarked 未标注 /
// reviewed 已复核。排序把机器建议排在最前。
func (a *App) RegionReviewList(filter, state string, limit, offset int) ([]RegionReviewRow, error) {
	m, err := a.regionMarker()
	if err != nil {
		return nil, err
	}
	const marked = "region_code IS NOT NULL AND region_code<>''"
	conds, args := []string{"1=1"}, []any{}
	if f := strings.TrimSpace(filter); f != "" {
		conds = append(conds, "(law_id LIKE ? OR name LIKE ? OR COALESCE(issuer,'') LIKE ?)")
		args = append(args, "%"+f+"%", "%"+f+"%", "%"+f+"%")
	}
	switch strings.TrimSpace(state) {
	case "suggest":
		conds = append(conds, marked, "region_reviewer IS NULL")
	case "unmarked":
		conds = append(conds, "("+marked+")=0", "region_reviewer IS NULL")
	case "reviewed":
		conds = append(conds, "region_reviewer IS NOT NULL")
	}
	if limit <= 0 || limit > 300 {
		limit = 300
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := m.DB.Query(`SELECT law_id, name, COALESCE(issuer,''), COALESCE(effect_level,''),
		COALESCE(region_code,''), COALESCE(region_source,''), COALESCE(region_rule,''),
		COALESCE(region_reviewer,'')
		FROM law WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY CASE
			WHEN `+marked+` AND region_reviewer IS NULL THEN 0
			WHEN region_reviewer IS NOT NULL THEN 2
			ELSE 1 END, law_id
		LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RegionReviewRow
	for rows.Next() {
		var r RegionReviewRow
		if err := rows.Scan(&r.LawID, &r.Name, &r.Issuer, &r.EffectLevel, &r.Code,
			&r.Source, &r.Rule, &r.Reviewer); err != nil {
			return nil, err
		}
		r.State = regionState(r.Code, r.Reviewer)
		r.RegionPath = geo.PathName(r.Code)
		out = append(out, r)
	}
	return out, rows.Err()
}

// RegionReviewDetail 单条预览：当前标注、机器判定（含未写入原因）、复核记录。
func (a *App) RegionReviewDetail(lawID string) (*RegionReviewDetail, error) {
	m, err := a.regionMarker()
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
	d := &RegionReviewDetail{
		Row: RegionReviewRow{LawID: c.LawID, Name: c.Name, Issuer: c.Issuer, EffectLevel: c.EffectLevel},
		Suggest: RegionSuggestView{Code: c.Code, RegionPath: c.RegionPath,
			Confidence: c.Confidence, Rule: c.Rule, Note: c.Note},
		Log: logs,
	}
	// Row.Code 必须取库里的实际标注，不能用机器建议码——否则未标注行会被显示成已标注
	if err := m.DB.QueryRow(`SELECT COALESCE(region_code,''), COALESCE(region_source,''),
		COALESCE(region_rule,''), COALESCE(region_reviewer,'') FROM law WHERE law_id=?`, c.LawID).
		Scan(&d.Row.Code, &d.Row.Source, &d.Row.Rule, &d.Row.Reviewer); err != nil {
		return nil, err
	}
	d.Row.State = regionState(d.Row.Code, d.Row.Reviewer)
	d.Row.RegionPath = geo.PathName(d.Row.Code)
	return d, nil
}

// JudgeRegion 逐条/小批落判。verdict：confirm（确认，code 必填；与现值不同即改选地区）/
// reject（驳回，留空但署名）/ clear（清除标注）。复核人与依据必填。
func (a *App) JudgeRegion(lawIDs []string, code, verdict, reviewer, reason string) (*ingest.RegionReviewSummary, error) {
	m, err := a.regionMarker()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(reason) == "" {
		return nil, errors.New("地域落判须填写复核人与结论依据")
	}
	if len(lawIDs) == 0 {
		return nil, errors.New("未选择条目")
	}
	return m.Review(lawIDs, strings.TrimSpace(verdict), code, reviewer, reason)
}

// RegionBulkPreview 批量落判干跑：只计数不写库。
func (a *App) RegionBulkPreview(province, verdict string) (int, error) {
	m, err := a.regionMarker()
	if err != nil {
		return 0, err
	}
	return m.BulkReviewMatch(ingest.RegionFilter{Province: province, OnlySuggest: true}, verdict)
}

// RegionBulkApply 批量落判执行：expected 必须是刚才预览的条数，不符即中止。
func (a *App) RegionBulkApply(province, verdict, reviewer, reason string, expected int) (*ingest.RegionReviewSummary, error) {
	m, err := a.regionMarker()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(reason) == "" {
		return nil, errors.New("批量落判须填写复核人与结论依据")
	}
	return m.BulkReview(ingest.RegionFilter{Province: province, OnlySuggest: true}, verdict, reviewer, reason, expected)
}
