// Package store 本地 SQLite 存储层（开发文档 3.3、13 章）。
// 使用 modernc.org/sqlite（纯 Go 无 CGO，内置 FTS5），整个数据目录可拷贝迁移。
// DuckDB 仅用于离线数据体检，不在此层，不作为主库。
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SchemaVersion 当前数据模型版本，迁移框架据此判断（见 migrate）。
// v2（2026-10）：law +eli/celex、effect_level 扩展欧盟类别；law_version +application_date/lang；
// document +lang；relation_type +依据。v1 库自动原地升级，数据保留。
// v3（2026-10）：law.effect_level 枚举 +「宪法」（flk 收录宪法与宪法修正案，此前无处归类）；
// 新表 review_log（人工确认审计）与新索引经 schemaSQL IF NOT EXISTS 自动补齐，无需结构迁移。
// v4（2026-10）：law +地域标引 5 列（region_code/source/rule/reviewer/reviewed_at）与新表
// region_log（law 级地域审计）。加法式：不加 CHECK、不重建表（见 docs/design-region-write.md §3）。
// 自 v4 起，结构迁移前一律先自动备份（store/backup.go），备份失败即中止迁移。
// v5（2026-10）：document +confirm_source（agent|human，区分智能体确认与人工确认，诚实展示）
// 与新表 agent_review（智能体自审核留痕）+ 两索引。加法式：ALTER ADD COLUMN + IF NOT EXISTS，
// 见 docs/design-agent-review.md。
// v6（2026-10）：新表 pack_log（更新包安装台账：基础包/增量包按包幂等导入的依据）。
// 加法式：仅新表经 schemaSQL IF NOT EXISTS 自动补齐，无需结构迁移。
// v7（2026-10）：多法域（docs/design-multi-jurisdiction.md）——law +jurisdiction
// （CN|EU|US，空=未标注）；effect_level CHECK 扩展美国类别（美国宪法/联邦法律/
// 联邦规章/行政命令/联邦条约）。改 CHECK 须重建表（同 v2/v3 十二步法，迁移前已自动备份）。
// v8（2026-11）：element +confirm_status（OL-2，docs/tasks-opencourt-bridge.md）——
// 要件导入一律 pending，人工确认后才 confirmed；加法式：不加 CHECK、纯 ALTER。
// 只读消费方读 v7 语料时该列缺失，element 加载层按「未确认」如实处理（不被迫升级）。
// v9（2026-11）：新表 law_alias（OL-4 法名规范化：全称 ↔ 简称/别名，来源必须语料内可溯源）。
// 加法式：仅新表经 schemaSQL IF NOT EXISTS 自动补齐，无需结构迁移。只读消费方读 v8 语料时
// 该表不存在，法名解析按「无别名」如实处理并给出构建指引（不被迫升级）。
// v10（2026-11）：新表 usage_event（本机使用事件台账，产品评审「度量体系」）。
// 加法式：仅新表经 schemaSQL IF NOT EXISTS 自动补齐，无需结构迁移。台账只记计数与结论，
// 不记检索词与案件内容，且只在写入入口记录（只读消费方不写本机台账）。
// 自 v10 起，每次真实结构迁移都在 meta.migration_report 留下「从哪个版本来、备份在哪」，
// 界面据此如实说明本机数据被升级过（不再只有内部人才看得到）。
const SchemaVersion = "10"

// OL-8 版本协商（docs/tasks-opencourt-bridge.md）：
//   - SchemaMax：本 SDK 可支持的最高 schema（高于它一律 fail-closed：不迁移、
//     不部分读、绝不把 meta.schema_version 回写降级——那是数据完整性事故）；
//   - SchemaMin：只读入口可接受的最低 schema（消费方遇到更低版本 → 指引其用
//     OpenLaw 升级语料，消费方不得代跑迁移）；
//   - 写入入口（Open）可接受任何 ≤ SchemaMax 的旧版本并执行迁移链（v1→v2→v3）。
const (
	SchemaMax = SchemaVersion
	// SchemaMin 保持 "3"（2026-10 拍板，docs/design-region-write.md §9.1）：
	// 只读消费方读 v3 语料时没有地域标引列，界面显示「地域未标注」即可，
	// 不被迫升级；代价仅是看不到地域列。需要写地域则必须升到 v4。
	SchemaMin = "3"
)

