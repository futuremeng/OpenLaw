// Package search 检索层（开发文档 3.3、13.2、FR-07）。
// FTS5 为主；unicode61 对连续中文按整段切词，长句召回差，
// 因此提供 LIKE 兜底；术语辅助列与向量混合检索、重排序为后续版本（hybrid.go）。
package search

import (
	"fmt"
	"strings"

	"github.com/futuremeng/OpenLaw/store"
)

// Hit 一条检索命中。
type Hit struct {
	ArticleID string `json:"article_id"`
	ArticleNo string `json:"article_no"`
	Snippet   string `json:"snippet"`
	DocID     string `json:"doc_id"`
}

// Search 全文检索：优先 FTS5 MATCH，零结果时退化为 LIKE 兜底。
// 性能目标：全文检索 1 秒以内（2.2）。
func Search(db *store.DB, query string, topK int) ([]Hit, error) {
	if query == "" {
		return nil, fmt.Errorf("检索词不能为空")
	}
	if topK <= 0 {
		topK = 10
	}

	rows, err := db.Query(`
		SELECT f.article_id, f.article_no,
		       snippet(article_fts, 2, '[', ']', '…', 12), a.doc_id
		FROM article_fts f
		JOIN article a ON a.article_id = f.article_id
		WHERE article_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, quoteFTS(query), topK)
	if err != nil {
		return nil, fmt.Errorf("FTS 检索失败: %w", err)
	}
	var hits []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ArticleID, &h.ArticleNo, &h.Snippet, &h.DocID); err != nil {
			rows.Close()
			return nil, err
		}
		hits = append(hits, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(hits) > 0 {
		return hits, nil
	}

	// LIKE 兜底：中文整段切词导致 FTS 零结果时保证基本召回
	rows, err = db.Query(`
		SELECT a.article_id, a.article_no, substr(a.content, 1, 80), a.doc_id
		FROM article a
		WHERE a.content LIKE '%' || ? || '%'
		ORDER BY a.article_id
		LIMIT ?`, query, topK)
	if err != nil {
		return nil, fmt.Errorf("LIKE 兜底检索失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ArticleID, &h.ArticleNo, &h.Snippet, &h.DocID); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// quoteFTS 将用户输入作为短语处理，避免 FTS 语法注入。
func quoteFTS(q string) string {
	return `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
}
