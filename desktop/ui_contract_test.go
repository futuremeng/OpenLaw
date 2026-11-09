// ui_contract_test.go 界面重构门禁。
//
// 重做界面时最容易悄悄退化的四件事，都在这里锁住：
//
//	① 类名漂移：HTML/JS 用到的类名必须在样式层有规则（否则元素无样式，
//	   面板「能显示但难看」——这类问题编译期与绑定层都无感）；
//	② 页头缺失：每个面板必须有一句话处境（page-head 或首页 hero），
//	   否则用户只剩一堆控件而不知道自己在解决什么问题；
//	③ 渐进披露：schema 版本、数据目录、端点与通配符等技术字样只能出现在
//	   <details> 折叠内，不得回到主视图（docs/design-ui-language.md）；
//	④ 深色模式：样式层必须同时给出系统偏好与手动切换两套令牌。
package main

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func readFrontend(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("frontend", "dist", rel))
	if err != nil {
		t.Fatalf("前端文件不可读 %s: %v", rel, err)
	}
	return string(raw)
}

var htmlClassRe = regexp.MustCompile(`class="([^"]+)"`)
var classTokenRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// TestFrontendClassesStyled HTML 与 app.js 里出现的每个类名都必须有样式规则。
// 模板串里的片段（`class="task ${...}"`）不是合法类名，直接跳过。
func TestFrontendClassesStyled(t *testing.T) {
	css := readFrontend(t, filepath.Join("assets", "style.css"))
	used := map[string]bool{}
	for _, src := range []string{readFrontend(t, "index.html"), readFrontend(t, filepath.Join("assets", "app.js"))} {
		for _, m := range htmlClassRe.FindAllStringSubmatch(src, -1) {
			for _, c := range strings.Fields(m[1]) {
				if classTokenRe.MatchString(c) {
					used[c] = true
				}
			}
		}
	}
	if len(used) < 40 {
		t.Fatalf("仅扫到 %d 个类名，扫描器可能已失效", len(used))
	}
	var missing []string
	for c := range used {
		if !strings.Contains(css, "."+c) {
			missing = append(missing, c)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("以下类名在 style.css 中没有规则: %v——元素会退化成无样式文本", missing)
		return
	}
	t.Logf("样式契约核对通过：%d 个类名全部有规则", len(used))
}

// TestPanelsHaveHeadline 每个面板都要先给出处境（page-head），公众首页用 hero 承担同一职责。
// Go 的正则不支持前瞻，故按 <section 切块逐块检查。
func TestPanelsHaveHeadline(t *testing.T) {
	html := readFrontend(t, "index.html")
	parts := strings.Split(html, `<section id="`)
	if len(parts) < 7 {
		t.Fatalf("仅扫到 %d 段，扫描器可能已失效", len(parts)-1)
	}
	seen := 0
	for _, p := range parts[1:] {
		name := p[:strings.IndexByte(p, '"')]
		if !strings.HasPrefix(name, "panel-") {
			continue
		}
		seen++
		body := p
		if i := strings.Index(body, `<section id="`); i >= 0 {
			body = body[:i]
		}
		if !strings.Contains(body, `class="page-head"`) && !strings.Contains(body, `class="hero"`) {
			t.Errorf("%s 缺少页头（处境一句话 + 主行动），用户只剩控件", name)
		}
	}
	if seen < 6 {
		t.Errorf("仅核对到 %d 个面板，扫描器可能已失效", seen)
	}
}

// TestTechnicalTermsOnlyInDisclosure 技术字样不得出现在折叠之外的主视图。
func TestTechnicalTermsOnlyInDisclosure(t *testing.T) {
	html := readFrontend(t, "index.html")
	stripped := regexp.MustCompile(`(?s)<details.*?</details>`).ReplaceAllString(html, "")
	banned := []string{"schema_version", "OPENLAW_", "http://", "127.0.0.1", "-endpoint", "gov.cn%", "flk%", "LIKE"}
	for _, b := range banned {
		if strings.Contains(stripped, b) {
			t.Errorf("主视图出现技术字样 %q——按规范只能放在「技术细节」折叠内", b)
		}
	}
}

// TestVisibleTextNoInternalJargon 上面那道技术字样门禁只扫静态 HTML，JS 渲染的文案
// 是盲区——「语料」与 ISO UTC 时间戳正是从这条缝里漏进主视图的（2026-11-08 评审）。
// 数据层内部词「语料」不得出现在用户可见文案里（AGENTS.md：语料→法规库数据；
// 代码注释不算）；目录与完整性档案的 generated_at 必经 fmtWhen——主视图禁止裸渲染
// ISO UTC 时间戳，显示本机时间、原始串留 title（docs/design-ui-language.md
// 「主视图禁止出现」）。
func TestVisibleTextNoInternalJargon(t *testing.T) {
	html := regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(readFrontend(t, "index.html"), "")
	if strings.Contains(html, "语料") {
		t.Errorf("index.html 可见文案出现「语料」——数据层内部词，界面一律说「法规库数据」")
	}
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	js = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(js, "")
	js = regexp.MustCompile(`//[^\n]*`).ReplaceAllString(js, "")
	if strings.Contains(js, "语料") {
		t.Errorf("app.js 可见文案出现「语料」——数据层内部词，界面一律说「法规库数据」")
	}
	for _, want := range []string{"fmtWhen(c.generated_at)", "fmtWhen(m.generated_at)"} {
		if !strings.Contains(js, want) {
			t.Errorf("generated_at 未经 %s 渲染——主视图禁止原始 ISO UTC，须显示本机时间、UTC 原串留 title", want)
		}
	}
}

// TestFilterRailLivesInLeftColumn 三栏布局契约：左列 = 导航 + 筛选栏（仅「按条件筛选」
// 展开）、中列 = 面板列表、右列 = 共享详情。筛选是「定位法规」的手段，常驻左列；
// 左列不得携带状态与外观工具（那些在顶栏）。计数与列表必须同源
// （前端同一处调用 BrowseLawList / BrowseFacets，Go 侧共用 browseWhere）。
func TestFilterRailLivesInLeftColumn(t *testing.T) {
	html := readFrontend(t, "index.html")
	start := strings.Index(html, `<aside id="side">`)
	end := strings.Index(html, `</aside>`)
	if start < 0 || end <= start {
		t.Fatalf("未定位到左列结构")
	}
	side := html[start:end]
	for _, want := range []string{`id="filters"`, `class="filters"`, `id="f-levels"`, `id="f-confirms"`,
		`id="f-sources"`, `id="f-years"`, `id="f-regions"`, `id="f-subjects"`, `id="f-timeliness"`, `id="btn-filter-clear"`} {
		if !strings.Contains(side, want) {
			t.Errorf("左列缺少 %q：多维筛选与分面计数是定位法规的手段，常驻左列", want)
		}
	}
	// 分面顺序固定（docs/design-taxonomy.md §6）：法域 → 效力等级 → 部门法 → 地区 →
	// 公布年代 → 时效状态 → 复核状态 → 来源。顺序打乱用户空间记忆，改动须过设计。
	prev := -1
	for _, want := range []string{`id="g-juris"`, `id="g-level"`, `id="g-subject"`, `id="g-region"`,
		`id="g-year"`, `id="g-timeliness"`, `id="g-confirm"`, `id="g-source"`} {
		at := strings.Index(side, want)
		if at < 0 {
			t.Fatalf("左列缺少 %s——§6 分面顺序契约要求该组存在", want)
		}
		if at < prev {
			t.Errorf("%s 位置不符 §6 分面顺序（法域→效力等级→部门法→地区→年代→时效→复核→来源）", want)
		}
		prev = at
	}
	// 时效徽章五态映射集中一处（lawtext statusClassMap 的教训）；无时效视图的
	// 如实兜底文案不得被「现行有效」等乐观默认替换。
	jsAll := readFrontend(t, filepath.Join("assets", "app.js"))
	if !strings.Contains(jsAll, "TIMELINESS_BADGE") || !strings.Contains(jsAll, "function timelinessBadge(") {
		t.Errorf("时效徽章缺少集中映射——五态在多处映射必然出现口径漂移")
	}
	if !strings.Contains(jsAll, "未知（法规库数据版本不含时效信息）") {
		t.Errorf("缺少无时效视图的如实兜底——v10 数据被显示成假「未知」或假「现行有效」都属诚实缺陷")
	}
	if !strings.Contains(jsAll, "app().LawMeta(") || !strings.Contains(jsAll, "function lawMetaCard(") {
		t.Errorf("条目页信息卡未接线——§6 字段序（时效徽章/部门法状态/文档形态/来源链接）必须走 LawMeta")
	}
	for _, banned := range []string{"theme-toggle", "t-lib", "t-pending", "schema-badge"} {
		if strings.Contains(side, banned) {
			t.Errorf("左列出现 %q——状态与外观工具在顶栏，左列只留导航与筛选，不得两处各一套", banned)
		}
	}
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	for _, want := range []string{"app().BrowseLawList(", "app().BrowseFacets("} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——列表与计数必须走同一条件，不能各算各的", want)
		}
	}
	// 库总览联动契约：分法域与构成必须同一口径——一个法域一个块（jgroup）、
	// 块头可下钻（data-juris → browseFilter.juris）、精确数字折叠归入本法域列。
	for _, want := range []string{`各等级的精确数字`, `<table class="lv">`} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——精确数字须折叠在本法域列内，与占比条同批数据可就地核对", want)
		}
	}
	for _, want := range []string{`data-juris`, `browseFilter.juris = b.dataset.juris`, `class="jgroup"`} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——分法域与构成必须联动：法域行可下钻、构成按法域分组", want)
		}
	}
}

