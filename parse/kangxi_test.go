package parse

import "testing"

// TestArticleNumber 条号标记解析为整数：阿拉伯与中文写法必须落到同一个数字，
// 否则引用核验会把「库里找得到」错报成「找不到条文」。无法确定时一律 ok=false，不得猜。
func TestArticleNumber(t *testing.T) {
	ok := map[string]int{
		"第264条": 264, "第二百六十四条": 264, "第 584 条": 584, "第五百八十四条": 584,
		"第十条": 10, "第二十条": 20, "第一百零八条": 108, "第一千二百六十条": 1260,
	}
	for in, want := range ok {
		if got, good := ArticleNumber(in); !good || got != want {
			t.Errorf("ArticleNumber(%q) = %d/%v，期望 %d", in, got, good, want)
		}
	}
	for _, in := range []string{"", "第条", "第X条", "第二百六十四", "款第二", "第〇条"} {
		if got, good := ArticleNumber(in); good {
			t.Errorf("ArticleNumber(%q) = %d，应判定为无法确定", in, got)
		}
	}
}

// TestChineseNumber 中文条号只用于显示与检索建议，判定一律走数字比对。
func TestChineseNumber(t *testing.T) {
	want := map[int]string{
		1: "一", 10: "十", 11: "十一", 20: "二十", 108: "一百零八", 584: "五百八十四",
		1000: "一千", 1260: "一千二百六十", 9999: "九千九百九十九",
	}
	for n, w := range want {
		if got := ChineseNumber(n); got != w {
			t.Errorf("ChineseNumber(%d) = %q，期望 %q", n, got, w)
		}
	}
	for _, n := range []int{0, -1, 10000} {
		if got := ChineseNumber(n); got != "" {
			t.Errorf("ChineseNumber(%d) = %q，超出范围应返回空串", n, got)
		}
	}
	// 往返一致：中文写法必须能被 ArticleNumber 解回原数字。
	for n := range want {
		if got, ok := ArticleNumber("第" + ChineseNumber(n) + "条"); !ok || got != n {
			t.Errorf("中文条号往返失败：%d → %q → %d/%v", n, ChineseNumber(n), got, ok)
		}
	}
}
