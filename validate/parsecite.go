// parsecite.go 引用文本解析与「粘贴即核验」（产品评审 P0-2：招牌流程）。
//
// 律师手头的引用是一段**自由文字**（判决书、合同条款、备忘录里的「《民法典》第584条，
// 2021-01-01」），不是四个字段。过去的核验要求用户自己拆成法规名/条号/引文/时点，
// 等于把产品的核心能力摊派给用户。本文件负责把自由文字认成要素，再交同一条
// CheckCitation 六环节链路判定——**解析只负责认出要素，判定一律由既有链路做出**，
// 认不出的要素如实说明并给出可照做的下一步（诚实展示纪律）。
package validate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
)

// 法规名称的识别依据（界面据此说明「凭什么认定」，不粉饰）。
const (
	FromBookTitle  = "book-title"  // 书名号《…》内文字
	FromCorpusName = "corpus-name" // 库内法规名/简称在文中出现（最长匹配）
	FromCorpusText = "corpus-text" // 库内别名表命中（出处见 CitationResult.AliasSource）
	FromNone       = ""            // 没认出来
)

// 核验结论的六态（界面必须逐态给出不同下一步，不得一律显示「未找到」）。
const (
	VerdictVerified      = "verified"       // 命中且文本一致
	VerdictQuoteMismatch = "quote_mismatch" // 命中条文但引文与原文差异大（OL-1004）
	VerdictNotFound      = "not_found"      // 法名/时点/条号有一项对不上（OL-1001/2/3）
	VerdictAmbiguous     = "ambiguous"      // 同名多法无法唯一确定（OL-1010）
	VerdictExpired       = "expired"        // 所引版本已失效（OL-1005）
	VerdictBrokenChain   = "broken_chain"   // 溯源断链：不得给出结论（OL-1006）
	VerdictIncomplete    = "incomplete"     // 文字里认不出法规名，无法核验
)

var (
	bookTitleRe   = regexp.MustCompile(`《([^》\n]{2,80})》`)
	articleMarkRe = regexp.MustCompile(`第\s*[一二三四五六七八九十百千零〇两0-9\x{2e80}-\x{2fdf}]+\s*条`)
	isoDateRe     = regexp.MustCompile(`\d{4}-\d{1,2}-\d{1,2}`)
	cnDateRe      = regexp.MustCompile(`(\d{4})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*日`)
	spaceRunRe    = regexp.MustCompile(`[ \t]+`)
	punctStripRe  = regexp.MustCompile(`[\s　]+`)
	quotePairs    = [][2]string{{"“", "”"}, {"「", "」"}, {"『", "』"}}
)

// ParsedCitation 从一段自由文字里认出的引用要素。
// Recognized/Missing 是**给用户看的白话**：认出了什么（含依据）、没认出什么（含怎么补）。
type ParsedCitation struct {
	Raw            string   `json:"raw"`
	LawName        string   `json:"law_name"`
	LawNameFrom    string   `json:"law_name_from"`
	ArticleNo      string   `json:"article_no"`      // 交核验链路的条号（尽量库内写法）
	ArticleWritten string   `json:"article_written"` // 用户原文写法（可能阿拉伯数字）
	ArticleNumber  int      `json:"article_number"`  // 条号数字值；0=未识别
	ArticleNote    string   `json:"article_note"`    // 写法与库内不一致时的如实说明
	AtDate         string   `json:"at_date"`         // 空=用户未给（核验按今天，必须声明）
	Quote          string   `json:"quote"`
	Recognized     []string `json:"recognized"`
	Missing        []string `json:"missing"`
}