// TestThreeColumnAgentReviewUI 三栏结构与智能审核面板契约：右列共享详情带一键纠正
// 工具条；人工复核入口已由「智能审核」取代——人工只处理例外。技术操作
// （批量署名落判、手输路径、构建签名包、重载缓存）不得回流到常规界面。
// 工作区契约：维护项只经左列底部独立入口进入（公众/专业模式切换已撤除，不得回流）；
// 空库有「初始化法规库」引导；离线更新包控件唯一落点在 panel-pack（collect 内不得再现）。
func TestThreeColumnAgentReviewUI(t *testing.T) {
	html := readFrontend(t, "index.html")
	for _, want := range []string{`<aside id="side">`, `<main>`, `<aside id="detail"`, `id="fix-bar"`,
		`id="panel-review"`, `id="btn-review-run"`, `id="rv-stats"`, `id="rv-channel"`,
		`id="ex-list"`, `id="fix-confirm"`, `id="fix-reject"`, `id="btn-pack-pick"`,
		`id="nav-maintain"`, `id="nav-group-main"`, `id="nav-home"`, `id="nav-browse"`, `id="nav-search"`,
		`id="nav-group-maintain"`, `id="nav-exit-maintain"`,
		`id="panel-setup"`, `id="su-status"`, `id="su-go-pack"`, `id="su-go-collect"`, `id="su-go-home"`,
		`id="su-catalog-refresh"`, `id="su-catalog"`, `id="su-install"`, `id="su-sel"`,
		`id="ov-juris"`, `id="g-juris"`, `id="f-juris"`,
		`id="panel-pack"`, `id="h-empty"`} {
		if !strings.Contains(html, want) {
			t.Errorf("缺少 %s——三栏布局、智能审核控件、独立维护入口与初始化引导是界面契约", want)
		}
	}
	for _, gone := range []string{`id="panel-confirm"`, `id="queue-list"`, `id="reviewer"`, `id="reason"`,
		`id="btn-bulk-preview"`, `id="btn-bulk-apply"`, `id="modal"`, `id="p-dir"`, `id="p-key"`,
		`id="btn-pack-build"`, `id="btn-risk-reload"`, `id="schema-badge"`,
		`id="mode-public"`, `id="mode-pro"`, `class="modes"`, `data-mode=`} {
		if strings.Contains(html, gone) {
			t.Errorf("%s 重新出现——模式切换已撤除、技术操作已收进 CLI 与折叠，不得回流常规界面", gone)
		}
	}
	// 离线更新包控件唯一落点：panel-pack 内必须在，panel-collect 内不得再现（一处写路径，不摆两套按钮）。
	pack := sectionOf(t, html, "panel-pack")
	for _, want := range []string{`id="btn-pack-pick"`, `id="btn-pack-verify"`, `id="btn-pack-import"`} {
		if !strings.Contains(pack, want) {
			t.Errorf("panel-pack 缺少 %s——导入更新包的唯一落点必须完整", want)
		}
	}
	collect := sectionOf(t, html, "panel-collect")
	for _, gone := range []string{`id="btn-pack-pick"`, `id="btn-pack-import"`} {
		if strings.Contains(collect, gone) {
			t.Errorf("panel-collect 内再现 %s——更新包控件已迁至独立面板，不得两处各一套", gone)
		}
	}
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	for _, want := range []string{"app().AgentStatus(", "app().StartReviewTask(", "app().AgentStats(",
		"app().ExceptionList(", "app().CorrectDoc(", "app().PickUpdatePack(",
		"loadReview(", "statusBadge(s, src)", `src === "agent"`, `src === "human"`,
		"function enterMaintain(", "function exitMaintain(", "function applyWorkspace(",
		"function loadSetup(", `showPanel("setup")`, "MAIN_NAV", "MAINTAIN_NAV",
		"app().PackCatalogRefresh(", "app().StartRemoteInstallTask(", "function loadPackCatalog(",
		"function renderJuris(", `facetChips("#f-juris", "juris"`} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——智能审核的计数、例外与一键纠正必须走同一库层路径；维护工作区与空库引导必须接线", want)
		}
	}
	css := readFrontend(t, filepath.Join("assets", "style.css"))
	for _, want := range []string{".badge.agent", ".badge.human", "#detail", "#fix-bar", "#nav-maintain"} {
		if !strings.Contains(css, want) {
			t.Errorf("样式缺少 %s——智能体确认与人工确认必须区别显示，右列详情与维护入口必须有布局规则", want)
		}
	}
	// hidden 属性必须真生效：自带 display 的类（.nav/.nav-group）会盖过 UA 的
	// [hidden]{display:none}，缺这条样式，收起的维护子导航会显示在常规界面上。
	if !regexp.MustCompile(`\[hidden\]\s*\{\s*display:\s*none`).MatchString(css) {
		t.Errorf("样式缺少 [hidden] { display: none } 规则——带 hidden 属性的维护子导航会原样显示")
	}
	// 维护入口钉在侧栏最下方：DOM 上位于筛选栏之后，样式用 margin-top:auto 顶开剩余空间。
	if strings.Index(html, `id="filters"`) > strings.Index(html, `id="nav-maintain"`) {
		t.Errorf("#nav-maintain 未排在筛选栏之后——「维护法规库」入口必须固定在侧栏最下方")
	}
	if !regexp.MustCompile(`#nav-maintain\s*\{[^}]*margin:\s*auto`).MatchString(css) {
		t.Errorf("样式缺少 #nav-maintain 的 margin-top:auto——入口会跟在筛选栏后面而不是钉在最下方")
	}
}

