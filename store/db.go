// Package store 本地 SQLite 存储层（开发文档 3.3、13 章）。
// 使用 modernc.org/sqlite（纯 Go 无 CGO，内置 FTS5），整个数据目录可拷贝迁移。
// DuckDB 仅用于离线数据体检，不在此层，不作为主库。
package store

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// SchemaVersion 当前数据模型版本，迁移框架据此判断（见 migrate）。
// v2（2026-10）：law +eli/celex、effect_level 扩展欧盟类别；law_version +application_date/lang；
// document +lang；relation_type +依据。v1 库自动原地升级，数据保留。
// v3（2026-10）：law.effect_level 枚举 +「宪法」（flk 收录宪法与宪法修正案，此前无处归类）；
// 新表 review_log（人工确认审计）与新索引经 schemaSQL IF NOT EXISTS 自动补齐，无需结构迁移。
const SchemaVersion = "3"

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

// migrate 幂等迁移：先升级旧结构（v1→v2→v3），再执行内嵌 schema（IF NOT EXISTS 补齐）。
func migrate(db *sql.DB) error {
	if err := upgradeToV2(db); err != nil {
		return fmt.Errorf("数据库迁移失败（v1→v2）: %w", err)
	}
	if err := upgradeToV3(db); err != nil {
		return fmt.Errorf("数据库迁移失败（v2→v3）: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	_, err := db.Exec(`INSERT INTO meta(key, value) VALUES('schema_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, SchemaVersion)
	return err
}

// hasColumn 判断表是否存在某列（表不存在返回 false）。
func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf(`SELECT 1 FROM pragma_table_info('%s') WHERE name = ?`, table), column)
	if err != nil {
		return false, nil // 表不存在等 → 视为无列
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

// upgradeToV2 将 v1 库原地升级到 v2（全新库跳过：无 law 表时由 schemaSQL 直接建 v2 结构）。
// SQLite 修改 CHECK 约束须重建表：foreign_keys 关闭 + 建新表 + 拷贝 + 改名（官方 12 步法）。
func upgradeToV2(db *sql.DB) error {
	if ok, _ := hasColumn(db, "law", "law_id"); !ok {
		return nil // 全新库（或空库），schemaSQL 直接建 v2
	}
	if ok, _ := hasColumn(db, "law", "eli"); ok {
		return nil // 已是 v2
	}
	// MaxOpenConns(1) 保证 PRAGMA 与应用共用同一连接
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmts := []string{
		`ALTER TABLE law ADD COLUMN eli TEXT`,
		`ALTER TABLE law ADD COLUMN celex TEXT`,
		`CREATE TABLE law_v2 (
  law_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  short_name TEXT,
  issuer TEXT,
  effect_level TEXT CHECK (effect_level IN ('法律','行政法规','监察法规','地方性法规','部门规章','地方政府规章','司法解释','其他','欧盟条例','欧盟指令','欧盟决定')),
  is_local INTEGER NOT NULL DEFAULT 0,
  remark TEXT,
  eli TEXT,
  celex TEXT
)`,
		`INSERT INTO law_v2 SELECT law_id,name,short_name,issuer,effect_level,is_local,remark,eli,celex FROM law`,
		`DROP TABLE law`,
		`ALTER TABLE law_v2 RENAME TO law`,
		`ALTER TABLE law_version ADD COLUMN application_date TEXT`,
		`ALTER TABLE law_version ADD COLUMN lang TEXT NOT NULL DEFAULT 'zho'`,
		`ALTER TABLE document ADD COLUMN lang TEXT NOT NULL DEFAULT 'zho'`,
		`CREATE TABLE relation_v2 (
  src_law_id TEXT NOT NULL REFERENCES law(law_id),
  dst_law_id TEXT NOT NULL REFERENCES law(law_id),
  relation_type TEXT NOT NULL CHECK (relation_type IN ('修改','废止','解释','配套','依据')),
  based_on_doc_id TEXT REFERENCES document(doc_id),
  confirm_status TEXT NOT NULL DEFAULT 'pending',
  PRIMARY KEY (src_law_id, dst_law_id, relation_type)
)`,
		`INSERT INTO relation_v2 SELECT src_law_id,dst_law_id,relation_type,based_on_doc_id,confirm_status FROM relation`,
		`DROP TABLE relation`,
		`ALTER TABLE relation_v2 RENAME TO relation`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("%v（语句: %.60s…）", err, s)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = db.Exec(`PRAGMA foreign_keys=ON`)
	return err
}

// upgradeToV3 law.effect_level CHECK 增加「宪法」（SQLite 改 CHECK 须重建表，同 v2 的 12 步法）。
// 判定依据：law 表定义文本是否已含 '宪法'（全新库由 schemaSQL 直接建 v3 结构）。
func upgradeToV3(db *sql.DB) error {
	var tblSQL string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='law'`).Scan(&tblSQL)
	if err == sql.ErrNoRows {
		return nil // 全新库，schemaSQL 直接建 v3
	} else if err != nil {
		return err
	}
	if strings.Contains(tblSQL, "'宪法'") {
		return nil // 已是 v3
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmts := []string{
		`CREATE TABLE law_v3 (
  law_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  short_name TEXT,
  issuer TEXT,
  effect_level TEXT CHECK (effect_level IN ('宪法','法律','行政法规','监察法规','地方性法规','部门规章','地方政府规章','司法解释','其他','欧盟条例','欧盟指令','欧盟决定')),
  is_local INTEGER NOT NULL DEFAULT 0,
  remark TEXT,
  eli TEXT,
  celex TEXT
)`,
		`INSERT INTO law_v3 SELECT law_id,name,short_name,issuer,effect_level,is_local,remark,eli,celex FROM law`,
		`DROP TABLE law`,
		`ALTER TABLE law_v3 RENAME TO law`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("%v（语句: %.60s…）", err, s)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = db.Exec(`PRAGMA foreign_keys=ON`)
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
