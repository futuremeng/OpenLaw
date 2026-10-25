// EU Cellar 全文通道——FORMEX ACT XML 解析器（docs/sources-eu-text.md，2026-10-25 实网样本）。
// FORMEX 是 well-formed XML，条文边界来自 <ARTICLE IDENTIFIER> 结构元素，零正则零猜测：
//
//	<ARTICLE IDENTIFIER="001"><TI.ART>Article 1</TI.ART><STI.ART>标题</STI.ART>
//	  <PARAG IDENTIFIER="001.001"><NO.PARAG>1.</NO.PARAG><ALINEA>正文…</ALINEA></PARAG>…
//
// PREAMBLE（序言/鉴于条款）与 FINAL（结语/签署/附录区）M1 只进 md 全文层，不切 article（M3 细化）。
package parse

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// ParserVersionEU FORMEX 解析器版本（article.parser_version 可追溯；
// 特征查询须排除 EU 通道时用此后缀，惯例同 +whole）。
const ParserVersionEU = "openlaw-parse/0.1.0+formex-eu"

// FormexDoc 一篇 FORMEX ACT 的解析产出。
type FormexDoc struct {
	Articles []Article // 结构化条文（No=「Article N」，Content 以条号开头，与中文语料惯例一致）
	FullText string    // 全文 Markdown 渲染（序言+条文+结语），md 层与 pack 往返用
}

// artAcc 单条 ARTICLE 子树的采集累加器。
type artAcc struct {
	no, title string
	segs      []string
	cur       strings.Builder
	hasCur    bool
	inTI      bool
	inSTI     bool
	inNO      bool
	noBuf     strings.Builder
	inAL      bool
}

func (a *artAcc) flush() {
	if a.hasCur {
		if s := normSpace(a.cur.String()); s != "" {
			a.segs = append(a.segs, s)
		}
		a.cur.Reset()
		a.hasCur = false
	}
}

// content 组装条文全文：条号行 + 标题行（如有）+ 各段。
func (a *artAcc) content() string {
	var b strings.Builder
	b.WriteString(normSpace(a.no))
	if t := normSpace(a.title); t != "" {
		b.WriteString("\n" + t)
	}
	for _, s := range a.segs {
		b.WriteString("\n\n" + s)
	}
	return b.String()
}

// paraPara PREAMBLE/FINAL 区内的段落级元素（其 subtree 全部文本合成一行）。
func paraPara(name string) bool {
	switch name {
	case "PREAMBLE.INIT", "VISA", "NP", "PREAMBLE.FINAL", "SIGNATURE", "PLACE", "OJ":
		return true
	}
	return false
}

// normSpace 折叠空白为单空格并 Trim（XML 缩进噪声清除）。
func normSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// zoneOf 段落采集结束时的归属区。
func zoneOf(inPreamble, inFinal bool) string {
	if inPreamble {
		return "pre"
	}
	if inFinal {
		return "post"
	}
	return "body"
}

// ParseFormexACT 解析 FORMEX ACT XML（Cellar fmx4 manifestation 的正文 item）。
// 无任何 <ARTICLE> 元素视为解析失败（调用方按设计走 XHTML 回退或 PDF 归档）。
func ParseFormexACT(data []byte) (*FormexDoc, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	doc := &FormexDoc{}
	var cur *artAcc
	var preLines, bodyLines, postLines []string
	inPreamble, inFinal := false, false
	// 段落级子树采集：paraDepth>0 期间所有 CharData 进 paraBuf，子树闭合时成一行
	paraDepth := 0
	var paraBuf strings.Builder
	pendingZone := ""

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("FORMEX XML 解析失败: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			switch {
			case name == "ARTICLE":
				if cur != nil {
					return nil, fmt.Errorf("FORMEX 结构异常：ARTICLE 嵌套（当前=%s）", cur.no)
				}
				cur = &artAcc{}
			case cur != nil:
				switch name {
				case "TI.ART":
					cur.inTI = true
				case "STI.ART":
					cur.inSTI = true
				case "NO.PARAG":
					cur.flush()
					cur.inNO = true
					cur.noBuf.Reset()
				case "ALINEA":
					cur.hasCur = true
					cur.inAL = true
				}
			case name == "PREAMBLE":
				inPreamble = true
			case name == "FINAL":
				inFinal = true
			case paraDepth > 0:
				paraDepth++ // 采集子树内部任意元素：只加深度，保持平衡
			case (inPreamble || inFinal) && paraPara(name):
				paraDepth, paraBuf = 1, strings.Builder{}
				pendingZone = zoneOf(inPreamble, inFinal)
			case name == "TITLE" || name == "STI":
				if !inPreamble && !inFinal {
					paraDepth, paraBuf = 1, strings.Builder{}
					pendingZone = "body"
				}
			}
		case xml.EndElement:
			name := t.Name.Local
			switch {
			case name == "ARTICLE" && cur != nil:
				cur.flush()
				if cur.no == "" {
					return nil, fmt.Errorf("FORMEX 结构异常：ARTICLE 缺 TI.ART 条号")
				}
				doc.Articles = append(doc.Articles, Article{No: normSpace(cur.no), Content: cur.content()})
				cur = nil
			case cur != nil:
				switch name {
				case "TI.ART":
					cur.inTI = false
				case "STI.ART":
					cur.inSTI = false
				case "NO.PARAG":
					if n := normSpace(cur.noBuf.String()); n != "" {
						cur.cur.WriteString(n + " ")
						cur.hasCur = true
					}
					cur.inNO = false
				case "ALINEA":
					cur.inAL = false
				}
			case name == "PREAMBLE":
				inPreamble = false
			case name == "FINAL":
				inFinal = false
			case paraDepth > 0:
				paraDepth--
				if paraDepth == 0 {
					if s := normSpace(paraBuf.String()); s != "" {
						switch pendingZone {
						case "pre":
							preLines = append(preLines, s)
						case "body":
							bodyLines = append(bodyLines, s)
						default:
							postLines = append(postLines, s)
						}
					}
				}
			}
		case xml.CharData:
			if cur != nil {
				switch {
				case cur.inTI:
					cur.no += string(t)
				case cur.inSTI:
					cur.title += string(t)
				case cur.inNO:
					cur.noBuf.Write(t)
				case cur.inAL:
					cur.cur.Write(t)
				}
			} else if paraDepth > 0 {
				paraBuf.Write(t)
			}
		}
	}
	if len(doc.Articles) == 0 {
		return nil, fmt.Errorf("FORMEX 文档无 <ARTICLE> 条文元素（考虑 XHTML 回退或 PDF 归档）")
	}
	var md strings.Builder
	for _, l := range preLines {
		md.WriteString(l + "\n\n")
	}
	// 章/节标题行（bodyLines）置于条文前——M1 不还原穿插位置，结构信息不丢失；
	// 条文以「## Article N」标记，SplitArticlesEU 可逆切。
	for _, l := range bodyLines {
		md.WriteString("### " + l + "\n\n")
	}
	for _, a := range doc.Articles {
		lines := strings.SplitN(a.Content, "\n", 2)
		md.WriteString("## " + lines[0])
		if len(lines) == 2 {
			md.WriteString("\n\n" + lines[1])
		}
		md.WriteString("\n\n")
	}
	for _, l := range postLines {
		md.WriteString(l + "\n\n")
	}
	doc.FullText = strings.TrimSpace(md.String())
	return doc, nil
}
