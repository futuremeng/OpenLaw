package store

import (
	"database/sql"
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
