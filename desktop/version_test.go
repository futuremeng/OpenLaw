package main

// 版本注入门禁（遗留项「二进制无版本注入」的修复面）：
//  1. Status 必须恒带程序版本——技术信息折叠与「关于」靠它，库没打开成功
//     也要如实报出（诚实展示纪律）；
//  2. 树上 var version 必须是 "x.y.z" 形态——release 作业的
//     「the tag is the version」互检拿它与 tag 比对，形态跑偏即互检失灵。

import (
	"regexp"
	"testing"
)

func TestStatusCarriesAppVersion(t *testing.T) {
	app := NewApp(t.TempDir())
	st, _ := app.Status() // 不 Open()：即使数据打不开，版本也要给出
	if st.AppVersion != version {
		t.Fatalf("Status.AppVersion = %q，应恒等于树上 var version %q", st.AppVersion, version)
	}
}

func TestVersionLooksLikeSemver(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+`).MatchString(version) {
		t.Fatalf(`var version = %q，应为 "x.y.z" 形态（release 作业据此与 tag 互检）`, version)
	}
}
