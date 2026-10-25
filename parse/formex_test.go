package parse

import (
	"strings"
	"testing"
)

// 小型 FORMEX ACT 样本：序言 + 2 条（含嵌套段与点列）+ 结语。
const formexSample = `<?xml version="1.0" encoding="UTF-8"?>
<ACT><BIB.INSTANCE><DATE ISO="20160427">20160427</DATE></BIB.INSTANCE>
<PREAMBLE><PREAMBLE.INIT>THE EUROPEAN PARLIAMENT AND THE COUNCIL OF THE EUROPEAN UNION,</PREAMBLE.INIT>
<GR.VISA><VISA>Having regard to the Treaty on the Functioning of the European Union,</VISA></GR.VISA>
<GR.CONSID><NP><P>(1)  Protection is a right.</P></NP></GR.CONSID>
<PREAMBLE.FINAL>HAVE ADOPTED THIS REGULATION:</PREAMBLE.FINAL></PREAMBLE>
<TITLE><TI>CHAPTER I  GENERAL PROVISIONS</TI></TITLE>
<ARTICLE IDENTIFIER="001"><TI.ART>Article 1</TI.ART><STI.ART>Subject-matter and objectives</STI.ART>
<PARAG IDENTIFIER="001.001"><NO.PARAG>1.</NO.PARAG><ALINEA>This Regulation lays down rules.</ALINEA></PARAG>
<PARAG IDENTIFIER="001.002"><NO.PARAG>2.</NO.PARAG><ALINEA>It facilitates free movement.</ALINEA></PARAG>
</ARTICLE>
<ARTICLE IDENTIFIER="002"><TI.ART>Article 2</TI.ART>
<PARAG IDENTIFIER="002.001"><NO.PARAG>1.</NO.PARAG><ALINEA>It applies to processing of personal data:
<POINTS><POINT>(a) wholly or partly by automated means;</POINT><POINT>(b) other than by automated means.</POINT></POINTS></ALINEA></PARAG>
</ARTICLE>
<FINAL><SIGNATURE>Brussels, 27 April 2016.</SIGNATURE></FINAL>
</ACT>`

func TestParseFormexACT(t *testing.T) {
	doc, err := ParseFormexACT([]byte(formexSample))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(doc.Articles) != 2 {
		t.Fatalf("条文数 = %d, 期望 2", len(doc.Articles))
	}
	a1 := doc.Articles[0]
	if a1.No != "Article 1" {
		t.Errorf("条号 = %q, 期望 Article 1", a1.No)
	}
	if !strings.HasPrefix(a1.Content, "Article 1\nSubject-matter and objectives\n\n1. This Regulation lays down rules.\n\n2. It facilitates free movement.") {
		t.Errorf("条文1内容异常:\n%s", a1.Content)
	}
	a2 := doc.Articles[1]
	if !strings.Contains(a2.Content, "(a) wholly or partly by automated means;") ||
		!strings.Contains(a2.Content, "(b) other than by automated means.") {
		t.Errorf("点列未并入段落:\n%s", a2.Content)
	}
	// FullText：序言行、条文 ## 标记、结语行齐全
	for _, want := range []string{"THE EUROPEAN PARLIAMENT", "## Article 1", "## Article 2", "HAVE ADOPTED THIS REGULATION:", "Brussels, 27 April 2016."} {
		if !strings.Contains(doc.FullText, want) {
			t.Errorf("FullText 缺 %q", want)
		}
	}
	// 往返：FullText 可被 SplitArticlesEU 逆切回同数条文
	arts := SplitArticlesEU(doc.FullText)
	if len(arts) != 2 || arts[0].No != "Article 1" || arts[1].No != "Article 2" {
		t.Errorf("SplitArticlesEU 往返失败: %+v", arts)
	}
}

func TestParseFormexACTNoArticle(t *testing.T) {
	if _, err := ParseFormexACT([]byte(`<ACT><PREAMBLE><NP><P>(1) x</P></NP></PREAMBLE></ACT>`)); err == nil {
		t.Fatal("无 ARTICLE 元素应报错（走回退/归档）")
	}
}

func TestSplitArticlesEU(t *testing.T) {
	md := "preamble text\n## Article 1 A\ncontent one\n\n## Article 2\ncontent two\n\n### ANNEX\nannex text"
	arts := SplitArticlesEU(md)
	if len(arts) != 2 {
		t.Fatalf("条文数 = %d, 期望 2", len(arts))
	}
	if arts[0].No != "Article 1" || arts[1].No != "Article 2" {
		t.Errorf("条号异常: %q %q", arts[0].No, arts[1].No)
	}
	if !strings.Contains(arts[1].Content, "content two") {
		t.Errorf("条文2内容缺正文: %q", arts[1].Content)
	}
	// 纯 XHTML 风格（无 # 前缀）
	arts2 := SplitArticlesEU("Article 1 Foo\nbar\nArticle 2 Baz\nqux")
	if len(arts2) != 2 || arts2[0].No != "Article 1" {
		t.Errorf("无标题前缀风格切分失败: %+v", arts2)
	}
	// 中文正文不误切
	if n := len(SplitArticlesEU("第一条 范围\n正文")); n != 0 {
		t.Errorf("中文正文不应被 EU 切分器命中: %d", n)
	}
}

// TestSplitArticlesEUCrossRef 行首交叉引用误拆防御（2026-10 review 补）：
// XHTML 文本 <p> 换行后可出现行首 "Article N …" 引用句——条号递增过滤须丢弃
// 号 ≤ 上一保留号的候选，引用文本归入上一条文。
func TestSplitArticlesEUCrossRef(t *testing.T) {
	// 前向引用（大条号出现在小条号正文行首）无法用递增判别——已知局限的代价
	// 显式钉住：Article 6（6>2）保留为切分点，其后 Article 3（3<6）被吞并入。
	// 现网无此碰撞：pack 回灌的 FullText 经 normSpace 段并为单行（交叉引用不可能
	// 行首）；XHTML 回退对原始字节流匹配不到行首 Article 而报错转人工。
	// M2 把 XHTML 文本化后此问题才活跃——届时切分器改 HTML 结构感知（<p> 边界）。
	md := "Article 1 Subject-matter\ncontent one\n\nArticle 2 General obligations\n" +
		"compliance with\nArticle 6 shall be ensured.\nmore text\n\nArticle 3 Tasks\ncontent three"
	arts := SplitArticlesEU(md)
	if len(arts) != 3 || arts[2].No != "Article 6" {
		t.Errorf("前向引用行为应如注释所述: %+v", arts)
	}
	// 后向引用：Article 92 正文内行首引用 Article 6 → 必须丢弃
	md2 := "Article 91 Relations to other acts\nstart\n\nArticle 92 Derogations\nbegin\nArticle 6 references here\nend\n\nArticle 93 Committee\nx"
	arts2 := SplitArticlesEU(md2)
	if len(arts2) != 3 {
		t.Fatalf("条文数 = %d, 期望 3（行首 Article 6 应被递增过滤丢弃）: %+v", len(arts2), arts2)
	}
	if arts2[1].No != "Article 92" || !strings.Contains(arts2[1].Content, "Article 6 references here") {
		t.Errorf("引用文本应归入 Article 92: %+v", arts2[1])
	}
	if arts2[2].No != "Article 93" {
		t.Errorf("过滤后条文边界错位: %+v", arts2[2])
	}
}
