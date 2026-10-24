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