// VersionGuidance schema 不匹配时统一给出的两条出路（升级消费方应用 / 换兼容
// 数据包；生产者侧则升级 OpenLaw），错误文案必须可行动。
// 文案遵守 docs/design-ui-language.md：不得出现「语料」这类内部说法（列名
// schema_version 属技术标识，保留）。
const VersionGuidance = "两条出路：① 升级本应用至支持该数据格式的版本；② 使用 OpenLaw 生成的兼容数据包（openlaw-pack build）。数据生产者请用新版 OpenLaw CLI 升级法规库数据后重新分发。"

// DB OpenLaw 数据库句柄，内嵌 *sql.DB 供上层在事务内复用。
type DB struct {
	*sql.DB
}

// Open 打开（或创建）数据目录下的 SQLite 库并完成幂等模式迁移。
// 建议路径：<dataDir>/db/openlaw.db。
// OL-8：迁移前先读 meta.schema_version——高于 SchemaMax 直接报错（不迁移、
// 不回写标记）；范围内旧版本走迁移链升级。
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// SQLite 单写者；限制连接数避免导入事务与查询互锁导致 busy 超时误报。
	sqlDB.SetMaxOpenConns(1)
	ver, err := readSchemaVersion(sqlDB)
	if err != nil {
		sqlDB.Close()
		return nil, err
	}
	if versionLess(SchemaMax, ver) {
		sqlDB.Close()
		return nil, fmt.Errorf("法规库数据格式版本 schema_version=%s 高于本版本支持上限 %s——拒绝打开（不迁移、不回写标记）。%s", ver, SchemaMax, VersionGuidance)
	}
	// 结构迁移前自动备份（release-process.md §4 承诺的机制；store/backup.go 实现）。
	// ver=="" 是空库/未初始化，无数据可失，不备份；版本一致不备份（避免每次启动产文件）。
	// 备份失败一律 fail-closed：不迁移、不改结构，原库保持原样。
	backupPath := ""
	if ver != "" && ver != SchemaVersion {
		backupPath, err = backupBeforeMigration(sqlDB, path, ver)
		if err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("迁移前自动备份失败，迁移已中止（库未改动）: %w", err)
		}
	}
	if err := migrate(sqlDB, ver, backupPath); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return &DB{sqlDB}, nil
}

// OpenReadOnly 只读打开（OL-7）：供消费方（如 OpenCourt）使用——
//   - DSN 追加 _pragma=query_only(1)：按连接生效，与 WAL 兼容（不用 mode=ro，
//     WAL 库只读打开仍需 -shm/-wal 可写，易失败）；
//   - 不执行 migrate、不写 meta：消费方绝不修改生产者数据；
//   - schema_version 不在 [SchemaMin, SchemaMax] 内直接报错（fail-closed）。
func OpenReadOnly(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=query_only(1)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("只读打开数据库失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	ver, err := readSchemaVersion(sqlDB)
	if err != nil {
		sqlDB.Close()
		return nil, err
	}
	switch {
	case ver == "":
		sqlDB.Close()
		return nil, fmt.Errorf("法规库数据缺少 schema_version 标记（空库或未初始化）——只读入口不代跑迁移。%s", VersionGuidance)
	case versionLess(SchemaMax, ver):
		sqlDB.Close()
		return nil, fmt.Errorf("法规库数据格式版本 schema_version=%s 高于本版本支持上限 %s——拒绝打开（不部分读、不猜）。%s", ver, SchemaMax, VersionGuidance)
	case versionLess(ver, SchemaMin):
		sqlDB.Close()
		return nil, fmt.Errorf("法规库数据格式版本 schema_version=%s 低于本版本只读下限 %s——数据过旧，请用新版 OpenLaw 升级法规库数据后重试（消费方不代跑迁移）", ver, SchemaMin)
	}
	return &DB{sqlDB}, nil
}

// versionLess 按数值比较 schema 版本（双位数起字符串序失真："10" < "3"）。
// 任一侧不是纯十进制整数时回退字符串序——保持对异常值的保守行为。
func versionLess(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}

// readSchemaVersion 读取 meta.schema_version（只读查询，绝不写）。
// meta 表不存在或无该键 → 返回 ""（空库/未初始化/极旧库），由调用方按入口语义处置。
func readSchemaVersion(db *sql.DB) (string, error) {
	var ver string
	err := db.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&ver)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		// meta 表尚不存在（全新库/空库文件）不是错误：按未标记处理。
		if strings.Contains(err.Error(), "no such table") {
			return "", nil
		}
		return "", fmt.Errorf("读取 schema_version 失败: %w", err)
	}
	return strings.TrimSpace(ver), nil
}

