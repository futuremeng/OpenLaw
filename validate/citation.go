package validate

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/futuremeng/OpenLaw/store"
)

// 引用校验错误码（开发文档 5.4）
const (
	CodeLawNotFound     = "OL-1001"
	CodeNoVersionAtDate = "OL-1002"
	CodeArticleNotFound = "OL-1003"
	CodeQuoteMismatch   = "OL-1004"
	CodeVersionExpired  = "OL-1005"
)

// SimilarThreshold 引用文本相似度下限，低于此值提示可能引用错误（OL-1004）。
const SimilarThreshold = 0.60

// CitationResult 引用校验结构化结果（5.3 check_citation）。
type CitationResult struct {
	Hit            bool    `json:"hit"`
	Similarity     float64 `json:"similarity"`
	Expired        bool    `json:"expired"`
	ArticleID      string  `json:"article_id"`
	ArticleNo      string  `json:"article_no"`
	Content        string  `json:"content"` // 原文，供人工比对
	VersionID      string  `json:"version_id"`
	VersionNo      string  `json:"version_no"`
	CurrentVersion string  `json:"current_version"` // 已失效时给出当前有效版本
	DocID          string  `json:"doc_id"`
	PDFPath        string  `json:"pdf_path"`
	ConfirmStatus  string  `json:"confirm_status"`
	ChainIssues    []Issue `json:"chain_issues,omitempty"`
	ErrorCode      string  `json:"error_code,omitempty"`
	Reason         string  `json:"reason,omitempty"`
}

// ResolveVersionRow 时间点版本解析结果（13.3 时间回溯查询）。
type ResolveVersionRow struct {
	VersionID     string
	VersionNo     string
	EffectiveDate string
	ExpireDate    string
	DocID         string
	PDFPath       string
	ConfirmStatus string
}

// ResolveVersion 按法规名称与时间点解析当时生效版本（左闭右开）。
// 返回 sql.ErrNoRows 表示该日期无生效版本；law 是否存在由调用方另行判断。
func ResolveVersion(db *store.DB, lawName, atDate string) (*ResolveVersionRow, error) {
	row := db.QueryRow(`
		SELECT v.version_id, v.version_no, v.effective_date, COALESCE(v.expire_date, ''),
		       d.doc_id, d.file_path, d.confirm_status
		FROM law_version v
		JOIN document d ON d.doc_id = v.doc_id
		JOIN law l ON l.law_id = v.law_id
		WHERE (l.name = :law_name OR l.short_name = :law_name)
		  AND v.effective_date <= :at_date
		  AND (v.expire_date IS NULL OR v.expire_date > :at_date)
		ORDER BY v.effective_date DESC
		LIMIT 1`,
		sql.Named("law_name", lawName), sql.Named("at_date", atDate))
	var r ResolveVersionRow
	if err := row.Scan(&r.VersionID, &r.VersionNo, &r.EffectiveDate, &r.ExpireDate,
		&r.DocID, &r.PDFPath, &r.ConfirmStatus); err != nil {
		return nil, err
	}
	return &r, nil
}

// LawExists 法规名称是否存在（区分 OL-1001 与 OL-1002）。
func LawExists(db *store.DB, lawName string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM law WHERE name = ? OR short_name = ?`, lawName, lawName).Scan(&n)
	return n > 0, err
}

// CheckCitation 引用校验主流程（6.2）：解析法规名称 → 按时间点取版本 → 匹配条款号
// → 比对引用文本 → 检查效力与溯源链。溯源失败由调用方直接返回 OL-1006。
func CheckCitation(db *store.DB, dataDir, lawName, articleNo, quote, atDate, today string) (CitationResult, error) {
	var res CitationResult

	exists, err := LawExists(db, lawName)
	if err != nil {
		return res, err
	}
	if !exists {
		res.ErrorCode = CodeLawNotFound
		res.Reason = "未找到法规，请核对名称与简称"
		return res, nil
	}

	rv, err := ResolveVersion(db, lawName, atDate)
	if errors.Is(err, sql.ErrNoRows) {
		res.ErrorCode = CodeNoVersionAtDate
		res.Reason = "指定日期无生效版本，请确认时间范围"
		return res, nil
	} else if err != nil {
		return res, err
	}

	var content string
	err = db.QueryRow(`SELECT article_id, content FROM article WHERE version_id = ? AND article_no = ?`,
		rv.VersionID, articleNo).Scan(&res.ArticleID, &content)
	if errors.Is(err, sql.ErrNoRows) {
		res.ErrorCode = CodeArticleNotFound
		res.Reason = fmt.Sprintf("版本 %s 中不存在条款 %s，请核对条款号与版本", rv.VersionNo, articleNo)
		return res, nil
	} else if err != nil {
		return res, err
	}

	res.Hit = true
	res.Content = content
	res.ArticleNo = articleNo
	res.VersionID = rv.VersionID
	res.VersionNo = rv.VersionNo
	res.DocID = rv.DocID
	res.PDFPath = rv.PDFPath
	res.ConfirmStatus = rv.ConfirmStatus
	res.Similarity = Similar(quote, content)
	if res.Similarity < SimilarThreshold {
		res.ErrorCode = CodeQuoteMismatch
		res.Reason = "引用内容与原文差异大，展示原文供人工比对"
	}

	// 效力检查：所引版本在今天是否仍有效；失效则给出当前有效版本（OL-1005）
	if rv.ExpireDate != "" && rv.ExpireDate <= today {
		res.Expired = true
		res.ErrorCode = CodeVersionExpired
		res.Reason = "版本已失效，返回当前有效版本"
		if cur, err := ResolveVersion(db, lawName, today); err == nil {
			res.CurrentVersion = cur.VersionNo
		}
	}

	// 溯源链检查
	res.ChainIssues = CheckDocument(db, dataDir, rv.DocID)
	return res, nil
}

// Similar 引用文本相似度：去空白后互相包含记 1.0，否则用字符二元组 Dice 系数。
func Similar(a, b string) float64 {
	na, nb := normalizeText(a), normalizeText(b)
	if na == "" || nb == "" {
		return 0
	}
	if strings.Contains(na, nb) || strings.Contains(nb, na) {
		return 1.0
	}
	return diceBigrams(na, nb)
}

func normalizeText(s string) string {
	return strings.Join(strings.Fields(s), "")
}

func bigrams(s string) map[[2]rune]int {
	m := map[[2]rune]int{}
	rs := []rune(s)
	if len(rs) < 2 {
		m[[2]rune{rs[0], 0}] = 1
		return m
	}
	for i := 0; i+1 < len(rs); i++ {
		m[[2]rune{rs[i], rs[i+1]}]++
	}
	return m
}

func diceBigrams(a, b string) float64 {
	ba, bb := bigrams(a), bigrams(b)
	total := 0.0
	for g, ca := range ba {
		total += float64(min(ca, bb[g]))
	}
	denom := float64(len([]rune(a)) + len([]rune(b)) - 2)
	if denom <= 0 {
		return 0
	}
	return 2 * total / denom
}