// ParseCitationText 从自由文字里认引用要素。db 可为 nil（无库时只做文本层识别，
// 并如实说明「未与法规库比对名称」——不得装作已确认法规名）。
func ParseCitationText(db *store.DB, text string) ParsedCitation {
	raw := strings.TrimSpace(parse.FoldKangxi(text))
	p := ParsedCitation{Raw: raw}
	if raw == "" {
		p.Missing = append(p.Missing, "没有可解析的内容——请粘贴包含法规名称的一段文字。")
		return p
	}

	// ① 法规名称：书名号优先；没有书名号则在库内做最长名称/简称/别名匹配。
	if m := bookTitleRe.FindStringSubmatch(raw); len(m) == 2 {
		p.LawName = punctStripRe.ReplaceAllString(m[1], "")
		p.LawNameFrom = FromBookTitle
		p.Recognized = append(p.Recognized, "法规名称《"+p.LawName+"》（按书名号识别）")
	} else if name, from, ok := findLawNameInText(db, raw); ok {
		p.LawName, p.LawNameFrom = name, from
		how := "按库内已登记的名称/简称在文中匹配"
		if from == FromCorpusText {
			how = "按库内别名表在文中匹配"
		}
		p.Recognized = append(p.Recognized, "法规名称《"+name+"》（"+how+"，请确认为所指法规）")
	} else {
		p.Missing = append(p.Missing, "没能从这段文字里认出法规名称。请写明全称或加书名号，例如《中华人民共和国民法典》。")
	}

	// ② 条款号：按**数字**比对，写法差异不得变成「找不到」。
	if m := articleMarkRe.FindString(raw); m != "" {
		p.ArticleWritten = spaceRunRe.ReplaceAllString(m, "")
		if n, ok := parse.ArticleNumber(p.ArticleWritten); ok {
			p.ArticleNumber = n
			p.ArticleNo = "第" + parse.ChineseNumber(n) + "条"
			if p.ArticleNo != p.ArticleWritten {
				p.ArticleNote = fmt.Sprintf("您写的「%s」按数字对应为「%s」后再比对", p.ArticleWritten, p.ArticleNo)
			}
			p.Recognized = append(p.Recognized, "条款号 "+p.ArticleNo+"（第 "+fmt.Sprint(n)+" 条）")
		} else {
			p.ArticleNo = p.ArticleWritten
			p.Recognized = append(p.Recognized, "条款号 "+p.ArticleWritten+"（数字无法确定，按原样比对）")
		}
	} else {
		p.Missing = append(p.Missing, "没能认出条款号。请写明「第×条」，否则只能核验到法规层面。")
	}

	// ③ 引用时点：两种写法都认；认不出就按今天，并如实声明（结论与时点有关）。
	if m := cnDateRe.FindStringSubmatch(raw); len(m) == 4 {
		p.AtDate = fmt.Sprintf("%s-%02d-%02d", m[1], atoiSafe(m[2]), atoiSafe(m[3]))
	} else if isoDateRe.MatchString(raw) {
		p.AtDate = normalizeISODate(isoDateRe.FindString(raw))
	}
	if p.AtDate != "" {
		p.Recognized = append(p.Recognized, "引用时点 "+p.AtDate)
	}

	// ④ 引文：引号内的内容优先；没有引号时不猜，只核验到条文是否存在的层面。
	p.Quote = extractQuote(raw)
	if p.Quote != "" {
		p.Recognized = append(p.Recognized, fmt.Sprintf("引文 %d 字（用于与留证原文逐字比对）", len([]rune(p.Quote))))
	} else if len([]rune(raw)) > 60 {
		p.Missing = append(p.Missing, "这段文字较长但没有引号，无法确定哪一句是引文——本次只核验到条文是否存在的层面。")
	}
	return p
}

// VerifyResult 「粘贴即核验」的结构化结论：解析要素 + 六环节判定 + 下一步。
type VerifyResult struct {
	Parsed    ParsedCitation `json:"parsed"`
	Verdict   string         `json:"verdict"`
	Citation  CitationResult `json:"citation"`
	NextSteps []string       `json:"next_steps"`
}