// MigrationReport 一次真实结构迁移的留痕（meta.migration_report，界面如实展示用）。
// 没有发生过迁移时 ok=false——「没升级过」与「读不到」必须区分（诚实展示）。
type MigrationReport struct {
	OK     bool   `json:"ok"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	At     string `json:"at,omitempty"`
	Backup string `json:"backup,omitempty"`
}

// LoadMigrationReport 读取迁移留痕；meta 表缺失或无该键 → ok=false。
func LoadMigrationReport(db *sql.DB) MigrationReport {
	var raw string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='migration_report'`).Scan(&raw); err != nil || raw == "" {
		return MigrationReport{}
	}
	var r MigrationReport
	if err := json.Unmarshal([]byte(raw), &r); err != nil || !r.OK {
		return MigrationReport{}
	}
	return r
}

// migrate 幂等迁移：先升级旧结构（v1→v2→…→v8），再执行内嵌 schema（IF NOT EXISTS 补齐）。
// from/backup 来自调用方（Open 在迁移前读到的版本与自动备份位置）：真实发生过迁移时
// 把「从哪个版本来、备份在哪」写进 meta，界面据此如实说明本机数据被升级过。
func migrate(db *sql.DB, from, backup string) error {
	if err := upgradeToV2(db); err != nil {
		return fmt.Errorf("数据库迁移失败（v1→v2）: %w", err)
	}
	if err := upgradeToV3(db); err != nil {
		return fmt.Errorf("数据库迁移失败（v2→v3）: %w", err)
	}
	if err := upgradeToV4(db); err != nil {
		return fmt.Errorf("数据库迁移失败（v3→v4）: %w", err)
	}
	if err := upgradeToV5(db); err != nil {
		return fmt.Errorf("数据库迁移失败（v4→v5）: %w", err)
	}
	if err := upgradeToV7(db); err != nil {
		return fmt.Errorf("数据库迁移失败（v6→v7）: %w", err)
	}
	if err := upgradeToV8(db); err != nil {
		return fmt.Errorf("数据库迁移失败（v7→v8）: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES('schema_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, SchemaVersion); err != nil {
		return err
	}
	// 真实发生过迁移（旧版本 → 当前版本）才留迁移报告；空库首建与版本一致都不写。
	if from != "" && from != SchemaVersion {
		raw, err := json.Marshal(MigrationReport{OK: true, From: from, To: SchemaVersion,
			At: time.Now().UTC().Format(time.RFC3339), Backup: backup})
		if err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES('migration_report', ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, string(raw)); err != nil {
			return err
		}
	}
	return nil
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

// upgradeToV4 地域标引列（docs/design-region-write.md §3）：纯加法式 ALTER ADD COLUMN。
// 刻意不加 CHECK、不重建表——v2/v3 改 CHECK 要走 12 步重建，风险高；地域取值合法性
// 放应用层与门禁测试。判定依据：law 是否已有 region_code 列（全新库由 schemaSQL 直接建 v4）。
// region_log 表与索引由随后的 schemaSQL（IF NOT EXISTS）补齐。
func upgradeToV4(db *sql.DB) error {
	if ok, _ := hasColumn(db, "law", "law_id"); !ok {
		return nil // 全新库（或空库），schemaSQL 直接建 v4 结构
	}
	if ok, _ := hasColumn(db, "law", "region_code"); ok {
		return nil // 已是 v4
	}
	for _, col := range []string{
		"region_code TEXT",
		"region_source TEXT",
		"region_rule TEXT",
		"region_reviewer TEXT",
		"region_reviewed_at TEXT",
	} {
		if _, err := db.Exec(`ALTER TABLE law ADD COLUMN ` + col); err != nil {
			return fmt.Errorf("加列 %s 失败: %w", col, err)
		}
	}
	return nil
}

// upgradeToV5 智能体自审核（docs/design-agent-review.md）：document +confirm_source。
// 纯加法式 ALTER ADD COLUMN；agent_review 表与索引由随后的 schemaSQL（IF NOT EXISTS）补齐。
// 判定依据：document 是否已有 confirm_source 列（全新库由 schemaSQL 直接建 v5）。
func upgradeToV5(db *sql.DB) error {
	if ok, _ := hasColumn(db, "document", "doc_id"); !ok {
		return nil // 全新库（或空库），schemaSQL 直接建 v5 结构
	}
	if ok, _ := hasColumn(db, "document", "confirm_source"); ok {
		return nil // 已是 v5
	}
	if _, err := db.Exec(`ALTER TABLE document ADD COLUMN confirm_source TEXT`); err != nil {
		return fmt.Errorf("加列 confirm_source 失败: %w", err)
	}
	return nil
}

// upgradeToV7 多法域（docs/design-multi-jurisdiction.md）：law 重建以扩展
// effect_level CHECK（美国类别）并新增 jurisdiction 列（SQLite 改 CHECK 须重建表，
// 同 v2/v3 十二步法；结构迁移前已自动备份，见 backup.go）。
// 判定依据：law 表是否已有 jurisdiction 列（全新库由 schemaSQL 直接建 v7 结构）。
// 回填为机器推断（依据充分、如实标注于注释）：欧盟三类效力或带 eli/celex → EU；
// 其余 → CN（v6 及以前的语料全部来自中国官方渠道与欧盟 Cellar 通道，无美国条目）。
func upgradeToV7(db *sql.DB) error {
	if ok, _ := hasColumn(db, "law", "law_id"); !ok {
		return nil // 全新库（或空库），schemaSQL 直接建 v7 结构
	}
	if ok, _ := hasColumn(db, "law", "jurisdiction"); ok {
		return nil // 已是 v7
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
		`CREATE TABLE law_v7 (
  law_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  short_name TEXT,
  issuer TEXT,
  effect_level TEXT CHECK (effect_level IN ('宪法','法律','行政法规','监察法规','地方性法规','部门规章','地方政府规章','司法解释','其他','欧盟条例','欧盟指令','欧盟决定','美国宪法','联邦法律','联邦规章','行政命令','联邦条约')),
  is_local INTEGER NOT NULL DEFAULT 0,
  remark TEXT,
  eli TEXT,
  celex TEXT,
  region_code TEXT,
  region_source TEXT,
  region_rule TEXT,
  region_reviewer TEXT,
  region_reviewed_at TEXT,
  jurisdiction TEXT
)`,
		`INSERT INTO law_v7 (law_id,name,short_name,issuer,effect_level,is_local,remark,eli,celex,region_code,region_source,region_rule,region_reviewer,region_reviewed_at)
   SELECT law_id,name,short_name,issuer,effect_level,is_local,remark,eli,celex,region_code,region_source,region_rule,region_reviewer,region_reviewed_at FROM law`,
		`UPDATE law_v7 SET jurisdiction='EU' WHERE effect_level IN ('欧盟条例','欧盟指令','欧盟决定') OR COALESCE(eli,'')<>'' OR COALESCE(celex,'')<>''`,
		`UPDATE law_v7 SET jurisdiction='CN' WHERE jurisdiction IS NULL`,
		`DROP TABLE law`,
		`ALTER TABLE law_v7 RENAME TO law`,
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

// upgradeToV8 要件确认列（OL-2）：element 加法式 +confirm_status，存量一律 pending
// （旧库要件从未有过确认状态记录，按「未确认」如实处理，不得默认视为已确认）。
// 判定依据：element 表是否已有 confirm_status 列（全新库由 schemaSQL 直接建 v8 结构）。
func upgradeToV8(db *sql.DB) error {
	if ok, _ := hasColumn(db, "element", "element_id"); !ok {
		return nil // 全新库（或空库），schemaSQL 直接建 v8 结构
	}
	if ok, _ := hasColumn(db, "element", "confirm_status"); ok {
		return nil // 已是 v8
	}
	if _, err := db.Exec(`ALTER TABLE element ADD COLUMN confirm_status TEXT NOT NULL DEFAULT 'pending'`); err != nil {
		return fmt.Errorf("加列 confirm_status 失败: %w", err)
	}
	return nil
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