// sectionOf 取 <section id="name" ...> 到下一个 <section 或 </main> 之间的片段。
func sectionOf(t *testing.T, html, name string) string {
	t.Helper()
	start := strings.Index(html, `<section id="`+name+`"`)
	if start < 0 {
		t.Fatalf("未找到面板 %s", name)
	}
	rest := html[start:]
	if i := strings.Index(rest[len(`<section id="`+name+`"`):], `<section id="`); i >= 0 {
		rest = rest[:len(`<section id="`+name+`"`)+i]
	}
	if i := strings.Index(rest, `</main>`); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// TestFrontendWiringSmoke 跑 frontend/wiring_smoke.mjs：桩化 DOM 与 Go 绑定，把
// api.js + app.js 真实执行一遍（切面板、检索、载入队列、渲染任务、切外观）。
// 绑定契约测试只核对 id 是否存在与实参个数，这里补上「执行期不抛错、
// 关键结构确实产出」这一层。本机无 node 时跳过（不假装跑过）。
func TestFrontendWiringSmoke(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("本机未安装 node，跳过前端接线冒烟（不计为通过）")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("frontend", "wiring_smoke.mjs"))
	out, err := cmd.CombinedOutput()
	if !strings.Contains(string(out), "SMOKE_OK") {
		t.Errorf("前端接线冒烟未通过（err=%v）：\n%s", err, strings.TrimSpace(string(out)))
		return
	}
	for _, want := range []string{"侧栏待复核计数", "待复核为 0 时清空徽标\": true", "总览占比条含 bar-row\": true",
		"任务卡 class 含 running\": true", "筛选前后总数联动\": true", "浏览分面给出计数\": true",
		"未标注地区如实说明\": true", "检索命中可就地收窄\": true",
		"条目行是按钮（键盘可达）\": true",
		"缺失项写明缺什么与去哪补\": true", "缺失说明不藏在 title\": true", "未封顶时不谎报上限\": true",
		"法域卡箭头是注入的 SVG\": true",
		"取列失败时进行态改口\": true", "计数到手才交出句子\": true",
		"智能审核统计分开计数\": true", "例外队列给出没把握原因\": true", "智能审核通道回显\": true",
		"条目徽章如实报智能体确认\": true",
		"维护入口进入独立工作区\": true", "两套导航完整互斥切换\": true", "离线更新包在维护区可达\": true", "退出维护回主导航\": true",
		"初始化面板如实报空库\": true", "空库时不得装作可查询\": true", "库非空初始化如实报完成\": true"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("接线冒烟缺少期望产出 %q：\n%s", want, strings.TrimSpace(string(out)))
		}
	}
}

