package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// v1Schema 迁移前（schema_version=1）的表结构，用于测试 v1→v2 原地升级。
const v1Schema = `
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE law (
  law_id TEXT PRIMARY KEY, name TEXT NOT NULL, short_name TEXT, issuer TEXT,
  effect_level TEXT CHECK (effect_level IN ('法律','行政法规','监察法规','地方性法规','部门规章','地方政府规章','司法解释','其他')),
  is_local INTEGER NOT NULL DEFAULT 0, remark TEXT
);
CREATE TABLE document (
  doc_id TEXT PRIMARY KEY, file_name TEXT NOT NULL, file_path TEXT NOT NULL,
  file_size INTEGER NOT NULL, sha256 TEXT NOT NULL, hash_algo TEXT NOT NULL DEFAULT 'SHA-256',
  hash_computed_at TEXT NOT NULL, source_url TEXT, source_type TEXT,
  is_gongbao INTEGER NOT NULL DEFAULT 0, publish_date TEXT,
  ocr_used INTEGER NOT NULL DEFAULT 0, ocr_confidence REAL,
  confirm_status TEXT NOT NULL DEFAULT 'pending'
    CHECK (confirm_status IN ('confirmed','pending','rejected'))
);
CREATE TABLE law_version (
  version_id TEXT PRIMARY KEY, law_id TEXT NOT NULL REFERENCES law(law_id),
  version_no TEXT NOT NULL, effective_date TEXT NOT NULL, expire_date TEXT,
  doc_id TEXT NOT NULL REFERENCES document(doc_id), is_gongbao INTEGER NOT NULL DEFAULT 0,
  UNIQUE (law_id, version_no), CHECK (expire_date IS NULL OR expire_date > effective_date)
);
CREATE TABLE relation (
  src_law_id TEXT NOT NULL REFERENCES law(law_id),
  dst_law_id TEXT NOT NULL REFERENCES law(law_id),
  relation_type TEXT NOT NULL CHECK (relation_type IN ('修改','废止','解释','配套')),
  based_on_doc_id TEXT REFERENCES document(doc_id),
  confirm_status TEXT NOT NULL DEFAULT 'pending',
  PRIMARY KEY (src_law_id, dst_law_id, relation_type)
);
INSERT INTO meta(key,value) VALUES('schema_version','1');
INSERT INTO law(law_id,name,effect_level) VALUES ('law_x','测试法','法律');
INSERT INTO law(law_id,name,effect_level) VALUES ('law_y','被废法','行政法规');
INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at)
	VALUES ('doc_x','x.pdf','pdf/doc_x.pdf',1,'aa','2026-01-01T00:00:00Z');
INSERT INTO law_version(version_id,law_id,version_no,effective_date,doc_id)
	VALUES ('law_x_2018版','law_x','2018版','2018-01-01','doc_x');
INSERT INTO relation(src_law_id,dst_law_id,relation_type) VALUES ('law_x','law_y','废止');
`

