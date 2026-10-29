package main

// binding_test.go 前端 ↔ Go 绑定契约门禁。
// Wails 在实参个数与导出方法不符时运行期直接拒绝调用
// （"received N arguments to method 'main.App.X', expected M"），而面板加载是
// 一整段 try/catch——一处漂移就让整个面板落入 catch，只剩「读取失败」一行，
// 其余数据全部空白。此类错误编译期无感（前端是 JS），必须在测试期暴露。
//
// 门禁内容：① app.js 中每一处 app().Method(...) 的实参个数，必须与 main.App
// 同名导出方法的参数个数一致（调用不存在的方法、或扫描器无法判定的调用同样报错）；
// ② app.js 引用的每个 `$("#id")` 元素必须在 index.html 中声明。

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// jsSkipString 从引号起始下标跳到该字符串/模板字面量结束之后的位置。
// 模板字面量整体跳过（含 ${...} 内的逗号与括号，不计入实参分隔）。
func jsSkipString(src string, i int) int {
	q := src[i]
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case q:
			return j + 1
		}
	}
	return len(src)
}

// jsArgCount 从调用左括号下标 open 起扫描，返回（顶层实参个数，右括号之后的下标）；
// 括号不闭合返回 (-1, len(src))。嵌套 ()/[]/{} 内的逗号不计，
// 字符串与模板字面量整体跳过（对象实参如 StartGovcnTask({...}) 计 1 个）。
func jsArgCount(src string, open int) (int, int) {
	depth, commas := 0, 0
	for i := open; i < len(src); i++ {
		switch c := src[i]; c {
		case '\'', '"', '`':
			i = jsSkipString(src, i) - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				if strings.TrimSpace(src[open+1:i]) == "" {
					return 0, i + 1
				}
				return commas + 1, i + 1
			}
		case ',':
			if depth == 1 {
				commas++
			}
		}
	}
	return -1, len(src)
}

// jsCallRe 匹配前端绑定调用 app().Method(。app() 是 api.js 定义的唯一绑定入口
// （window.go.main.App）；本前端源码中该形式不出现在字符串/注释里，故直接按
// 正则定位调用点，再由 jsArgCount 逐个核对实参个数。
var jsCallRe = regexp.MustCompile(`app\(\)\.([A-Za-z0-9_$]+)\(`)

// jsBindingCalls 返回 方法名 → 各处调用的实参个数，以及括号不闭合无法判定的方法名。
func jsBindingCalls(src string) (map[string][]int, []string) {
	out := map[string][]int{}
	var unparsed []string
	for _, m := range jsCallRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		n, _ := jsArgCount(src, m[1]-1)
		if n < 0 {
			unparsed = append(unparsed, name)
			continue
		}
		out[name] = append(out[name], n)
	}
	return out, unparsed
}

func TestFrontendBindingArity(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("frontend", "dist", "assets", "app.js"))
	if err != nil {
		t.Fatalf("前端源码不可读: %v", err)
	}
	calls, unparsed := jsBindingCalls(string(src))
	if len(calls) < 20 {
		t.Fatalf("仅扫到 %d 个 app() 绑定方法，扫描器可能已失效", len(calls))
	}
	if len(unparsed) > 0 {
		t.Errorf("以下绑定调用括号不闭合，无法核对实参个数: %v", unparsed)
	}
	rt := reflect.TypeOf(&App{})
	checked := 0
	for name, arities := range calls {
		m, ok := rt.MethodByName(name)
		if !ok {
			t.Errorf("前端调用 app().%s，但 main.App 无此导出方法", name)
			continue
		}
		if m.Type.IsVariadic() {
			t.Errorf("main.App.%s 为变参方法，契约门禁需扩展后再放行", name)
			continue
		}
		want := m.Type.NumIn() - 1 // 首参为接收者
		for _, got := range arities {
			checked++
			if got != want {
				t.Errorf("app().%s 实参 %d 个，main.App.%s 期望 %d 个——Wails 运行期会拒绝该调用，面板整段空白",
					name, got, name, want)
			}
		}
	}
	t.Logf("绑定契约核对通过：%d 个方法 / %d 处调用", len(calls), checked)
}

// ---- DOM 元素契约 ----
// 与实参漂移同一类故障：app.js 里 $("#x") 指向 index.html 中不存在的元素时，
// $() 返回 null，后续 .addEventListener/.innerHTML 抛错，整段面板加载落入 catch。
// 编译期与绑定层都无感，只能在测试期暴露。

var jsDomSelRe = regexp.MustCompile(`\$\("#([A-Za-z0-9_\-]+)"\)`)
var htmlIDRe = regexp.MustCompile(`id="([A-Za-z0-9_\-]+)"`)

func TestFrontendDOMElementIDs(t *testing.T) {
	js, err := os.ReadFile(filepath.Join("frontend", "dist", "assets", "app.js"))
	if err != nil {
		t.Fatalf("前端脚本不可读: %v", err)
	}
	html, err := os.ReadFile(filepath.Join("frontend", "dist", "index.html"))
	if err != nil {
		t.Fatalf("前端页面不可读: %v", err)
	}
	used := map[string]bool{}
	for _, m := range jsDomSelRe.FindAllStringSubmatch(string(js), -1) {
		used[m[1]] = true
	}
	if len(used) < 20 {
		t.Fatalf("仅扫到 %d 个元素 id，扫描器可能已失效", len(used))
	}
	declared := map[string]bool{}
	for _, m := range htmlIDRe.FindAllStringSubmatch(string(html), -1) {
		declared[m[1]] = true
	}
	// app.js 自己插入的元素（insertAdjacentHTML 等）也算已声明。
	for _, m := range htmlIDRe.FindAllStringSubmatch(string(js), -1) {
		declared[m[1]] = true
	}
	var missing []string
	for id := range used {
		if !declared[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("app.js 引用了 index.html 中不存在的元素 id: %v——$() 返回 null，整段面板会空白", missing)
		return
	}
	t.Logf("DOM 元素契约核对通过：%d 个 id 全部存在", len(used))
}
