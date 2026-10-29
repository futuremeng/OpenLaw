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
