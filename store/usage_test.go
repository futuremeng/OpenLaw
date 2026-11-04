// usage_test.go 本机使用事件台账：只记种类与结论、隐私边界与「未记录 ≠ 0」。
package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func openTmpDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestUsageLogRecordsKindOnly(t *testing.T) {
	db := openTmpDB(t)
	if err := UsageLog(db, EventSearch, "zero", ""); err != nil {
		t.Fatal(err)
	}
	if err := UsageLog(db, EventVerify, "verified", "结论 verified"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_event WHERE kind='search' AND outcome='zero'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("检索事件未落账: %d %v", n, err)
	}
	var detail string
	if err := db.QueryRow(`SELECT detail FROM usage_event WHERE kind='verify'`).Scan(&detail); err != nil || detail == "" {
		t.Errorf("结论细节应保留: %q %v", detail, err)
	}
}

// TestUsageLogPrivacyBoundary 空种类拒绝；超长 detail 截断（防止把检索词/案件内容塞进台账）。
func TestUsageLogPrivacyBoundary(t *testing.T) {
	db := openTmpDB(t)
	if err := UsageLog(db, "", "ok", ""); err == nil {
		t.Error("事件种类为空必须拒绝")
	}
	long := strings.Repeat("条文原文", 200)
	if err := UsageLog(db, EventSearch, "ok", long); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := db.QueryRow(`SELECT detail FROM usage_event WHERE kind='search'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) > 200 {
		t.Errorf("detail 必须截断到 200 字节内，实际 %d", len(got))
	}
}

// TestUsageAvailable v10 起有表；旧库（表被删除）必须报「未记录」而不是 0。
func TestUsageAvailable(t *testing.T) {
	db := openTmpDB(t)
	if !UsageAvailable(db) {
		t.Fatal("v10 库应有 usage_event 表")
	}
	if _, err := db.Exec(`DROP TABLE usage_event`); err != nil {
		t.Fatal(err)
	}
	if UsageAvailable(db) {
		t.Error("表缺失必须报未记录（不得当成 0 条记录）")
	}
	if UsageAvailable(nil) {
		t.Error("库未打开时必须报未记录")
	}
}
