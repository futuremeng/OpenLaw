package parse

import "testing"

func TestNormalizeDate(t *testing.T) {
	cases := map[string]string{
		"2023-12-29":  "2023-12-29",
		"2023年12月29日": "2023-12-29",
		"2018年5月1日":   "2018-05-01",
		"2018/5/1":    "2018-05-01",
	}
	for in, want := range cases {
		got, err := NormalizeDate(in)
		if err != nil {
			t.Fatalf("%q 应成功，实际报错: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q 期望 %s，实际 %s", in, want, got)
		}
	}
	for _, bad := range []string{"", "2023年", "2023-13-01", "abc", "2023年13月1日"} {
		if _, err := NormalizeDate(bad); err == nil {
			t.Fatalf("%q 应报错", bad)
		}
	}
}

// TestActiveAtBoundary 时效边界（验收 8.1 时间回溯边界）：
// 左闭右开——生效当天命中，失效当天不命中。
func TestActiveAtBoundary(t *testing.T) {
	eff, exp := "2018-10-26", "2024-07-01"
	boundary := []struct {
		at   string
		want bool
	}{
		{"2018-10-25", false}, // 生效前一天
		{"2018-10-26", true},  // 生效当天（左闭）
		{"2024-06-30", true},  // 失效前一天
		{"2024-07-01", false}, // 失效当天（右开）
		{"2024-07-02", false},
	}
	for _, c := range boundary {
		if got := ActiveAt(eff, exp, c.at); got != c.want {
			t.Fatalf("ActiveAt(%s) 期望 %v，实际 %v", c.at, c.want, got)
		}
	}
	// 未失效：expire 为空
	if !ActiveAt(eff, "", "2099-01-01") {
		t.Fatal("未失效版本应长期命中")
	}
}
