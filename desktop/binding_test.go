package main

// binding_test.go 前端 ↔ Go 绑定契约门禁。
// Wails 在实参个数与导出方法不符时运行期直接拒绝调用
// （"received N arguments to method 'main.App.X', expected M"），而面板加载是
// 一整段 try/catch——一处漂移就让整个面板落入 catch，只剩「读取失败」一行，
// 其余数据全部空白。此类错误编译期无感（前端是 JS），必须在测试期暴露。
//
// 门禁内容：app.js 中每一处 app().Method(...) 的实参个数，必须与 main.App
// 同名导出方法的参数个数一致；调用不存在的方法、或扫描器无法判定的调用，同样报错。

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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
