package parse

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// 日期统一标准化为 ISO 8601（YYYY-MM-DD），时效区间左闭右开
// [effective_date, expire_date)，未失效留空，不使用 9999 年占位（开发文档 4.2）。

var cnDateRe = regexp.MustCompile(`^(\d{4})年(\d{1,2})月(\d{1,2})日?$`)
var slashDateRe = regexp.MustCompile(`^(\d{4})/(\d{1,2})/(\d{1,2})$`)
var isoDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// NormalizeDate 将「2023年12月29日」「2023/12/29」「2023-12-29」统一为 ISO 格式。
func NormalizeDate(s string) (string, error) {
	s = trimBlankLines(s)
	if isoDateRe.MatchString(s) {
		// 校验分量合法性（13 月等非法日期必须拒绝）
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return "", fmt.Errorf("非法日期: %q", s)
		}
		return s, nil
	}
	m := cnDateRe.FindStringSubmatch(s)
	if m == nil {
		m = slashDateRe.FindStringSubmatch(s)
	}
	if m == nil {
		return "", fmt.Errorf("无法识别的日期格式: %q（支持 YYYY-MM-DD / YYYY年M月D日 / YYYY/M/D）", s)
	}
	y, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	d, _ := strconv.Atoi(m[3])
	if mo < 1 || mo > 12 || d < 1 || d > 31 {
		return "", fmt.Errorf("日期分量越界: %q", s)
	}
	return fmt.Sprintf("%04d-%02d-%02d", y, mo, d), nil
}

// ActiveAt 判断 ISO 日期 at 是否落在版本时效区间 [effective, expire) 内（左闭右开）。
// expire 为空串表示未失效。边界语义：生效当天命中，失效当天不命中。
func ActiveAt(effective, expire, at string) bool {
	if at < effective {
		return false
	}
	if expire != "" && at >= expire {
		return false
	}
	return true
}
