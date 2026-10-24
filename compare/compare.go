package compare

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/futuremeng/OpenLaw/parse"
)

// 跨源一致性核验（compare）：对同一法律在不同官方来源的文件做双重复核：
//   1. 字节级：SHA-256（不同来源的 PDF 生成器不同，字节级几乎必然不一致，仅作记录）；
//   2. 文本级：提取文本层/HTML 正文 → 归一化（去全部空白 + Kangxi 部首码位映射）→
//      判断 完全一致 / 连续子串包含（单行法 PDF ⊂ 整期公报 PDF 属正常）/ 实质差异。
// 归一化仅折叠排版差异（空白、部首码位），不折叠标点与用字差异——
// 凡剩余差异一律如实报告并给出首个差异点上下文，供人工裁定。
// 支持 .pdf / .html / .htm / .md / .txt。
//
// 扫描图像版 PDF 无文本层（OL-1009），无法参与文本级核验——
// 已知两侧均存在扫描件：国务院公报整期 PDF 均为扫描版；flk 的单行 PDF 亦有一部分为扫描版。
// 替代路径：flk 用浏览器在线浏览页手动复制全文存为 .txt；gov.cn 用公报单文档 HTML 页；
// 两侧皆扫描且无 HTML 文字源时只能转 OCR（暂未实现），绝不静默入库。

// Normalize 文本级归一化：去除全部空白字符，并把 Kangxi 部首码位映射为 CJK 用字。
// 不做其他折叠（标点、繁简、异体字差异会如实保留为差异）。
// 部首码位表已下沉至 parse（parse.FoldKangxi），此处复用避免双表漂移。
func Normalize(text string) string {
	b := strings.Builder{}
	b.Grow(len(text))
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return parse.FoldKangxi(b.String())
}

var (
	scriptStyleRe = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	tagRe         = regexp.MustCompile(`(?s)<[^>]+>`)
)

// LoadText 按扩展名提取可比较文本：PDF 走文本层（无文本层报错转 OCR），
// HTML 去标签并解码实体，其余按纯文本读取。
func LoadText(path string) (string, error) {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".pdf"):
		return parse.ExtractPDFText(path)
	case strings.HasSuffix(lower, ".html"), strings.HasSuffix(lower, ".htm"):
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		s := extractMainContent(string(raw))
		s = scriptStyleRe.ReplaceAllString(s, " ")
		s = tagRe.ReplaceAllString(s, "\n")
		s = html.UnescapeString(s)
		return s, nil
	case strings.HasSuffix(lower, ".md"), strings.HasSuffix(lower, ".txt"):
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	default:
		return "", fmt.Errorf("不支持的文件类型（仅 .pdf/.html/.htm/.md/.txt）: %s", path)
	}
}

// extractMainContent 若为 TRS CMS 页面（gov.cn 公报等，正文容器 id="UCAP-CONTENT"
// 或 class="pages_content"），只取正文容器内部（按 div 配平），剔除页头页脚导航。
func extractMainContent(s string) string {
	i := strings.Index(s, `id="UCAP-CONTENT"`)
	if i < 0 {
		i = strings.Index(s, `class="pages_content"`)
	}
	if i < 0 {
		return s
	}
	open := strings.Index(s[i:], ">")
	if open < 0 {
		return s
	}
	pos := i + open + 1
	depth := 1
	for depth > 0 {
		nextOpen := strings.Index(s[pos:], "<div")
		nextClose := strings.Index(s[pos:], "</div>")
		if nextClose < 0 {
			return s[i+open+1:]
		}
		if nextOpen >= 0 && nextOpen < nextClose {
			depth++
			pos += nextOpen + 4
			continue
		}
		depth--
		if depth == 0 {
			return s[i+open+1 : pos+nextClose]
		}
		pos += nextClose + 6
	}
	return s
}

func sha256File(path string) (string, int64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), int64(len(raw)), nil
}

// Report 核验报告（JSON 友好）。
type Report struct {
	FileA string `json:"file_a"`
	FileB string `json:"file_b"`

	SHA256A        string `json:"sha256_a"`
	SHA256B        string `json:"sha256_b"`
	SizeA          int64  `json:"size_a"`
	SizeB          int64  `json:"size_b"`
	BytesIdentical bool   `json:"bytes_identical"`

	TextLenA      int     `json:"text_len_a"`
	TextLenB      int     `json:"text_len_b"`
	TextIdentical bool    `json:"text_identical"`
	Containment   string  `json:"containment"` // "" | "a_in_b" | "b_in_a"
	Similarity    float64 `json:"similarity"`  // 字符频率相似度 0..1（排版折叠后）

	FirstDiffPos int    `json:"first_diff_pos"` // 归一化文本首个差异位点；-1=无差异
	ContextA     string `json:"context_a,omitempty"`
	ContextB     string `json:"context_b,omitempty"`
	DiffLenA     int    `json:"diff_len_a"` // 中段差异区长度（掐去公共前后缀后）
	DiffLenB     int    `json:"diff_len_b"`

	CoverAInB int      `json:"cover_a_in_b"` // a 的句段在 b 中命中的数量
	TotalA    int      `json:"total_a_chunks"`
	CoverBInA int      `json:"cover_b_in_a"`
	TotalB    int      `json:"total_b_chunks"`
	Missing   []string `json:"missing_samples,omitempty"` // 未命中句段样例（≤5 条）

	Verdict string `json:"verdict"` // bytes_identical | identical | contained | covered | different
}

