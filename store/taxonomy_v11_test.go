package store

import (
	"path/filepath"
	"testing"
)

// simulateV10 把当前库回退成 v10 形态：law 重建为不含 v11 六列、删除 v11 新表与视图、
// 版本标记改写 10。用于验证 v10→v11 真实迁移链路（不许只验证新版本自身）。
func simulateV10(t *testing.T, db *DB) {
	t.Helper()
	stmts := []string{
		`PRAGMA foreign_keys=OFF`,
		`DROP VIEW law_status`,
		`CREATE TABLE law_v10 (
  law_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  short_name TEXT,
  issuer TEXT,
  effect_level TEXT,
  is_local INTEGER NOT NULL DEFAULT 0,
  remark TEXT,
  eli TEXT,
  celex TEXT,
  jurisdiction TEXT,
  region_code TEXT,
  region_source TEXT,
  region_rule TEXT,
  region_reviewer TEXT,
  region_reviewed_at TEXT
)`,
		`INSERT INTO law_v10 (law_id,name,short_name,issuer,effect_level,is_local,remark,eli,celex,jurisdiction,region_code,region_source,region_rule,region_reviewer,region_reviewed_at)
		 SELECT law_id,name,short_name,issuer,effect_level,is_local,remark,eli,celex,jurisdiction,region_code,region_source,region_rule,region_reviewer,region_reviewed_at FROM law`,
		`DROP TABLE law`,
		`ALTER TABLE law_v10 RENAME TO law`,
		`DROP TABLE dict_subject`,
		`DROP TABLE dict_doc_form`,
		`DROP TABLE subject_log`,
		`UPDATE meta SET value='10' WHERE key='schema_version'`,
		`PRAGMA foreign_keys=ON`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("模拟 v10 失败（%.40s…）: %v", s, err)
		}
	}
}

// TestMigrateV10ToV11Additive v11 加法式迁移：v10 旧库打开后六列 ALTER 补齐、
// 词表/审计表/视图自动建立并播种、旧数据与版本标记到位。
func TestMigrateV10ToV11Additive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO law(law_id, name) VALUES('l1','测试法')`); err != nil {
		t.Fatal(err)
	}
	simulateV10(t, db)
	db.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("v10 语料应可原地升级: %v", err)
	}
	defer db2.Close()

	var got string
	if err := db2.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != SchemaVersion {
		t.Errorf("版本标记应升到 %s，实得 %s", SchemaVersion, got)
	}
	for _, col := range []string{"subject_code", "subject_source", "subject_rule",
		"subject_reviewer", "subject_reviewed_at", "doc_form"} {
		if ok, _ := hasColumn(db2.DB, "law", col); !ok {
			t.Errorf("law.%s 应经 ALTER 补齐", col)
		}
	}
	for _, obj := range []struct{ typ, name string }{
		{"table", "dict_subject"}, {"table", "dict_doc_form"},
		{"table", "subject_log"}, {"view", "law_status"},
	} {
		rows, err := db2.Query(`SELECT 1 FROM sqlite_master WHERE type=? AND name=? LIMIT 1`, obj.typ, obj.name)
		if err != nil {
			t.Fatal(err)
		}
		if !rows.Next() {
			t.Errorf("%s %s 应自动补齐", obj.typ, obj.name)
		}
		rows.Close()
	}
	var name string
	if err := db2.QueryRow(`SELECT name FROM law WHERE law_id='l1'`).Scan(&name); err != nil || name != "测试法" {
		t.Errorf("旧数据必须保留: %q %v", name, err)
	}
	// 词表种子：CN 七类齐备（拍板项 1 的官方口径），且带法域。
	var n int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM dict_subject WHERE jurisdiction='CN'`).Scan(&n); err != nil || n != 7 {
		t.Errorf("CN 部门法词表应播种 7 条，实得 %d (%v)", n, err)
	}
}

