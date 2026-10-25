// levelinfer.go：从法规名称保守推断效力等级（flk 存量无元数据时的补标通道）。
// flk（国家法律法规数据库）仅收录五类：宪法、法律、行政法规、地方性法规、司法解释。
// 规则只取高置信名称模式；无法判定一律留空（绝不猜）。推断结果属机器建议：
// law.remark 留痕规则号，等待人工 confirm 复核（confirm 流程可纠正）。
package parse

import (
	"regexp"
	"strings"
)

// 省级行政区全称前缀（34 个；名称以此开头 → 地方性法规）
var provincePrefixes = []string{
	"北京市", "天津市", "河北省", "山西省", "内蒙古自治区", "辽宁省", "吉林省", "黑龙江省",
	"上海市", "江苏省", "浙江省", "安徽省", "福建省", "江西省", "山东省", "河南省",
	"湖北省", "湖南省", "广东省", "广西壮族自治区", "海南省", "重庆市", "四川省", "贵州省",
	"云南省", "西藏自治区", "陕西省", "甘肃省", "青海省", "宁夏回族自治区", "新疆维吾尔自治区",
	"香港特别行政区", "澳门特别行政区", "台湾", // 「台湾」仅用于涉台法规名称，不视为本地立法则不标（见规则内处理）
}

// 设区的市/地区开头：1-7 个汉字后紧跟「市/盟/地区」（如 吉林市…、呼伦贝尔盟…、阿里地区…）。
// 国家层面名称无以此开头的（国务院/全国人大/两高已在前序规则截获）。
var cityPrefixRe = regexp.MustCompile(`^[\p{Han}]{1,7}(市|盟|地区)`)

// InferEffectLevel 按名称推断效力等级；返回等级与命中规则号（0=无法判定）。
// 规则顺序即置信度顺序，先强后弱，避免「吉林省吉林市…」误判。
func InferEffectLevel(name string) (level string, rule int) {
	n := strings.TrimRight(strings.TrimSpace(name), "_ ") // 文件名产物尾巴（`名称_.docx`）不参与判定
	// 宪法自成一类（含修正案、修正文本）
	if strings.HasPrefix(n, "中华人民共和国宪法") {
		return "宪法", 8
	}
	switch {
	case strings.HasPrefix(n, "最高人民法院"), strings.HasPrefix(n, "最高人民检察院"):
		return "司法解释", 1
	case strings.HasPrefix(n, "国务院"):
		// 国办文件效力等级存疑（规范性文件），不猜，留人工
		if strings.HasPrefix(n, "国务院办公厅") {
			return "", 0
		}
		return "行政法规", 2
	case strings.HasPrefix(n, "全国人民代表大会"), strings.HasPrefix(n, "全国人大常委会"):
		return "法律", 3
	}
	for _, p := range provincePrefixes {
		if strings.HasPrefix(n, p) {
			// 「台湾」开头非地方立法命名习惯（涉台决定多由全国人大/常委会发布，已被规则 3 截获）
			if p == "台湾" {
				return "", 0
			}
			return "地方性法规", 4
		}
	}
	if strings.Contains(n, "自治州") || strings.Contains(n, "自治县") ||
		strings.Contains(n, "自治旗") || strings.Contains(n, "经济特区") {
		return "地方性法规", 5
	}
	if cityPrefixRe.MatchString(n) {
		return "地方性法规", 6
	}
	// 「中华人民共和国X法」精确结尾 → 法律（宪法自成一类不标；「…法实施条例/办法」不是法律，排除）
	if n != "中华人民共和国宪法" &&
		strings.HasPrefix(n, "中华人民共和国") && strings.HasSuffix(n, "法") &&
		!strings.Contains(n, "条例") && !strings.Contains(n, "办法") && !strings.Contains(n, "决议") && !strings.Contains(n, "决定") {
		return "法律", 7
	}
	return "", 0
}

// IsLocalLevel 该等级是否应置 law.is_local=1。
func IsLocalLevel(level string) bool { return level == "地方性法规" }
