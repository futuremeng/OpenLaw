package element

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

// setupDB 最小可溯源语料：l1/v1/条文 a1（document d1）。
func setupDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		`INSERT INTO law(law_id, name) VALUES('l1', '合同法')`,
		`INSERT INTO document(doc_id, file_name, file_path, file_size, sha256, hash_computed_at)
		 VALUES('d1','d1.pdf','pdf/d1.pdf',1,'aa','2026-01-01T00:00:00Z')`,
		`INSERT INTO law_version(version_id, law_id, version_no, effective_date, doc_id)
		 VALUES('l1_v1','l1','v1','1999-10-01','d1')`,
		`INSERT INTO article(article_id, version_id, article_no, content, md_path, doc_id, parser_version)
		 VALUES('a1','l1_v1','第二百一十条','自然人之间的借款合同，自贷款人提供借款时生效。','md/d1.md','d1','test')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func in1() Input {
	return Input{ElementID: "e1", ArticleID: "a1", CaseType: "民间借贷",
		Name: "借贷合意", Description: "双方达成借款合意", HowToAttack: "核对借条与转账凭证"}
}

// TestImportDryRunThenApply 默认干跑不写库；apply 后新增一律 pending。
func TestImportDryRunThenApply(t *testing.T) {
	db := setupDB(t)
	rep, err := Import(db, []Input{in1()}, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Applied || rep.Added != 1 || len(rep.Rejected) != 0 {
		t.Fatalf("干跑计数异常: %+v", rep)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM element`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("干跑不得写库，element 有 %d 行", n)
	}
	rep, err = Import(db, []Input{in1()}, true)
	if err != nil || !rep.Applied || rep.Added != 1 {
		t.Fatalf("apply 导入异常: %+v %v", rep, err)
	}
	var cs string
	if err := db.QueryRow(`SELECT confirm_status FROM element WHERE element_id='e1'`).Scan(&cs); err != nil {
		t.Fatal(err)
	}
	if cs != "pending" {
		t.Fatalf("导入要件必须 pending（机器不裁决），got %s", cs)
	}
}

// TestImportRejectsUntraceableRow 关联不到条文/必填缺失的行如实拒绝并报告，其余照常。
func TestImportRejectsUntraceableRow(t *testing.T) {
	db := setupDB(t)
	bad := in1()
	bad.ElementID, bad.ArticleID = "e2", "a_missing"
	bad2 := in1()
	bad2.ElementID, bad2.Name = "e3", ""
	rep, err := Import(db, []Input{in1(), bad, bad2}, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Added != 1 || len(rep.Rejected) != 2 {
		t.Fatalf("应导入 1 拒绝 2: %+v", rep)
	}
	if rep.Rejected[0].Line != 2 || rep.Rejected[0].ElementID != "e2" {
		t.Errorf("拒绝行定位错误: %+v", rep.Rejected[0])
	}
}

// TestImportIdempotentKeepsHumanVerdict 同 element_id 重导入 = 更新正文，
// 人工已落判的 confirm_status 不被顶掉。
func TestImportIdempotentKeepsHumanVerdict(t *testing.T) {
	db := setupDB(t)
	if _, err := Import(db, []Input{in1()}, true); err != nil {
		t.Fatal(err)
	}
	if n, err := Confirm(db, []string{"e1"}, "confirm", "王复核", "对照原文确认"); err != nil || n != 1 {
		t.Fatalf("落判失败: %v", err)
	}
	updated := in1()
	updated.Name = "借款合意（修订）"
	rep, err := Import(db, []Input{updated}, true)
	if err != nil || rep.Added != 0 || rep.Updated != 1 {
		t.Fatalf("重导入应更新 1: %+v %v", rep, err)
	}
	var name, cs string
	if err := db.QueryRow(`SELECT name, confirm_status FROM element WHERE element_id='e1'`).Scan(&name, &cs); err != nil {
		t.Fatal(err)
	}
	if name != "借款合意（修订）" || cs != "confirmed" {
		t.Fatalf("人工结论被重导入顶掉: %s %s", name, cs)
	}
}

// TestConfirmAuditAndGuards 落判写 review_log（doc_id=所依条文文档，reason 带要件前缀）；
// 缺署名/依据、未知编号、非法 verdict 一律拒绝。
func TestConfirmAuditAndGuards(t *testing.T) {
	db := setupDB(t)
	if _, err := Import(db, []Input{in1()}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := Confirm(db, []string{"e1"}, "confirm", "", "依据"); err == nil {
		t.Error("缺署名必须拒绝")
	}
	if _, err := Confirm(db, []string{"e1"}, "confirm", "王复核", ""); err == nil {
		t.Error("缺结论依据必须拒绝")
	}
	if _, err := Confirm(db, []string{"e1"}, "reset", "王复核", "依据"); err == nil {
		t.Error("非法 verdict 必须拒绝")
	}
	if _, err := Confirm(db, []string{"nope"}, "confirm", "王复核", "依据"); err == nil {
		t.Error("库外编号必须报错而非静默跳过")
	}
	if _, err := Confirm(db, []string{"e1"}, "reject", "王复核", "与原文不符"); err != nil {
		t.Fatal(err)
	}
	var cs, docID, reason string
	if err := db.QueryRow(`SELECT confirm_status FROM element WHERE element_id='e1'`).Scan(&cs); err != nil {
		t.Fatal(err)
	}
	if cs != "rejected" {
		t.Fatalf("驳回后应 rejected, got %s", cs)
	}
	if err := db.QueryRow(`SELECT doc_id, reason FROM review_log ORDER BY review_id DESC LIMIT 1`).
		Scan(&docID, &reason); err != nil {
		t.Fatal(err)
	}
	if docID != "d1" || reason == "" {
		t.Fatalf("复核记录缺失或挂错文档: %s %q", docID, reason)
	}
}

// TestParseCSVGuards 陌生表头拒绝；缺必需列拒绝。
func TestParseCSVGuards(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.csv")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("element_id,article_id,case_type,name,notes\ne1,a1,民间借贷,借贷合意,x\n")
	if _, err := ParseCSVFile(p); err == nil {
		t.Error("陌生表头列必须拒绝")
	}
	write("element_id,case_type,name\ne1,民间借贷,借贷合意\n")
	if _, err := ParseCSVFile(p); err == nil {
		t.Error("缺 article_id 列必须拒绝")
	}
	write("element_id,article_id,case_type,name\ne1,a1,民间借贷,借贷合意\n")
	in, err := ParseCSVFile(p)
	if err != nil || len(in) != 1 || in[0].Name != "借贷合意" {
		t.Fatalf("正常 CSV 解析失败: %+v %v", in, err)
	}
}
