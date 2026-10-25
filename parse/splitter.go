// Package parse 正文解析：条文拆分与日期标准化（开发文档 3.2、7）。
// 解析器必须版本化：每次解析记录 ParserVersion 与运行批次（article.parser_version）。
// 人工修订必须生成新版本（新 version_id），禁止覆盖原始文本。
package parse

import (
	"regexp"
	"strconv"
)

// ParserVersion 解析器版本，任何入库条文必须携带（开发文档 7）。
const ParserVersion = "openlaw-parse/0.1.0"

// ParserVersionWhole 全文即一节模式（无「第X条」结构的决定/决议/名单类）：
// 单条 article 承载全文，parser_version 带 +whole 后缀以便审计区分（见 docs/data-dictionary.md）。
const ParserVersionWhole = ParserVersion + "+whole"

// WholeArticleNo 全文即一节模式的约定条号（article.article_no）。
const WholeArticleNo = "全文"

// articleRe 匹配「第X条」条文标记（中文数字与阿拉伯数字均可）。
// articleMarkerRe 不带行首锚点：用于在连写文本中定位条文标记以重插换行锚点。
var articleMarkerRe = regexp.MustCompile(`第[一二三四五六七八九十百千零〇两0-9\x{2e80}-\x{2fdf}]+条`)

// articleRe 匹配条文标记行首「第X条」。
// 附加 \x{2e80}-\x{2fdf}（部首补充/楷体部首区）：PDF 文本层提取可能把
// 「二/人」等映射为部首码位「⼆/⼈」，标记识别须容忍，正文保真由人工确认兜底。
var articleRe = regexp.MustCompile(`(?m)^(第[一二三四五六七八九十百千零〇两0-9\x{2e80}-\x{2fdf}]+条)`)

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

// articleEURe 匹配 EU 条文标记行首「Article N」（可带 ## 标题前缀，
// FORMEX FullText 渲染与 XHTML 回退共用；大小写敏感——官方文本固定 Title 格）。
// 已知局限：字母尾条号（如个别法规的 "Article 24a"）不匹配 \b 边界，内容并入前条——
// M1 语料（GDPR 等）无此形态，M2 批量遇再扩。
var articleEURe = regexp.MustCompile(`(?m)^(?:#{1,6} *)?(Article [0-9]+)\b`)

// SplitArticlesEU 按「Article N」标记拆分英文正文（EU 通道专用：
// pack 回灌 lang=eng 的 md、XHTML 回退文本切分）。语义与 SplitArticles 一致：
// 首个标记之前的文字不入库；返回空切片表示未识别出任何条文。
// 条号递增过滤（与中文通道交叉引用误拆同型防御，2026-10 review 补）：EU 条号严格
// 递增，行首交叉引用（如 XHTML <p> 换行后 "Article 6 …"）号必 ≤ 上一保留号，丢弃。
// 前向引用（大条号出现在小条号正文行首）无法用递增判别——同中文通道，靠人工确认兜底；
// 现网两路均无碰撞（FORMEX FullText 段并为单行；XHTML 原始字节匹配不到转人工），
// M2 XHTML 文本化后须改 HTML 结构感知切分。
func SplitArticlesEU(md string) []Article {
	locs := articleEURe.FindAllStringSubmatchIndex(md, -1)
	if len(locs) == 0 {
		return nil
	}
	kept := locs[:0]
	last := 0
	for _, loc := range locs {
		n, err := strconv.Atoi(md[loc[2]+len("Article ") : loc[3]])
		if err != nil {
			continue
		}
		if n <= last {
			continue // 交叉引用误拆点：文本仍归入上一保留条文
		}
		last = n
		kept = append(kept, loc)
	}
	if len(kept) == 0 {
		return nil
	}
	arts := make([]Article, 0, len(kept))
	for i, loc := range kept {
		end := len(md)
		if i+1 < len(kept) {
			end = kept[i+1][0]
		}
		arts = append(arts, Article{
			No:      md[loc[2]:loc[3]],
			Content: trimBlankLines(md[loc[0]:end]),
		})
	}
	return arts
}
