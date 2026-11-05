// subjectinfer.go：从法规名称保守推断部门法与文档形态（docs/design-taxonomy.md §5）。
// 规则只取高置信名称模式；无法判定一律留空（绝不猜）——推断结果属机器建议，
// subject_reviewer 留空 = 待复核，界面必须如实显示「机器建议 · 待复核」。
// 部门法规则仅适用中国法域：EU/US 名称模式与中文关键词不可靠，一律转智能体/人工队列。
package parse

import "strings"

// InferSubject 按名称推断部门法；返回词表码与命中规则号（0 = 无法判定）。
// 规则顺序即置信度顺序（设计 §5 保守枚举），先强后弱：
// S1 宪法 → CN-CON；S2 刑法/刑事 → CN-CRI；S3 民事/商事/公司/合同 → CN-CIV；
// S4 劳动/社会保险/未成年人 → CN-SOC；S5 税/反垄断/价格/审计 → CN-ECO；
// S6 诉讼/仲裁/程序 → CN-PRO；S7 处罚/治安/环境管理/许可 → CN-ADM。
// 顺序保证「劳动争议调解仲裁法」按社会法（S4）而非程序法（S6）——与官方口径一致。
func InferSubject(name, jurisdiction string) (code string, rule int) {
	if jurisdiction != "CN" {
		return "", 0 // 法域未标注或非中国法：中文关键词规则不适用，绝不猜
	}
	n := strings.TrimRight(strings.TrimSpace(name), "_ ") // 文件名产物尾巴不参与判定
	switch {
	case strings.Contains(n, "宪法"):
		return "CN-CON", 1
	case strings.Contains(n, "刑法"), strings.Contains(n, "刑事"):
		return "CN-CRI", 2
	case strings.Contains(n, "民事"), strings.Contains(n, "商事"),
		strings.Contains(n, "公司"), strings.Contains(n, "合同"):
		return "CN-CIV", 3
	case strings.Contains(n, "劳动"), strings.Contains(n, "社会保险"),
		strings.Contains(n, "未成年人"):
		return "CN-SOC", 4
	case strings.Contains(n, "税"), strings.Contains(n, "反垄断"),
		strings.Contains(n, "价格"), strings.Contains(n, "审计"):
		return "CN-ECO", 5
	case strings.Contains(n, "诉讼"), strings.Contains(n, "仲裁"),
		strings.Contains(n, "程序"):
		return "CN-PRO", 6
	case strings.Contains(n, "处罚"), strings.Contains(n, "治安"),
		strings.Contains(n, "环境管理"), strings.Contains(n, "许可"):
		return "CN-ADM", 7
	}
	return "", 0
}

// InferDocForm 按标题后缀/文种推断文档形态（全自动，拍板项 2）；
// 返回词表码与命中规则号（0 = 判不出，留空计入待办，随规则迭代收敛）。
// CN：F1 法典 / F2 修正案 / F3 条例 / F4 细则 / F5 规则 / F6 办法 / F7 规定 /
// F8 批复 / F9 决定 / F10 解释 / F11 通知 / F12 以「法」结尾。
// 顺序即优先级：「办法」先于「法」判尾，避免「XX办法」误判为「法」。
// EU：F13 条例 / F14 指令 / F15 决定（中文译名后缀）。
// US：F16 Act / F17 Public Law / F18 Executive Order。
func InferDocForm(name, jurisdiction string) (code string, rule int) {
	n := strings.TrimRight(strings.TrimSpace(name), "_ ")
	switch jurisdiction {
	case "CN":
		switch {
		case strings.HasSuffix(n, "法典"):
			return "CN-CODE", 1
		case strings.Contains(n, "修正案"):
			return "CN-AMD", 2
		case strings.HasSuffix(n, "条例"):
			return "CN-REG", 3
		case strings.HasSuffix(n, "细则"):
			return "CN-DET", 4
		case strings.HasSuffix(n, "规则"):
			return "CN-RUL", 5
		case strings.HasSuffix(n, "办法"):
			return "CN-MEA", 6
		case strings.HasSuffix(n, "规定"):
			return "CN-PRO", 7
		case strings.HasSuffix(n, "批复"):
			return "CN-REP", 8
		case strings.HasSuffix(n, "决定"):
			return "CN-DEC", 9
		case strings.HasSuffix(n, "解释"):
			return "CN-INT", 10
		case strings.HasSuffix(n, "通知"):
			return "CN-NOT", 11
		case len([]rune(n)) > 2 && strings.HasSuffix(n, "法"):
			return "CN-LAW", 12
		}
	case "EU":
		switch {
		case strings.HasSuffix(n, "条例"):
			return "EU-REG", 13
		case strings.HasSuffix(n, "指令"):
			return "EU-DIR", 14
		case strings.HasSuffix(n, "决定"):
			return "EU-DEC", 15
		}
	case "US":
		switch {
		case strings.Contains(n, "Executive Order"):
			return "US-EO", 18
		case strings.Contains(n, "Public Law"):
			return "US-PL", 17
		case strings.HasSuffix(n, " Act"), strings.Contains(n, " Act of "):
			return "US-ACT", 16
		}
	}
	return "", 0
}
