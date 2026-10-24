package parse

import (
	"fmt"
	"strings"
	"unicode"
)

// InferDates 从法规正文保守推断公布日/生效日（存量文件名缺日期段的兜底通道）。
//
// 规则（全部命中才可用，否则 ok=false 转人工，绝不猜）：
//   - 生效日：日期后紧跟「(起)施行/生效/执行」→ 取最后一个命中（附则通常位于末尾）；
//   - 公布日：日期前 80 rune 或后 30 rune 内出现「通过/批准/公布/发布/签署/修订/修正」
//     → 取最后一个命中（版本落款通常在文末）；
//   - 仅有公布日时，生效日暂按公布日填充（调用方必须标记 inferred 并转人工确认）；
//   - 日期合法性：1949–2030 年、月日范围校验，非法日期直接跳过。
func InferDates(text string) (publish, effective string, ok bool) {
	rs := []rune(text)
	n := len(rs)
	isDigit := func(r rune) bool { return unicode.IsDigit(r) }
	readNum := func(i, maxDigits int) (val, end, found int) {
		v, cnt := 0, 0
		for i < n && isDigit(rs[i]) && cnt < maxDigits {
			v = v*10 + int(rs[i]-'0')
			i++
			cnt++
		}
		return v, i, cnt
	}
	skipSpaces := func(i int) int {
		for i < n && unicode.IsSpace(rs[i]) {
			i++
		}
		return i
	}
	// matchKW 在 [lo,hi) 窗口内查找关键词
	matchKW := func(lo, hi int, kws ...string) bool {
		if lo < 0 {
			lo = 0
		}
		if hi > n {
			hi = n
		}
		win := string(rs[lo:hi])
		for _, k := range kws {
			if strings.Contains(win, k) {
				return true
			}
		}
		return false
	}
	pubKws := []string{"通过", "批准", "公布", "发布", "签署", "修订", "修正"}
	for i := 0; i < n; i++ {
		if !isDigit(rs[i]) {
			continue
		}
		y, j, f1 := readNum(i, 4)
		if f1 == 0 || j >= n || rs[j] != '年' {
			continue
		}
		mo, k, f2 := readNum(skipSpaces(j+1), 2)
		if f2 == 0 || k >= n || rs[k] != '月' {
			continue
		}
		d, e, f3 := readNum(skipSpaces(k+1), 2)
		if f3 == 0 || e >= n || rs[e] != '日' {
			continue
		}
		if y < 1949 || y > 2030 || mo < 1 || mo > 12 || d < 1 || d > 31 {
			i = e
			continue
		}
		iso := fmt.Sprintf("%04d-%02d-%02d", y, mo, d)
		after := skipSpaces(e + 1)
		switch {
		case after < n && rs[after] == '起' && after+1 < n &&
			(rs[after+1] == '施' || rs[after+1] == '生' || rs[after+1] == '执'):
			effective = iso
		case after+1 < n && rs[after] == '施' && rs[after+1] == '行':
			effective = iso
		case after+1 < n && rs[after] == '生' && rs[after+1] == '效':
			effective = iso
		case after+1 < n && rs[after] == '执' && rs[after+1] == '行':
			effective = iso
		}
		if matchKW(i-80, i, pubKws...) || matchKW(e+1, e+31, pubKws...) {
			publish = iso
		}
		i = e
	}
	if effective == "" && publish != "" {
		effective = publish
	}
	return publish, effective, effective != ""
}
