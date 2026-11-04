package store

import (
	"path/filepath"
	"testing"
)

// TestMigrateV8ToV9Additive v9 只加表（law_alias）：旧库打开后新表自动补齐、
// 旧数据与版本标记都到位；不需要专用迁移步骤（schemaSQL 的 IF NOT EXISTS 负责）。
func TestMigrateV8ToV9Additive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO law(law_id, name, short_name) VALUES('l1','测试法','测法')`); err != nil {
		t.Fatal(err)
	}
	// 模拟 v8 语料：别名表不存在，版本标记回写 8
	if _, err := db.Exec(`DROP TABLE law_alias; UPDATE meta SET value='8' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("v8 语料应可原地升级: %v", err)
	}
	defer db2.Close()

	var got string
	if err := db2.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != SchemaVersion {
		t.Errorf("版本标记应升到 %s，实得 %s", SchemaVersion, got)
	}
	rows, err := db2.Query(`SELECT 1 FROM sqlite_master WHERE type='table' AND name='law_alias' LIMIT 1`)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Errorf("law_alias 表应自动补齐")
	}
	rows.Close()
	var name string
	if err := db2.QueryRow(`SELECT name FROM law WHERE law_id='l1'`).Scan(&name); err != nil || name != "测试法" {
		t.Errorf("旧数据必须保留: %q %v", name, err)
	}
}

// TestOpenReadOnlyOnV8CorpusReportsMissingAliasTable 只读消费旧语料：不得失败，
// 别名表缺失由解析层如实标注（本用例只验证库层仍可用，语义见 validate 层）。
func TestOpenReadOnlyOnV8CorpusReportsMissingAliasTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE law_alias; UPDATE meta SET value='8' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("v8 语料只读打开应可用（SchemaMin=%s）: %v", SchemaMin, err)
	}
	defer ro.Close()
	rows, err := ro.Query(`SELECT 1 FROM sqlite_master WHERE type='table' AND name='law_alias' LIMIT 1`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Errorf("只读打开不得改结构（不应自动补建 law_alias）")
	}
	rows.Close()
}
