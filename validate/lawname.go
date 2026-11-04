// 法名规范化与别名解析（OL-4，docs/tasks-opencourt-bridge.md）。
// 纪律：文书里简称引用极常见（《民法典》↔《中华人民共和国民法典》），但别名**必须**来自
// 语料内可溯源的信息——law.name、law.short_name，或法规文本中的「以下简称」条款
// （law_alias.source=corpus-text，附出处条文）；国名省略式简称走确定性名称规则
// name-prefix(R1)，与 openlaw-level 的效力等级规则同口径。**绝不由模型生成别名**。
// 解析不确定（同名多法、别名指向多法）时一律并列候选交人工，不自动采信任何一条。
package validate

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/futuremeng/OpenLaw/store"
)

// 解析来源（Data.resolved_from 取值；界面据此显示「按什么命中」）
const (
	ResolvedFromName      = "full_name"  // law.name 精确命中
	ResolvedFromShortName = "short_name" // law.short_name 精确命中
	ResolvedFromAlias     = "alias"      // law_alias 别名表命中
	ResolvedFromNameRule  = "name_rule"  // 国名省略式简称（确定性名称规则）
)

// 别名来源（law_alias.source 取值；合法性放应用层——schema 刻意不加 CHECK）
const (
	AliasSourceShortName = "corpus-short_name"
	AliasSourceText      = "corpus-text"
	AliasSourceNameRule  = "name-prefix(R1)"
)

// StatePrefixRule R1：国名省略式简称的确定性规则（仅用于反查全称，不写入别名表）。
const StatePrefixRule = "中华人民共和国"

// aliasTableAvailable 别名表是否存在（v8 及更早语料无该表——必须如实降级，不猜）。
func aliasTableAvailable(db *store.DB) bool {
	rows, err := db.Query(`SELECT 1 FROM sqlite_master WHERE type='table' AND name='law_alias' LIMIT 1`)
	if err != nil {
		return false
	}
	defer rows.Close()
	return rows.Next()
}

// NormalizeLawName 法名规范化：去书名号/引号与空白，全角括号转半角后比较。
// 只做确定性清理，不做任何缩写推断或模糊改写（规范化 ≠ 猜测）。
func NormalizeLawName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '《', '》', '「', '」', '『', '』', '\'', '’', '“', '”',
			' ', '\t', '\n', '\r', '　':
			continue
		case '（':
			b.WriteRune('(')
		case '）':
			b.WriteRune(')')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// LawCandidate 一条法名解析候选（多候选时并列展示交人工）。
type LawCandidate struct {
	LawID            string `json:"law_id"`
	Name             string `json:"name"`
	Issuer           string `json:"issuer,omitempty"`
	ResolvedFrom     string `json:"resolved_from"`                 // full_name|short_name|alias|name_rule
	AliasSource      string `json:"alias_source,omitempty"`        // 别名来源
	BasedOnArticleID string `json:"based_on_article_id,omitempty"` // 别名出处条文（可溯源）
}

// LawResolution 法名解析结论。
type LawResolution struct {
	Query             string         `json:"query"`
	Normalized        string         `json:"normalized"`
	Candidates        []LawCandidate `json:"candidates"`
	Ambiguous         bool           `json:"ambiguous"`           // 指向多部法规：必须交人工
	DateDisambiguated bool           `json:"date_disambiguated"`  // 多候选按时间点唯一收敛（须如实提示）
	AliasTableMissing bool           `json:"alias_table_missing"` // 语料无别名表（v8 及更早）
}