// VerifyCitationText 解析自由文字并走 CheckCitation 同一条链路（6.2）。
// 判定不在此处重做：本函数只负责把要素认出来、按数字复核条号、把结论翻译成人话。
// atDateOverride 非空时优先于文中认出的时点（用户在界面上显式指定，须声明来源）。
func VerifyCitationText(db *store.DB, dataDir, text, atDateOverride, today string) VerifyResult {
	p := ParseCitationText(db, text)
	res := VerifyResult{Parsed: p}

	if p.LawName == "" {
		res.Verdict = VerdictIncomplete
		res.NextSteps = []string{
			"补上法规名称（全称或《书名号》内名称）后重试。",
			"不确定是哪部法规时，可先到「条文检索」用关键词找到条文，再回来核对原文。",
		}
		return res
	}

	atDate, dateNote := p.AtDate, ""
	switch {
	case normalizeISODate(atDateOverride) != "":
		atDate = normalizeISODate(atDateOverride)
		dateNote = "已按您指定的引用时点（" + atDate + "）判定。"
	case atDate == "":
		atDate, dateNote = today, "您未给出引用时点，已按本机今天（"+today+"）判定。"
	}

	cit, err := CheckCitation(db, dataDir, p.LawName, p.ArticleNo, p.Quote, atDate, today)
	if err != nil {
		res.Verdict = VerdictNotFound
		res.NextSteps = []string{"查询失败：" + err.Error(),
			"请确认法规库可正常读取（「维护法规库 → 库总览」查看状态）。"}
		return res
	}

	// 条号写法差异：按数字在**同一生效版本**内比对；对上就如实说明是怎么对上的。
	if cit.ErrorCode == CodeArticleNotFound && p.ArticleNumber > 0 {
		if got, ok := matchArticleByNumber(db, p.LawName, atDate, p.ArticleNumber); ok && got != p.ArticleNo {
			p.ArticleNo, p.ArticleNote = got,
				fmt.Sprintf("您写的「%s」在本版本文本里写作「%s」，已按条号数字对应", p.ArticleWritten, got)
			res.Parsed = p
			if cit2, err2 := CheckCitation(db, dataDir, p.LawName, got, p.Quote, atDate, today); err2 == nil {
				cit = cit2
			}
		}
	}
	res.Citation = cit

	switch {
	case len(cit.ChainIssues) > 0:
		res.Verdict = VerdictBrokenChain
		res.NextSteps = []string{
			"溯源链有断点，本次不给出「引用正确」的结论（OL-1006）。",
			"请在「维护法规库 → 库总览」运行数据自检，按断链清单补齐留证原文。",
		}
	case cit.ErrorCode == CodeAmbiguousLawName:
		res.Verdict = VerdictAmbiguous
		res.NextSteps = []string{
			"该名称对应多部同名法规，系统不自动采信其中任何一部。",
			"请改用完整名称（含制定机关或发文字号），或补上引用时点后重试。",
		}
	case !cit.Hit:
		res.Verdict = VerdictNotFound
		res.NextSteps = notFoundSteps(cit.ErrorCode, p)
	case cit.ErrorCode == CodeQuoteMismatch:
		res.Verdict = VerdictQuoteMismatch
		res.NextSteps = []string{
			"条文找到了，但您手头的文字与留证原文差异较大——请以原文为准逐字比对。",
			"若差异来自省略号或跨款节选，属正常；若是措辞不同，请以官方原文替换后再引用。",
		}
	case cit.Expired:
		res.Verdict = VerdictExpired
		res.NextSteps = []string{
			"所引版本在所判定时点之后已失效，请改引现行版本条文。",
			"在「修订沿革」中确认现行版本，并核对同一内容在现行文本中的条号。",
		}
	default:
		res.Verdict = VerdictVerified
		res.NextSteps = []string{"可在右侧查看留证原文并复制；引用时请附条目编号与版本，便于他人复验。"}
	}
	if p.ArticleNote != "" {
		res.NextSteps = append([]string{p.ArticleNote}, res.NextSteps...)
	}
	if dateNote != "" {
		res.NextSteps = append([]string{dateNote}, res.NextSteps...)
	}
	return res
}

func notFoundSteps(code string, p ParsedCitation) []string {
	switch code {
	case CodeLawNotFound:
		return []string{
			"法规库中没有这个名称。可能是简称未登记、或该法规尚未导入本机。",
			"可在「按条件筛选」按名称片段查找；确认缺失时到「获取最新法规」从官方渠道获取。",
		}
	case CodeNoVersionAtDate:
		return []string{
			"该法规在您给的时点没有生效版本——请核对该日期是否早于施行日或晚于失效日。",
			"在「修订沿革」中查看各版本的施行与失效日期后再重试。",
		}
	case CodeArticleNotFound:
		note := "该版本中没有这个条号。"
		if p.ArticleNumber > 0 {
			note += fmt.Sprintf("已按数字（第 %d 条）在该版本内比对仍无结果。", p.ArticleNumber)
		}
		return []string{
			note + "可能是该版本未分条（决定/决议类文本），或条号有误。",
			"可在「条文检索」中按法规名 + 关键词定位实际条文。",
		}
	}
	return []string{"请核对法规名称、条款号与引用时点后重试。"}
}

