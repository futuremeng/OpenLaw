-- OpenLaw SQLite 模式（开发文档 13.1、13.2）
-- 幂等：全部 IF NOT EXISTS；迁移策略见 store/migrate.go
PRAGMA foreign_keys = ON;

-- 元信息（模式版本等）
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- law 法规主表（v2：新增 eli/celex 欧盟标识列；effect_level 扩展欧盟类别；
-- v4：新增地域标引 5 列，见 docs/design-region-write.md §3）
CREATE TABLE IF NOT EXISTS law (
  law_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  short_name TEXT,
  issuer TEXT,
  effect_level TEXT CHECK (effect_level IN ('宪法','法律','行政法规','监察法规','地方性法规','部门规章','地方政府规章','司法解释','其他','欧盟条例','欧盟指令','欧盟决定')),
  is_local INTEGER NOT NULL DEFAULT 0,
  remark TEXT,
  eli TEXT,      -- 欧盟 European Legislation Identifier（docs/sources-eu.md）
  celex TEXT,    -- 欧盟 CELEX 编号
  -- v4 地域标引：机器建议与人工落判共用同一组列，靠 region_source/region_reviewer 区分。
  -- 空 = 地域未标注，界面必须如实显示，不得留白或渲染占位。
  -- 刻意不加 CHECK、不重建表（v2/v3 改 CHECK 需重建表，风险高）：取值合法性放应用层与门禁测试。
  region_code TEXT,         -- GB/T 2260 区划码（与 geo 包一致）；空 = 未标注
  region_source TEXT,       -- auto（真实坐标判定）|manual（人工）|trial（试判定）
  region_rule TEXT,         -- 依据与规则号，如 name-prefix(R1)
  region_reviewer TEXT,     -- 人工落判署名；空 = 仅机器建议，界面须显示「机器建议 · 待复核」
  region_reviewed_at TEXT   -- 人工落判时间
);

-- document 原始文档与溯源（开发文档 4.3；MD5 仅判下载损坏，SHA-256 为防篡改依据）
-- v5：+confirm_source（复核结论出自谁：agent=智能体自审核，human=人工落判；
-- 空=历史数据或未经复核）。诚实展示纪律：界面必须区分「智能体确认」与「人工确认」，
-- 不得把智能体确认渲染成人工复核。取值合法性放应用层（刻意不加 CHECK，与 v4 地域列同策略）。
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
    CHECK (confirm_status IN ('confirmed','pending','rejected')),
  lang TEXT NOT NULL DEFAULT 'zho',  -- 文本语言（ISO 639-3；欧盟 24 语同等作准，v2）
  confirm_source TEXT                 -- v5：agent|human；空=历史/未复核
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
  application_date TEXT,             -- 欧盟「适用日」（≠生效日，v2；中国法留空）
  lang TEXT NOT NULL DEFAULT 'zho',  -- 版本正文语言（v2）
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
  relation_type TEXT NOT NULL CHECK (relation_type IN ('修改','废止','解释','配套','依据')),
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
-- review_log 人工确认审计（confirm 流程）：谁在何时依据什么把哪个文档确认/驳回/重置。
-- 批量确认必须写明过滤条件与理由；绝不允许无审计痕迹的整库确认。
CREATE TABLE IF NOT EXISTS review_log (
  review_id INTEGER PRIMARY KEY AUTOINCREMENT,
  doc_id TEXT NOT NULL REFERENCES document(doc_id),
  action TEXT NOT NULL CHECK (action IN ('confirm','reject','reset')),
  from_status TEXT NOT NULL,
  to_status TEXT NOT NULL,
  reviewer TEXT NOT NULL,
  reason TEXT,
  created_at TEXT NOT NULL
);

-- agent_log 智能体自审核留痕（v5，docs/design-agent-review.md）。
-- 与 review_log 并列而非复用：review_log 记「状态流转」（谁把条目从什么改成什么），
-- agent_log 记「智能体每次审核的完整结论」（分类建议 + 审核裁决 + 置信度），
-- 无论是否落判都留痕——例外队列、纠正界面与事后抽查都以它为依据。
-- classify/audit 为两智能体的原始 JSON 结论（诚实留证，不做美化）。
CREATE TABLE IF NOT EXISTS agent_review (
  review_id   INTEGER PRIMARY KEY AUTOINCREMENT,
  doc_id      TEXT NOT NULL REFERENCES document(doc_id),
  verdict     TEXT NOT NULL CHECK (verdict IN ('confirm','escalate')),
  confidence  REAL NOT NULL,          -- 审核智能体置信度（0–1）
  classify    TEXT NOT NULL,          -- 分类智能体结论 JSON
  audit       TEXT NOT NULL,          -- 审核智能体结论 JSON
  model       TEXT NOT NULL,          -- 使用的模型标识（如实记录）
  created_at  TEXT NOT NULL
);

-- region_log 地域标引审计（law 级，docs/design-region-write.md §3）。
-- 与 review_log 并列而非复用：review_log 外键是 doc_id（文档级复核），
-- 地域是法规级判定——正文已复核 ≠ 地域已复核，两者不得混同。
-- 机器写入 actor 固定为 openlaw-region 且 region_reviewer 留空；人工落判必须署名 + 依据。
CREATE TABLE IF NOT EXISTS region_log (
  log_id INTEGER PRIMARY KEY AUTOINCREMENT,
  law_id TEXT NOT NULL REFERENCES law(law_id),
  action TEXT NOT NULL CHECK (action IN ('suggest','confirm','reject','clear','undo')),
  from_code TEXT,
  to_code   TEXT,
  source    TEXT NOT NULL,          -- auto|manual|trial
  rule      TEXT,                   -- 机器：规则号；人工：依据
  actor     TEXT NOT NULL,          -- 机器固定 openlaw-region；人工必须署名
  reason    TEXT,
  batch_id  TEXT,                   -- 同一次批量写入/撤销的关联号（-undo 依此回滚）
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
CREATE INDEX IF NOT EXISTS idx_article_doc ON article(doc_id);      -- 按文档反查条文（复核清单/断链检测热点）
CREATE INDEX IF NOT EXISTS idx_document_confirm ON document(confirm_status); -- pending 队列统计与抽样
CREATE INDEX IF NOT EXISTS idx_element_case ON element(case_type);
CREATE INDEX IF NOT EXISTS idx_relation_src ON relation(src_law_id);
CREATE INDEX IF NOT EXISTS idx_law_region_code ON law(region_code); -- 按区划码浏览/过滤（v4）
CREATE INDEX IF NOT EXISTS idx_region_log_law ON region_log(law_id);
CREATE INDEX IF NOT EXISTS idx_region_log_batch ON region_log(batch_id); -- 按批次撤销
CREATE INDEX IF NOT EXISTS idx_agent_review_doc ON agent_review(doc_id); -- 例外队列按条目取最近结论
CREATE INDEX IF NOT EXISTS idx_document_confirm_source ON document(confirm_source); -- 智能体/人工确认分开统计

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
