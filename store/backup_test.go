// backup_test.go 迁移前自动备份的门禁测试（release-process.md §4 承诺的机制）。
// 关键断言：备份是**迁移前**的快照（版本与结构都停留在旧版）；备份失败必须中止迁移
// 且原库分毫未动；不发生迁移就不产备份。
package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrationBackupsBeforeUpgrade v3 库升级前必须留下可读的迁移前快照。
func TestMigrationBackupsBeforeUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db", "openlaw.db")
	setupV3Raw(t, path)

	db, err := Open(path)
	if err != nil {
		t.Fatalf("v3→v4 迁移失败: %v", err)
	}
	db.Close()

	bp, err := LatestBackup(path)
	if err != nil {
		t.Fatal(err)
	}
	if bp == "" {
		t.Fatal("发生结构迁移却没有自动备份")
	}
	if dir := filepath.Dir(bp); dir != filepath.Join(filepath.Dir(filepath.Dir(path)), "backups") {
		t.Errorf("备份应落在 <dataDir>/backups，实际 %s", dir)
	}
	// 备份必须是迁移前的快照：版本仍为 3、law 仍无 region_code、旧数据在
	bk, err := sql.Open("sqlite", "file:"+bp+"?_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("备份无法打开: %v", err)
	}
	defer bk.Close()
	var ver string
	if err := bk.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&ver); err != nil {
		t.Fatalf("备份内读不到 schema_version: %v", err)
	}
	if ver != "3" {
		t.Errorf("备份应是迁移前快照（schema_version=3），实际 %q", ver)
	}
	if ok, _ := hasColumn(bk, "law", "region_code"); ok {
		t.Error("备份不该含迁移后才有的 region_code 列")
	}
	var name string
	if err := bk.QueryRow(`SELECT name FROM law WHERE law_id='law_beijing'`).Scan(&name); err != nil || name != "北京市河湖管理条例" {
		t.Errorf("备份内旧数据缺失: %v %q", err, name)
	}
}

// TestBackupFailureAbortsMigration 备份写不下去时必须中止迁移，原库保持原样（fail-closed）。
func TestBackupFailureAbortsMigration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "db", "openlaw.db")
	setupV3Raw(t, path)

	bdir := filepath.Join(root, "backups")
	if err := os.MkdirAll(bdir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bdir, 0o755) })
	if err := os.Chmod(bdir, 0o555); err != nil {
		t.Skipf("当前环境无法把备份目录设为只读: %v", err)
	}

	_, err := Open(path)
	if err == nil {
		t.Fatal("备份不可写时 Open 必须失败，不得静默迁移")
	}
	if !strings.Contains(err.Error(), "备份") || !strings.Contains(err.Error(), "中止") {
		t.Errorf("错误文案应说明备份失败且迁移中止，实际: %v", err)
	}
	// 原库必须分毫未动：版本仍 3、无 region_code、旧数据仍在
	if got := getVersion(t, path); got != "3" {
		t.Errorf("备份失败后仍改写了 schema_version: %q", got)
	}
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if ok, _ := hasColumn(raw, "law", "region_code"); ok {
		t.Error("备份失败后不得添加 region_code 列")
	}
	var n int
	if err := raw.QueryRow(`SELECT count(*) FROM law`).Scan(&n); err != nil || n != 2 {
		t.Errorf("备份失败不得影响既有数据，实际 %d 条 (%v)", n, err)
	}
}

// TestNoBackupWhenNoMigration 版本一致（含重复打开）不产备份，避免每次启动堆文件。
func TestNoBackupWhenNoMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db", "openlaw.db")
	// Open 不建目录（既有约定：调用方先 MkdirAll，见 cmd/openlaw-confirm）
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		db, err := Open(path)
		if err != nil {
			t.Fatalf("第 %d 次打开失败: %v", i+1, err)
		}
		db.Close()
	}
	bp, err := LatestBackup(path)
	if err != nil {
		t.Fatal(err)
	}
	if bp != "" {
		t.Errorf("未发生迁移不应产备份，实际 %s", bp)
	}
}

// TestBackupPathWithApostrophe 路径含单引号时 VACUUM INTO 的文本拼接必须安全转义
// （真实用户目录里出现 it's / 中文 / 空格都可能，不能靠运气）。
func TestBackupPathWithApostrophe(t *testing.T) {
	root := filepath.Join(t.TempDir(), "it's here")
	path := filepath.Join(root, "db", "openlaw.db")
	setupV3Raw(t, path)

	db, err := Open(path)
	if err != nil {
		t.Fatalf("含单引号的路径应能迁移: %v", err)
	}
	db.Close()
	bp, err := LatestBackup(path)
	if err != nil {
		t.Fatal(err)
	}
	if bp == "" {
		t.Fatal("含单引号路径下未产生备份")
	}
	if _, err := os.Stat(bp); err != nil {
		t.Errorf("备份文件不存在: %v", err)
	}
}

// TestSQLLiteralEscaping 转义规则的最小单元断言（' → ”）。
func TestSQLLiteralEscaping(t *testing.T) {
	if got := sqlLiteral(`a'b`); got != `'a''b'` {
		t.Errorf("sqlLiteral=%s", got)
	}
}
