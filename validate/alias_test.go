package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildLawAliasesDryRunThenApply 默认试采不写库；apply 后幂等（再跑只更新不重复新增）。
func TestBuildLawAliasesDryRunThenApply(t *testing.T) {
	db := setupLawNameDB(t)
	if _, err := db.Exec(`INSERT INTO article(article_id, version_id, article_no, content, md_path, doc_id, parser_version)
		VALUES('v_fld_2','v_fld','第二条','《中华人民共和国未知法》（以下简称《未知法》）','md/b.md','d1','test')`); err != nil {
		t.Fatal(err)
	}

	dry, err := BuildLawAliases(db, false)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Applied {
		t.Errorf("apply=false 不得标记为已写入")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM law_alias`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("试采绝不可写库，实得 %d 行", n)
	}
	if dry.ArticlesScanned != 2 || dry.Extracted != 2 {
		t.Errorf("扫描/抽取计数 = %d/%d，期望 2/2", dry.ArticlesScanned, dry.Extracted)
	}
	if dry.Unresolved != 1 {
		t.Errorf("全称查不到应如实计数: %+v", dry)
	}
	// 期望收录：简称列（公司法）+ 文本式（民法典）= 2 条；未知法不收录
	if dry.Added != 2 {
		t.Errorf("will_add = %d，期望 2（%+v）", dry.Added, dry.Rows)
	}
	for _, r := range dry.Rows {
		if r.Alias == "未知法" && r.Status != "unresolved_full_name" {
			t.Errorf("无法反查全称的简称必须如实标未收录: %+v", r)
		}
	}

	rep, err := BuildLawAliases(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Applied || rep.Added != 2 || rep.Unresolved != 1 {
		t.Fatalf("写入报告异常: %+v", rep)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM law_alias`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("别名表应 2 行，实得 %d", n)
	}
	var src, art string
	if err := db.QueryRow(`SELECT source, COALESCE(source_article_id,'') FROM law_alias WHERE alias='民法典'`).Scan(&src, &art); err != nil {
		t.Fatal(err)
	}
	if src != AliasSourceText || art != "v_fld_1" {
		t.Errorf("文本式别名必须留来源与出处条文: %s %s", src, art)
	}

	// 幂等：再跑一次只更新，不重复新增
	again, err := BuildLawAliases(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if again.Added != 0 || again.Updated != 2 {
		t.Errorf("重复构建应全部计入更新: %+v", again)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM law_alias`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("重复构建不得增加行数: %d", n)
	}
}

// TestBuildLawAliasesSameAliasFromBothSources 简称列与文本条款给出同一别名时只落一行，
// 且 dry-run 计数不虚高（诚实展示纪律）。
func TestBuildLawAliasesSameAliasFromBothSources(t *testing.T) {
	db := setupLawNameDB(t)
	if _, err := db.Exec(`INSERT INTO article(article_id, version_id, article_no, content, md_path, doc_id, parser_version)
		VALUES('v_gssf_1','v_gssf','第一条','《中华人民共和国公司法》（以下简称公司法）','md/d.md','d1','test')`); err != nil {
		t.Fatal(err)
	}
	dry, err := BuildLawAliases(db, false)
	if err != nil {
		t.Fatal(err)
	}
	// 本例另有 民法典 一条文本式别名，故新增 2 条；公司法 的同批重复只算一次
	if dry.Added != 2 || dry.Duplicates != 1 {
		t.Errorf("同批重复不得虚高新增计数: added=%d dup=%d", dry.Added, dry.Duplicates)
	}
	rep, err := BuildLawAliases(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Added != 2 || rep.Duplicates != 1 {
		t.Errorf("写入报告同样不得虚高: %+v", rep)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM law_alias WHERE alias='公司法'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("同一 (alias, law_id) 只应一行: %d", n)
	}
	// 文本条款带出处条文，写入后必须保留（不被供应商镜像覆盖依据）
	var src, art string
	if err := db.QueryRow(`SELECT source, COALESCE(source_article_id,'') FROM law_alias WHERE alias='公司法'`).Scan(&src, &art); err != nil {
		t.Fatal(err)
	}
	if src != AliasSourceText || art != "v_gssf_1" {
		t.Errorf("必须保留更可溯源的来源与出处: %s %s", src, art)
	}
}

// TestWriteAliasReport CSV 报告含未收录样本，供人工巡检（诚实展示）。
func TestWriteAliasReport(t *testing.T) {
	db := setupLawNameDB(t)
	if _, err := db.Exec(`INSERT INTO article(article_id, version_id, article_no, content, md_path, doc_id, parser_version)
		VALUES('v_x_1','v_fld','第二条','《中华人民共和国未知法》（以下简称《未知法》）','md/c.md','d1','test')`); err != nil {
		t.Fatal(err)
	}
	rep, err := BuildLawAliases(db, false)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "alias.csv")
	if err := WriteAliasReport(path, rep); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	csv := string(b)
	for _, want := range []string{"alias,law_id,law_name,source,article_id,status",
		"will_add", "unresolved_full_name"} {
		if !strings.Contains(csv, want) {
			t.Errorf("报告缺少 %q:\n%s", want, csv)
		}
	}
}

// TestLawAliasStats 别名表规模与来源分布；表不存在时如实报缺失（不报错、不猜）。
func TestLawAliasStats(t *testing.T) {
	db := setupLawNameDB(t)
	st, err := LawAliasStats(db)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Exists || st.Total != 0 {
		t.Fatalf("空表应存在且为 0: %+v", st)
	}
	if _, err := BuildLawAliases(db, true); err != nil {
		t.Fatal(err)
	}
	st, err = LawAliasStats(db)
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 2 || st.Sources[AliasSourceText] != 1 || st.Sources[AliasSourceShortName] != 1 {
		t.Errorf("来源分布异常: %+v", st)
	}
	if len(st.Samples) != 2 {
		t.Errorf("样例应 2 条: %+v", st.Samples)
	}

	if _, err := db.Exec(`DROP TABLE law_alias`); err != nil {
		t.Fatal(err)
	}
	st, err = LawAliasStats(db)
	if err != nil {
		t.Fatal(err)
	}
	if st.Exists {
		t.Errorf("表缺失必须如实报 Exists=false: %+v", st)
	}
}
