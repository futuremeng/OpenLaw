package parse

import (
	"strings"
	"testing"
)

// uslmFixture 合成 ECFR 风格 USLM：title>chapter>part(div)>div(SECTION) 嵌套，
// 外加一个只有 num+head 无正文的节（如实只留条号）。
const uslmFixture = `<?xml version="1.0" encoding="UTF-8"?>
<uslm>
  <ident>
    <citation>CFR Title 12</citation>
    <document-id country="us" agency="ecfr">title-12</document-id>
  </ident>
  <meta>
    <dc>
      <dc:title>12 CFR Parts 1-199</dc:title>
    </dc>
  </meta>
  <body>
    <title type="TITLE" N="12">
      <head>TITLE 12—BANKS AND BANKING</head>
      <chapter type="CHAPTER" N="I">
        <head>CHAPTER I—COMPTROLLER OF THE CURRENCY</head>
        <div type="PART" N="1">
          <num>Part 1</num>
          <head>PART 1—AUTHORITY AND PURPOSE</head>
          <div type="SECTION" N="1.1">
            <num>§ 1.1</num>
            <head>Authority.</head>
            <p>The Office of the Comptroller of the Currency administers
            the national banking system.</p>
            <p>Additional sentence.</p>
          </div>
          <div type="SECTION" N="1.2">
            <num>§ 1.2</num>
            <head>Scope.</head>
            <p>Applies to national banks.</p>
          </div>
        </div>
        <div type="PART" N="2">
          <num>Part 2</num>
          <head>PART 2—RULES OF PRACTICE</head>
        </div>
      </chapter>
    </title>
  </body>
</uslm>`

func TestParseUSLM(t *testing.T) {
	got, err := ParseUSLM([]byte(uslmFixture))
	if err != nil {
		t.Fatal(err)
	}
	if got.Citation != "CFR Title 12" {
		t.Errorf("citation: %q", got.Citation)
	}
	if !strings.Contains(got.Title, "BANKS AND BANKING") {
		t.Errorf("title: %q", got.Title)
	}
	// PART 1 含 SECTION 子 div——只作容器；SECTION×2 + 无子 div 的 PART 2 = 3 条。
	if len(got.Articles) != 3 {
		t.Fatalf("应解析出 3 条，实际 %d: %+v", len(got.Articles), got.Articles)
	}
	if got.Articles[0].No != "§ 1.1" {
		t.Errorf("条号: %q", got.Articles[0].No)
	}
	if !strings.Contains(got.Articles[0].Content, "national banking system") ||
		!strings.Contains(got.Articles[0].Content, "Additional sentence.") {
		t.Errorf("正文缺段: %q", got.Articles[0].Content)
	}
	if !strings.Contains(got.Articles[0].Content, "Authority.") {
		t.Errorf("正文缺 head: %q", got.Articles[0].Content)
	}
	// 无正文的 PART 2：如实保留 num+head，不得凭空造正文。
	if got.Articles[2].No != "Part 2" || !strings.Contains(got.Articles[2].Content, "RULES OF PRACTICE") {
		t.Errorf("纯标题节点: %+v", got.Articles[2])
	}
}

func TestParseUSLMRejectsEmpty(t *testing.T) {
	if _, err := ParseUSLM([]byte(`<uslm><body><title><head>Nothing</head></title></body></uslm>`)); err == nil {
		t.Fatal("零条文必须如实失败，不得产出空解析")
	}
	if _, err := ParseUSLM([]byte(`<uslm>`)); err == nil {
		t.Fatal("非法 XML 必须失败")
	}
}
