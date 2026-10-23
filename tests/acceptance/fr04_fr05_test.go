// FR-04 关联查询 / FR-05 要件比对 验收（追溯矩阵补齐）。
// 关联查询：pending 关系必须提示人工确认（10 章「机器不自动判定废止」）。
// 要件比对：输出风险假设清单（covered/missing），不输出胜诉结论（2.3）。
package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/sdk"
	"github.com/futuremeng/OpenLaw/store"
)

// setupWithLaw 导入样例法规，返回数据目录与 db。
func setupWithLaw(t *testing.T) (string, *store.DB) {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"pdf", "md", "db", "provenance"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	im := &ingest.Importer{DataDir: dir, DB: db}
	_, err = im.Import(ingest.Request{
		DocID: "law_company_2018", LawID: "law_company", LawName: "中华人民共和国公司法",
		ShortName: "公司法", Issuer: "全国人大常委会", EffectLevel: "法律",
		VersionNo: "2018修正", EffectiveDate: "2018-10-26", SourceType: "测试样例",
		PDFPath: filepath.Join("..", "..", "testdata", "fake_law.pdf"),
		MDPath:  filepath.Join("..", "..", "testdata", "law_company_2018.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir, db
}

// TestFR05AnalyzeElements 要件比对：命中/缺失分组正确，缺失带突破方向，含风险假设告警。
func TestFR05AnalyzeElements(t *testing.T) {
	dir, db := setupWithLaw(t)

	articleID := "law_company_2018修正_第二十六条"
	if _, err := db.Exec(`INSERT INTO element(element_id, article_id, case_type, name, description, how_to_attack)
		VALUES
		('el_confusion', ?, '法人人格否认', '人格混同', '财产、业务、人员混同', '要求对方提供独立账册与审计报告'),
		('el_abuse', ?, '法人人格否认', '滥用行为', '滥用法人独立地位逃避债务', '核查债务形成时点与转移资产行为')`,
		articleID, articleID); err != nil {
		t.Fatal(err)
	}

	ol, err := sdk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ol.Close()

	res := ol.AnalyzeElements("法人人格否认", []string{
		"两公司共用账户与人员，人格混同明显",
	})
	if res.Status != sdk.StatusOK {
		t.Fatalf("应为 ok，实际 %s (%s)", res.Status, res.ErrorCode)
	}
	data := res.Data
	covered, _ := data["covered"].([]map[string]any)
	missing, _ := data["missing_elements"].([]map[string]any)
	if len(covered) != 1 || covered[0]["name"] != "人格混同" {
		t.Fatalf("covered 应命中人格混同: %+v", covered)
	}
	if len(missing) != 1 || missing[0]["name"] != "滥用行为" {
		t.Fatalf("missing 应含滥用行为: %+v", missing)
	}
	if h, _ := missing[0]["how_to_attack"].(string); h == "" {
		t.Fatal("缺失要件必须给出突破方向 how_to_attack")
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "不构成胜诉") {
		t.Fatalf("必须带「风险假设清单，不构成胜诉结论」告警: %v", res.Warnings)
	}

	// 案由无要件 → not_found，提示先人工导入
	res2 := ol.AnalyzeElements("未录入案由", []string{"任意事实"})
	if res2.Status != sdk.StatusNotFound {
		t.Fatalf("无要件案由应 not_found，实际 %s", res2.Status)
	}
}

// TestFR04FindRelation 关联查询：pending 关系返回并告警；confirmed 关系不告警。
func TestFR04FindRelation(t *testing.T) {
	dir, db := setupWithLaw(t)

	if _, err := db.Exec(`INSERT INTO law(law_id, name) VALUES('law_jd_company', '最高人民法院关于适用公司法若干问题的规定')`); err != nil {
		t.Fatal(err)
	}
	// 机器推断的「修改」关系默认 pending（10 章：不自动判定废止）
	if _, err := db.Exec(`INSERT INTO relation(src_law_id, dst_law_id, relation_type, based_on_doc_id, confirm_status)
		VALUES('law_company', 'law_jd_company', '修改', 'law_company_2018', 'pending')`); err != nil {
		t.Fatal(err)
	}

	ol, err := sdk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ol.Close()

	res := ol.FindRelation("law_company")
	if res.Status != sdk.StatusOK {
		t.Fatalf("应为 ok，实际 %s (%s)", res.Status, res.ErrorCode)
	}
	rels, _ := res.Data["relations"].([]map[string]any)
	if len(rels) != 1 || rels[0]["relation_type"] != "修改" || rels[0]["confirm_status"] != "pending" {
		t.Fatalf("relations 异常: %+v", rels)
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "待人工确认") {
		t.Fatalf("pending 关系必须告警转人工: %v", res.Warnings)
	}

	// 人工确认后不再告警；从 dst 侧也应查到（双向）
	if _, err := db.Exec(`UPDATE relation SET confirm_status = 'confirmed'`); err != nil {
		t.Fatal(err)
	}
	res2 := ol.FindRelation("law_jd_company")
	if res2.Status != sdk.StatusOK {
		t.Fatalf("dst 侧应可查到: %s", res2.Status)
	}
	if len(res2.Warnings) != 0 {
		t.Fatalf("confirmed 关系不应告警: %v", res2.Warnings)
	}
}
