package validate

import (
	"errors"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

// setupLawNameDB 最小语料：全称、简称列、文本式简称、同名多法、国名省略式简称各一部。
func setupLawNameDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir() + "/openlaw.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO law(law_id, name, short_name, issuer) VALUES
		('l_gssf','中华人民共和国公司法','公司法','全国人大常委会'),
		('l_pbf','中华人民共和国反外国制裁法',NULL,'全国人大常委会'),
		('l_aqf1','中华人民共和国安全生产法',NULL,'全国人大常委会'),
		('l_aqf2','中华人民共和国安全生产法',NULL,'国务院'),
		('l_fld','中华人民共和国民法典',NULL,'全国人民代表大会')`)
	must(`INSERT INTO document(doc_id, file_name, file_path, file_size, sha256, hash_computed_at)
		VALUES('d1','d1.pdf','pdf/d1.pdf',1,'aa','2026-01-01T00:00:00Z')`)
	must(`INSERT INTO law_version(version_id, law_id, version_no, effective_date, doc_id) VALUES
		('v_gssf','l_gssf','2024版','2024-07-01','d1'),
		('v_pbf','l_pbf','2021版','2021-06-10','d1'),
		('v_aqf1','l_aqf1','2021版','2021-09-01','d1'),
		('v_aqf2','l_aqf2','2014版','2014-12-01','d1'),
		('v_fld','l_fld','2020版','2021-01-01','d1')`)
	// 文本式简称：官方「以下简称」条款（可溯源，附出处条文）
	must(`INSERT INTO article(article_id, version_id, article_no, content, md_path, doc_id, parser_version)
		VALUES('v_fld_1','v_fld','第一条','《中华人民共和国民法典》（以下简称《民法典》）……','md/a.md','d1','test')`)
	must(`INSERT INTO article(article_id, version_id, article_no, content, md_path, doc_id, parser_version)
		VALUES('v_aqf2_1','v_aqf2','第一条','第一条 为了加强安全生产监督管理，防止和减少生产安全事故。','md/b.md','d1','test')`)
	return db
}

// TestNormalizeLawName 只做确定性清理：去书名号/引号与空白、全角括号转半角。
func TestNormalizeLawName(t *testing.T) {
	cases := map[string]string{
		"《中华人民共和国公司法》":       "中华人民共和国公司法",
		" 中华\n人民共和国 公司法　":    "中华人民共和国公司法",
		"中华人民共和国公司法（2024修订）": "中华人民共和国公司法(2024修订)",
		"「民法典」":              "民法典",
		"“公司法”":              "公司法",
		"":                   "",
	}
	for in, want := range cases {
		if got := NormalizeLawName(in); got != want {
			t.Errorf("NormalizeLawName(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestResolveLawNameSources 四级解析顺序与命中依据标注（OL-4）。
func TestResolveLawNameSources(t *testing.T) {
	db := setupLawNameDB(t)
	if _, err := BuildLawAliases(db, true); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name        string
		query       string
		wantLaw     string
		wantFrom    string
		wantArticle string
	}{
		{"全称精确", "中华人民共和国公司法", "l_gssf", ResolvedFromName, ""},
		{"书名号与空白清理后命中", "《 中华人民共和国公司法 》", "l_gssf", ResolvedFromName, ""},
		{"简称列命中", "《公司法》", "l_gssf", ResolvedFromShortName, ""},
		{"文本式简称命中并带出处条文", "《民法典》", "l_fld", ResolvedFromAlias, "v_fld_1"},
		{"国名省略式规则命中", "反外国制裁法", "l_pbf", ResolvedFromNameRule, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := ResolveLawName(db, c.query)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Candidates) != 1 || res.Ambiguous {
				t.Fatalf("应唯一命中，实得 %+v", res.Candidates)
			}
			got := res.Candidates[0]
			if got.LawID != c.wantLaw || got.ResolvedFrom != c.wantFrom {
				t.Errorf("命中 %+v，期望 %s / %s", got, c.wantLaw, c.wantFrom)
			}
			if got.BasedOnArticleID != c.wantArticle {
				t.Errorf("出处条文 = %q，期望 %q", got.BasedOnArticleID, c.wantArticle)
			}
		})
	}

	// 未命中不是错误：空候选，由调用方给 OL-1001
	res, err := ResolveLawName(db, "不存在的法")
	if err != nil || len(res.Candidates) != 0 {
		t.Errorf("未命中应返回空候选: %+v %v", res, err)
	}
	// 空输入
	if res, err := ResolveLawName(db, "  "); err != nil || len(res.Candidates) != 0 {
		t.Errorf("空白输入应返回空候选: %+v %v", res, err)
	}
}

// TestResolveLawNameAmbiguous 同名多法必须并列候选，不自动采信任何一条（OL-4）。
func TestResolveLawNameAmbiguous(t *testing.T) {
	db := setupLawNameDB(t)
	res, err := ResolveLawName(db, "中华人民共和国安全生产法")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Ambiguous || len(res.Candidates) != 2 {
		t.Fatalf("应给出 2 个候选并标注歧义: %+v", res)
	}
	if res.Candidates[0].LawID == res.Candidates[1].LawID {
		t.Errorf("候选必须按 law_id 去重: %+v", res.Candidates)
	}
}

// TestResolveLawNameWithoutAliasTable 语料无别名表（v8 及更早）：如实标注缺失，
// 仍走确定性名称规则，不猜别名。
func TestResolveLawNameWithoutAliasTable(t *testing.T) {
	db := setupLawNameDB(t)
	if _, err := db.Exec(`DROP TABLE law_alias`); err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLawName(db, "《民法典》")
	if err != nil {
		t.Fatal(err)
	}
	if !res.AliasTableMissing {
		t.Errorf("缺表必须如实标注: %+v", res)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].ResolvedFrom != ResolvedFromNameRule {
		t.Errorf("无别名表时应退回名称规则: %+v", res.Candidates)
	}
	if res.Candidates[0].BasedOnArticleID != "" {
		t.Errorf("无别名表时不应有别名出处: %+v", res.Candidates[0])
	}
}

// TestResolveNamedVersion 名称 → 版本第二跳：唯一命中 / 时间点收敛同名 / 歧义 / 该日期无版本。
func TestResolveNamedVersion(t *testing.T) {
	db := setupLawNameDB(t)
	if _, err := BuildLawAliases(db, true); err != nil {
		t.Fatal(err)
	}

	t.Run("简称解析到版本", func(t *testing.T) {
		rv, res, err := ResolveNamedVersion(db, "《公司法》", "2025-01-01")
		if err != nil {
			t.Fatal(err)
		}
		if rv == nil || rv.LawID != "l_gssf" || res.Candidates[0].ResolvedFrom != ResolvedFromShortName {
			t.Fatalf("解析结果 %+v %+v", rv, res.Candidates)
		}
	})

	t.Run("同名多法按时间点唯一收敛", func(t *testing.T) {
		rv, res, err := ResolveNamedVersion(db, "中华人民共和国安全生产法", "2015-01-01")
		if err != nil {
			t.Fatal(err)
		}
		if rv == nil || rv.LawID != "l_aqf2" || !res.DateDisambiguated || res.Ambiguous {
			t.Fatalf("应按时间点收敛为一部: %+v %+v", rv, res)
		}
	})

	t.Run("同名多法时间点无法收敛则报歧义", func(t *testing.T) {
		_, res, err := ResolveNamedVersion(db, "中华人民共和国安全生产法", "2021-12-01")
		if !errors.Is(err, ErrAmbiguousLawName) {
			t.Fatalf("应报歧义，实得 %v", err)
		}
		if len(res.Candidates) != 2 {
			t.Errorf("歧义必须并列候选: %+v", res.Candidates)
		}
	})

	t.Run("法规存在但该日期无生效版本", func(t *testing.T) {
		rv, res, err := ResolveNamedVersion(db, "《民法典》", "2001-01-01")
		if err != nil {
			t.Fatal(err)
		}
		if rv != nil || len(res.Candidates) == 0 {
			t.Fatalf("应为「有法规、无版本」: %+v %+v", rv, res.Candidates)
		}
	})

	t.Run("法规库中没有这部法规", func(t *testing.T) {
		rv, res, err := ResolveNamedVersion(db, "不存在的法", "2025-01-01")
		if err != nil {
			t.Fatal(err)
		}
		if rv != nil || len(res.Candidates) != 0 {
			t.Fatalf("应为未找到: %+v %+v", rv, res.Candidates)
		}
	})
}

// TestCheckCitationByShortName 引用校验按简称命中，并带出命中依据（OL-4）。
func TestCheckCitationByShortName(t *testing.T) {
	db := setupLawNameDB(t)
	if _, err := BuildLawAliases(db, true); err != nil {
		t.Fatal(err)
	}
	res, err := CheckCitation(db, t.TempDir(), "《民法典》", "第一条",
		"《中华人民共和国民法典》（以下简称《民法典》）", "2025-01-01", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if res.ErrorCode != "" {
		t.Fatalf("简称引用应可校验: %+v", res)
	}
	if res.ResolvedFrom != ResolvedFromAlias || res.AliasSource != AliasSourceText ||
		res.AliasArticle != "v_fld_1" || res.LawID != "l_fld" {
		t.Errorf("必须带出别名依据: %+v", res)
	}
}

// TestCheckCitationAmbiguousName 同名多法且时间点无法收敛：OL-1010 + 并列候选。
func TestCheckCitationAmbiguousName(t *testing.T) {
	db := setupLawNameDB(t)
	res, err := CheckCitation(db, t.TempDir(), "中华人民共和国安全生产法", "第一条", "任意", "2021-12-01", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if res.ErrorCode != CodeAmbiguousLawName || len(res.Candidates) != 2 {
		t.Fatalf("应报 OL-1010 并并列候选: %+v", res)
	}
}

// TestCheckCitationDateDisambiguated 同名多法在所给日期只有一部生效：按时间点唯一收敛
// 必须如实标注（不得静默认定其中一部）。
func TestCheckCitationDateDisambiguated(t *testing.T) {
	db := setupLawNameDB(t)
	res, err := CheckCitation(db, t.TempDir(), "中华人民共和国安全生产法", "第一条",
		"为了加强安全生产监督管理", "2015-01-01", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if res.ErrorCode != "" || !res.DateDisambiguated || res.LawID != "l_aqf2" {
		t.Errorf("按日期唯一收敛必须标注: %+v", res)
	}
}