// TestListRowsKeyboardOperable 列表条目必须是原生可聚焦元素。
//
// 条目行是产品的招牌动作（选中一条 → 读原文）。写成 <div onclick> 时鼠标能用、
// 键盘完全不能用（WCAG 2.1.1 与 4.1.2 都是 A 级），而这类退化在绑定测试与
// 类名门禁里毫无征兆——面板照样渲染，只是键盘用户点不动。故在此锁死：
// 条目类名只允许出现在原生交互元素上，且焦点必须进得去右列、退得回来路。
func TestListRowsKeyboardOperable(t *testing.T) {
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	html := readFrontend(t, "index.html")
	rowTagRe := regexp.MustCompile(`<(?:div|span|li|a|p|tr)\b[^>]*class="(?:item|lawcard)[" $]`)
	for _, src := range []struct {
		name string
		text string
	}{{"app.js", js}, {"index.html", html}} {
		if m := rowTagRe.FindString(src.text); m != "" {
			t.Errorf("%s 的条目行用了非交互元素（键盘不可达）：%s", src.name, m)
		}
	}
	// 七个列表各产出一处按钮条目，一处都不能少（少一处即该列表键盘不可达）。
	n := strings.Count(js, `<button type="button" class="item`) + strings.Count(js, `<button type="button" class="lawcard`)
	if n != 7 {
		t.Errorf("条目模板产出的按钮为 %d 处，应为 7 处（浏览/首页命中/条款/条文检索/例外/地区/部门法）", n)
	}
	if !strings.Contains(html, `id="detail" class="hidden" tabindex="-1"`) {
		t.Error(`详情列必须 tabindex="-1"：键盘激活条目后焦点要能进右列，否则详情既读不到也滚不动`)
	}
	if !strings.Contains(js, "detailOpener.focus()") {
		t.Error("关闭详情必须把焦点还给打开它的那一行（Esc / ✕ 之后不得把用户丢在原地）")
	}
}

// TestTokenContrastAA 令牌成对取值必须真的达到 WCAG AA（4.5:1）。
//
// 对比度是算出来的，不是声明出来的：本测试从样式层的 :root 与深色块里取出
// 令牌实际色值，按 WCAG 2.x 相对亮度公式实算每对「前景/底色」。改任何一个
// 颜色令牌都会当场重算，因此调暗强调色、换主题底色都可能让它变红——
// 这正是目的：深色主题下「白字叠亮蓝」曾经只有 2.51:1，而所有字符串门禁全绿。
func TestTokenContrastAA(t *testing.T) {
	css := readFrontend(t, filepath.Join("assets", "style.css"))
	pairs := []struct {
		what string
		fg   string
		bg   string
	}{
		{"正文/页面底色", "--ink", "--bg"},
		{"次要文字/面板", "--muted", "--panel"},
		{"次要文字/页面底色", "--muted", "--bg"},
		{"次要文字/表层", "--muted", "--surface-2"},
		{"强调色底上的前景", "--on-acc", "--acc"},
		{"成功色底上的前景", "--on-ok", "--ok"},
		{"强调文字/强调浅底", "--acc", "--acc-soft"},
		{"成功文字/成功浅底", "--ok", "--ok-soft"},
		{"警示文字/警示浅底", "--warn", "--warn-soft"},
		{"错误文字/错误浅底", "--bad", "--bad-soft"},
		{"侧栏文字/侧栏底色", "--side-ink", "--side"},
		{"选中文字/选中底色", "--ink", "--acc-soft"},
	}
	for _, theme := range []struct {
		label  string
		marker string
	}{{"浅色", ":root {"}, {"深色", `html[data-theme="dark"] {`}} {
		block := cssBlock(t, css, theme.marker)
		for _, p := range pairs {
			fg, bg := cssToken(t, block, p.fg), cssToken(t, block, p.bg)
			if r := contrastRatio(t, fg, bg); r < 4.5 {
				t.Errorf("%s主题「%s」%s(%s) on %s(%s) = %.2f:1，低于 AA 4.5:1",
					theme.label, p.what, p.fg, fg, p.bg, bg, r)
			}
		}
	}
	// 强调色底上的前景必须走令牌：写死白色即绕过了主题翻转。
	for _, lit := range []string{"color: #fff", "color:#fff", "color: #ffffff", "color: white"} {
		if strings.Contains(css, lit) {
			t.Errorf("前景色写死为 %q：必须用 --on-acc/--on-ok，否则深色主题下不翻转而不达 AA", lit)
		}
	}
	if strings.Contains(css, "color-mix(in srgb, var(--muted)") {
		t.Error("不得用 color-mix 把次要文字再调淡：「未标注」与其他次要文字同权（诚实展示纪律）")
	}
}

// cssBlock 取 marker 之后到下一个右花括号之间的声明块（令牌块无嵌套）。
func cssBlock(t *testing.T, css, marker string) string {
	t.Helper()
	i := strings.Index(css, marker)
	if i < 0 {
		t.Fatalf("样式层找不到 %q", marker)
	}
	rest := css[i:]
	j := strings.Index(rest, "}")
	if j < 0 {
		t.Fatalf("%q 块未闭合", marker)
	}
	return rest[:j]
}

var cssTokenRe = regexp.MustCompile(`(?:^|[^-\w])--([a-z0-9-]+)\s*:\s*(#[0-9a-fA-F]{6}|#[0-9a-fA-F]{3})\s*;`)

// cssToken 取块内某令牌的十六进制色值（缺失即测试失败，不静默跳过）。
func cssToken(t *testing.T, block, name string) string {
	t.Helper()
	for _, m := range cssTokenRe.FindAllStringSubmatch(block, -1) {
		if m[1] == strings.TrimPrefix(name, "--") {
			return m[2]
		}
	}
	t.Fatalf("令牌 %s 在该主题块中缺失或不是 #rrggbb 色值", name)
	return ""
}

// hexLuminance 按 WCAG 2.x 相对亮度公式计算。
func hexLuminance(t *testing.T, hex string) float64 {
	t.Helper()
	h := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(h) == 3 { // 缩写形式 #rgb 展开为 #rrggbb（--panel: #fff 即此）
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		t.Fatalf("色值不是 #rgb 或 #rrggbb：%q", hex)
	}
	var sum float64
	w := [3]float64{0.2126, 0.7152, 0.0722}
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseInt(h[i*2:i*2+2], 16, 32)
		if err != nil {
			t.Fatalf("色值解析失败 %q: %v", hex, err)
		}
		s := float64(v) / 255
		if s <= 0.03928 {
			sum += w[i] * (s / 12.92)
			continue
		}
		sum += w[i] * math.Pow((s+0.055)/1.055, 2.4)
	}
	return sum
}