// TestLawStatusViewDerivation 时效五态派生（拍板项 3/4）：「已修改」按版本链判定，
// relation 不参与。用例覆盖设计 §4 的全部边界。
func TestLawStatusViewDerivation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at)
		VALUES('d1','d1.md','md/d1.md',1,'aa','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	laws := []string{"L1", "L2", "L3", "L4", "L5", "L6", "L7", "L8", "L9"}
	for _, id := range laws {
		if _, err := db.Exec(`INSERT INTO law(law_id,name) VALUES(?,?)`, id, id+"法"); err != nil {
			t.Fatal(err)
		}
	}
	type ver struct {
		vid, lid string
		eff, exp any
	}
	vers := []ver{
		{"v1", "L1", "2020-01-01", nil},                                             // 单版本现行
		{"v2", "L2", "2021-01-01", "2024-01-01"},                                    // 单版本已废止
		{"v3", "L3", "2027-01-01", nil},                                             // 尚未生效
		{"v4a", "L4", "1997-01-01", "2024-01-01"}, {"v4b", "L4", "2024-01-01", nil}, // 修改后现行 → 已修改
		{"v5a", "L5", "2020-01-01", nil}, {"v5b", "L5", "2027-01-01", nil}, // 首版现行+未来新版 → 现行有效
		// L6/L7 无版本 → 未知（law_version.effective_date 为 NOT NULL，
		// 日期缺失在库层不可能出现；视图的未知分支同时兜住无版本与异常空值）
		{"v8a", "L8", "2019-01-01", "2023-01-01"}, {"v8b", "L8", "2027-01-01", nil}, // 换代空窗 → 已废止
		{"v9a", "L9", "2019-01-01", "2023-01-01"}, {"v9b", "L9", "2023-01-01", "2024-06-01"}, // 全部过期 → 已废止
	}
	for _, v := range vers {
		if _, err := db.Exec(`INSERT INTO law_version(version_id,law_id,version_no,effective_date,expire_date,doc_id)
			VALUES(?,?,?, ?,?,'d1')`, v.vid, v.lid, v.vid, v.eff, v.exp); err != nil {
			t.Fatal(err)
		}
	}
	// pending 的「修改」relation 必须不影响派生（relation 不参与时效）。
	if _, err := db.Exec(`INSERT INTO relation(src_law_id,dst_law_id,relation_type,confirm_status)
		VALUES('L2','L1','修改','pending')`); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"L1": "现行有效", "L2": "已废止", "L3": "尚未生效", "L4": "已修改",
		"L5": "现行有效", "L6": "未知", "L7": "未知", "L8": "已废止", "L9": "已废止",
	}
	for id, expect := range want {
		var got string
		if err := db.QueryRow(`SELECT timeliness FROM law_status WHERE law_id=?`, id).Scan(&got); err != nil {
			t.Errorf("%s 派生失败: %v", id, err)
			continue
		}
		if got != expect {
			t.Errorf("%s 时效应为 %s，实得 %s", id, expect, got)
		}
	}
}

// TestDictSeedIdempotent 词表种子幂等：重复打开不重复插入，人工修订的词条不被覆盖。
func TestDictSeedIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE dict_subject SET name='民商法（人工修订）' WHERE code='CN-CIV'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var name string
	if err := db2.QueryRow(`SELECT name FROM dict_subject WHERE code='CN-CIV'`).Scan(&name); err != nil || name != "民商法（人工修订）" {
		t.Errorf("种子不得覆盖既有人工修订词条: %q %v", name, err)
	}
	var n int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM dict_subject`).Scan(&n); err != nil || n != 31 {
		t.Errorf("词表总数应为 31（CN7+EU12+US12），实得 %d (%v)", n, err)
	}
}

// TestOpenReadOnlyOnV10CorpusHonest 只读消费 v10 语料：库层照常可用；
// law_status 视图不存在由消费层按「未知」如实处理（本用例验证视图确实缺失且库可打开）。
func TestOpenReadOnlyOnV10CorpusHonest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	simulateV10(t, db)
	db.Close()

	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("v10 语料只读打开应可用（SchemaMin=%s）: %v", SchemaMin, err)
	}
	defer ro.Close()
	rows, err := ro.Query(`SELECT 1 FROM sqlite_master WHERE type='view' AND name='law_status' LIMIT 1`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Errorf("只读打开不得改结构（不应自动补建 law_status 视图）")
	}
	rows.Close()
}
