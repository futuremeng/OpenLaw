package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// setupV3 建一个正常 v3 库。
func setupV3(t *testing.T, path string) {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

// forceVersion 绕过 Open 的版本闸口直改 meta.schema_version（模拟未来版本语料）。
func forceVersion(t *testing.T, path, ver string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`UPDATE meta SET value=? WHERE key='schema_version'`, ver)
	if err != nil {
		t.Fatal(err)
	}
}

func getVersion(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var ver string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	return ver
}

// TestOpenRejectsFutureSchema OL-8 验收：schema_version='99' 的库，两种入口
// 均报错且**不改动该值**（绝不回写降级）。
func TestOpenRejectsFutureSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	setupV3(t, path)
	forceVersion(t, path, "99")

	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "高于") {
		t.Errorf("写入入口应拒绝未来 schema: %v", err)
	}
	if _, err := OpenReadOnly(path); err == nil || !strings.Contains(err.Error(), "高于") {
		t.Errorf("只读入口应拒绝未来 schema: %v", err)
	}
	if got := getVersion(t, path); got != "99" {
		t.Errorf("schema_version 被回写: %q（必须保持 99）", got)
	}
}

// TestOpenReadOnlyNoMigrate OL-7 验收：只读入口不跑迁移、不写 meta；
// 低于只读下限（v2）报错且不改标记；写入入口对范围内旧版本正常迁移。
func TestOpenReadOnlyNoMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	setupV3(t, path)
	forceVersion(t, path, "2")

	if _, err := OpenReadOnly(path); err == nil || !strings.Contains(err.Error(), "过旧") {
		t.Errorf("只读入口应拒绝过旧语料: %v", err)
	}
	if got := getVersion(t, path); got != "2" {
		t.Fatalf("只读入口改写了 schema_version: %q", got)
	}
	// 写入入口：范围内旧版本 → 迁移链升级并更新标记（生产者特权）
	db, err := Open(path)
	if err != nil {
		t.Fatalf("写入入口应升级 v2 语料: %v", err)
	}
	db.Close()
	if got := getVersion(t, path); got != SchemaVersion {
		t.Errorf("写入入口应把版本升到 %s: %q", SchemaVersion, got)
	}
}

// TestOpenReadOnlyQueryOnly OL-7 验收：query_only 挡住误写 SQL；正常读可用。
func TestOpenReadOnlyQueryOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	setupV3(t, path)

	db, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO meta(key,value) VALUES('hack','1')`); err == nil {
		t.Error("只读连接必须拒绝 INSERT")
	}
	var ver string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&ver); err != nil {
		t.Errorf("只读连接应可正常查询: %v", err)
	}
}

// TestOpenReadOnlyEmptyDB 空库/未初始化：只读入口 fail-closed（不代跑迁移）。
func TestOpenReadOnlyEmptyDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	if _, err := OpenReadOnly(path); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Errorf("空库只读应报错: %v", err)
	}
}
