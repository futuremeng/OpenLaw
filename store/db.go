// Package store 本地 SQLite 存储层（开发文档 3.3、13 章）。
// 使用 modernc.org/sqlite（纯 Go 无 CGO，内置 FTS5），整个数据目录可拷贝迁移。
// DuckDB 仅用于离线数据体检，不在此层，不作为主库。
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// SchemaVersion 当前数据模型版本，迁移框架据此判断（见 migrate.go）。
const SchemaVersion = "1"

// DB OpenLaw 数据库句柄，内嵌 *sql.DB 供上层在事务内复用。
type DB struct {
	*sql.DB
}

// Open 打开（或创建）数据目录下的 SQLite 库并完成幂等模式迁移。
// 建议路径：<dataDir>/db/openlaw.db。
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// SQLite 单写者；限制连接数避免导入事务与查询互锁导致 busy 超时误报。
	sqlDB.SetMaxOpenConns(1)
	if err := migrate(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return &DB{sqlDB}, nil
}

// migrate 执行内嵌 schema（全部 IF NOT EXISTS，幂等）。
func migrate(db *sql.DB) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	_, err := db.Exec(`INSERT INTO meta(key, value) VALUES('schema_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, SchemaVersion)
	return err
}

// CheckVersionOverlap 版本时效区间重叠检查（开发文档 13.3）。
// 时效为左闭右开区间 [effective, expire)；expire 为空串表示未失效。
// 若与既有版本区间重叠返回错误——导入器必须拦截，否则同一天会出现两个生效版本。
func (db *DB) CheckVersionOverlap(lawID, effective, expire string) error {
	const far = "9999-12-31"
	rows, err := db.Query(`
		SELECT version_id, version_no, effective_date, COALESCE(expire_date, '')
		FROM law_version
		WHERE law_id = ?
		  AND effective_date < COALESCE(NULLIF(?, ''), ?)
		  AND COALESCE(expire_date, ?) > ?`,
		lawID, expire, far, far, effective)
	if err != nil {
		return fmt.Errorf("区间重叠检查失败: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var vid, vno, eff, exp string
		if err := rows.Scan(&vid, &vno, &eff, &exp); err != nil {
			return err
		}
		return fmt.Errorf("版本区间重叠: 既有版本 %s(%s) [%s,%s) 与待导入区间 [%s,%s) 冲突",
			vid, vno, eff, orOpen(exp), effective, orOpen(expire))
	}
	return rows.Err()
}

func orOpen(expire string) string {
	if expire == "" {
		return "未失效"
	}
	return expire
}