func contrastRatio(t *testing.T, fg, bg string) float64 {
	t.Helper()
	a, b := hexLuminance(t, fg), hexLuminance(t, bg)
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}

// TestDarkThemeTokens 深色必须由令牌给出（系统偏好 + 手动切换两条路径），
// 而不是零散的硬编码颜色。
func TestDarkThemeTokens(t *testing.T) {
	css := readFrontend(t, filepath.Join("assets", "style.css"))
	for _, want := range []string{"prefers-color-scheme: dark", `html[data-theme="dark"]`, "--bg:", "--panel:"} {
		if !strings.Contains(css, want) {
			t.Errorf("样式层缺少 %q：深色模式必须走语义令牌", want)
		}
	}
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	if !strings.Contains(js, "data-theme") {
		t.Errorf("前端未提供外观切换（data-theme），深色只能依赖系统偏好")
	}
}

// TestTopbarCarriesContextAndStatus 顶栏承载「当前位置 + 本机状态 + 外观/密度工具」：
// 状态必须在任何面板都看得见（不是进某个面板才出现一次），侧栏退回纯导航职责。
func TestTopbarCarriesContextAndStatus(t *testing.T) {
	html := readFrontend(t, "index.html")
	main := strings.Split(html, `<main>`)[1]
	bar := strings.Index(main, `<header id="topbar">`)
	panels := strings.Index(main, `<div id="panels">`)
	if bar < 0 || panels < 0 || bar > panels {
		t.Errorf("顶栏未先于面板出现（topbar=%d, panels=%d）——当前位置与本机状态应常驻顶栏，不随面板切换消失", bar, panels)
	}
	for _, want := range []string{`id="crumb-cur"`, `id="crumb-ctx"`, `id="t-lib"`,
		`id="t-pending"`, `id="t-task"`, `id="t-region"`, `id="t-density"`, `id="theme-toggle"`} {
		if !strings.Contains(main, want) {
			t.Errorf("顶栏缺少 %s——当前位置、法规库总数、待复核提醒、进行中的获取任务与外观/密度工具都在顶栏", want)
		}
	}
	start := strings.Index(html, `<aside id="side">`)
	end := strings.Index(html, `</aside>`)
	side := html[start:end]
	for _, banned := range []string{"btn-theme", "theme-toggle", "t-lib", "t-pending"} {
		if strings.Contains(side, banned) {
			t.Errorf("侧栏出现 %q——状态与外观工具已上移顶栏，侧栏只留导航，不得两处各一套", banned)
		}
	}
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	for _, want := range []string{`$("#theme-toggle")`, `$("#t-density")`, "setTopStatus(", `$("#t-region")`, "setCrumb("} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——顶栏的状态与工具必须由脚本接上，不能只是静态标记", want)
		}
	}
}

// TestNavAndPanesUseIconChrome 导航项与面板头要有图标承载识别，且图标由脚本集中注入
// （界面文件不堆 SVG 源码）。图标只承载识别：结论仍是文字与数字。
// 阅读控制（字号/行宽）挂在右列详情头——原文只在右列展开。
func TestNavAndPanesUseIconChrome(t *testing.T) {
	html := readFrontend(t, "index.html")
	nav := strings.Split(strings.Split(html, `<nav id="nav">`)[1], `</nav>`)[1]
	for _, p := range strings.Split(nav, `class="nav`)[1:] {
		if !strings.Contains(p, "data-icon=") {
			t.Errorf("导航项缺少 data-icon：图标承载识别，缺图标即导航层级塌陷")
			break
		}
	}
	browse := strings.Split(html, `<section id="panel-browse"`)[1]
	browse = browse[:strings.Index(browse, `<section id="`)]
	for _, want := range []string{`class="panehd"`, `id="b-pane-cnt"`} {
		if !strings.Contains(browse, want) {
			t.Errorf("浏览面板缺少 %s——列表列有面板头并如实报列出/共计", want)
		}
	}
	detail := strings.Split(html, `<aside id="detail"`)[1]
	detail = detail[:strings.Index(detail, `</aside>`)]
	for _, want := range []string{`id="detail-title"`, `id="detail-body"`,
		`id="b-fs-down"`, `id="b-fs-up"`, `id="b-wide"`, `id="detail-close"`} {
		if !strings.Contains(detail, want) {
			t.Errorf("右列详情缺少 %s——原文在右列展开，阅读控制挂在详情头", want)
		}
	}
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	for _, want := range []string{"ICON_PATHS", "applyIcons(", "applyIcons();"} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——图标集中在脚本注入，缺失即界面图标整体消失", want)
		}
	}
	css := readFrontend(t, filepath.Join("assets", "style.css"))
	if !strings.Contains(css, ".ic {") {
		t.Errorf("样式缺少 .ic 图标规则")
	}
}

// TestFacetGroupsAreCollapsible 分面组必须可折叠并自带组头（chevron + 微标题 +
// 已选/计数 adornment），折叠状态记住；长维度默认收起并给出「显示全部」。
// 分面栏常驻左列（三栏布局），不再随面板搬运。
func TestFacetGroupsAreCollapsible(t *testing.T) {
	html := readFrontend(t, "index.html")
	rail := strings.Split(html, `<div id="filters"`)[1]
	rail = rail[:strings.Index(rail, `</aside>`)]
	for _, dim := range []string{"level", "confirm", "year", "source", "region"} {
		if !strings.Contains(rail, `id="g-`+dim+`"`) || !strings.Contains(rail, `id="ad-`+dim+`"`) {
			t.Errorf("分面组 %s 缺少组头或计数 adornment——折叠状态下也要能看出该维度筛的是哪一项", dim)
		}
	}
	if !strings.Contains(rail, `aria-controls="f-`) {
		t.Errorf("分面组头缺少 aria-controls——折叠控件必须可被辅助技术定位")
	}
	if got := strings.Count(rail, `class="fhd"`); got < 5 {
		t.Errorf("分面组头只有 %d 处，应为 5 个维度各有其主", got)
	}
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	for _, want := range []string{"GROUP_DEFAULT", "setGroupOpen(", `"ol-fgrp"`, "data-more="} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——折叠状态需持久化，长维度需给出「显示全部」", want)
		}
	}
}

