-- OpenLaw SQLite 模式（开发文档 13.1、13.2）
-- 幂等：全部 IF NOT EXISTS；迁移策略见 store/migrate.go
PRAGMA foreign_keys = ON;

-- 元信息（模式版本等）
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- law 法规主表
CREATE TABLE IF NOT EXISTS law (
  law_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  short_name TEXT,
  issuer TEXT,
  effect_level TEXT CHECK (effect_level IN ('法律','行政法规','监察法规','地方性法规','部门规章','地方政府规章','司法解释','其他')),
  is_local INTEGER NOT NULL DEFAULT 0,
  remark TEXT
);

-- document 原始文档与溯源（开发文档 4.3；MD5 仅判下载损坏，SHA-256 为防篡改依据）
CREATE TABLE IF NOT EXISTS document (
  doc_id TEXT PRIMARY KEY,
  file_name TEXT NOT NULL,
  file_path TEXT NOT NULL,          -- 相对数据目录，如 pdf/<doc_id>.pdf
  file_size INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  hash_algo TEXT NOT NULL DEFAULT 'SHA-256',
  hash_computed_at TEXT NOT NULL,
  source_url TEXT,
  source_type TEXT,
  is_gongbao INTEGER NOT NULL DEFAULT 0,
  publish_date TEXT,
  ocr_used INTEGER NOT NULL DEFAULT 0,
  ocr_confidence REAL,
  confirm_status TEXT NOT NULL DEFAULT 'pending'
    CHECK (confirm_status IN ('confirmed','pending','rejected'))
);

-- law_version 版本表（时效左闭右开 [effective_date, expire_date)，未失效留 NULL，不用 9999 占位）
CREATE TABLE IF NOT EXISTS law_version (
  version_id TEXT PRIMARY KEY,
  law_id TEXT NOT NULL REFERENCES law(law_id),
  version_no TEXT NOT NULL,
  effective_date TEXT NOT NULL,
  expire_date TEXT,
  doc_id TEXT NOT NULL REFERENCES document(doc_id),
  is_gongbao INTEGER NOT NULL DEFAULT 0,
  UNIQUE (law_id, version_no),
  CHECK (expire_date IS NULL OR expire_date > effective_date)
);

-- article 条文表（parser_version 强制版本化，开发文档 7）
CREATE TABLE IF NOT EXISTS article (
  article_id TEXT PRIMARY KEY,
  version_id TEXT NOT NULL REFERENCES law_version(version_id),
  article_no TEXT NOT NULL,
  content TEXT NOT NULL,
  md_path TEXT NOT NULL,
  doc_id TEXT NOT NULL REFERENCES document(doc_id),
  parser_version TEXT NOT NULL,
  UNIQUE (version_id, article_no)
);

-- element 构成要件表
CREATE TABLE IF NOT EXISTS element (
  element_id TEXT PRIMARY KEY,
  article_id TEXT NOT NULL REFERENCES article(article_id),
  case_type TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  how_to_attack TEXT
);

-- case_fact 案件事实表（状态：已支撑/缺证据/相互矛盾）
CREATE TABLE IF NOT EXISTS case_fact (
  fact_id TEXT PRIMARY KEY,
  case_id TEXT NOT NULL,
  description TEXT NOT NULL,
  element_id TEXT REFERENCES element(element_id),
  evidence TEXT,
  status TEXT NOT NULL DEFAULT 'unsupported'
    CHECK (status IN ('supported','unsupported','conflict'))
);

-- relation 关系表（不自动采信，默认 pending 待人工确认）
CREATE TABLE IF NOT EXISTS relation (
  src_law_id TEXT NOT NULL REFERENCES law(law_id),
  dst_law_id TEXT NOT NULL REFERENCES law(law_id),
  relation_type TEXT NOT NULL CHECK (relation_type IN ('修改','废止','解释','配套')),
  based_on_doc_id TEXT REFERENCES document(doc_id),
  confirm_status TEXT NOT NULL DEFAULT 'pending',
  PRIMARY KEY (src_law_id, dst_law_id, relation_type)
);

-- conflict 冲突表（多源不一致并列记录，不自动采信任一方）
CREATE TABLE IF NOT EXISTS conflict (
  conflict_id INTEGER PRIMARY KEY AUTOINCREMENT,
  law_id TEXT NOT NULL REFERENCES law(law_id),
  article_no TEXT,
  source_a TEXT,
  source_b TEXT,
  diff_summary TEXT,
  found_at TEXT NOT NULL,
  handle_status TEXT NOT NULL DEFAULT 'open'
    CHECK (handle_status IN ('open','resolved','ignored'))
);

-- import_log 导入日志
CREATE TABLE IF NOT EXISTS import_log (
  import_id INTEGER PRIMARY KEY AUTOINCREMENT,
  doc_id TEXT REFERENCES document(doc_id),
  source_url TEXT,
  file_path TEXT,
  file_size INTEGER,
  sha256 TEXT,
  http_status INTEGER,
  content_type TEXT,
  result TEXT NOT NULL CHECK (result IN ('success','failed','rolled_back')),
  reason TEXT,
  created_at TEXT NOT NULL
);

-- validate_log 校验日志
CREATE TABLE IF NOT EXISTS validate_log (
  validate_id INTEGER PRIMARY KEY AUTOINCREMENT,
  target_type TEXT NOT NULL,
  target_id TEXT,
  validate_type TEXT NOT NULL,
  result TEXT NOT NULL CHECK (result IN ('pass','fail')),
  reason TEXT,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_version_law_date ON law_version(law_id, effective_date, expire_date);
CREATE INDEX IF NOT EXISTS idx_article_version ON article(version_id);
CREATE INDEX IF NOT EXISTS idx_element_case ON element(case_type);
CREATE INDEX IF NOT EXISTS idx_relation_src ON relation(src_law_id);

-- 全文检索（开发文档 13.2）。中文分词不足由 search 层 LIKE 兜底，术语辅助列为后续版本。
CREATE VIRTUAL TABLE IF NOT EXISTS article_fts USING fts5(
  article_id UNINDEXED,
  article_no,
  content,
  tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER IF NOT EXISTS article_ai AFTER INSERT ON article BEGIN
  INSERT INTO article_fts(article_id, article_no, content)
  VALUES (new.article_id, new.article_no, new.content);
END;

CREATE TRIGGER IF NOT EXISTS article_ad AFTER DELETE ON article BEGIN
  DELETE FROM article_fts WHERE article_id = old.article_id;
END;

CREATE TRIGGER IF NOT EXISTS article_au AFTER UPDATE ON article BEGIN
  DELETE FROM article_fts WHERE article_id = old.article_id;
  INSERT INTO article_fts(article_id, article_no, content)
  VALUES (new.article_id, new.article_no, new.content);
END;
