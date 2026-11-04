package parse

import (
	"strconv"
	"strings"
	"unicode"
)

// Kangxi 部首码位折叠（U+2F00..U+2FDF → CJK 正文用字）。
// PDF 文本层常把 二 输出为 ⼆（U+2F06）等部首码位；条文标记识别与跨源比对都须先折叠。
// 此表由 compare 包下沉至此（compare 依赖 parse，避免反向依赖）。

const kangxiTargets = "一丨丶丿乙亅二亠人儿入八冂冖冫几凵刀力勹匕匚匸十卜卩厂厶又口囗土士夂夊夕大女子宀寸小尢尸屮山巛工己巾干幺广廴廾弋弓彐彡彳心戈戶手支攴文斗斤方无日曰月木欠止歹殳毋比毛氏气水火爪父爻爿片牙牛犬玄玉瓜瓦甘生用田疋疒癶白皮皿目矛矢石示禸禾穴立竹米糸缶网羊羽老而耒耳聿肉臣自至臼舌舛舟艮色艸虍虫血行衣襾見角言谷豆豕豸貝赤走足身車辛辰辵邑酉釆里金長門阜隶隹雨靑非面革韋韭音頁風飛食首香馬骨高髟鬥鬯鬲鬼魚鳥鹵鹿麥麻黃黍黑黹黽鼎鼓鼠鼻齊齒龍龜龠"

var kangxiMap = buildKangxiMap()

func buildKangxiMap() map[rune]rune {
	m := make(map[rune]rune, 214)
	r := 0x2F00
	for _, t := range kangxiTargets {
		if r > 0x2FDF {
			break
		}
		m[rune(r)] = t
		r++
	}
	return m
}

// FoldKangxi 把 Kangxi 部首码位映射为 CJK 正文用字（不改空白与其他字符）。
func FoldKangxi(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0x2F00 && r <= 0x2FDF {
			if t, ok := kangxiMap[r]; ok {
				r = t
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// articleNoValue 解析「第X条」中的 X 为整数（中文数字/阿拉伯数字，先折叠部首码位）。
// 解析失败返回 ok=false（调用方按原行为保守处理）。
func articleNoValue(s string) (int, bool) {
	s = FoldKangxi(s)
	if isASCIIDigits(s) {
		n, err := strconv.Atoi(s)
		return n, err == nil
	}
	digits := map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4,
		'五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	total, cur, seenAny := 0, 0, false
	for _, r := range s {
		switch {
		case r == '零' || r == '〇':
			continue
		case unicode.IsDigit(r): // 全角数字等保守拒绝
			return 0, false
		default:
			if d, ok := digits[r]; ok {
				cur, seenAny = d, true
				continue
			}
			var unit int
			switch r {
			case '十':
				unit = 10
			case '百':
				unit = 100
			case '千':
				unit = 1000
			default:
				return 0, false
			}
			if cur == 0 {
				cur = 1 // 十/十X：省略的一
			}
			total, cur, seenAny = total+cur*unit, 0, true
		}
	}
	return total + cur, seenAny && total+cur > 0
}

// ArticleNumber 把条号标记（「第二百六十四条」「第264条」，含 Kangxi 部首码位）
// 解析为整数；无法确定时返回 ok=false（调用方保守处理，不得猜）。
// 只认「第…条」完整标记：裸数字或省掉「条」的片段可能是章/节/款，不得当条号解析。
//
// 导出理由：用户手写的引用与库内写法常不一致（阿拉伯数字 vs 中文数字），
// 引用核验必须按**数字**比对条号，否则会把「库里找得到」错报成「找不到条文」。
func ArticleNumber(marker string) (int, bool) {
	s := FoldKangxi(strings.TrimSpace(marker))
	if !strings.HasPrefix(s, "第") || !strings.HasSuffix(s, "条") {
		return 0, false
	}
	// 标记内部不留空白（「第 584 条」这类手写排版常见）；库内条号从无空白。
	s = strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimPrefix(s, "第"), "条")), "")
	if s == "" {
		return 0, false
	}
	return articleNoValue(s)
}

// ChineseNumber 把 1..9999 写成中文条号用字（第五百八十四条）。
// 只用于**显示与检索建议**（把用户写的「第584条」提示成库内可能的写法），
// 不参与判定：判定一律走 ArticleNumber 的数字比对。
func ChineseNumber(n int) string {
	if n <= 0 || n > 9999 {
		return ""
	}
	digits := []rune("零一二三四五六七八九")
	units := []string{"", "十", "百", "千"}
	var b strings.Builder
	s := strconv.Itoa(n)
	zero := false
	for i, r := range s {
		d := int(r - '0')
		pos := len(s) - i - 1
		if d == 0 {
			if b.Len() > 0 {
				zero = true
			}
			continue
		}
		if zero {
			b.WriteString("零")
			zero = false
		}
		// 「一十X」按通行写法省作「十X」
		if !(d == 1 && pos == 1 && b.Len() == 0) {
			b.WriteRune(digits[d])
		}
		b.WriteString(units[pos])
	}
	return b.String()
}

func isASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