// TestAsyncFeedbackGuards 异步反馈与按钮状态纪律（2026-11 交互评审结论）：
// ① toast 必须能被辅助技术播报（role=status + aria-live）——读屏用户不得对全部
//
//	异步反馈完全静默；
//
// ② 取列必须有进行状态（面板头「正在取列…」）与序号守卫（lawsSeq）——改完条件
//
//	界面不得停在旧列表上，先发后到的旧响应不得覆盖新结果；
//
// ③ 离线更新包导入必须默认锁定、校验通过且来源受信才解锁——「先校验」要如实
//
//	反映在按钮状态上，不得只靠后端拒绝（AGENTS.md：写路径从严）；
//
// ④ 定位流程进行中必须锁定按钮防重入——授权与坐标获取最长一分钟，连点不得
//
//	产生并发轮询。
func TestAsyncFeedbackGuards(t *testing.T) {
	html := readFrontend(t, "index.html")
	toast := strings.Split(html, `<div id="toast"`)[1]
	toast = toast[:strings.Index(toast, ">")]
	if !strings.Contains(toast, `role="status"`) || !strings.Contains(toast, `aria-live="polite"`) {
		t.Errorf(`toast 缺 role="status"/aria-live="polite"——异步反馈必须可被辅助技术播报`)
	}
	if !strings.Contains(html, `id="btn-pack-import" class="warn" disabled`) {
		t.Errorf(`导入按钮必须默认锁定——「必须先校验」要如实反映在按钮状态上`)
	}
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	for _, want := range []string{"lawsSeq", "正在取列…", "packVerified", "btn.disabled = true", "locateFlow"} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——进行/校验状态必须如实反映在界面上，不得让用户对着旧结果猜测", want)
		}
	}
	// 批量处理与离线包同一纪律：执行按钮默认锁定、预览有可处理条目才解锁；
	// 导入终态回显并入统一轮询（不得每轮导入自开计时器）。
	if !strings.Contains(html, `id="rg-b-apply" class="ok" disabled`) {
		t.Errorf(`批量执行按钮必须默认锁定——「先干跑再执行」要如实反映在按钮状态上`)
	}
	for _, want := range []string{"rgBulkLock", "packWatchID", "到条文检索查看全部"} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——封顶与范围核对的出口/锁定必须在位", want)
		}
	}
	// 面板命名：全库条文全文检索面板必须叫「条文检索」——与首页「关键词检索」
	// 一字之差的旧名「法规检索」会让首次用户混用两个入口。
	if strings.Contains(html, ">法规检索<") || strings.Contains(js, `"法规检索"`) {
		t.Errorf(`「法规检索」旧名回流——第三个查法规入口必须叫「条文检索」（见 design-ui-language.md 术语表）`)
	}
}

// TestResponsiveTiers 响应式分档门禁（2026-11 响应式评审）：
// ① 详情列必须按「每列最小可用宽」收窄并在 ≤1150 起浮层——旧实现浮层太晚（≤860），
//
//	861–1150 段主列被挤到约 270px；
//
// ② 双列表单必须 ≤1240 单列——旧断点（≤1080）在 1081–1235 段留下横向溢出带；
// ③ 浮层必须给键盘出口（Esc）；
// ④ browse-split 是筛选栏迁入左列后的死规则，不得回流。
func TestResponsiveTiers(t *testing.T) {
	css := readFrontend(t, filepath.Join("assets", "style.css"))
	for _, want := range []string{"@media (max-width: 1150px)", "@media (max-width: 1240px)", "@media (max-width: 1439px)"} {
		if !strings.Contains(css, want) {
			t.Errorf("样式缺少 %s 分档——窄窗口必须按每列最小可用宽退化，不得回到整段溢出/挤没的旧断点", want)
		}
	}
	if strings.Contains(css, "browse-split") {
		t.Errorf("browse-split 死规则回流——筛选栏已常驻左列，浏览页没有第三套分栏")
	}
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	if !strings.Contains(js, `"Escape"`) || !strings.Contains(js, "hideDetail()") {
		t.Errorf("详情浮层缺少 Esc 出口——浮层不得只有 ✕ 一个关闭方式")
	}
}

// TestCountStatesAreWordsNotPlaceholders 计数在取到之前与失败之后都必须是一句白话。
//
// 筛选栏摘要原写作「— 部法规符合当前条件」：那个破折号是未知计数的占位符。请求在途时
// 它读起来像一个数，请求失败后它就永远留在屏上，而下方列表同一时刻写着「读取失败」——
// 同一屏两处对同一件事两种说法。诚实展示纪律要求缺失写成话并给出去处，不是写成符号。
// 顺带把两处令牌化收口：字体栈与向上投影都必须在令牌块里，深色主题才跟得上。
func TestCountStatesAreWordsNotPlaceholders(t *testing.T) {
	html := readFrontend(t, "index.html")
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	css := readFrontend(t, filepath.Join("assets", "style.css"))

	if strings.Contains(html, `class="num">—`) || strings.Contains(html, `class="num">-<`) {
		t.Error("计数槽位用占位符当初始值：进行中与失败必须各有一句白话（诚实展示纪律）")
	}
	if !strings.Contains(html, `id="f-sum-state">正在统计`) {
		t.Error(`进行态必须写明「正在统计」，不得留空或留符号`)
	}
	for _, want := range []string{`$("#f-sum-ok")`, `$("#f-sum-state")`, "计数未能读取", "取列未完成"} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——计数交接或失败态缺失：数没到手之前不得把句子交出去", want)
		}
	}
	if strings.Contains(css, `font: 700 13px/1 sans-serif`) {
		t.Error("品牌字仍用 sans-serif 绕过字体栈：「法」字实际落在 UA 默认字体上，必须走 --font-ui")
	}
	for _, want := range []string{"--font-ui:", "overflow-x: clip", "box-shadow: var(--shadow-up)"} {
		if !strings.Contains(css, want) {
			t.Errorf("样式缺少 %s——字体栈、横向裁剪与向上投影必须令牌化或成文", want)
		}
	}
}

