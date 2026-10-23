package parse

import (
	"strings"
	"testing"
)

func TestSplitArticles(t *testing.T) {
	md := "# 中华人民共和国公司法（2018修正）\n\n" +
		"第二十六条 公司股东滥用公司法人独立地位和股东有限责任，逃避债务，严重损害公司债权人利益的，应当对公司债务承担连带责任。\n\n" +
		"第二十七条 股东可以用货币出资，也可以用实物、知识产权、土地使用权等作价出资。\n"
	arts := SplitArticles(md)
	if len(arts) != 2 {
		t.Fatalf("应拆出 2 条，实际 %d 条", len(arts))
	}
	if arts[0].No != "第二十六条" || arts[1].No != "第二十七条" {
		t.Fatalf("条号解析错误: %s / %s", arts[0].No, arts[1].No)
	}
	if len(arts[0].Content) <= len(arts[1].Content) {
		t.Fatal("第一条内容应包含完整条文直到下一条之前")
	}
	if !strings.HasPrefix(arts[0].Content, "第二十六条") {
		t.Fatalf("条文内容应以条号起始: %q", arts[0].Content[:20])
	}
}

func TestSplitArticlesNoArticle(t *testing.T) {
	if got := SplitArticles("# 说明\n本文件没有条文标记。\n"); got != nil {
		t.Fatalf("无条文应返回 nil，实际 %d 条", len(got))
	}
}

func TestSplitArticlesArabicNumber(t *testing.T) {
	arts := SplitArticles("第12条 测试条文内容。\n第13条 另一条。\n")
	if len(arts) != 2 || arts[0].No != "第12条" {
		t.Fatalf("阿拉伯数字条号解析失败: %+v", arts)
	}
}