// TestMigrateV1ToV2 v1 库自动升级：新列出现、旧数据保留、CHECK 扩展、非法值仍拒绝。
func TestMigrateV1ToV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(v1Schema); err != nil {
		t.Fatalf("构造 v1 库失败: %v", err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("打开 v1 库应自动迁移成功: %v", err)
	}
	defer db.Close()

	// 旧数据保留
	var name string
	if err := db.QueryRow(`SELECT name FROM law WHERE law_id='law_x'`).Scan(&name); err != nil || name != "测试法" {
		t.Fatalf("旧 law 数据丢失: %v %q", err, name)
	}
	var relType string
	if err := db.QueryRow(`SELECT relation_type FROM relation WHERE src_law_id='law_x'`).Scan(&relType); err != nil || relType != "废止" {
		t.Fatalf("旧 relation 数据丢失: %v %q", err, relType)
	}
	// 新列存在且可写欧盟类别（v2 CHECK）
	if _, err := db.Exec(`INSERT INTO law(law_id,name,effect_level,eli,celex)
		VALUES('eu_reg_2016_679','Regulation (EU) 2016/679','欧盟条例',
		'http://data.europa.eu/eli/reg/2016/679/oj','32016R0679')`); err != nil {
		t.Fatalf("v2 新列/新枚举应可写: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO relation(src_law_id,dst_law_id,relation_type)
		VALUES('eu_reg_2016_679','law_y','依据')`); err != nil {
		t.Fatalf("relation 新增枚举「依据」应可写: %v", err)
	}
	// v3：effect_level 新枚举「宪法」可写
	if _, err := db.Exec(`INSERT INTO law(law_id,name,effect_level) VALUES('law_const','中华人民共和国宪法','宪法')`); err != nil {
		t.Fatalf("v3 新枚举「宪法」应可写: %v", err)
	}
	// 非法枚举仍被拒绝
	if _, err := db.Exec(`INSERT INTO law(law_id,name,effect_level) VALUES('bad','坏','银河系法')`); err == nil {
		t.Fatal("非法 effect_level 应被 CHECK 拒绝")
	}
	// 版本记录升级
	var ver string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&ver); err != nil || ver != SchemaVersion {
		t.Fatalf("schema_version 应为 %s，实际 %q (%v)", SchemaVersion, ver, err)
	}
	// 幂等：再次打开不报错、不重复迁移
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("重复打开应幂等: %v", err)
	}
	db2.Close()
}

// v3Schema 迁移前（schema_version=3）的结构：law 已含「宪法」枚举但无地域标引列。
// 用于测试 v3→v4 加法式迁移与迁移前自动备份（store/backup.go）。
const v3Schema = `
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE law (
  law_id TEXT PRIMARY KEY, name TEXT NOT NULL, short_name TEXT, issuer TEXT,
  effect_level TEXT CHECK (effect_level IN ('宪法','法律','行政法规','监察法规','地方性法规','部门规章','地方政府规章','司法解释','其他','欧盟条例','欧盟指令','欧盟决定')),
  is_local INTEGER NOT NULL DEFAULT 0, remark TEXT, eli TEXT, celex TEXT
);
CREATE TABLE document (
  doc_id TEXT PRIMARY KEY, file_name TEXT NOT NULL, file_path TEXT NOT NULL,
  file_size INTEGER NOT NULL, sha256 TEXT NOT NULL, hash_algo TEXT NOT NULL DEFAULT 'SHA-256',
  hash_computed_at TEXT NOT NULL, source_url TEXT, source_type TEXT,
  is_gongbao INTEGER NOT NULL DEFAULT 0, publish_date TEXT,
  ocr_used INTEGER NOT NULL DEFAULT 0, ocr_confidence REAL,
  confirm_status TEXT NOT NULL DEFAULT 'pending'
    CHECK (confirm_status IN ('confirmed','pending','rejected'))
);
CREATE TABLE law_version (
  version_id TEXT PRIMARY KEY, law_id TEXT NOT NULL REFERENCES law(law_id),
  version_no TEXT NOT NULL, effective_date TEXT NOT NULL, expire_date TEXT,
  doc_id TEXT NOT NULL REFERENCES document(doc_id), is_gongbao INTEGER NOT NULL DEFAULT 0,
  UNIQUE (law_id, version_no), CHECK (expire_date IS NULL OR expire_date > effective_date)
);
CREATE TABLE review_log (
  review_id INTEGER PRIMARY KEY AUTOINCREMENT,
  doc_id TEXT NOT NULL REFERENCES document(doc_id),
  action TEXT NOT NULL CHECK (action IN ('confirm','reject','reset')),
  from_status TEXT NOT NULL, to_status TEXT NOT NULL,
  reviewer TEXT NOT NULL, reason TEXT, created_at TEXT NOT NULL
);
INSERT INTO meta(key,value) VALUES('schema_version','3');
INSERT INTO law(law_id,name,effect_level) VALUES ('law_x','测试法','法律');
INSERT INTO law(law_id,name,effect_level) VALUES ('law_beijing','北京市河湖管理条例','地方性法规');
INSERT INTO document(doc_id,file_name,file_path,file_size,sha256,hash_computed_at)
	VALUES ('doc_x','x.pdf','pdf/doc_x.pdf',1,'aa','2026-01-01T00:00:00Z');
INSERT INTO law_version(version_id,law_id,version_no,effective_date,doc_id)
	VALUES ('law_x_2018版','law_x','2018版','2018-01-01','doc_x');
`

// setupV3Raw 在 path 落一个真实的 v3 库（含样本数据）。
func setupV3Raw(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(v3Schema); err != nil {
		t.Fatalf("构造 v3 库失败: %v", err)
	}
}

// TestMigrateV3ToV4 v4 加法式迁移：旧数据保留、新列出现且可写、region_log 可用且
// 约束生效（law 级外键 + action 枚举）、重复打开幂等。
func TestMigrateV3ToV4(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db", "openlaw.db")
	setupV3Raw(t, path)

	db, err := Open(path)
	if err != nil {
		t.Fatalf("打开 v3 库应自动迁移成功: %v", err)
	}
	defer db.Close()

	// 旧数据保留，且新列默认空 = 地域未标注
	var name string
	var code sql.NullString
	if err := db.QueryRow(`SELECT name, region_code FROM law WHERE law_id='law_beijing'`).Scan(&name, &code); err != nil {
		t.Fatalf("旧 law 数据丢失或 region_code 列缺失: %v", err)
	}
	if name != "北京市河湖管理条例" || code.Valid {
		t.Errorf("迁移后地域应为空（未标注），实际 %q / %v", name, code)
	}
	// 机器建议写法：署名必须留空（人工落判才填）
	if _, err := db.Exec(`UPDATE law SET region_code='110100', region_source='auto', region_rule='name-prefix(R1)'
		WHERE law_id='law_beijing'`); err != nil {
		t.Fatalf("地域标引列应可写: %v", err)
	}
	var reviewer sql.NullString
	if err := db.QueryRow(`SELECT region_reviewer FROM law WHERE law_id='law_beijing'`).Scan(&reviewer); err != nil {
		t.Fatal(err)
	}
	if reviewer.Valid {
		t.Error("机器建议不得带复核署名（界面据此显示「机器建议 · 待复核」）")
	}
	// law 级审计可写
	if _, err := db.Exec(`INSERT INTO region_log(law_id,action,to_code,source,rule,actor,batch_id,created_at)
		VALUES('law_beijing','suggest','110100','auto','name-prefix(R1)','openlaw-region','b1','2026-10-29T00:00:00Z')`); err != nil {
		t.Fatalf("region_log 应可写: %v", err)
	}
	// 外键：不存在的 law 必须被拒（地域审计不能挂空）
	if _, err := db.Exec(`INSERT INTO region_log(law_id,action,source,actor,created_at)
		VALUES('law_missing','suggest','auto','openlaw-region','2026-10-29T00:00:00Z')`); err == nil {
		t.Error("region_log 必须拒绝不存在的 law_id")
	}
	// action 枚举：非法值必须被拒（confirm/reject/clear 属人工，机器侧禁写由应用层保证）
	if _, err := db.Exec(`INSERT INTO region_log(law_id,action,source,actor,created_at)
		VALUES('law_x','approve','auto','openlaw-region','2026-10-29T00:00:00Z')`); err == nil {
		t.Error("region_log.action 枚举应拒绝非法值")
	}
	// 版本记录 + 幂等
	if got := getVersion(t, path); got != SchemaVersion {
		t.Errorf("schema_version 应为 %s，实际 %q", SchemaVersion, got)
	}
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("重复打开应幂等: %v", err)
	}
	db2.Close()
}

// setupV4Raw 在 path 落一个真实的 v4 库（v3 结构 + 地域标引列/region_log，schema_version=4）。
// 用于测试 v4→v5 加法式迁移的真实链路（AGENTS.md：必须验证旧数据到新版本的真实链路）。
func setupV4Raw(t *testing.T, path string) {
	t.Helper()
	setupV3Raw(t, path)
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	stmts := []string{
		`ALTER TABLE law ADD COLUMN region_code TEXT`,
		`ALTER TABLE law ADD COLUMN region_source TEXT`,
		`ALTER TABLE law ADD COLUMN region_rule TEXT`,
		`ALTER TABLE law ADD COLUMN region_reviewer TEXT`,
		`ALTER TABLE law ADD COLUMN region_reviewed_at TEXT`,
		`CREATE TABLE region_log (
  log_id INTEGER PRIMARY KEY AUTOINCREMENT,
  law_id TEXT NOT NULL REFERENCES law(law_id),
  action TEXT NOT NULL CHECK (action IN ('suggest','confirm','reject','clear','undo')),
  from_code TEXT, to_code TEXT, source TEXT NOT NULL, rule TEXT, actor TEXT NOT NULL,
  reason TEXT, batch_id TEXT, created_at TEXT NOT NULL
)`,
		`UPDATE meta SET value='4' WHERE key='schema_version'`,
	}
	for _, s := range stmts {
		if _, err := raw.Exec(s); err != nil {
			t.Fatalf("构造 v4 库失败: %v（语句: %.40s…）", err, s)
		}
	}
}

// TestMigrateV4ToV5 v5 加法式迁移：旧数据保留、confirm_source 列出现且可写、
// agent_review 表可用且约束生效（doc_id 外键 + verdict 枚举）、重复打开幂等。
func TestMigrateV4ToV5(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db", "openlaw.db")
	setupV4Raw(t, path)

	db, err := Open(path)
	if err != nil {
		t.Fatalf("打开 v4 库应自动迁移成功: %v", err)
	}
	defer db.Close()

	// 旧数据保留，confirm_source 默认空 = 历史数据（界面不得渲染成智能体确认）
	var status string
	var src sql.NullString
	if err := db.QueryRow(`SELECT confirm_status, confirm_source FROM document WHERE doc_id='doc_x'`).Scan(&status, &src); err != nil {
		t.Fatalf("旧 document 数据丢失或 confirm_source 列缺失: %v", err)
	}
	if status != "pending" || src.Valid {
		t.Errorf("迁移后 confirm_source 应为空，实际 %q / %v", status, src)
	}
	// 新列可写：agent/human 两种来源
	if _, err := db.Exec(`UPDATE document SET confirm_status='confirmed', confirm_source='agent' WHERE doc_id='doc_x'`); err != nil {
		t.Fatalf("confirm_source 应可写: %v", err)
	}
	// agent_review 留痕可写
	if _, err := db.Exec(`INSERT INTO agent_review(doc_id,verdict,confidence,classify,audit,model,created_at)
		VALUES('doc_x','confirm',0.95,'{}','{}','test-model','2026-10-30T00:00:00Z')`); err != nil {
		t.Fatalf("agent_review 应可写: %v", err)
	}
	// 外键：不存在的 doc 必须被拒（智能体留痕不能挂空）
	if _, err := db.Exec(`INSERT INTO agent_review(doc_id,verdict,confidence,classify,audit,model,created_at)
		VALUES('doc_missing','confirm',0.9,'{}','{}','m','2026-10-30T00:00:00Z')`); err == nil {
		t.Error("agent_review 必须拒绝不存在的 doc_id")
	}
	// verdict 枚举：非法裁决必须被拒（fail-closed 语义在库层同样成立）
	if _, err := db.Exec(`INSERT INTO agent_review(doc_id,verdict,confidence,classify,audit,model,created_at)
		VALUES('doc_x','approve',0.9,'{}','{}','m','2026-10-30T00:00:00Z')`); err == nil {
		t.Error("agent_review.verdict 枚举应拒绝非法值")
	}
	if got := getVersion(t, path); got != SchemaVersion {
		t.Errorf("schema_version 应为 %s，实际 %q", SchemaVersion, got)
	}
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("重复打开应幂等: %v", err)
	}
	db2.Close()
}

// TestMigrateV5ToV6 v6 加法式升级：v5 库（无 pack_log）打开后自动补齐台账表、
// 版本升到当前值；旧数据保留，重复打开幂等。
func TestMigrateV5ToV6(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db", "openlaw.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO meta(key,value) VALUES('probe','keep')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE pack_log`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE meta SET value='5' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("打开 v5 库应自动补齐 v6 结构: %v", err)
	}
	defer db2.Close()
	if _, err := db2.Exec(`INSERT INTO pack_log(pack_id,kind,regions,since,until,key_id,total,imported,skipped,status,applied_at)
		VALUES('base-cn-2026','base','CN','','','k1',10,10,0,'applied','2026-10-31T00:00:00Z')`); err != nil {
		t.Fatalf("pack_log 应可用: %v", err)
	}
	if _, err := db2.Exec(`INSERT INTO pack_log(pack_id,kind,total,imported,skipped,status,applied_at)
		VALUES('bad','full',0,0,0,'done','2026-10-31T00:00:00Z')`); err == nil {
		t.Error("pack_log.status 枚举应拒绝非法值")
	}
	var probe string
	if err := db2.QueryRow(`SELECT value FROM meta WHERE key='probe'`).Scan(&probe); err != nil || probe != "keep" {
		t.Errorf("迁移不得丢旧数据: %q %v", probe, err)
	}
	if got := getVersion(t, path); got != SchemaVersion {
		t.Errorf("schema_version 应为 %s，实际 %q", SchemaVersion, got)
	}
}

// TestMigrateV6ToV7 多法域升级：v6 库（law 无 jurisdiction、CHECK 无美国类别）打开后
// 自动重建 law 表并回填法域（欧盟三类/带 eli·celex → EU，其余 → CN），旧列旧值全保留；
// 美国类别效力与新法域可写；非法值仍被拒；重复打开幂等。
func TestMigrateV6ToV7(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db", "openlaw.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// 造 v6 现状：law 无 jurisdiction、CHECK 无美国类别（重建回 v6 形态）。
	// law_status 是 v11 视图，真实 v6 库不存在——回退形态必须先删，否则 RENAME 校验失败。
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP VIEW IF EXISTS law_status`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE law_v6 (
  law_id TEXT PRIMARY KEY, name TEXT NOT NULL, short_name TEXT, issuer TEXT,
  effect_level TEXT CHECK (effect_level IN ('宪法','法律','行政法规','监察法规','地方性法规','部门规章','地方政府规章','司法解释','其他','欧盟条例','欧盟指令','欧盟决定')),
  is_local INTEGER NOT NULL DEFAULT 0, remark TEXT, eli TEXT, celex TEXT,
  region_code TEXT, region_source TEXT, region_rule TEXT, region_reviewer TEXT, region_reviewed_at TEXT
)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO law_v6 (law_id,name,effect_level,region_code,region_source)
		VALUES ('law_cn','测试法','法律','110000','auto')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO law_v6 (law_id,name,effect_level,celex)
		VALUES ('law_eu','GDPR','欧盟条例','32016R0679')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO law_v6 (law_id,name) VALUES ('law_nil','未标等级法')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE law`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE law_v6 RENAME TO law`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE meta SET value='6' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("打开 v6 库应自动迁移到 v7: %v", err)
	}
	defer db2.Close()

	// 回填口径：CN 条目、EU 条目（按效力+celex 判定）、等级未标条目按语料现状回填 CN。
	for _, want := range []struct{ id, jur string }{
		{"law_cn", "CN"}, {"law_eu", "EU"}, {"law_nil", "CN"},
	} {
		var got string
		if err := db2.QueryRow(`SELECT jurisdiction FROM law WHERE law_id=?`, want.id).Scan(&got); err != nil {
			t.Fatalf("法域回填缺失 %s: %v", want.id, err)
		}
		if got != want.jur {
			t.Errorf("%s 法域应回填 %s，实际 %q", want.id, want.jur, got)
		}
	}
	// 旧列旧值保留（地域标引不因重建丢失）。
	var rc, rs string
	if err := db2.QueryRow(`SELECT region_code, region_source FROM law WHERE law_id='law_cn'`).Scan(&rc, &rs); err != nil {
		t.Fatalf("地域标引列丢失: %v", err)
	}
	if rc != "110000" || rs != "auto" {
		t.Errorf("地域标引旧值丢失: %q %q", rc, rs)
	}
	// CHECK 扩展：美国类别可写；法域可写 US；非法效力仍拒。
	if _, err := db2.Exec(`INSERT INTO law(law_id,name,effect_level,jurisdiction)
		VALUES ('law_us','测试联邦法','联邦法律','US')`); err != nil {
		t.Fatalf("美国类别效力应可写: %v", err)
	}
	if _, err := db2.Exec(`INSERT INTO law(law_id,name,effect_level)
		VALUES ('law_bad','非法等级','联邦州法')`); err == nil {
		t.Error("effect_level CHECK 应拒绝名单外取值")
	}
	if got := getVersion(t, path); got != SchemaVersion {
		t.Errorf("schema_version 应为 %s，实际 %q", SchemaVersion, got)
	}
	db3, err := Open(path)
	if err != nil {
		t.Fatalf("重复打开应幂等: %v", err)
	}
	db3.Close()
}

// TestMigrateV7ToV8 v7 库（element 无 confirm_status）自动升级：加法式加列、
// 存量要件一律 pending（旧库从未记录确认状态，不得默认视为已确认）。
func TestMigrateV7ToV8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlaw.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`
		CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE element (
		  element_id TEXT PRIMARY KEY, article_id TEXT NOT NULL,
		  case_type TEXT NOT NULL, name TEXT NOT NULL,
		  description TEXT, how_to_attack TEXT);
		INSERT INTO meta(key,value) VALUES('schema_version','7');
		INSERT INTO element(element_id,article_id,case_type,name) VALUES('e1','a1','民间借贷','借贷合意');
	`); err != nil {
		t.Fatalf("构造 v7 库失败: %v", err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("v7→v8 迁移失败: %v", err)
	}
	defer db.Close()
	if ok, _ := hasColumn(db.DB, "element", "confirm_status"); !ok {
		t.Fatal("confirm_status 列未出现")
	}
	var cs string
	if err := db.QueryRow(`SELECT confirm_status FROM element WHERE element_id='e1'`).Scan(&cs); err != nil {
		t.Fatal(err)
	}
	if cs != "pending" {
		t.Errorf("存量要件必须如实 pending（未确认≠已确认），实际 %q", cs)
	}
	if got := getVersion(t, path); got != SchemaVersion {
		t.Errorf("schema_version 应为 %s，实际 %q", SchemaVersion, got)
	}
}
