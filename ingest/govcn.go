package ingest

// 国家行政法规库（www.gov.cn/zhengce/xzfgk/）与国家规章库（www.gov.cn/zhengce/xxgk/gjgzk/）
// 客户端（docs/sources-govcn.md，2026-10-25/26 实查）。两库同一合规画像（与 flk / Cellar
// 并列的第三种形态：详情页可抓、清单不可程序发现），共用本模块，Kind 区分：
//   - www.gov.cn 主站 robots.txt：User-agent: *，/zhengce/ 不在 Disallow（仅禁 2016 旧路径），
//     详情页允许抓取；站点主动发布 4 个 sitemap 欢迎机器索引；
//   - sousuo.www.gov.cn（检索/清单 API）robots.txt：Disallow: / —— 自动化访问禁止；
//     国家规章库的 Athena 检索接口（sousuoht.www.gov.cn/athena/forward/<hash>）另带
//     RSA appKey 应用层鉴权——逆向鉴权属绕过技术措施，绝不尝试。清单一律由用户浏览器
//     浏览时导出（tools/govcn-xzfgk-export.user.js / tools/govgz-export.user.js，
//     只整理用户已浏览页面的 DOM，不代为调用检索接口），程序只消费人工导出的 list.json；
//   - bmfw.www.gov.cn（司法部政策文件库平台）境外 DNS 不可达，不使用；
//   - 法规/规章原文属《著作权法》第 5 条官方文件（公有领域），但不镜像站点汇编：只取文本，
//     自有汇编事实由 manifest 记录，来源 URL 全链路留证。
// 硬性纪律（与全部抓取通道一致）：默认离线（仅 openlaw-govcn 显式触发）、robots 强制、
// 主机锁死 www.gov.cn（重定向逐跳复查）、礼貌限速（默认 2s/页）、UA 如实标识、
// 每次访问写 import_log、原始 HTML 落 incoming/<govcn|govgz>/ 留证、confirm_status=pending 人工闸口。
// 两库详情页 content_id 站点全局唯一 → doc_id 前缀统一 govcn_（跨库天然去重）。

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
)

// GovcnHost 法规库详情页所在主机（硬编码白名单；测试注入见 TestHost）。
const GovcnHost = "www.gov.cn"

// Govcn 国家行政法规库 / 国家规章库客户端（Kind 区分）。
type Govcn struct {
	DataDir     string
	DB          *store.DB
	MinInterval time.Duration // 礼貌限速（默认 2s/页）
	DryRun      bool          // 只抓取与解析（含留证），不入库——人工预览用
	Kind        string        // "" = xzfgk 行政法规库；"gzk" = 国家规章库
	TestHost    string        // 仅测试注入（httptest 回环）；生产留空 = GovcnHost
	robots      *RobotsCache
	lastGet     time.Time
}

// subdir 留证子目录（两库分离，互不污染）。
func (g *Govcn) subdir() string {
	if g.Kind == "gzk" {
		return "govgz"
	}
	return "govcn"
}

// GovcnEntry 人工导出清单（list.json）的一条。字段来自用户在浏览器中浏览的检索结果页。
type GovcnEntry struct {
	Title   string `json:"title"`
	PCode   string `json:"pcode"`   // 文号，如 国令第849号 / 教育部令第53号（可空）
	PubTime string `json:"pubtime"` // 站点发布日期 2026.10.26（可空，日期兜底用）
	URL     string `json:"url"`
	Cat     string `json:"cat"`    // gzk：清单分类 部门规章/地方政府规章（可空，见 govgzResolveLevel）
	PubOrg  string `json:"puborg"` // gzk：发布机关（DOM 提取，可空）
}

