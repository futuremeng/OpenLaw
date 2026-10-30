// browse.go 面向「开源法律法规数据库」UI 的只读浏览层：
//   - BrowseLaws：按效力等级/名称下钻（层级浏览第一层，代表版本=最新施行版本）；
//   - BrowseArticles：某法规现行版本的条款列表；
//   - LawVersions：修订沿革时间线（law_version 左闭右开时效）；
//   - SourceStatus：各官方渠道最近抓取情况（数据源状态卡）。
//
// 全部只读，不动任何写路径；列名与 store/schema.sql 对齐。
package main

import (
	"errors"
	"strings"

	"github.com/futuremeng/OpenLaw/geo"
)

// LawRow 层级浏览一行（一部法规，代表其最新施行版本）。
type LawRow struct {
	LawID        string `json:"law_id"`
	Name         string `json:"name"`
	Issuer       string `json:"issuer"`
	Level        string `json:"level"`
	Region       string `json:"region"`        // 显示用地域：已标注取库中区划码，未标注取名称推定；空 = 判不出
	RegionCode   string `json:"region_code"`   // 库中区划码；空 = 地域未标注
	RegionSource string `json:"region_source"` // auto/manual/pack=已标注来源；none=未标注（显示的是名称推定值）；unknown=码不在内置区划表
	VersionNo    string `json:"version_no"`
	Effective    string `json:"effective_date"`
	Expire       string `json:"expire_date"` // 空 = 仍现行有效
	Publish      string `json:"publish_date"`
	Status       string `json:"confirm_status"`
}

// LawFilter 多维筛选条件（空 = 该维度不限）。公众模式常用其中一两个维度
// （类别、地区），专业模式靠维度组合定位条目——两者共用同一套条件与同一条 SQL 口径。
type LawFilter struct {
	Level   string `json:"level"`   // 效力等级
	Confirm string `json:"confirm"` // confirmed/pending/rejected
	Source  string `json:"source"`  // document.source_type 精确值（来自下拉，不是通配符）
	Year    string `json:"year"`    // 公布年代桶（"2000" = 2000 年代）；"none" = 公布日期缺失
	Region  string `json:"region"`  // 区划码（省级含下辖地级）
	Kw      string `json:"kw"`      // 名称包含
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
}

// browseFrom 法规行与其代表版本（施行日最晚的一条）及该版本的留证文档。
// 复核状态/来源/公布日期都取自代表版本的文档——与列表显示的是同一份，计数口径不得另起炉灶。
const browseFrom = `FROM law l
	LEFT JOIN law_version v ON v.law_id=l.law_id
		AND NOT EXISTS (SELECT 1 FROM law_version v2 WHERE v2.law_id=l.law_id AND v2.effective_date > v.effective_date)
	LEFT JOIN document d ON d.doc_id=v.doc_id
	`

// browseOrder 默认取列顺序：按效力等级分组、名称字典序，**未标注效力等级的条目排在最后**。
// SQLite 中 NULL 排在最前，直接 ORDER BY effect_level 会让「什么都没标注」的条目占满第一页
// （实测本机语料前 300 条全部未标注）——那是显示缺陷而不是诚实：缺失仍在行内如实标出，
// 只是不占用第一眼。该规则同时写进筛选栏脚注，不得悄悄发生。
const browseOrder = `ORDER BY (l.effect_level IS NULL), l.effect_level, l.name`

// yearBucketExpr 公布年代桶（十年一档）；公布日期缺失单独成桶，不静默并入任何年代。
const yearBucketExpr = `CASE WHEN COALESCE(d.publish_date,'')='' THEN 'none'
		ELSE CAST((CAST(substr(d.publish_date,1,4) AS INTEGER)/10)*10 AS TEXT) END`

// browseWhere 构造筛选子句与参数；skip 指名某维度时不计该维度条件——分面计数的
// 标准口径：某维度各可选值的计数按「其他维度已生效、本维度不限」给出，
// 这样用户选中一项后，其余维度的计数仍能用于继续收窄（界面实时联动的依据）。
func browseWhere(f LawFilter, skip string) (string, []any) {
	where := `WHERE 1=1`
	var args []any
	if skip != "level" {
		where += ` AND (?='' OR COALESCE(l.effect_level,'')=?)`
		args = append(args, f.Level, f.Level)
	}
	if skip != "confirm" {
		where += ` AND (?='' OR COALESCE(d.confirm_status,'')=?)`
		args = append(args, f.Confirm, f.Confirm)
	}
	if skip != "source" {
		where += ` AND (?='' OR COALESCE(d.source_type,'')=?)`
		args = append(args, f.Source, f.Source)
	}
	if skip != "year" {
		where += ` AND (?='' OR ` + yearBucketExpr + `=?)`
		args = append(args, f.Year, f.Year)
	}
	kw := strings.TrimSpace(f.Kw)
	where += ` AND (?='' OR l.name LIKE '%'||?||'%')`
	args = append(args, kw, kw)
	if clause, rargs := regionClause(newRegionFilter(f.Region)); clause != "" {
		where += clause
		args = append(args, rargs...)
	}
	return where, args
}

