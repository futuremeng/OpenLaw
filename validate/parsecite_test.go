package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
)

// setupCiteDB 最小语料：一部有留证原文与签名清单的法规（溯源链完好）+ 一部文件缺失的法规。
// 溯源链完好是核验结论能落到 verified/quote_mismatch 等真实判定的前提，
// 否则一切都会被断链优先规则吞掉（那本身也是一条测试）。
func setupCiteDB(t *testing.T) (string, *store.DB) {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"pdf", "db"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	content := []byte("%PDF-1.4 mfd\n")
	sum := sha256.Sum256(content)
	if err := os.WriteFile(filepath.Join(dir, "pdf", "d_mfd.pdf"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO law(law_id, name, short_name, issuer, effect_level) VALUES
		('l_mfd','中华人民共和国民法典','民法典','全国人民代表大会','法律'),
		('l_pdf','中华人民共和国促进法',NULL,'全国人大常委会','法律')`)
	must(`INSERT INTO document(doc_id, file_name, file_path, file_size, sha256, hash_algo, hash_computed_at, confirm_status)
		VALUES('d_mfd','d_mfd.pdf','pdf/d_mfd.pdf',?,?, 'SHA-256','2026-01-01T00:00:00Z','confirmed'),
		('d_pdf','d_pdf.pdf','pdf/d_pdf.pdf',1,'bb','SHA-256','2026-01-01T00:00:00Z','pending')`,
		len(content), hex.EncodeToString(sum[:]))
	must(`INSERT INTO law_version(version_id, law_id, version_no, effective_date, doc_id) VALUES
		('v_mfd','l_mfd','2020版','2021-01-01','d_mfd'),
		('v_pdf','l_pdf','2025版','2025-01-01','d_pdf')`)
	must(`INSERT INTO article(article_id, version_id, article_no, content, md_path, doc_id, parser_version) VALUES
		('a_mfd_1','v_mfd','第一条','第一条 为了保护民事主体的合法权益……制定本法。','md/a.md','d_mfd','test'),
		('a_mfd_584','v_mfd','第五百八十四条','当事人一方不履行合同义务或者履行合同义务不符合约定，造成对方损失的，损失赔偿额应当相当于因违约所造成的损失。','md/b.md','d_mfd','test'),
		('a_pdf_1','v_pdf','第一条','第一条 为了促进……制定本法。','md/c.md','d_pdf','test')`)
	if err := BuildManifest(dir, keyPath(t), "", ""); err != nil {
		t.Fatal(err)
	}
	return dir, db
}

// TestParseCitationText 自由文字 → 要素：书名号、阿拉伯条号按数字归一、两种日期写法、引号内引文。
func TestParseCitationText(t *testing.T) {
	_, db := setupCiteDB(t)

	p := ParseCitationText(db, "依据《中华人民共和国民法典》第584条（2021-03-01）：「当事人一方不履行合同义务或者履行合同义务不符合约定」")
	if p.LawName != "中华人民共和国民法典" || p.LawNameFrom != FromBookTitle {
		t.Errorf("法规名识别错误: %q / %q", p.LawName, p.LawNameFrom)
	}
	if p.ArticleNumber != 584 || p.ArticleNo != "第五百八十四条" {
		t.Errorf("阿拉伯条号未按数字归一: %q (%d)", p.ArticleNo, p.ArticleNumber)
	}
	if p.AtDate != "2021-03-01" {
		t.Errorf("日期解析错误: %q", p.AtDate)
	}
	if !strings.Contains(p.Quote, "不履行合同义务") {
		t.Errorf("引文未从引号中提取: %q", p.Quote)
	}
	if p.ArticleNote == "" {
		t.Error("写法差异必须如实说明（第584条 → 第五百八十四条）")
	}

	p2 := ParseCitationText(db, "《中华人民共和国民法典》第五百八十四条规定，2021年3月1日 起适用")
	if p2.ArticleNo != "第五百八十四条" || p2.ArticleNote != "" {
		t.Errorf("库内写法不应产生归一说明: %q %q", p2.ArticleNo, p2.ArticleNote)
	}
	if p2.AtDate != "2021-03-01" {
		t.Errorf("中文日期解析错误: %q", p2.AtDate)
	}

	// 无书名号：按库内简称在文中匹配，并如实标注识别依据。
	p3 := ParseCitationText(db, "民法典第一千条")
	if p3.LawName != "中华人民共和国民法典" || p3.LawNameFrom != FromCorpusName {
		t.Errorf("库内简称匹配错误: %q / %q", p3.LawName, p3.LawNameFrom)
	}

	// 认不出法规名时必须如实说明，绝不从文中截取片段充当法规名。
	p4 := ParseCitationText(db, "根据相关规定，双方应当遵循诚信原则")
	if p4.LawName != "" || p4.LawNameFrom != FromNone || len(p4.Missing) == 0 {
		t.Errorf("认不出法规名时不得猜: %+v", p4)
	}
	if got := ParseCitationText(nil, ""); len(got.Missing) == 0 {
		t.Error("空输入必须给出可照做的说明")
	}
}

// TestVerifyCitationTextVerdicts 六态逐一给出：不同结论必须配不同下一步。
func TestVerifyCitationTextVerdicts(t *testing.T) {
	dir, db := setupCiteDB(t)
	const today = "2026-11-04"

	cases := []struct{ name, text, want string }{
		{"命中且一致", "《中华人民共和国民法典》第584条「当事人一方不履行合同义务或者履行合同义务不符合约定」", VerdictVerified},
		{"引文差异大", "《中华人民共和国民法典》第五百八十四条「本合同自双方签字盖章之日起生效并立即履行」", VerdictQuoteMismatch},
		{"库内没有该法规", "《中华人民共和国不确定法》第一条", VerdictNotFound},
		{"该版本无此条号", "《中华人民共和国民法典》第九千九百九十九条", VerdictNotFound},
		{"认不出法规名", "根据相关规定处理", VerdictIncomplete},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := VerifyCitationText(db, dir, c.text, "", today)
			if res.Verdict != c.want {
				t.Errorf("verdict = %q，期望 %q（%+v）", res.Verdict, c.want, res.Citation)
			}
			if len(res.NextSteps) == 0 {
				t.Error("每个结论都必须给出下一步（诚实展示纪律）")
			}
		})
	}

	// 现行版本不得谎报失效；所引版本已失效必须给出当前版本。
	if res := VerifyCitationText(db, dir, "《中华人民共和国民法典》第一条", "", today); res.Citation.Expired {
		t.Error("现行版本被误判为已失效")
	}
	if _, err := db.Exec(`UPDATE law_version SET expire_date='2025-01-01' WHERE version_id='v_mfd'`); err != nil {
		t.Fatal(err)
	}
	res := VerifyCitationText(db, dir, "《中华人民共和国民法典》第一条 2021-06-01", "", today)
	if res.Verdict != VerdictExpired {
		t.Errorf("失效版本应报 expired，实际 %q", res.Verdict)
	}

	// 溯源断链优先于一切结论：断链时不得给出「引用正确」（OL-1006）。
	if _, err := db.Exec(`UPDATE law_version SET expire_date=NULL WHERE version_id='v_mfd'`); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "manifest", "manifest.json")); err != nil {
		t.Fatal(err)
	}
	res = VerifyCitationText(db, dir, "《中华人民共和国民法典》第五百八十四条「当事人一方不履行合同义务或者履行合同义务不符合约定」", "", today)
	if res.Verdict != VerdictBrokenChain {
		t.Errorf("断链必须优先报 broken_chain，实际 %q", res.Verdict)
	}
}

// TestVerifyCitationTextDateNote 未给时点必须声明按今天判定，不得静默。
func TestVerifyCitationTextDateNote(t *testing.T) {
	dir, db := setupCiteDB(t)
	res := VerifyCitationText(db, dir, "《中华人民共和国民法典》第一条", "", "2026-11-04")
	if !strings.Contains(strings.Join(res.NextSteps, "\n"), "已按本机今天") {
		t.Errorf("未给引用时点必须如实声明判定口径: %q", res.NextSteps)
	}
}

// TestVerifyCitationTextDateOverride 界面显式指定的时点优先于文中认出的时点，
// 且声明里必须给出**实际使用**的那个日期（曾把文中日期写进「您指定的时点」）。
func TestVerifyCitationTextDateOverride(t *testing.T) {
	dir, db := setupCiteDB(t)
	res := VerifyCitationText(db, dir, "《中华人民共和国民法典》第一条 2021-01-01", "2025-06-01", "2026-11-04")
	joined := strings.Join(res.NextSteps, "\n")
	if !strings.Contains(joined, "已按您指定的引用时点（2025-06-01）") {
		t.Errorf("显式时点必须优先并如实声明: %q", res.NextSteps)
	}
	if strings.Contains(joined, "您指定的引用时点（2021") {
		t.Errorf("声明里的日期与实际判定不符: %q", res.NextSteps)
	}
	// 非法时点一律忽略，回落到文中/今天的口径——不得拿脏值去判定。
	res = VerifyCitationText(db, dir, "《中华人民共和国民法典》第一条", "今天", "2026-11-04")
	if strings.Contains(strings.Join(res.NextSteps, "\n"), "您指定的引用时点") {
		t.Errorf("非法显式时点不得当成已指定: %q", res.NextSteps)
	}
}

// TestChineseNumber 中文条号写法（仅用于显示与建议，判定一律走数字比对）。
func TestChineseNumber(t *testing.T) {
	cases := map[int]string{1: "一", 10: "十", 11: "十一", 21: "二十一", 105: "一百零五",
		584: "五百八十四", 1000: "一千", 2026: "二千零二十六"}
	for n, want := range cases {
		if got := parse.ChineseNumber(n); got != want {
			t.Errorf("ChineseNumber(%d) = %q，期望 %q", n, got, want)
		}
	}
	if got := parse.ChineseNumber(0); got != "" {
		t.Errorf("非法取值必须返回空串，实际 %q", got)
	}
	if n, ok := parse.ArticleNumber("第584条"); !ok || n != 584 {
		t.Errorf("ArticleNumber 阿拉伯写法解析失败: %d %v", n, ok)
	}
	if n, ok := parse.ArticleNumber("第五百八十四条"); !ok || n != 584 {
		t.Errorf("ArticleNumber 中文写法解析失败: %d %v", n, ok)
	}
}
