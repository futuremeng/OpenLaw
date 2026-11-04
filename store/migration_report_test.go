// migration_test.go 迁移留痕与版本比较：数据被升级过必须可查（不只是内部人知道），
// 空库首建不得写报告；双位数版本号必须按数值比较（"10" 不得小于 "3"）。
package store

import (
	"path/filepath"
	"testing"
)

func TestMigrationReportWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// 伪装成 v9 语料：去掉 v10 才有的台账表并把版本改回 9。
	if _, err := db.Exec(`DROP TABLE usage_event`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE meta SET value='9' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if got := LoadMigrationReport(db.DB); got.OK {
		t.Fatal("版本一致/首建不得写迁移报告")
	}
	db.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("v9→v10 迁移失败: %v", err)
	}
	defer db2.Close()
	rep := LoadMigrationReport(db2.DB)
	if !rep.OK || rep.From != "9" || rep.To != SchemaVersion {
		t.Fatalf("迁移留痕缺失或不符: %+v", rep)
	}
	if rep.At == "" || rep.Backup == "" {
		t.Errorf("迁移留痕必须含时间与备份位置: %+v", rep)
	}
	if !UsageAvailable(db2) {
		t.Error("迁移后台账表必须存在")
	}
}

func TestMigrationReportAbsentOnFreshDB(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got := LoadMigrationReport(db.DB); got.OK {
		t.Errorf("空库首建不得报「已迁移」: %+v", got)
	}
}

func TestVersionLessNumeric(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"9", "10", true}, {"10", "9", false}, {"3", "10", true}, {"10", "10", false},
	} {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%q,%q) = %v，期望 %v", c.a, c.b, got, c.want)
		}
	}
	// 非数值取值回退字符串序（保守行为，不得 panic）。
	if !versionLess("2", "3") || versionLess("3", "2") {
		t.Error("非数值分支异常")
	}
	if versionLess("v10", "9") {
		t.Error("异常取值应回退字符串序而不报错")
	}
}
