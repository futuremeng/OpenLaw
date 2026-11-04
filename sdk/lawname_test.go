package sdk

import (
	"path/filepath"
	"testing"

	"github.com/futuremeng/OpenLaw/element"
	"github.com/futuremeng/OpenLaw/validate"
)

// addAliasCorpus 在最小语料上补一部带「以下简称」条款的法规，并构建别名表。
// manifest 必须重建：溯源链要求 manifest 收录该文件（否则诚实报断链，属预期行为）。
func addAliasCorpus(t *testing.T, o *OpenLaw) {
	t.Helper()
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := o.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO law(law_id, name, short_name) VALUES('l2','中华人民共和国民法典',NULL)`)
	must(`INSERT INTO law_version(version_id, law_id, version_no, effective_date, doc_id)
		VALUES('l2_v1','l2','2020版','2021-01-01','d1')`)
	must(`INSERT INTO article(article_id, version_id, article_no, content, md_path, doc_id, parser_version)
		VALUES('l2_v1_第一条','l2_v1','第一条','《中华人民共和国民法典》（以下简称《民法典》）……','md/d2.md','d1','test')`)
	if _, err := validate.BuildLawAliases(o.db, true); err != nil {
		t.Fatal(err)
	}
	signManifest(t, o)
}

// signManifest 生成签名密钥并重建 manifest（溯源链基准；与验收测试同一做法）。
func signManifest(t *testing.T, o *OpenLaw) {
	t.Helper()
	key := filepath.Join(t.TempDir(), "sign.key")
	if err := validate.GenerateKey(key); err != nil {
		t.Fatal(err)
	}
	if err := validate.BuildManifest(o.DataDir(), key, "", ""); err != nil {
		t.Fatal(err)
	}
}

// TestResolveVersionByShortName OL-4：简称解析到版本，命中依据与出处如实进 warnings。
func TestResolveVersionByShortName(t *testing.T) {
	o := openSDK(t)
	addAliasCorpus(t, o)

	res := o.ResolveVersion("《民法典》", "2025-01-01")
	if res.Status != StatusOK {
		t.Fatalf("简称应可解析: %+v", res)
	}
	if res.Data["law_id"] != "l2" || res.Data["resolved_from"] != validate.ResolvedFromAlias {
		t.Errorf("必须标注命中依据: %v", res.Data)
	}
	if !hasWarning(res.Warnings, "以下简称") || !hasWarning(res.Warnings, "l2_v1_第一条") {
		t.Errorf("别名依据与出处条文必须进 warnings: %v", res.Warnings)
	}

	// 未找到仍是 OL-1001；有法无版本仍是 OL-1002（既有契约不变）
	if got := o.ResolveVersion("不存在的法", "2025-01-01"); got.ErrorCode != CodeLawNotFound {
		t.Errorf("未找到应为 OL-1001: %+v", got)
	}
	if got := o.ResolveVersion("《民法典》", "2001-01-01"); got.ErrorCode != CodeNoVersionAtDate {
		t.Errorf("有法无版本应为 OL-1002: %+v", got)
	}
}

// TestResolveLawNameAmbiguousCandidates 同名多法：need_manual + 并列候选，不自动采信。
func TestResolveLawNameAmbiguousCandidates(t *testing.T) {
	o := openSDK(t)
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := o.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO law(law_id, name, issuer) VALUES
		('la','中华人民共和国安全生产法','全国人大常委会'),
		('lb','中华人民共和国安全生产法','国务院')`)
	must(`INSERT INTO law_version(version_id, law_id, version_no, effective_date, doc_id) VALUES
		('la_v1','la','2021版','2021-09-01','d1'),('lb_v1','lb','2014版','2014-12-01','d1')`)
	signManifest(t, o)

	res := o.ResolveLawName("中华人民共和国安全生产法")
	if res.Status != StatusNeedManual || res.ErrorCode != CodeAmbiguousLawName {
		t.Fatalf("多候选必须交人工: %+v", res)
	}
	cs, _ := res.Data["candidates"].([]map[string]any)
	if len(cs) != 2 {
		t.Fatalf("应并列 2 个候选: %v", res.Data["candidates"])
	}
	// 时间点能唯一收敛时按收敛处理，并如实提示同名
	if got := o.ResolveVersion("中华人民共和国安全生产法", "2015-01-01"); got.Status != StatusOK ||
		!hasWarning(got.Warnings, "同名") {
		t.Errorf("按时间点唯一收敛须提示同名: %+v", got)
	}
	if got := o.ResolveVersion("中华人民共和国安全生产法", "2021-12-01"); got.Status != StatusNeedManual ||
		got.ErrorCode != CodeAmbiguousLawName {
		t.Errorf("无法收敛应 need_manual: %+v", got)
	}
}

// TestCheckCitationResolvedFrom 引用校验按简称命中时带出依据（OL-4）。
func TestCheckCitationResolvedFrom(t *testing.T) {
	o := openSDK(t)
	addAliasCorpus(t, o)
	res := o.CheckCitation("《民法典》", "第一条", "《中华人民共和国民法典》（以下简称《民法典》）", "2025-01-01")
	if res.Status != StatusOK {
		t.Fatalf("简称引用应可校验: %+v", res)
	}
	if res.Data["resolved_from"] != validate.ResolvedFromAlias {
		t.Errorf("应带出命中依据: %v", res.Data)
	}
	if !hasWarning(res.Warnings, "别名") && !hasWarning(res.Warnings, "简称") {
		t.Errorf("须提示按简称命中: %v", res.Warnings)
	}
}

// TestListElements OL-3：已确认要件作为封闭候选；未登记案由 not_found；
// 已登记但全部待复核 → ok + 空表 + 提示。
func TestListElements(t *testing.T) {
	o := openSDK(t)
	if got := o.ListElements("不存在的案由"); got.Status != StatusNotFound {
		t.Fatalf("未登记案由应 not_found: %+v", got)
	}
	if got := o.ListElements("  "); got.Status != StatusNotFound {
		t.Fatalf("缺案由应 not_found: %+v", got)
	}

	if _, err := element.Import(o.db, []element.Input{
		{ElementID: "m1", ArticleID: "l1_v1_第一条", CaseType: "民间借贷", Name: "借贷合意"},
	}, true); err != nil {
		t.Fatal(err)
	}
	got := o.ListElements("民间借贷")
	if got.Status != StatusOK {
		t.Fatalf("案由已登记但无已确认要件应为 ok: %+v", got)
	}
	if es, _ := got.Data["elements"].([]map[string]any); len(es) != 0 {
		t.Errorf("pending 要件不得进候选: %v", got.Data["elements"])
	}
	if !hasWarning(got.Warnings, "尚未复核确认") {
		t.Errorf("必须提示待复核: %v", got.Warnings)
	}

	if _, err := element.Confirm(o.db, []string{"m1"}, "confirm", "王复核", "对照原文"); err != nil {
		t.Fatal(err)
	}
	got = o.ListElements("民间借贷")
	es, _ := got.Data["elements"].([]map[string]any)
	if len(es) != 1 || es[0]["element_id"] != "m1" || es[0]["name"] != "借贷合意" {
		t.Fatalf("已确认要件应作为候选: %v", got.Data["elements"])
	}
	if len(got.Warnings) != 0 {
		t.Errorf("有已确认要件时不应再有提示: %v", got.Warnings)
	}
}