// findLawNameInText 在库内做最长名称/简称/别名匹配（文中出现即候选，取最长者）。
// 返回的始终是**法规全称**（law.name）——简称/别名只用于认定，不作为核验入参。
// 库不可用或表缺失时如实返回 not-ok——绝不拿文中任意片段当法规名（那是猜）。
func findLawNameInText(db *store.DB, text string) (name, from string, ok bool) {
	if db == nil {
		return "", FromNone, false
	}
	const base = `SELECT l.name, c.src FROM (
		SELECT law_id, 'corpus-name' AS src, name AS hit FROM law WHERE COALESCE(name,'') <> '' AND INSTR(?, name) > 0
		UNION ALL
		SELECT law_id, 'corpus-name' AS src, short_name AS hit FROM law WHERE COALESCE(short_name,'') <> '' AND INSTR(?, short_name) > 0`
	const tail = `) c JOIN law l ON l.law_id = c.law_id ORDER BY LENGTH(c.hit) DESC LIMIT 1`
	if hasAliasTable(db) {
		return scanLongestName(db, base+`
		UNION ALL
		SELECT law_id, 'corpus-text' AS src, alias AS hit FROM law_alias WHERE INSTR(?, alias) > 0`+tail, text, text, text)
	}
	return scanLongestName(db, base+tail, text, text)
}

func scanLongestName(db *store.DB, query string, args ...any) (string, string, bool) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return "", FromNone, false
	}
	defer rows.Close()
	if rows.Next() {
		var name, src string
		if err := rows.Scan(&name, &src); err == nil && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name), src, true
		}
	}
	return "", FromNone, false
}

func hasAliasTable(db *store.DB) bool {
	var n int
	return db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='law_alias'`).Scan(&n) == nil && n > 0
}

// matchArticleByNumber 在该时点生效版本内按条号数字找条文（写法差异兜底）。
func matchArticleByNumber(db *store.DB, lawName, atDate string, want int) (string, bool) {
	rv, _, err := ResolveNamedVersion(db, lawName, atDate)
	if err != nil || rv == nil {
		return "", false
	}
	rows, err := db.Query(`SELECT article_no FROM article WHERE version_id=?`, rv.VersionID)
	if err != nil {
		return "", false
	}
	defer rows.Close()
	for rows.Next() {
		var no string
		if err := rows.Scan(&no); err != nil {
			continue
		}
		if n, ok := parse.ArticleNumber(no); ok && n == want {
			return no, true
		}
	}
	return "", false
}

// extractQuote 引号内的内容优先；没有引号时**不猜**（返回空，只核验条文存在性）。
func extractQuote(text string) string {
	for _, pr := range quotePairs {
		i := strings.Index(text, pr[0])
		if i < 0 {
			continue
		}
		rest := text[i+len(pr[0]):]
		if j := strings.Index(rest, pr[1]); j > 0 {
			if q := strings.TrimSpace(rest[:j]); len([]rune(q)) >= 8 {
				return q
			}
		}
	}
	return ""
}

// normalizeISODate 把 2021-1-1 补零成 2021-01-01（左闭右开区间比较依赖统一格式）。
// 整串必须就是一个日期：夹带别的内容或月份/日期越界一律返回空串——
// 脏值参与版本判定比不判定更危险（会静默选错生效版本）。
func normalizeISODate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || isoDateRe.FindString(s) != s {
		return ""
	}
	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return ""
	}
	y, m, d := atoiSafe(parts[0]), atoiSafe(parts[1]), atoiSafe(parts[2])
	if y < 1900 || y > 2999 || m < 1 || m > 12 || d < 1 || d > 31 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", y, m, d)
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