// GovcnItem 清单一条的处理结果（报告 CSV 行）。
type GovcnItem struct {
	URL     string `json:"url"`
	DocID   string `json:"doc_id"`
	Status  string `json:"status"` // ok / ok_inferred / skip_dup_db / skip_no_date / failed
	Reason  string `json:"reason,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
	Article int    `json:"article,omitempty"`
}

// govcnContentURLRe 详情页 URL 形态：https://www.gov.cn/<…>/content_<id>.htm[l]。
// 两库共用：行政法规库为 /zhengce/…，规章库详情页挂在 /zhengce/zhengceku/ 或 /gongbao/…
// （Athena 返回的 docurl 为完整 URL，形态由站点决定）——主机锁与 content_id 形态不变。
var govcnContentURLRe = regexp.MustCompile(`^https://www\.gov\.cn/.+/content_(\d+)\.html?$`)

// govcnContentIDRe 详情页路径本体（抓取层主机校验独立于此，便于 httptest 注入回环）。
var govcnContentIDRe = regexp.MustCompile(`/content_(\d+)\.html?$`)

// GovcnDocID 由详情页 URL 派生 doc_id（govcn_<content_id>；content_id 站点全局唯一）。
func GovcnDocID(rawURL string) (string, error) {
	m := govcnContentURLRe.FindStringSubmatch(rawURL)
	if m == nil {
		return "", fmt.Errorf("URL 不是 gov.cn 法规/规章详情页（须 https://%s/…/content_<id>.htm）: %s", GovcnHost, rawURL)
	}
	return "govcn_" + m[1], nil
}

// LoadGovcnList 读取人工导出的 list.json（数组），校验 URL 形态并按 URL 去重。
// 返回 (可抓取条目, 不符合本通道详情页形态的条目, error)——不合规条目不整单
// 拒绝而是如实交回（导入报告记 skip_bad_url 转人工；如 gzk 混合清单中的
// 地方政府直链，是否抓取由通道主机策略决定，清单层不擅自丢弃）。
// 清单是人工浏览的产物——本函数绝不联网「补全」清单（sousuo 域 robots 禁止自动化）。
func LoadGovcnList(path string) ([]GovcnEntry, []GovcnEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("读取清单失败: %w", err)
	}
	var entries []GovcnEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, nil, fmt.Errorf("清单 JSON 解析失败: %w", err)
	}
	seen := map[string]bool{}
	var out, skipped []GovcnEntry
	for _, e := range entries {
		e.URL = strings.TrimSpace(e.URL)
		// 主站 http:// 归一化为 https://：真机实测（2026-10-27）官方清单 docurl
		// 大量 http:// 形态（主站 301 → https，抓取层主机锁 + 重定向拒绝兜底）。
		// 只归一主站 GovcnHost；地方政府直链原样（由通道主机策略处置）。
		// 归一在去重前——同一详情 http/https 两形态并成一条。
		if strings.HasPrefix(e.URL, "http://"+GovcnHost+"/") {
			e.URL = "https://" + strings.TrimPrefix(e.URL, "http://")
		}
		if e.URL == "" {
			continue
		}
		if seen[e.URL] {
			continue
		}
		seen[e.URL] = true
		if _, err := GovcnDocID(e.URL); err != nil {
			skipped = append(skipped, e)
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 && len(skipped) == 0 {
		return nil, nil, fmt.Errorf("清单为空或无有效条目")
	}
	return out, skipped, nil
}

// host 生产锁死 GovcnHost；仅 TestHost 非空（测试）时替换。
func (g *Govcn) host() string {
	if g.TestHost != "" {
		return g.TestHost
	}
	return GovcnHost
}

func (g *Govcn) robotsCache() *RobotsCache {
	if g.robots == nil {
		g.robots = NewRobotsCache(g.policy())
	}
	return g.robots
}

func (g *Govcn) policy() FetchPolicy {
	return FetchPolicy{
		AllowHosts: []string{g.host()},
		MaxSize:    8 << 20, // 8 MiB 足够最大法规页（实测 ~50 KB）
		Timeout:    30 * time.Second,
		UserAgent:  defaultUserAgent(),
	}
}

// fetchPage 抓取详情页原始 HTML：限速 → robots → 主机锁 → 落盘 incoming/govcn/<docID>.html。
// 已存在则直接复用（断点续跑不重复联网），每次真实联网写 import_log。
func (g *Govcn) fetchPage(ctx context.Context, rawURL, docID string) (path string, fromCache bool, err error) {
	if g.MinInterval == 0 {
		g.MinInterval = 2 * time.Second
	}
	dir := filepath.Join(g.DataDir, "incoming", g.subdir())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	dst := filepath.Join(dir, docID+".html")
	if b, err := os.ReadFile(dst); err == nil && len(b) > 0 {
		return dst, true, nil
	}

	if d := time.Since(g.lastGet); d < g.MinInterval {
		select {
		case <-time.After(g.MinInterval - d):
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
	g.lastGet = time.Now()

	if ok, why := g.robotsCache().Allowed(ctx, rawURL); !ok {
		g.logFetch(rawURL, docID, "failed", fmt.Sprintf("robots 策略拒绝: %s", why), 0, "")
		return "", false, fmt.Errorf("robots 策略拒绝抓取: %s", why)
	}

	client := &http.Client{Timeout: g.policy().Timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return fmt.Errorf("重定向次数过多")
		}
		if req.URL.Hostname() != g.host() {
			return fmt.Errorf("重定向离开 %s，拒绝: %s", g.host(), req.URL.Host)
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", g.policy().UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		g.logFetch(rawURL, docID, "failed", fmt.Sprintf("请求失败: %v", err), 0, "")
		return "", false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, g.policy().MaxSize+1))
	if err != nil {
		g.logFetch(rawURL, docID, "failed", err.Error(), 0, resp.Header.Get("Content-Type"))
		return "", false, err
	}
	if resp.StatusCode != http.StatusOK {
		reason := fmt.Sprintf("HTTP %d", resp.StatusCode)
		g.logFetch(rawURL, docID, "failed", reason, int64(len(body)), resp.Header.Get("Content-Type"))
		return "", false, fmt.Errorf("详情页 %s: %s", rawURL, reason)
	}
	if int64(len(body)) > g.policy().MaxSize {
		g.logFetch(rawURL, docID, "failed", "超过大小上限 8 MiB", int64(len(body)), resp.Header.Get("Content-Type"))
		return "", false, fmt.Errorf("详情页超过大小上限")
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		g.logFetch(rawURL, docID, "failed", fmt.Sprintf("Content-Type 非 HTML（%q）", resp.Header.Get("Content-Type")), int64(len(body)), resp.Header.Get("Content-Type"))
		return "", false, fmt.Errorf("详情页 Content-Type 非 HTML，可能是门户/错误页")
	}
	tmp := dst + ".part"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return "", false, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", false, err
	}
	g.logFetch(rawURL, docID, "success", "原始 HTML 已留证 incoming/"+g.subdir()+"/", int64(len(body)), resp.Header.Get("Content-Type"))
	return dst, false, nil
}

// logFetch 每次详情页访问进 import_log（4.2：不得静默）。
func (g *Govcn) logFetch(rawURL, docID, result, reason string, size int64, ctype string) {
	if ctype == "" {
		ctype = "text/html"
	}
	_, _ = g.DB.Exec(`INSERT INTO import_log(doc_id, source_url, file_path, file_size, sha256,
		http_status, content_type, result, reason, created_at)
		VALUES(NULL,?,NULL,?,NULL,NULL,?,?,?,?)`,
		rawURL, size, ctype, result, "[govcn doc_id="+docID+"] "+reason, time.Now().UTC().Format(time.RFC3339))
}

// ---- 详情页解析（TRS CMS 正文容器 → Markdown）----

// trsContentRe 正文容器起始标签（gov.cn 政策详情页 TRS_UEDITOR 惯例，2026-10 实查）。
var trsContentRe = regexp.MustCompile(`(?i)<div\b[^>]*class="[^"]*(?:trs_editor_view|TRS_UEDITOR|pages_content)[^"]*"[^>]*>`)

// divTagRe 深度扫描用（嵌套 div 计数找容器闭合）。
var divTagRe = regexp.MustCompile(`(?i)<div\b[^>]*>|</div\s*>`)

// govcnTitleRe 页面标题。
var govcnTitleRe = regexp.MustCompile(`(?is)<title>(.*?)</title>`)

// extractGovcnContent 提取正文容器 HTML 片段（div 深度配对扫描；未找到返回空串）。
func extractGovcnContent(h string) string {
	m := trsContentRe.FindStringIndex(h)
	if m == nil {
		return ""
	}
	depth, pos, end := 1, m[1], -1
	for pos < len(h) {
		t := divTagRe.FindStringIndex(h[pos:])
		if t == nil {
			break
		}
		if strings.HasPrefix(strings.ToLower(h[pos+t[0]:pos+t[0]+2]), "</") {
			depth--
		} else {
			depth++
		}
		pos += t[1]
		if depth == 0 {
			end = pos - (t[1] - t[0]) // 回到本 </div> 起点
			break
		}
	}
	if end < 0 {
		end = len(h) // 容器未闭合（畸形页）：取到结尾，由 ParseGovcnDetail 再验内容
	}
	return h[m[1]:end]
}

// govcnBlockRe 块级标签边界 → 换行。
var govcnBlockRe = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</h[1-6]>|</tr>|</li>|</table>`)

// govcnTagRe 其余标签。
var govcnTagRe = regexp.MustCompile(`(?s)<[^>]+>`)

// govcnUnescape 正文常用实体还原。
func govcnUnescape(s string) string {
	for _, r := range []struct{ from, to string }{
		{"&nbsp;", " "}, {"&#160;", " "}, {"&emsp;", "　"}, {"&amp;", "&"},
		{"&lt;", "<"}, {"&gt;", ">"}, {"&quot;", `"`}, {"&#39;", "'"},
	} {
		s = strings.ReplaceAll(s, r.from, r.to)
	}
	return s
}

