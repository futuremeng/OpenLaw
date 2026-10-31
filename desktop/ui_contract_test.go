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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
		`id="f-sources"`, `id="f-years"`, `id="f-regions"`, `id="btn-filter-clear"`} {
		if !strings.Contains(side, want) {
			t.Errorf("左列缺少 %q：多维筛选与分面计数是定位法规的手段，常驻左列", want)
		}
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
}

// TestThreeColumnAgentReviewUI 三栏结构与智能审核面板契约：右列共享详情带一键纠正
// 工具条；人工复核入口已由「智能审核」取代——人工只处理例外。技术操作
// （批量署名落判、手输路径、构建签名包、重载缓存）不得回流到常规界面。
func TestThreeColumnAgentReviewUI(t *testing.T) {
	html := readFrontend(t, "index.html")
	for _, want := range []string{`<aside id="side">`, `<main>`, `<aside id="detail"`, `id="fix-bar"`,
		`id="panel-review"`, `id="btn-review-run"`, `id="rv-stats"`, `id="rv-channel"`,
		`id="ex-list"`, `id="fix-confirm"`, `id="fix-reject"`, `id="btn-pack-pick"`} {
		if !strings.Contains(html, want) {
			t.Errorf("缺少 %s——三栏布局与智能审核控件是界面契约", want)
		}
	}
	for _, gone := range []string{`id="panel-confirm"`, `id="queue-list"`, `id="reviewer"`, `id="reason"`,
		`id="btn-bulk-preview"`, `id="btn-bulk-apply"`, `id="modal"`, `id="p-dir"`, `id="p-key"`,
		`id="btn-pack-build"`, `id="btn-risk-reload"`, `id="schema-badge"`} {
		if strings.Contains(html, gone) {
			t.Errorf("%s 重新出现——人工队列与技术操作已收进智能审核与折叠，不得回流常规界面", gone)
		}
	}
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	for _, want := range []string{"app().AgentStatus(", "app().StartReviewTask(", "app().AgentStats(",
		"app().ExceptionList(", "app().CorrectDoc(", "app().PickUpdatePack(",
		"loadReview(", "statusBadge(s, src)", `src === "agent"`, `src === "human"`} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——智能审核的计数、例外与一键纠正必须走同一库层路径；确认来源必须如实区分", want)
		}
	}
	css := readFrontend(t, filepath.Join("assets", "style.css"))
	for _, want := range []string{".badge.agent", ".badge.human", "#detail", "#fix-bar"} {
		if !strings.Contains(css, want) {
			t.Errorf("样式缺少 %s——智能体确认与人工确认必须区别显示，右列详情必须有布局规则", want)
		}
	}
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
		"智能审核统计分开计数\": true", "例外队列给出没把握原因\": true", "智能审核通道回显\": true",
		"条目徽章如实报智能体确认\": true"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("接线冒烟缺少期望产出 %q：\n%s", want, strings.TrimSpace(string(out)))
		}
	}
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

// TestRowMetaMergesMissing 条目行的缺失必须如实但只说一次：不得逐字段重复「未标注」
// 刷屏（同一行三遍未标注是噪声，不是信息），也不得把缺失藏起来。
func TestRowMetaMergesMissing(t *testing.T) {
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	if !strings.Contains(js, "rowMeta(") {
		t.Fatalf("前端缺少 rowMeta——条目元信息必须集中产出，便于核对其诚实与克制")
	}
	if !strings.Contains(js, `未标注 " + miss.length`) {
		t.Errorf("缺失项未合并成一个标记：应给出「未标注 N 项」并在 title 里列明缺什么、去哪补")
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
