package parse

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeDocx(t *testing.T, docXML string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "law.docx")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(docXML)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractDocxText(t *testing.T) {
	p := writeDocx(t, `<w:document><w:body>`+
		`<w:p><w:r><w:t>第一条　为规范测试，制定本法。</w:t></w:r></w:p>`+
		`<w:p><w:r><w:rPr/><w:t>第二条 本</w:t></w:r><w:r><w:t>法适用于 &amp; &#26657;关机关。</w:t></w:r></w:p>`+
		`</w:body></w:document>`)
	got, err := ExtractDocxText(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "第一条　为规范测试，制定本法。\n第二条 本法适用于 & 校关机关。"
	if got != want {
		t.Fatalf("提取异常:\n got %q\nwant %q", got, want)
	}
}

func TestExtractDocxEmptyAndInvalid(t *testing.T) {
	if _, err := ExtractDocxText(writeDocx(t, `<w:document><w:body><w:p/></w:body></w:document>`)); err == nil {
		t.Fatal("空正文必须报错（禁止静默入库）")
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.docx")
	if err := os.WriteFile(bad, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractDocxText(bad); err == nil {
		t.Fatal("非 zip 容器必须报错")
	}
	// 缺少 word/document.xml 的合法 zip
	p := filepath.Join(dir, "noxml.docx")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("word/styles.xml")
	_, _ = w.Write([]byte("<x/>"))
	zw.Close()
	f.Close()
	if _, err := ExtractDocxText(p); err == nil {
		t.Fatal("缺少 word/document.xml 必须报错")
	}
}

func TestDocxSplitArticles(t *testing.T) {
	p := writeDocx(t, `<w:document><w:body>`+
		`<w:p><w:r><w:t>第一条　甲方应当履行义务。</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t>第二条 乙方有权解除合同。</w:t></w:r></w:p>`+
		`</w:body></w:document>`)
	text, err := ExtractDocxText(p)
	if err != nil {
		t.Fatal(err)
	}
	arts := SplitArticles(NormalizePDFText(text))
	if len(arts) != 2 || arts[0].No != "第一条" || arts[1].No != "第二条" {
		t.Fatalf("docx 条文拆分异常: %+v", arts)
	}
}