// TestIconsAreSVGNotGlyphs 独立成控件的符号必须是集中注入的 SVG，不得用 Unicode 字形。
//
// 字形符号（▸ ✕ ›）落在各平台各自的字体上：macOS 与 Windows 会换成不同字形，尺寸无法
// 与 16px 线性图标对齐，深色主题下也不能随前景色翻转，而本仓库已有 ICON_PATHS 集中注入
// 这一条唯一路径（同一代码路径纪律同样适用于图标）。折叠组头有静态（index.html）与
// 动态（app.js 渲染法域卡）两种产出，两处都必须声明，否则删掉字形后那一处就没有箭头。
// 字号按钮 A− / A+ 保留文字形态——那是字号本身的隐喻，不是图标。
func TestIconsAreSVGNotGlyphs(t *testing.T) {
	css := readFrontend(t, filepath.Join("assets", "style.css"))
	html := readFrontend(t, "index.html")
	js := readFrontend(t, filepath.Join("assets", "app.js"))

	for _, g := range []struct{ name, ch string }{{"折叠三角", "\u25b8"}, {"关闭", "\u2715"}, {"右箭头", "\u203a"}} {
		if strings.Contains(css, `content: "`+g.ch+`"`) || strings.Contains(css, `content:"`+g.ch+`"`) {
			t.Errorf("样式用 content 画了 Unicode 字形 %s（%s）：图标必须走 ICON_PATHS", g.name, g.ch)
		}
		for _, src := range []struct{ name, text string }{{"index.html", html}, {"app.js", js}} {
			if strings.Contains(src.text, ">"+g.ch+"<") {
				t.Errorf("%s 把 %s（%s）当控件内容：字形随平台变样且不随前景翻转", src.name, g.name, g.ch)
			}
		}
	}
	for _, want := range []string{"close: '<path", "'chevron-right': '<path"} {
		if !strings.Contains(js, want) {
			t.Errorf("图标表缺少 %s——关闭与箭头必须与其余图标同一套 SVG", want)
		}
	}
	for _, src := range []struct{ name, text string }{{"index.html", html}, {"app.js", js}} {
		for _, line := range strings.Split(src.text, "\n") {
			if !strings.Contains(line, "<summary") {
				continue
			}
			if !strings.Contains(line, `data-icon="chevron"`) && !strings.Contains(line, `icon("chevron")`) {
				t.Errorf("%s 的折叠组头没有图标声明（删掉字形后这里就没有箭头了）：%s",
					src.name, strings.TrimSpace(line))
			}
		}
	}
	// 浏览器自己画的表面必须随主题：选区、插入符、滚动条（深色面板上默认值刺眼）。
	for _, want := range []string{"::selection {", "caret-color: var(--acc)", "scrollbar-color: var(--muted)"} {
		if !strings.Contains(css, want) {
			t.Errorf("样式缺少 %s——UA 默认表面不随主题，深色下对比与观感都不对", want)
		}
	}
	// 按下态不得缺席：此前只有 :hover，鼠标按下的瞬间界面毫无变化。
	for _, want := range []string{"button:active", ".item:active", ".lawcard:active", ".fchip:active", ".nav:active"} {
		if !strings.Contains(css, want) {
			t.Errorf("样式缺少 %s 的按下态——可点控件必须给出「按到了」的反馈", want)
		}
	}
}

// TestGuidanceNotTitleOnly 下一步指引不得只活在 title 里。
//
// title 对键盘用户与触屏用户完全不可达（WCAG 1.4.13 内容在聚焦或悬停时可见），
// 而本产品大量指引正是「屏上不是全部」「这里缺东西」这类会让人误判的话。
// 因此：能改变用户判断的说明必须落在可见文字里，title 只留纯核对细节
// （原始 UTC 时间、任务参数、计数明细）。图标按钮另有一层：title 不是可靠的
// 可编程名称，必须给 aria-label（WCAG 4.1.2）。
func TestGuidanceNotTitleOnly(t *testing.T) {
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	html := readFrontend(t, "index.html")

	// ① 取列封顶的下一步必须渲染成可见文本，不能只有 pc.title。
	if !strings.Contains(js, `class="capnote"`) {
		t.Error(`取列封顶（前 300 部）的说明必须渲染成可见文本（class="capnote"），不得只挂 pc.title`)
	}
	if !strings.Contains(js, "已列出前 300 部") {
		t.Error(`取列封顶的可见说明缺失——「屏上不是全部」必须让用户看得见`)
	}

	// ② 图标按钮必须有可编程名称：无文字内容的按钮必须带 aria-label。
	ibtnRe := regexp.MustCompile(`(?s)<button class="ibtn[^"]*"[^>]*>([^<]*)</button>`)
	for _, m := range ibtnRe.FindAllStringSubmatch(html, -1) {
		tag, text := m[0], strings.TrimSpace(m[1])
		if text == "" && !strings.Contains(tag, `aria-label=`) {
			t.Errorf("图标按钮无可编程名称（只有 title 不算）：%s", tag)
		}
	}
	// 状态型图标按钮的开合由 aria-pressed 报，名称必须在脚本里静态同步一份。
	if !strings.Contains(js, `setAttribute("aria-label", "紧凑模式")`) ||
		!strings.Contains(js, `setAttribute("aria-label", "阅读行宽")`) {
		t.Error(`紧凑模式 / 阅读行宽两个开关按钮未在脚本里同步 aria-label——只有 title 时读屏拿不到名称`)
	}
}

