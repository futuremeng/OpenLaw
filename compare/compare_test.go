package compare

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKangxiTableSize(t *testing.T) {
	// 表本体已下沉 parse，此处仅验证折叠行为经 Normalize 仍生效
	if got := Normalize("⼆"); got != "二" {
		t.Fatalf("部首码位折叠失败: %q", got)
	}
}

func TestNormalizeFoldsWhitespaceAndRadicals(t *testing.T) {
	a := "第一条　公司依法　登记。\n第二条⼆⼗条"
	b := "第一条公司依法登记。第二条二十条"
	if Normalize(a) != Normalize(b) {
		t.Fatalf("归一化不一致:\nA=%q\nB=%q", Normalize(a), Normalize(b))
	}
}

func writeTmp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCompareIdenticalAcrossLayout(t *testing.T) {
	a := writeTmp(t, "a.txt", "中华人民共和国\n城市 建设档案管理规定\n第一条　为了加强城市建设档案管理……")
	b := writeTmp(t, "b.txt", "中华人民共和国城市建设档案管理规定第一条为了加强城市建设档案管理……")
	rep, err := CompareFiles(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != "identical" || !rep.Consistent() {
		t.Fatalf("应判定 identical，实际 %+v", rep)
	}
	if rep.BytesIdentical {
		t.Fatal("字节级应不一致")
	}
}

func TestCompareContainment(t *testing.T) {
	a := writeTmp(t, "law.txt", "第一条　为了加强档案管理。第二条　适用本规定。")
	b := writeTmp(t, "issue.txt", "国务院公报目录\n中华人民共和国国务院任免人员\n第一条　为了加强档案管理。第二条　适用本规定。\n国务院关于批复\n")
	rep, err := CompareFiles(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != "contained" || rep.Containment != "a_in_b" || !rep.Consistent() {
		t.Fatalf("应判定 contained/a_in_b，实际 %+v", rep)
	}
}

func TestCompareDifferentReportsFirstDiff(t *testing.T) {
	a := writeTmp(t, "a.txt", "第一条　档案保管期限为三十年。")
	b := writeTmp(t, "b.txt", "第一条　档案保管期限为二十年。")
	rep, err := CompareFiles(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != "different" || rep.Consistent() {
		t.Fatalf("应判定 different，实际 %+v", rep)
	}
	if rep.FirstDiffPos < 0 || !strings.Contains(rep.ContextA, "三十") || !strings.Contains(rep.ContextB, "二十") {
		t.Fatalf("差异上下文不正确: %+v", rep)
	}
	if rep.Similarity < 0.8 {
		t.Fatalf("相似度应较高，实际 %f", rep.Similarity)
	}
}

func TestCompareHTMLStripsTags(t *testing.T) {
	htmlFile := writeTmp(t, "x.html", "<html><head><style>p{}</style><script>x()</script></head><body><p>第一条　档案&nbsp;保管。</p></body></html>")
	txtFile := writeTmp(t, "x.txt", "第一条档案保管。")
	rep, err := CompareFiles(htmlFile, txtFile)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != "identical" {
		t.Fatalf("HTML 去标签后应一致，实际 %+v", rep)
	}
}

func TestCompareBytesIdentical(t *testing.T) {
	p1 := writeTmp(t, "same1.txt", "同一样本")
	p2 := writeTmp(t, "same2.txt", "同一样本")
	rep, err := CompareFiles(p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != "bytes_identical" || !rep.BytesIdentical {
		t.Fatalf("应判定 bytes_identical，实际 %+v", rep)
	}
}

func TestCompareCoveredWithPageNoise(t *testing.T) {
	a := writeTmp(t, "law.txt", "第一条　为了加强档案管理，制定本法。第二条　本条适用范围如下。第三条　本规定自明年一月一日起施行。")
	b := writeTmp(t, "issue.txt", "第一条　为了加强档案管理，制定本法。国务院公报2026年第29号7第三条　本规定自明年一月一日起施行。第二条　本条适用范围如下。国务院公报2026年第29号8")
	rep, err := CompareFiles(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != "covered" || !rep.Consistent() {
		t.Fatalf("页码/页眉插在正文流中应判 covered，实际 %+v", rep)
	}
}

func TestCompareDifferentWithMissingChunk(t *testing.T) {
	a := writeTmp(t, "a.txt", "第一条　甲内容。第二条　乙内容。第三条　丙独有的内容。")
	b := writeTmp(t, "b.txt", "第一条　甲内容。第二条　乙内容改。")
	rep, err := CompareFiles(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != "different" {
		t.Fatalf("存在互缺句段应判 different，实际 %+v", rep)
	}
	found := false
	for _, m := range rep.Missing {
		if strings.Contains(m, "丙独有") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应给出缺失句段样例，实际 %+v", rep.Missing)
	}
}

func TestExtractMainContent(t *testing.T) {
	page := `<html><body><div class="head">页头导航</div>` +
		`<div class="pages_content" id="UCAP-CONTENT"><div class="trs"><p>第一条　正文。</p></div></div>` +
		`<div class="foot">页脚链接</div></body></html>`
	got := extractMainContent(page)
	if !strings.Contains(got, "正文") || strings.Contains(got, "页头") || strings.Contains(got, "页脚") {
		t.Fatalf("正文容器提取不正确: %q", got)
	}
}

func TestLoadTextRejectsUnknownExt(t *testing.T) {
	p := writeTmp(t, "x.doc", "不支持")
	if _, err := LoadText(p); err == nil {
		t.Fatal("未知扩展名应报错")
	}
}