// lawRowsBy 按 (列, 取值集合) 取 law 行；列名只取本文件内的白名单，取值走参数绑定。
func lawRowsBy(db *store.DB, col string, vals []string) ([]LawCandidate, error) {
	if len(vals) == 0 {
		return nil, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(vals)), ",")
	rows, err := db.Query(`SELECT law_id, name, COALESCE(issuer,'') FROM law WHERE `+col+` IN (`+ph+`) ORDER BY law_id`,
		toArgs(vals)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LawCandidate
	for rows.Next() {
		var c LawCandidate
		if err := rows.Scan(&c.LawID, &c.Name, &c.Issuer); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func toArgs(vals []string) []any {
	args := make([]any, 0, len(vals))
	for _, v := range vals {
		args = append(args, v)
	}
	return args
}

// ResolveLawName 把用户/文书给出的法名解析为语料内法规。
// 解析顺序（逐级放宽，命中即停；同级多条 = 多候选）：
//
//	① law.name 精确（原文与规范化后都试）→ full_name
//	② law.short_name 精确 → short_name
//	③ law_alias 别名表 → alias（附来源与出处条文）
//	④ 国名省略式简称（name = 「中华人民共和国」+ 规范化名）→ name_rule
//
// Candidates 已按 law_id 去重；Ambiguous=true 表示指向多部法规，调用方必须并列展示
// 交人工（不自动采信）。未命中返回空 Candidates，这不是错误。
func ResolveLawName(db *store.DB, raw string) (*LawResolution, error) {
	raw = strings.TrimSpace(raw)
	norm := NormalizeLawName(raw)
	res := &LawResolution{Query: raw, Normalized: norm}
	if norm == "" {
		return res, nil
	}
	forms := []string{raw}
	if norm != raw {
		forms = append(forms, norm)
	}

	// ① 全称精确
	cands, err := lawRowsBy(db, "name", forms)
	if err != nil {
		return nil, err
	}
	tag(cands, ResolvedFromName, "", "")
	if len(cands) > 0 {
		finish(res, cands)
		return res, nil
	}

	// ② 简称列精确
	if cands, err = lawRowsBy(db, "short_name", forms); err != nil {
		return nil, err
	}
	tag(cands, ResolvedFromShortName, AliasSourceShortName, "")
	if len(cands) > 0 {
		finish(res, cands)
		return res, nil
	}

	// ③ 别名表（v8 及更早语料无该表：如实标注缺失，继续走确定性名称规则）
	if aliasTableAvailable(db) {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(forms)), ",")
		rows, err := db.Query(`SELECT a.law_id, l.name, COALESCE(l.issuer,''), a.source,
			COALESCE(a.source_article_id,'')
			FROM law_alias a JOIN law l ON l.law_id = a.law_id
			WHERE a.alias IN (`+ph+`) ORDER BY a.alias, a.law_id`, toArgs(forms)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c LawCandidate
			if err := rows.Scan(&c.LawID, &c.Name, &c.Issuer, &c.AliasSource, &c.BasedOnArticleID); err != nil {
				rows.Close()
				return nil, err
			}
			c.ResolvedFrom = ResolvedFromAlias
			res.Candidates = append(res.Candidates, c)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		dedupe(res)
		if len(res.Candidates) > 0 {
			res.Ambiguous = len(res.Candidates) > 1
			return res, nil
		}
	} else {
		res.AliasTableMissing = true
	}

	// ④ 国名省略式简称：中华人民共和国X ↔ X（确定性规则；判不出不猜）
	if !strings.HasPrefix(norm, StatePrefixRule) {
		if cands, err = lawRowsBy(db, "name", []string{StatePrefixRule + norm}); err != nil {
			return nil, err
		}
		tag(cands, ResolvedFromNameRule, AliasSourceNameRule, "")
		res.Candidates = append(res.Candidates, cands...)
	}
	dedupe(res)
	res.Ambiguous = len(res.Candidates) > 1
	return res, nil
}

func tag(cs []LawCandidate, from, source, articleID string) {
	for i := range cs {
		cs[i].ResolvedFrom = from
		cs[i].AliasSource = source
		cs[i].BasedOnArticleID = articleID
	}
}

func finish(res *LawResolution, cs []LawCandidate) {
	res.Candidates = cs
	res.Ambiguous = len(cs) > 1
}

// dedupe 按 law_id 去重（同一部法规经多条别名命中只算一个候选）。
func dedupe(res *LawResolution) {
	seen := map[string]bool{}
	out := res.Candidates[:0]
	for _, c := range res.Candidates {
		if seen[c.LawID] {
			continue
		}
		seen[c.LawID] = true
		out = append(out, c)
	}
	res.Candidates = out
}

// ResolveVersionByLawID 按 law_id 与时间点解析当时生效版本（左闭右开）。
// 与 ResolveVersion 同一时间语义，只是入口换成已解析出的法规编号（OL-4 第二跳）。
func ResolveVersionByLawID(db *store.DB, lawID, atDate string) (*ResolveVersionRow, error) {
	row := db.QueryRow(`
		SELECT v.version_id, v.version_no, v.law_id, v.effective_date, COALESCE(v.expire_date, ''),
		       d.doc_id, d.file_path, d.confirm_status
		FROM law_version v
		JOIN document d ON d.doc_id = v.doc_id
		WHERE v.law_id = :law_id
		  AND v.effective_date <= :at_date
		  AND (v.expire_date IS NULL OR v.expire_date > :at_date)
		ORDER BY v.effective_date DESC
		LIMIT 1`,
		sql.Named("law_id", lawID), sql.Named("at_date", atDate))
	var r ResolveVersionRow
	if err := row.Scan(&r.VersionID, &r.VersionNo, &r.LawID, &r.EffectiveDate, &r.ExpireDate,
		&r.DocID, &r.PDFPath, &r.ConfirmStatus); err != nil {
		return nil, err
	}
	return &r, nil
}

// ResolveNamedVersion 法名（含简称/别名解析）+ 时间点 → 当时生效版本（OL-4 统一入口）。
// CLI / 桌面 / SDK 共用本函数，不存在第二条解析路径。返回语义：
//   - rv != nil：唯一确定，res 标注按什么命中（resolved_from）；
//   - rv == nil 且 Candidates 为空：法规库中没有这部法规（OL-1001）；
//   - rv == nil 且 Candidates 非空：法规存在但该日期无生效版本（OL-1002）；
//   - ErrAmbiguousLawName：多候选且时间点也无法唯一确定——调用方必须并列交人工。
//
// 多候选时先按时间点收敛：同名法规在该日期只有一部有生效版本 → 视为确定，
// 但必须在 DateDisambiguated 上如实标注（界面要提示「同名 N 部，按时间点唯一确定」）。
func ResolveNamedVersion(db *store.DB, rawName, atDate string) (*ResolveVersionRow, *LawResolution, error) {
	res, err := ResolveLawName(db, rawName)
	if err != nil {
		return nil, nil, err
	}
	if len(res.Candidates) == 0 {
		return nil, res, nil
	}
	if !res.Ambiguous {
		rv, err := ResolveVersionByLawID(db, res.Candidates[0].LawID, atDate)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, res, nil
		}
		if err != nil {
			return nil, nil, err
		}
		return rv, res, nil
	}

	var hit *ResolveVersionRow
	var hitCand *LawCandidate
	n := 0
	for i := range res.Candidates {
		rv, err := ResolveVersionByLawID(db, res.Candidates[i].LawID, atDate)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return nil, nil, err
		}
		n++
		hit, hitCand = rv, &res.Candidates[i]
		if n > 1 {
			break
		}
	}
	switch {
	case n == 1:
		res.Candidates = []LawCandidate{*hitCand}
		res.Ambiguous = false
		res.DateDisambiguated = true
		return hit, res, nil
	case n == 0:
		return nil, res, nil // 法规存在，但该日期无生效版本
	default:
		return nil, res, ErrAmbiguousLawName
	}
}
