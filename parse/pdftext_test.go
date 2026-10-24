package parse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdata/text_layer.pdf：含中文文本层的样例（cupsfilter 生成）。
// 注意其文本层存在换行碎片化与部首码位映射，正是规范化与人工确认要兜底的场景。
func fixturePDF(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "testdata", "text_layer.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skip("缺少 testdata/text_layer.pdf")
	}
	return p
}

// TestExtractPDFText_TextLayer 文本层 PDF 应提取出正文（含部首码位变体）。
func TestExtractPDFText_TextLayer(t *testing.T) {
	text, err := ExtractPDFText(fixturePDF(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "公司股东") || !strings.Contains(text, "有限责任") {
		t.Fatalf("提取文本缺少关键内容: %q", text[:min(200, len(text))])
	}
}

// TestExtractPDFText_NotPDF 非 PDF 文件必须报错，不得静默返回空文本。
func TestExtractPDFText_NotPDF(t *testing.T) {
	if _, err := ExtractPDFText(filepath.Join("..", "testdata", "fake_law.pdf")); err == nil {
		t.Fatal("非 PDF 应报错")
	}
}

// TestNormalizeAndSplit 碎片化文本规范化后应能拆出全部条文（含部首码位标记）。
func TestNormalizeAndSplit(t *testing.T) {
	text, err := ExtractPDFText(fixturePDF(t))
	if err != nil {
		t.Fatal(err)
	}
	normalized := NormalizePDFText(text)
	arts := SplitArticles(normalized)
	if len(arts) != 2 {
		t.Fatalf("应拆出 2 条，实际 %d：%q", len(arts), normalized[:min(300, len(normalized))])
	}
	if !strings.Contains(arts[0].Content, "公司股东") {
		t.Fatalf("第一条内容异常: %q", arts[0].Content)
	}
	if !strings.Contains(arts[1].Content, "货币出资") {
		t.Fatalf("第二条内容异常: %q", arts[1].Content)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestNormalizeSequenceFilterCrossRef(t *testing.T) {
	// 交叉引用（罚则引用他条）不得被提升为条文起点——flk 存量实测的 UNIQUE 冲突根因
	in := "第一条　为规范测试，制定本法。第二条 违反第一条、第二条的，予以处罚。第三条 本法自2020年1月1日起施行。"
	arts := SplitArticles(NormalizePDFText(in))
	if len(arts) != 3 {
		t.Fatalf("应为 3 条（交叉引用不拆），实际 %d: %+v", len(arts), arts)
	}
	if !strings.Contains(arts[1].Content, "违反第一条、第二条的") {
		t.Fatalf("交叉引用应保留在第二条正文内: %q", arts[1].Content)
	}
}

func TestArticleNoValue(t *testing.T) {
	if n := len([]rune(kangxiTargets)); n != 214 {
		t.Fatalf("Kangxi 映射表应为 214 项，实际 %d", n)
	}
	for s, want := range map[string]int{
		"一": 1, "十": 10, "二十": 20, "二十三": 23, "一百二十三": 123,
		"二百": 200, "一千零五": 1005, "23": 23, "⼆": 2, // ⼆ = Kangxi 部首「二」
	} {
		if n, ok := articleNoValue(s); !ok || n != want {
			t.Fatalf("articleNoValue(%q) = %d,%v want %d", s, n, ok, want)
		}
	}
	if _, ok := articleNoValue("甲乙"); ok {
		t.Fatal("非数字应拒绝")
	}
}
