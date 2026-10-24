package parse

import (
	"archive/zip"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// docx 正文提取（flk 批量下载存量以 .docx 为主）。
// docx = zip 容器：读 word/document.xml，按 <w:t> 节点顺序取文本、</w:p> 处断段。
// .doc 遗留二进制格式不支持（无稳定开源解析），须先 textutil -convert docx 离线转换。

var docxWTRe = regexp.MustCompile(`(?s)<w:t(?:\s[^>]*)?>(.*?)</w:t>`)

// ExtractDocxText 提取 docx 正文（段落间以换行分隔）。无正文返回错误，禁止静默入库。
func ExtractDocxText(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("docx 解析失败（可能损坏或为 .doc 遗留格式）: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("读取 word/document.xml 失败: %w", err)
		}
		xml, err := io.ReadAll(io.LimitReader(rc, 64<<20))
		rc.Close()
		if err != nil {
			return "", fmt.Errorf("读取 word/document.xml 失败: %w", err)
		}
		return docxTextFromXML(xml)
	}
	return "", fmt.Errorf("docx 缺少 word/document.xml（非法 docx）")
}

// docxTextFromXML 从 document.xml 字节提取纯文本。
func docxTextFromXML(xml []byte) (string, error) {
	var b strings.Builder
	for _, para := range strings.Split(string(xml), "</w:p>") {
		for _, m := range docxWTRe.FindAllStringSubmatch(para, -1) {
			b.WriteString(xmlUnescape(m[1]))
		}
		b.WriteString("\n")
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("docx 无正文文本（空文档或纯对象嵌入），须人工处理，禁止静默入库")
	}
	return out, nil
}

var xmlEntities = strings.NewReplacer(
	"&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&nbsp;", " ", "&amp;", "&")

// xmlUnescape 处理命名实体与 &#...; 数字实体。
func xmlUnescape(s string) string {
	s = xmlEntities.Replace(s)
	for {
		i := strings.Index(s, "&#")
		if i < 0 {
			return s
		}
		end := strings.Index(s[i:], ";")
		if end < 0 {
			return s
		}
		body := s[i+2 : i+end]
		base, digits := 10, body
		if strings.HasPrefix(body, "x") || strings.HasPrefix(body, "X") {
			base, digits = 16, body[1:]
		}
		n, err := strconv.ParseInt(digits, base, 32)
		if err != nil || n < 0 {
			return s
		}
		s = s[:i] + string(rune(n)) + s[i+end+1:]
	}
}
