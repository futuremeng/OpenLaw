// backup.go 迁移前自动备份——补上 docs/release-process.md §4 早已承诺但此前没有实现的机制
// （「数据模型变更：先自动备份 db，再执行迁移脚本」）。
//
// 纪律（fail-closed）：备份失败 = 迁移中止。绝不「先迁移再补备份」，也不在备份未落地的
// 情况下改动任何结构。备份只在将要发生结构迁移时产出（版本一致或空库不产文件）。
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// backupsDir 备份目录：库文件上级目录名为 db 时取 <dataDir>/backups（仓库标准布局
// <dataDir>/db/openlaw.db）；否则退化为与库文件同级的 backups/。
func backupsDir(dbPath string) string {
	dir := filepath.Dir(dbPath)
	if strings.EqualFold(filepath.Base(dir), "db") {
		return filepath.Join(filepath.Dir(dir), "backups")
	}
	return filepath.Join(dir, "backups")
}

// sqlLiteral 把路径包成 SQL 字符串字面量。VACUUM INTO 的目标是语法位置的文本，
// 不走参数占位符，因此这里必须自行转义单引号。
func sqlLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// backupBeforeMigration 用 VACUUM INTO 落一份单文件一致快照，并验证备份可读，
// 返回备份路径。from 为迁移前的 schema_version（用于命名与人工核对）。
func backupBeforeMigration(db *sql.DB, dbPath, from string) (string, error) {
	dir := backupsDir(dbPath)
	// 备份目录一律转绝对路径：VACUUM INTO 的相对目标是按进程 CWD 解析的，
	// 用相对路径会把备份写到意料之外的地方。
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("备份路径解析失败: %w", err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", fmt.Errorf("无法创建备份目录 %s: %w", absDir, err)
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	ver := strings.TrimSpace(from)
	if ver == "" {
		ver = "0"
	}
	dest := filepath.Join(absDir, fmt.Sprintf("openlaw-v%s-%s.db", ver, stamp))
	for i := 1; ; i++ {
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			break
		}
		if i > 999 {
			return "", fmt.Errorf("备份文件名冲突过多: %s", dest)
		}
		dest = filepath.Join(dir, fmt.Sprintf("openlaw-v%s-%s-%d.db", ver, stamp, i))
	}
	if _, err := db.Exec(`VACUUM INTO ` + sqlLiteral(dest)); err != nil {
		return "", fmt.Errorf("VACUUM INTO 备份失败: %w", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		return "", fmt.Errorf("备份未落地: %w", err)
	}
	if info.Size() == 0 {
		return "", fmt.Errorf("备份文件为空（异常快照）: %s", dest)
	}
	// 验证备份可读：能打开且能查 sqlite_master。完整 integrity_check 留给 openlaw-check
	// （大语料上逐页校验太慢，不能放进启动路径）。
	bk, err := sql.Open("sqlite", "file:"+dest+"?_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return "", fmt.Errorf("备份无法打开: %w", err)
	}
	defer bk.Close()
	var n int
	if err := bk.QueryRow(`SELECT count(*) FROM sqlite_master`).Scan(&n); err != nil {
		return "", fmt.Errorf("备份不可读: %w", err)
	}
	if n == 0 {
		return "", fmt.Errorf("备份内无任何对象（异常快照）: %s", dest)
	}
	return dest, nil
}

// LatestBackup 返回该库最近一次自动备份的路径；没有备份时返回空串且不报错
// （供 CLI/桌面端如实报告「本次未迁移，故无新备份」）。
func LatestBackup(dbPath string) (string, error) {
	dir := backupsDir(dbPath)
	names, err := filepath.Glob(filepath.Join(dir, "openlaw-*.db"))
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", nil
	}
	// 文件名含 UTC 时间戳，字典序即时间序（同秒冲突追加 -N 后缀，可能轻微错位，
	// 故再按 mtime 兜底比较）。
	sort.Strings(names)
	best, bestMod := names[len(names)-1], time.Time{}
	for _, n := range names {
		info, err := os.Stat(n)
		if err != nil {
			continue
		}
		if info.ModTime().After(bestMod) {
			best, bestMod = n, info.ModTime()
		}
	}
	return best, nil
}
