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
// 去除全部空白（修复逐字换行碎片），再在条文标记前重插换行锚点。
// 注：中文场景去空白安全；纯西文文本会被连写，属已知限制。
//
// 序列过滤：只有条号严格递增的「第X条」才被提升为条文起点；
// 「违反本规定第七条、第八条的」等交叉引用不满足递增，保留在正文内——
// 否则同一 article_no 会被拆出多次，触发 UNIQUE 冲突（实网 flk 存量实测）。
func NormalizePDFText(text string) string {
	collapsed := strings.Join(strings.Fields(text), "")
	var keep []int
	last := 0 // 已采纳条号（0=尚未采纳任何标记）
	for _, loc := range articleMarkerRe.FindAllStringIndex(collapsed, -1) {
		body := strings.TrimSuffix(strings.TrimPrefix(collapsed[loc[0]:loc[1]], "第"), "条")
		n, ok := articleNoValue(body)
		if !ok || n > last {
			keep = append(keep, loc[0])
			if ok {
				last = n
			}
		}
	}
	var b strings.Builder
	prev := 0
	for _, i := range keep {
		b.WriteString(collapsed[prev:i])
		b.WriteString("\n")
		prev = i
	}
	b.WriteString(collapsed[prev:])
	return b.String()
}
