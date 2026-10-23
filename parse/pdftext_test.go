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
