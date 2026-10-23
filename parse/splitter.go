// Package parse 正文解析：条文拆分与日期标准化（开发文档 3.2、7）。
// 解析器必须版本化：每次解析记录 ParserVersion 与运行批次（article.parser_version）。
// 人工修订必须生成新版本（新 version_id），禁止覆盖原始文本。
package parse

import "regexp"

// ParserVersion 解析器版本，任何入库条文必须携带（开发文档 7）。
const ParserVersion = "openlaw-parse/0.1.0"

// articleRe 匹配「第X条」条文标记（中文数字与阿拉伯数字均可）。
var articleRe = regexp.MustCompile(`(?m)^(第[一二三四五六七八九十百千零〇两0-9]+条)`)

// Article 拆分出的单个条文。
type Article struct {
	No      string // 条号，如「第二十六条」
	Content string // 条文全文（含条号起始）
}

// SplitArticles 按「第X条」标记拆分 Markdown 正文。
// 首个标记之前的标题/说明文字不入库；每个条文取到下一个标记为止。
// 返回空切片表示未识别出任何条文——导入器必须视为失败。
func SplitArticles(md string) []Article {
	locs := articleRe.FindAllStringIndex(md, -1)
	if len(locs) == 0 {
		return nil
	}
	arts := make([]Article, 0, len(locs))
	for i, loc := range locs {
		end := len(md)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		arts = append(arts, Article{
			No:      md[loc[0]:loc[1]],
			Content: trimBlankLines(md[loc[0]:end]),
		})
	}
	return arts
}

// trimBlankLines 去除首尾空白行，保留段内换行。
func trimBlankLines(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == '\n' || s[start] == '\r' || s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == '\n' || s[end-1] == '\r' || s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
