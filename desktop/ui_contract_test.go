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

// TestBrowseFilterRailIsPrimary 筛选必须是浏览页的主结构，且不得回流到侧栏：
// 地区等维度是「定位法规」的手段，属面板；侧栏只报状态。计数与列表必须同源
// （前端同一处调用 BrowseLawList / BrowseFacets，Go 侧共用 browseWhere）。
func TestBrowseFilterRailIsPrimary(t *testing.T) {
	html := readFrontend(t, "index.html")
	start := strings.Index(html, `<aside id="side">`)
	end := strings.Index(html, `</aside>`)
	if start < 0 || end <= start {
		t.Fatalf("未定位到侧栏结构")
	}
	side := html[start:end]
	for _, banned := range []string{"<select", "region-prov", "btn-locate", "region-bar"} {
		if strings.Contains(side, banned) {
			t.Errorf("侧栏出现筛选控件 %q——筛选属面板职责，侧栏只报状态", banned)
		}
	}
	parts := strings.Split(html, `<section id="panel-browse"`)
	if len(parts) != 2 {
		t.Fatalf("未定位到浏览面板")
	}
	body := parts[1]
	if i := strings.Index(body, `<section id="`); i >= 0 {
		body = body[:i]
	}
	for _, want := range []string{`class="filters"`, `id="f-levels"`, `id="f-confirms"`,
		`id="f-sources"`, `id="f-years"`, `id="f-regions"`, `id="btn-filter-clear"`} {
		if !strings.Contains(body, want) {
			t.Errorf("浏览面板缺少 %q：多维筛选与分面计数是本页主结构", want)
		}
	}
	js := readFrontend(t, filepath.Join("assets", "app.js"))
	for _, want := range []string{"app().BrowseLawList(", "app().BrowseFacets("} {
		if !strings.Contains(js, want) {
			t.Errorf("前端缺少 %s——列表与计数必须走同一条件，不能各算各的", want)
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
		"未标注地区如实说明\": true", "检索命中可就地收窄\": true"} {
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
