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
)

// LawRow 层级浏览一行（一部法规，代表其最新施行版本）。
type LawRow struct {
	LawID     string `json:"law_id"`
	Name      string `json:"name"`
	Issuer    string `json:"issuer"`
	Level     string `json:"level"`
	Region    string `json:"region"` // 按名称推导的地域；空 = 地域未标注（UI 如实显示）
	VersionNo string `json:"version_no"`
	Effective string `json:"effective_date"`
	Expire    string `json:"expire_date"` // 空 = 仍现行有效
	Publish   string `json:"publish_date"`
	Status    string `json:"confirm_status"`
}

// BrowseLaws 法规列表：level 空 = 全部等级；keyword 空 = 不过滤名称；
// region 空 = 不限地区，否则按地区（选省级时含其下辖地级）名称前缀过滤。
// 代表版本取施行日最晚的一条（未失效优先由前端按 Expire 呈现）。
// 注意：地域过滤是**可复现的名称前缀过滤**，不是相关性重排（FTS5 纪律同源）。
func (a *App) BrowseLaws(level, keyword, region string, limit, offset int) ([]LawRow, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	kw := strings.TrimSpace(keyword)
	where := `WHERE (?='' OR l.effect_level=?) AND (?='' OR l.name LIKE '%'||?||'%')`
	args := []any{level, level, kw, kw}
	if clause, rargs := regionClause(regionPatterns(region)); clause != "" {
		where += clause
		args = append(args, rargs...)
	}
	args = append(args, limit, offset)
	rows, err := a.db.Query(`SELECT l.law_id, l.name, COALESCE(l.issuer,''),
		COALESCE(l.effect_level,'（未标注）'), COALESCE(v.version_no,''),
		COALESCE(v.effective_date,''), COALESCE(v.expire_date,''),
		COALESCE(d.publish_date,''), COALESCE(d.confirm_status,'')
		FROM law l
		LEFT JOIN law_version v ON v.law_id=l.law_id
			AND NOT EXISTS (SELECT 1 FROM law_version v2 WHERE v2.law_id=l.law_id AND v2.effective_date > v.effective_date)
		LEFT JOIN document d ON d.doc_id=v.doc_id
		`+where+`
		ORDER BY l.effect_level, l.name LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LawRow
	for rows.Next() {
		var r LawRow
		if err := rows.Scan(&r.LawID, &r.Name, &r.Issuer, &r.Level, &r.VersionNo,
			&r.Effective, &r.Expire, &r.Publish, &r.Status); err != nil {
			return out, err
		}
		r.Region = lawRegion(r.Name)
		out = append(out, r)
	}
	return out, rows.Err()
}

// BrowseStats 同一筛选条件下的法规总数与其中「地域未标注」的数量。
// 未标注数如实给出——它决定「按地区显示」能覆盖多少，不得隐藏。
func (a *App) BrowseStats(level, keyword, region string) (int, int, error) {
	if a.db == nil {
		return 0, 0, errors.New(a.openErr)
	}
	kw := strings.TrimSpace(keyword)
	where := `WHERE (?='' OR l.effect_level=?) AND (?='' OR l.name LIKE '%'||?||'%')`
	args := []any{level, level, kw, kw}
	if clause, rargs := regionClause(regionPatterns(region)); clause != "" {
		where += clause
		args = append(args, rargs...)
	}
	rows, err := a.db.Query(`SELECT l.name FROM law l `+where, args...)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var total, unlabeled int
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return total, unlabeled, err
		}
		total++
		if lawRegion(name) == "" {
			unlabeled++
		}
	}
	return total, unlabeled, rows.Err()
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