// Consistent 判定两文件文本是否可视为一致（文本一致、连续包含或句段全覆盖）。
func (r *Report) Consistent() bool {
	return r.TextIdentical || r.Containment != "" || r.Verdict == "covered"
}

// CompareFiles 对两个文件做字节级 + 文本级双重复核。
func CompareFiles(pathA, pathB string) (*Report, error) {
	rep := &Report{FileA: pathA, FileB: pathB, FirstDiffPos: -1}
	var err error
	rep.SHA256A, rep.SizeA, err = sha256File(pathA)
	if err != nil {
		return nil, fmt.Errorf("读取 A 失败: %w", err)
	}
	rep.SHA256B, rep.SizeB, err = sha256File(pathB)
	if err != nil {
		return nil, fmt.Errorf("读取 B 失败: %w", err)
	}
	rep.BytesIdentical = rep.SHA256A == rep.SHA256B

	ta, err := LoadText(pathA)
	if err != nil {
		return nil, fmt.Errorf("提取 A 文本失败: %w", err)
	}
	tb, err := LoadText(pathB)
	if err != nil {
		return nil, fmt.Errorf("提取 B 文本失败: %w", err)
	}
	na, nb := Normalize(ta), Normalize(tb)
	rep.TextLenA, rep.TextLenB = len([]rune(na)), len([]rune(nb))

	switch {
	case na == nb:
		rep.TextIdentical = true
		rep.Verdict = "identical"
	case strings.Contains(nb, na):
		rep.Containment = "a_in_b"
		rep.Verdict = "contained"
	case strings.Contains(na, nb):
		rep.Containment = "b_in_a"
		rep.Verdict = "contained"
	default:
		// 连续包含失败：多为页眉/页码等排版噪声插在正文流中（整期 PDF）或两侧各有页面装饰（HTML）。
		// 以句段覆盖率复核：一侧全部句段在另一侧命中（允许句内因分页插入噪声）→ 判 covered。
		ca, ta, missA := coverage(na, nb)
		cb, tb, missB := coverage(nb, na)
		rep.CoverAInB, rep.TotalA, rep.CoverBInA, rep.TotalB = ca, ta, cb, tb
		switch {
		case rep.TotalA > 0 && rep.CoverAInB == rep.TotalA:
			rep.Verdict = "covered"
		case rep.TotalB > 0 && rep.CoverBInA == rep.TotalB:
			rep.Verdict = "covered"
			rep.Missing = missB
		default:
			rep.Verdict = "different"
			rep.Missing = missA
			compareDiff(na, nb, rep)
		}
	}
	rep.Similarity = quickSimilarity(na, nb)
	if rep.BytesIdentical && rep.Verdict == "identical" {
		rep.Verdict = "bytes_identical"
	}
	return rep, nil
}

// compareDiff 掐公共前后缀，定位中段差异并给出上下文。
func compareDiff(a, b string, rep *Report) {
	ra, rb := []rune(a), []rune(b)
	p := 0
	for p < len(ra) && p < len(rb) && ra[p] == rb[p] {
		p++
	}
	s := 0
	for s < len(ra)-p && s < len(rb)-p && ra[len(ra)-1-s] == rb[len(rb)-1-s] {
		s++
	}
	rep.FirstDiffPos = p
	rep.DiffLenA = len(ra) - p - s
	rep.DiffLenB = len(rb) - p - s
	ctx := 40
	lo := p - ctx
	if lo < 0 {
		lo = 0
	}
	hiA, hiB := p+rep.DiffLenA+ctx, p+rep.DiffLenB+ctx
	if hiA > len(ra) {
		hiA = len(ra)
	}
	if hiB > len(rb) {
		hiB = len(rb)
	}
	rep.ContextA = string(ra[lo:hiA])
	rep.ContextB = string(rb[lo:hiB])
}

// quickSimilarity 字符频率相似度：2·Σmin(fa,fb)/(|a|+|b|)，0..1。
func quickSimilarity(a, b string) float64 {
	if a == b {
		return 1
	}
	fa := freq(a)
	fb := freq(b)
	var common, total float64
	for r, ca := range fa {
		total += float64(ca)
		if cb, ok := fb[r]; ok {
			if ca < cb {
				common += float64(ca)
			} else {
				common += float64(cb)
			}
		}
	}
	for _, cb := range fb {
		total += float64(cb)
	}
	if total == 0 {
		return 1
	}
	return 2 * common / total
}

// chunkSentences 按句末标点切分为句段（保留标点），用于跨页噪声下的覆盖率比较。
func chunkSentences(s string) []string {
	var out []string
	start := 0
	for i, r := range s {
		if strings.ContainsRune("。！？；", r) {
			out = append(out, s[start:i+len(string(r))])
			start = i + len(string(r))
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// coverage 计算 a 的句段在 b 中的覆盖情况。
// 整句未命中时尝试对半拆开再找——两处之间允许存在页眉/页码等插入噪声
// （整期公报 PDF 每页页眉页码会插在正文流中，属排版噪声而非内容差异）。
func coverage(a, b string) (covered, total int, missing []string) {
	for _, ch := range chunkSentences(a) {
		total++
		if strings.Contains(b, ch) {
			covered++
			continue
		}
		r := []rune(ch)
		if len(r) >= 8 {
			h := len(r) / 2
			if strings.Contains(b, string(r[:h])) && strings.Contains(b, string(r[h:])) {
				covered++
				continue
			}
		}
		if len(missing) < 5 {
			missing = append(missing, ch)
		}
	}
	return
}

func freq(s string) map[rune]int {
	m := map[rune]int{}
	for _, r := range s {
		m[r]++
	}
	return m
}