// govcnToMarkdown 正文片段 → 纯文本 Markdown（段落切分 + 实体还原；样式语义丢弃）。
func govcnToMarkdown(seg string) string {
	s := govcnUnescape(govcnTagRe.ReplaceAllString(govcnBlockRe.ReplaceAllString(seg, "\n"), ""))
	var b strings.Builder
	blank := 0
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\u00a0", " "))
		if line == "" {
			blank++
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
			if blank > 0 {
				b.WriteString("\n")
			}
		}
		b.WriteString(line)
		blank = 0
	}
	return b.String()
}

// govcnTitleSuffixRe 标题站点后缀清理（含公报页的期号后缀，如 __2024年第20号国务院公报）。
var govcnTitleSuffixRe = regexp.MustCompile(`_{1,2}\d{4}年第\d+号(?:国务院)?公报$|_(?:中国政府网|国务院文件|国务院部门文件|政策解读|政策|国家规章库|国家行政法规库)$`)

// govgzResolveLevel 国家规章库效力等级/发布机关解析（站点自述分类，非猜测）：
//   - 清单 cat（用户浏览的分类页签）优先：部门规章 / 地方政府规章；
//   - cat 缺失时按发布机关保守归类：含「人民政府」→ 地方政府规章；其他机关 → 部门规章；
//   - 两者皆缺 → 不猜，转人工（skip_no_meta）。
func govgzResolveLevel(cat, puborg string) (level, issuer string, ok bool) {
	switch cat {
	case "部门规章", "地方政府规章":
		level = cat
	case "":
		puborg = strings.TrimSpace(puborg)
		if puborg == "" {
			return "", "", false
		}
		if strings.Contains(puborg, "人民政府") {
			level = "地方政府规章"
		} else {
			level = "部门规章"
		}
	default:
		return "", "", false
	}
	if org := strings.TrimSpace(puborg); org != "" {
		issuer = org
	} else if level == "地方政府规章" {
		issuer = "地方人民政府（清单未载机关，待人工补）"
	} else {
		issuer = "国务院部门（清单未载机关，待人工补）"
	}
	return level, issuer, true
}

