package parse

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

// PDF 文本层提取（开发文档 3.2 文本层、6.1 解析正文）。
// 仅处理含文本层的 PDF；扫描件（无文本层）明确报错转 OCR（OL-1009），绝不静默入库。
// 依赖 ledongthuc/pdf（纯 Go，与 OpenCourt 同款）。
//
// 保真度告警：文本层提取可能出现换行碎片化与部首码位映射（如 ⼆ ≠ 二），
// 自动提取的条目 confirm_status 必须为 pending，由人工比对公报原文后确认。

// ExtractPDFText 逐页提取文本层，返回原始拼接文本。
// 无文本层（扫描件）返回错误，调用方必须转 OCR 或人工。
func ExtractPDFText(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("PDF 解析失败（可能损坏或非 PDF）: %w", err)
	}
	defer f.Close()

	var b bytes.Buffer
	pages := r.NumPage()
	for i := 1; i <= pages; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("PDF 第 %d 页文本提取失败: %w", i, err)
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	if strings.TrimSpace(b.String()) == "" {
		return "", fmt.Errorf("PDF 无文本层（扫描件），必须经 OCR 或人工录入（OL-1009），禁止静默入库")
	}
	return b.String(), nil
}

// NormalizePDFText 将提取文本规整为可拆分的正文：
// 去除全部空白（修复逐字换行碎片），再在每个条文标记前重插换行锚点。
// 注：中文场景去空白安全；纯西文文本会被连写，属已知限制。
func NormalizePDFText(text string) string {
	collapsed := strings.Join(strings.Fields(text), "")
	var b strings.Builder
	last := 0
	for _, loc := range articleMarkerRe.FindAllStringIndex(collapsed, -1) {
		b.WriteString(collapsed[last:loc[0]])
		b.WriteString("\n")
		last = loc[0]
	}
	b.WriteString(collapsed[last:])
	return b.String()
}
