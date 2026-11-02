// USLM（United States Legislative Markup，govinfo 批量包的官方 XML 格式）条文解析
// （docs/design-multi-jurisdiction.md §6、docs/sources-us.md）。
// 条文边界来自 XML 结构（div type="SECTION"，回退 PART/CHAPTER/SUBPART 的
// <num>+<head>+<p>），零猜测——与 EU FORMEX 通道同一纪律：结构即证据。
// 解析器版本化：article.parser_version = openlaw-parse/0.1.0+uslm-us。
package parse

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// ParserVersionUS USLM 解析器版本。
const ParserVersionUS = ParserVersion + "+uslm-us"

// USLMTitle 一部美国法规（批量包中的单个 XML 文件）的解析结果。
type USLMTitle struct {
	Citation string    // <ident><citation> 原文（如 "CFR Title 12"）；缺失为空
	Title    string    // 正文首个 <head>（法规标题）；缺失为空
	Articles []Article // article_no = <num> 文本（如 "§ 1.1"）
}

// uslmNode 通用 XML 节点树（不预设 schema 层级，结构即证据地遍历——USLM 的 div
// 可嵌套在 body/title/chapter 等任意容器下，固定结构体会漏层）。
type uslmNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr  `xml:",any,attr"`
	Children []*uslmNode `xml:",any"`
	Text     string      `xml:",chardata"`
}

func (n *uslmNode) name() string {
	s := n.XMLName.Local
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func (n *uslmNode) attr(name string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// child 返回首个同名直接子节点文本（无则空）。
func (n *uslmNode) child(name string) string {
	for _, c := range n.Children {
		if c.name() == name {
			return collapseSpace(c.Text)
		}
	}
	return ""
}

func (n *uslmNode) childTexts(name string) []string {
	var out []string
	for _, c := range n.Children {
		if c.name() == name {
			if t := collapseSpace(c.Text); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

// uslmLeafTypes 条文边界 div type（govinfo USLM 惯用值；SECTION 优先）。
var uslmLeafTypes = map[string]bool{"SECTION": true, "PART": true, "CHAPTER": true, "SUBPART": true}

// ParseUSLM 解析 USLM XML。零条文视为失败（不得产出「看起来成功」的空解析）。
// 条文 = 编号 div（type 在名单内、带 <num>）且其内部不再含编号 div——
// 父 div（如 PART）若含子 div（SECTION），只作容器不重复成条。
func ParseUSLM(data []byte) (*USLMTitle, error) {
	root := &uslmNode{}
	if err := xml.Unmarshal(data, root); err != nil {
		return nil, fmt.Errorf("USLM XML 解析失败: %w", err)
	}
	out := &USLMTitle{Citation: findFirstTagText(data, "citation")}
	var arts []Article
	var hasNumberedDivDescendant func(n *uslmNode) bool
	hasNumberedDivDescendant = func(n *uslmNode) bool {
		for _, c := range n.Children {
			if c.name() == "div" && numberedDiv(c) {
				return true
			}
			if hasNumberedDivDescendant(c) {
				return true
			}
		}
		return false
	}
	var walk func(n *uslmNode)
	walk = func(n *uslmNode) {
		if out.Title == "" && n.name() == "head" && n.Text != "" {
			out.Title = collapseSpace(n.Text)
		}
		if numberedDiv(n) && !hasNumberedDivDescendant(n) {
			content := strings.TrimSpace(n.child("head") + "\n" + strings.Join(n.childTexts("p"), "\n"))
			if content == "" {
				content = collapseSpace(n.child("num")) // 纯标题节点：如实只留条号，不造正文
			}
			arts = append(arts, Article{No: collapseSpace(n.child("num")), Content: content})
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if len(arts) == 0 {
		return nil, fmt.Errorf("USLM 未解析出任何条文（div type=SECTION/PART 结构缺失，格式可能已变更）")
	}
	out.Articles = arts
	return out, nil
}

// numberedDiv 是否为编号条文 div（type 在名单内且带非空 <num>）。
func numberedDiv(n *uslmNode) bool {
	return n.name() == "div" && uslmLeafTypes[strings.ToUpper(strings.TrimSpace(n.attr("type")))] &&
		n.child("num") != ""
}

// findFirstTagText 原文里首个 <tag>…</tag> 的文本（ident/citation 在通用树里
// 与正文 head 无法区分层级，直接扫原文最稳）。
func findFirstTagText(data []byte, tag string) string {
	s := string(data)
	open, close := "<"+tag+">", "</"+tag+">"
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i+len(open):], close)
	if j < 0 {
		return ""
	}
	return collapseSpace(s[i+len(open) : i+len(open)+j])
}

// collapseSpace 折叠连续空白为单空格并去首尾（XML 缩进不进正文）。
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