// govcnDecisionTitleRe 修改/废止/停止执行类决定：正文「第X条」全部是被引法规的条文
// 而非本文件的条文边界（真实结构为「一、二、三、」）→ 强制全文即一节，防条文误拆。
var govcnDecisionTitleRe = regexp.MustCompile(`关于(修改|废止|停止执行)`)

// ParseGovcnDetail 解析详情页 HTML → (法规名, 正文 Markdown)。
// 名称取 <title>（去站点后缀）；正文取 TRS 容器；正文过短/缺名即报错（绝不静默入库）。
func ParseGovcnDetail(h string) (title, md string, err error) {
	if m := govcnTitleRe.FindStringSubmatch(h); m != nil {
		title = strings.TrimSpace(m[1])
		for {
			t2 := govcnTitleSuffixRe.ReplaceAllString(title, "")
			if t2 == title {
				break
			}
			title = t2
		}
	}
	md = govcnToMarkdown(extractGovcnContent(h))
	if len([]rune(md)) < 50 {
		return "", "", fmt.Errorf("正文容器缺失或过短（%d 字）——页面形态与 TRS 惯例不符，转人工核查", len([]rune(md)))
	}
	if title == "" {
		return "", "", fmt.Errorf("无法从 <title> 提取法规名称")
	}
	return title, md, nil
}