// TestRowMetaMergesMissing 条目行的缺失必须如实但只说一次：不得逐字段重复「未标注」
// 刷屏（同一行三遍未标注是噪声，不是信息），也不得把缺失藏起来。
func TestRowMetaMergesMissing(t *testing.T) {
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	if !strings.Contains(js, "rowMeta(") {
		t.Fatalf("前端缺少 rowMeta——条目元信息必须集中产出，便于核对其诚实与克制")
	}
	if !strings.Contains(js, `<span class="miss">未标注 `) {
		t.Errorf("缺失项未合并成一个标记：应给出可见的「未标注 N 项（缺哪些字段）」，说明必须紧跟在标签后可见")
	}
	if strings.Contains(js, `class="miss" title=`) || strings.Contains(js, `class="miss"title=`) {
		t.Errorf("「未标注」缺哪些字段与去哪补必须写在可见文字里：title 对键盘与触屏完全不可达（WCAG 1.4.13）")
	}
	// Go 侧对空效力等级返回诚实默认值「效力等级未标注」（非空串）：前端必须按它判定缺失，
	// 否则缺失会被当成已知值排进元信息行，合并标记永远不出现。
	if !strings.Contains(js, `LEVEL_UNLABELED = "效力等级未标注"`) {
		t.Errorf("前端未按 Go 侧的诚实默认值判定效力等级缺失——缺失标记会失效")
	}
	if strings.Contains(js, `escapeHTML(r.issuer || "发布机关未标注")`) {
		t.Errorf("条目行仍在逐字段重复「未标注」——同一行三遍未标注属设计缺陷")
	}
	css := readFrontend(t, filepath.Join("assets", "style.css"))
	if !strings.Contains(css, ".item .miss") || !strings.Contains(css, ".item.pend") {
		t.Errorf("样式缺少缺失标记或待复核强调条规则")
	}
}

// TestCitationPanelAmbiguousName OL-1010 的诚实展示：核验面板必须并列同名候选交人工，
// 不得把它显示成「未找到对应条文」；按简称/别名命中时必须把认定依据摆出来（OL-4）。
func TestCitationPanelAmbiguousName(t *testing.T) {
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	if !strings.Contains(js, `"OL-1010"`) {
		t.Errorf("核验面板未处理 OL-1010——多候选会被当成「未找到」，人工无从选择")
	}
	if !strings.Contains(js, "c.candidates") || !strings.Contains(js, "不自动采信") {
		t.Errorf("核验面板未并列候选或未声明不自动采信")
	}
	if !strings.Contains(js, "c.resolved_from") {
		t.Errorf("核验面板未按简称/别名命中显示认定依据")
	}
	if !strings.Contains(js, "c.date_disambiguated") {
		t.Errorf("核验面板未显示「同名按日期唯一确定」——静默认定同名法规属诚实展示缺陷")
	}
}

// TestProductReviewPanels 产品评审落地的界面契约（P0-1 / P0-2 / P1-1 / P1-4 / P1-5 / P2-7 / P2-8）：
// ① 核验引用是主工作区的一等入口（招牌动作不得藏在折叠里）；
// ② 复核兑现与本机使用情况必须在库总览可见，且带口径声明；
// ③ 只读查询版必须收掉写路径入口；
// ④ 零命中必须给出「为什么 + 可以怎么做」，不得只剩一句「没有找到」；
// ⑤ 实验通道（欧盟/美国）必须如实标注；机器确认的语义必须常驻。
func TestProductReviewPanels(t *testing.T) {
	html := readFrontend(t, "index.html")
	js := readFrontend(t, filepath.Join("assets", "app.js"))

	for _, want := range []string{`id="panel-verify"`, `id="nav-verify"`, `id="v-text"`, `id="btn-verify"`, `id="v-out"`,
		`id="h-verify"`, `id="search-guide"`, `id="ov-coverage"`, `id="ov-coverage-bar"`, `id="ov-coverage-list"`,
		`id="ov-cred"`, `id="ov-edition"`, `id="ov-usage"`} {
		if !strings.Contains(html, want) {
			t.Errorf("缺少 %s——评审落地的入口/说明缺失，用户看不到产品到底兑现了什么", want)
		}
	}
	// 主导航只谈「查法规」，核验引用属查法规动作。
	if !strings.Contains(js, `"nav-verify"`) || !strings.Contains(js, `verify: "核验引用"`) {
		t.Errorf("核验引用未进主导航/面包屑——招牌动作藏在折叠里等于没有")
	}
	// 六态逐态给出不同说法与下一步，不得一律「未找到」。
	for _, v := range []string{"verified:", "quote_mismatch:", "not_found:", "ambiguous:", "expired:", "broken_chain:", "incomplete:"} {
		if !strings.Contains(js, v) {
			t.Errorf("核验结论缺少 %s 的白话映射——六态必须逐态给出下一步", v)
		}
	}
	if !strings.Contains(js, "app().VerifyCitationText(") || !strings.Contains(js, "app().SearchGuided(") {
		t.Errorf("前端未走粘贴核验/带说明的检索绑定——不得在前端另判一套")
	}
	if !strings.Contains(js, "function guideBlock(") || !strings.Contains(js, "为什么") || !strings.Contains(js, "可以怎么做") {
		t.Errorf("零命中缺少「为什么 + 可以怎么做」——诚实出路是检索的一部分")
	}
	// 复核兑现必须带清单性质声明；本机使用情况必须带「不上传」口径。
	for _, want := range []string{"app().ReviewCoverage(", "cov.list_note", "app().UsageReport(", "u.note",
		"app().SetupRouteChosen("} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——复核兑现与使用情况的口径声明缺失会误导用户", want)
		}
	}
	// 只读查询版：隐藏的是入口，不是能力；档位由后端给出，前端不得自判。
	for _, want := range []string{"app().AppEdition(", "function applyEdition(", `edition === "reader"`} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——发行版分档必须由后端档位驱动", want)
		}
	}
	// 实验通道标注 + 机器确认免责常驻（不得只写在折叠里）。
	if !strings.Contains(js, `c === "EU" || c === "US"`) || !strings.Contains(js, "实验通道") {
		t.Errorf("欧盟/美国通道未标注实验通道——与主战场并列叙事会误导预期")
	}
	if !strings.Contains(html, `id="rv-channel"`) {
		t.Errorf("智能审核通道状态行缺失")
	}
	if !strings.Contains(html, "不等于官方认定") {
		t.Errorf("智能审核的语义未常驻主视图——机器确认被理解成官方认定属期望管理缺陷")
	}
	// 数据来源与可信度卡必须给出「不替人裁决」与「不构成法律意见」。
	for _, want := range []string{"不构成法律意见", "不替人裁决"} {
		if !strings.Contains(js, want) && !strings.Contains(html, want) {
			t.Errorf("关于/可信度说明缺少 %q", want)
		}
	}
}