// BrowseLawList 按多维条件列出法规（口径与 BrowseFacets 完全一致）。
// 代表版本取施行日最晚的一条；地域过滤是可复现的码前缀/名称前缀匹配，
// 不是相关性重排（FTS5 纪律同源）。
func (a *App) BrowseLawList(f LawFilter) ([]LawRow, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	where, args := browseWhere(f, "")
	args = append(args, f.Limit, f.Offset)
	rows, err := a.db.Query(`SELECT l.law_id, l.name, COALESCE(l.issuer,''),
		COALESCE(l.effect_level,'效力等级未标注'), COALESCE(l.region_code,''), COALESCE(l.region_source,''),
		COALESCE(v.version_no,''),
		COALESCE(v.effective_date,''), COALESCE(v.expire_date,''),
		COALESCE(d.publish_date,''), COALESCE(d.confirm_status,'')
		`+browseFrom+where+`
		`+browseOrder+` LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LawRow
	for rows.Next() {
		var r LawRow
		if err := rows.Scan(&r.LawID, &r.Name, &r.Issuer, &r.Level, &r.RegionCode, &r.RegionSource,
			&r.VersionNo, &r.Effective, &r.Expire, &r.Publish, &r.Status); err != nil {
			return out, err
		}
		r.Region, r.RegionSource = displayRegion(r.RegionCode, r.RegionSource, r.Name)
		out = append(out, r)
	}
	return out, rows.Err()
}

// BrowseLaws 旧签名（保留给既有调用与测试）：委托给 BrowseLawList，
// 不另起一套 SQL——过滤与计数必须同源。
func (a *App) BrowseLaws(level, keyword, region string, limit, offset int) ([]LawRow, error) {
	return a.BrowseLawList(LawFilter{Level: level, Kw: keyword, Region: region, Limit: limit, Offset: offset})
}

// FacetOption 某维度上一个可选值及其计数。
type FacetOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// FacetView 各维度的可选值与计数 + 当前条件下的总数。
// Unlabeled 如实给出「库中未标区划码」的数量：它决定按区划码过滤能覆盖多少，
// 未标注那部分实际是按标题前缀匹配的，不得隐藏。
type FacetView struct {
	Total     int           `json:"total"`
	Unlabeled int           `json:"unlabeled"`
	Levels    []FacetOption `json:"levels"`
	Confirms  []FacetOption `json:"confirms"`
	Sources   []FacetOption `json:"sources"`
	Years     []FacetOption `json:"years"`
	Regions   []FacetOption `json:"regions"`
}

// facetGroup 按表达式分组计数（skip 维度不计自身条件）。
func (a *App) facetGroup(f LawFilter, skip, expr string) ([]FacetOption, error) {
	where, args := browseWhere(f, skip)
	rows, err := a.db.Query(`SELECT `+expr+` AS k, COUNT(*) `+browseFrom+where+
		` GROUP BY k ORDER BY COUNT(*) DESC, k`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FacetOption
	for rows.Next() {
		var o FacetOption
		if err := rows.Scan(&o.Value, &o.Count); err != nil {
			return out, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// facetLabel 分组值 → 用户可读标签（缺失一律如实标「未记录/未标注」，不并入他桶）。
func facetLabel(dim, v string) string {
	switch dim {
	case "level":
		if v == "" {
			return "效力等级未标注"
		}
		return v
	case "confirm":
		switch v {
		case "confirmed":
			return "已复核确认"
		case "pending":
			return "待复核"
		case "rejected":
			return "已驳回"
		default:
			return "复核状态未记录"
		}
	case "source":
		if v == "" {
			return "来源未记录"
		}
		return v
	case "year":
		if v == "none" || v == "" {
			return "公布日期缺失"
		}
		return v + " 年代"
	case "region":
		if v == "" {
			return "地域未标注"
		}
		if r, ok := geo.ByCode(v + "0000"); ok {
			return r.Name
		}
		return "区划码 " + v + "（不在内置区划表）"
	}
	return v
}

// BrowseFacets 当前筛选下各维度的可选值与计数——界面「筛选 ↔ 数据」实时联动的数据来源。
// 地区维度只按已标注的区划码分组（未标注条目按标题前缀匹配，无法归到某个省下），
// 未标注数量由 Unlabeled 单独给出，界面必须如实说明。
func (a *App) BrowseFacets(f LawFilter) (*FacetView, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	v := &FacetView{}
	where, args := browseWhere(f, "")
	if err := a.db.QueryRow(`SELECT COUNT(*),
		SUM(CASE WHEN l.region_code IS NULL OR l.region_code='' THEN 1 ELSE 0 END)
		`+browseFrom+where, args...).Scan(&v.Total, &v.Unlabeled); err != nil {
		return nil, err
	}
	for _, g := range []struct {
		dim, expr string
		dst       *[]FacetOption
	}{
		{"level", `COALESCE(l.effect_level,'')`, &v.Levels},
		{"confirm", `COALESCE(d.confirm_status,'')`, &v.Confirms},
		{"source", `COALESCE(d.source_type,'')`, &v.Sources},
		{"year", yearBucketExpr, &v.Years},
		{"region", `CASE WHEN COALESCE(l.region_code,'')='' THEN '' ELSE substr(l.region_code,1,2) END`, &v.Regions},
	} {
		opts, err := a.facetGroup(f, g.dim, g.expr)
		if err != nil {
			return nil, err
		}
		for _, o := range opts {
			o.Label = facetLabel(g.dim, o.Value)
			*g.dst = append(*g.dst, o)
		}
	}
	return v, nil
}

// BrowseStats 同一筛选条件下的法规总数与其中「库中未标区划码」的数量（旧签名，
// 委托给 BrowseFacets，保证计数与过滤同源）。
func (a *App) BrowseStats(level, keyword, region string) (int, int, error) {
	v, err := a.BrowseFacets(LawFilter{Level: level, Kw: keyword, Region: region})
	if err != nil {
		return 0, 0, err
	}
	return v.Total, v.Unlabeled, nil
}

// ArticleRow 条款列表一行（摘要为正文前 120 字）。
type ArticleRow struct {
	ArticleID string `json:"article_id"`
	ArticleNo string `json:"article_no"`
	Snippet   string `json:"snippet"`
	DocID     string `json:"doc_id"`
}

// BrowseArticles 某法规最新施行版本的条款列表（按入库顺序）。
func (a *App) BrowseArticles(lawID string) ([]ArticleRow, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	rows, err := a.db.Query(`SELECT a.article_id, a.article_no, substr(a.content,1,120), a.doc_id
		FROM article a JOIN law_version v ON v.version_id=a.version_id
		WHERE v.law_id=? AND v.version_id=(SELECT version_id FROM law_version WHERE law_id=? ORDER BY effective_date DESC LIMIT 1)
		ORDER BY a.article_id`, lawID, lawID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArticleRow
	for rows.Next() {
		var r ArticleRow
		if err := rows.Scan(&r.ArticleID, &r.ArticleNo, &r.Snippet, &r.DocID); err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// VersionRow 修订沿革一行（时间线数据）。
type VersionRow struct {
	VersionID string `json:"version_id"`
	VersionNo string `json:"version_no"`
	Effective string `json:"effective_date"`
	Expire    string `json:"expire_date"` // 空 = 现行有效
	Publish   string `json:"publish_date"`
	DocID     string `json:"doc_id"`
	Status    string `json:"confirm_status"`
}

// LawVersions 某法规的全部版本（按施行日升序，前端渲染沿革时间线）。
func (a *App) LawVersions(lawID string) ([]VersionRow, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	rows, err := a.db.Query(`SELECT v.version_id, v.version_no, v.effective_date, COALESCE(v.expire_date,''),
		COALESCE(d.publish_date,''), v.doc_id, d.confirm_status
		FROM law_version v JOIN document d ON d.doc_id=v.doc_id
		WHERE v.law_id=? ORDER BY v.effective_date`, lawID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VersionRow
	for rows.Next() {
		var r VersionRow
		if err := rows.Scan(&r.VersionID, &r.VersionNo, &r.Effective, &r.Expire, &r.Publish, &r.DocID, &r.Status); err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