// govcnDotDateRe 清单 pubtime（2026.10.26）→ ISO。
var govcnDotDateRe = regexp.MustCompile(`^(\d{4})\.(\d{1,2})\.(\d{1,2})$`)

func govcnNormalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if m := govcnDotDateRe.FindStringSubmatch(s); m != nil {
		return fmt.Sprintf("%s-%02d-%02d", m[1], mustAtoi(m[2]), mustAtoi(m[3]))
	}
	if d, err := parse.NormalizeDate(s); err == nil {
		return d
	}
	return ""
}

func mustAtoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

// FetchAndImport 单条：抓取（或复用留证）→ 解析 → 日期（正文落款优先，清单 pubtime 兜底，
// 绝不猜）→ Importer 入库（行政法规 / pending 人工闸口 / 版本链接续）。
// 已入库（doc_id 在库）跳过联网与导入（幂等续跑）。
func (g *Govcn) FetchAndImport(ctx context.Context, im *Importer, e GovcnEntry) GovcnItem {
	it := GovcnItem{URL: e.URL}
	// 抓取层校验：主机锁（TestHost 仅测试注入）+ 详情页路径形态。
	// 清单层 LoadGovcnList 另强制生产形态 https://www.gov.cn/…。
	u, err := url.Parse(e.URL)
	if err != nil {
		it.Status, it.Reason = "failed", fmt.Sprintf("URL 非法: %v", err)
		return it
	}
	m := govcnContentIDRe.FindStringSubmatch(u.Path)
	if u.Hostname() != g.host() || m == nil {
		it.Status, it.Reason = "failed", fmt.Sprintf("URL 主机须为 %s 且路径为 /…/content_<id>.htm: %s", g.host(), e.URL)
		return it
	}
	docID := "govcn_" + m[1]
	it.DocID = docID

	var n int
	if err := g.DB.QueryRow(`SELECT COUNT(*) FROM document WHERE doc_id = ?`, docID).Scan(&n); err != nil {
		it.Status, it.Reason = "failed", err.Error()
		return it
	}
	if n > 0 {
		it.Status, it.Reason = "skip_dup_db", docID
		return it
	}

	htmlPath, fromCache, err := g.fetchPage(ctx, e.URL, docID)
	if err != nil {
		it.Status, it.Reason = "failed", err.Error()
		return it
	}
	b, err := os.ReadFile(htmlPath)
	if err != nil {
		it.Status, it.Reason = "failed", err.Error()
		return it
	}
	title, md, err := ParseGovcnDetail(string(b))
	if err != nil {
		it.Status, it.Reason = "failed", err.Error()
		return it
	}
	// 清单标题（人工浏览所见）优先——公报页 <title> 带令号/期号噪声；清单缺标题才用 HTML 提取。
	if t := strings.TrimSpace(e.Title); t != "" {
		title = t
	}

	// 日期：正文落款保守推断（parse.InferDates）优先；无生效日退回清单 pubtime
	// （站点发布日，保守上界）；仍无 → skip_no_date 转人工，绝不猜。
	pub, eff, inferred := "", "", false
	if p, ef, ok := parse.InferDates(md); ok {
		pub, eff, inferred = p, ef, true
	}
	if eff == "" {
		if d := govcnNormalizeDate(e.PubTime); d != "" {
			eff, inferred = d, true
			if pub == "" {
				pub = d
			}
		}
	}
	if eff == "" {
		it.Status, it.Reason = "skip_no_date", "正文落款与清单 pubtime 均无日期，转人工"
		return it
	}
	if pub == "" {
		pub = eff
	}

	mdPath := strings.TrimSuffix(htmlPath, ".html") + ".md"
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		it.Status, it.Reason = "failed", err.Error()
		return it
	}
	if g.DryRun {
		it.Status, it.Reason = "dry_run", "pub="+pub+" eff="+eff+" title="+title
		return it
	}

	sum := sha256.Sum256([]byte(title))
	lawID := fmt.Sprintf("govcn_%x", sum[:6])

	issuer, effectLevel, sourceType := "中华人民共和国国务院", "行政法规", "gov.cn 国家行政法规库（官方详情页抓取）"
	if g.Kind == "gzk" {
		lv, iss, ok := govgzResolveLevel(e.Cat, e.PubOrg)
		if !ok {
			it.Status, it.Reason = "skip_no_meta", "规章库条目缺分类且无发布机关，效力等级不可保守判定，转人工"
			return it
		}
		effectLevel, issuer, sourceType = lv, iss, "gov.cn 国家规章库（官方详情页抓取）"
	}

	res, err := im.Import(Request{
		DocID: docID, LawID: lawID, LawName: title,
		Issuer: issuer, EffectLevel: effectLevel,
		VersionNo: eff, EffectiveDate: eff, PublishDate: pub,
		SourceURL: e.URL, SourceType: sourceType,
		ConfirmStatus:         "pending",
		EffectiveDateInferred: inferred,
		PDFPath:               htmlPath,
		MDPath:                mdPath, // 解析产物显式提供（.html 源文件不走 extractByExt）
		AutoExpirePrior:       true,
		WholeTextMode:         true,                                    // 国发/国函类无「第X条」结构 → 全文即一节显式打标
		WholeTextForce:        govcnDecisionTitleRe.MatchString(title), // 修改/废止决定：第X条是引文，防误拆
		Notes:                 buildGovcnNotes(e, fromCache),
	})
	if err != nil {
		it.Status, it.Reason = "failed", err.Error()
		return it
	}
	it.Article = len(res.ArticleIDs)
	it.SHA256 = res.SHA256
	it.Status = "ok"
	if inferred {
		it.Status = "ok_inferred"
	}
	if res.WholeText {
		it.Status += "_whole"
	}
	return it
}

// buildGovcnNotes 导入备注（人工复核线索：文号/日期来源/续跑复用）。
func buildGovcnNotes(e GovcnEntry, fromCache bool) string {
	parts := []string{"gov.cn 官方库：日期取自正文落款或站点发布日（inferred 标记见溯源），待人工确认"}
	if e.PCode != "" {
		parts = append(parts, "文号 "+e.PCode)
	}
	if e.Cat != "" {
		parts = append(parts, "清单分类 "+e.Cat)
	}
	if fromCache {
		parts = append(parts, "HTML 复用既有留证（断点续跑）")
	}
	return strings.Join(parts, "；")
}
