-- OpenLaw SQLite 模式（开发文档 13.1、13.2）
-- 幂等：全部 IF NOT EXISTS；迁移策略见 store/migrate.go
PRAGMA foreign_keys = ON;

-- 元信息（模式版本等）
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- law 法规主表（v2：新增 eli/celex 欧盟标识列；effect_level 扩展欧盟类别；
-- v4：新增地域标引 5 列，见 docs/design-region-write.md §3；
-- v7：新增法域列 + effect_level 扩展美国类别，见 docs/design-multi-jurisdiction.md）
CREATE TABLE IF NOT EXISTS law (
  law_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  short_name TEXT,
  issuer TEXT,
  effect_level TEXT CHECK (effect_level IN ('宪法','法律','行政法规','监察法规','地方性法规','部门规章','地方政府规章','司法解释','其他','欧盟条例','欧盟指令','欧盟决定','美国宪法','联邦法律','联邦规章','行政命令','联邦条约')),
  is_local INTEGER NOT NULL DEFAULT 0,
  remark TEXT,
  eli TEXT,      -- 欧盟 European Legislation Identifier（docs/sources-eu.md）
  celex TEXT,    -- 欧盟 CELEX 编号
  -- v7 法域：CN=中国 EU=欧盟 US=美国；空 = 法域未标注（界面必须如实显示）。
  -- 刻意不加 CHECK（同 v4 地域列策略）：取值合法性放应用层与门禁测试，
  -- 法域加列走纯 ALTER，不为此再重建表。
  jurisdiction TEXT,
  -- v4 地域标引：机器建议与人工落判共用同一组列，靠 region_source/region_reviewer 区分。
  -- 空 = 地域未标注，界面必须如实显示，不得留白或渲染占位。
  -- 刻意不加 CHECK、不重建表（v2/v3 改 CHECK 需重建表，风险高）：取值合法性放应用层与门禁测试。
  region_code TEXT,         -- GB/T 2260 区划码（与 geo 包一致）；空 = 未标注
  region_source TEXT,       -- auto（真实坐标判定）|manual（人工）|trial（试判定）
  region_rule TEXT,         -- 依据与规则号，如 name-prefix(R1)
  region_reviewer TEXT,     -- 人工落判署名；空 = 仅机器建议，界面须显示「机器建议 · 待复核」
  region_reviewed_at TEXT,  -- 人工落判时间
  -- v11 部门法标引与文档形态（docs/design-taxonomy.md §4）：subject_* 五列与 region_*
  -- 完全同构（auto|manual|trial|pack；reviewer 空 = 仅机器建议）；doc_form 为机器判定
  -- 单列（全自动，无人工落判）。空 = 未标注，界面必须如实显示，不得留白或渲染占位。
  -- 刻意不加 CHECK（同 v4/v7 策略）：取值合法性放应用层与门禁测试。
  subject_code TEXT,
  subject_source TEXT,
  subject_rule TEXT,
  subject_reviewer TEXT,
  subject_reviewed_at TEXT,
  doc_form TEXT
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
-- v8（OL-2，docs/tasks-opencourt-bridge.md）：+confirm_status——导入的要件一律 pending，
-- 人工确认后才 confirmed（机器不裁决）。刻意不加 CHECK（同 v4/v7 策略：取值合法性放
-- 应用层与门禁测试，加列走纯 ALTER 不重建表）。取值 pending|confirmed|rejected。
CREATE TABLE IF NOT EXISTS element (
  element_id TEXT PRIMARY KEY,
  article_id TEXT NOT NULL REFERENCES article(article_id),
  case_type TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  how_to_attack TEXT,
  confirm_status TEXT NOT NULL DEFAULT 'pending'
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

-- pack_log 更新包安装台账（基础包/增量包分发模型）。
-- 与 import_log（逐条入库留痕）、region_log（地域批次审计）并列而非复用：
-- pack_log 记「包级事实」——哪个包、什么种类（base=基础包 | delta=增量包 | full=旧整包）、
-- 覆盖范围、装了多少。它是幂等依据：status=applied 的 pack_id 再导入直接跳过；
-- 中断的包记 partial，重放时已装条目按哈希比对跳过（内容不一致 = 冲突，中止转人工）。
CREATE TABLE IF NOT EXISTS pack_log (
  pack_id    TEXT PRIMARY KEY,          -- 包标识（manifest.pack_id；旧整包回退 "pack:"+generated_at）
  kind       TEXT NOT NULL CHECK (kind IN ('base','delta','full')),
  regions    TEXT,                      -- 覆盖地域（区划码逗号连接；空=全部地域）
  since      TEXT,                      -- 增量窗口起（publish_date，含）；空=非增量窗口
  until      TEXT,                      -- 增量窗口止（含）
  key_id     TEXT,                      -- 命中的受信发布者 key ID；空=未查信任（本地自验）
  total      INTEGER NOT NULL,          -- changes.csv 条目数
  imported   INTEGER NOT NULL,          -- 最近一次实际导入条数
  skipped    INTEGER NOT NULL,          -- 库内已存在且哈希一致而跳过的条数
  status     TEXT NOT NULL CHECK (status IN ('applied','partial')),
  applied_at TEXT NOT NULL
);

-- law_alias 法名规范化表（OL-4，docs/tasks-opencourt-bridge.md）：全称 ↔ 简称/别名。
-- 纪律：别名来源必须**语料内可溯源**——source 只允许 corpus-short_name（law.short_name 列）
-- 与 corpus-text（该法规文本中的「以下简称」条款，附出处条文 source_article_id）；
-- 禁止由模型生成别名。多行同 alias = 多候选，解析层必须并列交人工，不得自动采信。
-- 加法式：不加 CHECK（取值合法性放应用层与门禁测试）、仅新表经 schemaSQL IF NOT EXISTS
-- 自动补齐，无需结构迁移。只读消费方读 v8 及更早语料时本表不存在 → 解析层按「无别名」
-- 如实处理并给出构建指引（诚实展示纪律：查不到就说查不到，不猜）。
CREATE TABLE IF NOT EXISTS law_alias (
  alias TEXT NOT NULL,             -- 规范化别名（去书名号与空白后）
  law_id TEXT NOT NULL REFERENCES law(law_id),
  source TEXT NOT NULL,            -- corpus-short_name | corpus-text
  source_article_id TEXT,          -- corpus-text 的出处条文；空 = 出处未留
  built_at TEXT NOT NULL,
  PRIMARY KEY (alias, law_id)
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

-- usage_event 本机使用事件台账（schema v10，产品评审「度量体系」）。
-- 隐私与纪律：只记**计数与结论**，不记检索词、不记案件内容、不记原文；默认离线，
-- 绝不上传（AGENTS.md 默认离线）。只读消费方读 v9 及更早语料时本表不存在——
-- 度量层必须如实报「本机尚未记录」，不得把「没有记录」渲染成「0%」。
CREATE TABLE IF NOT EXISTS usage_event (
  event_id   INTEGER PRIMARY KEY AUTOINCREMENT,
  kind       TEXT NOT NULL,           -- app_open|setup_route|pack_install|search|verify|review_run
  outcome    TEXT NOT NULL DEFAULT '',-- ok|zero|fail|installed|…（结论，不含内容）
  detail     TEXT NOT NULL DEFAULT '',-- 白话细节（不含检索词与案件内容）
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_version_law_date ON law_version(law_id, effective_date, expire_date);
CREATE INDEX IF NOT EXISTS idx_article_version ON article(version_id);
CREATE INDEX IF NOT EXISTS idx_article_doc ON article(doc_id);      -- 按文档反查条文（复核清单/断链检测热点）
CREATE INDEX IF NOT EXISTS idx_document_confirm ON document(confirm_status); -- pending 队列统计与抽样
CREATE INDEX IF NOT EXISTS idx_element_case ON element(case_type);
CREATE INDEX IF NOT EXISTS idx_relation_src ON relation(src_law_id);
CREATE INDEX IF NOT EXISTS idx_law_region_code ON law(region_code); -- 按区划码浏览/过滤（v4）
CREATE INDEX IF NOT EXISTS idx_law_jurisdiction ON law(jurisdiction); -- 按法域浏览/统计（v7）
CREATE INDEX IF NOT EXISTS idx_region_log_law ON region_log(law_id);
CREATE INDEX IF NOT EXISTS idx_region_log_batch ON region_log(batch_id); -- 按批次撤销
CREATE INDEX IF NOT EXISTS idx_agent_review_doc ON agent_review(doc_id); -- 例外队列按条目取最近结论
CREATE INDEX IF NOT EXISTS idx_law_alias_alias ON law_alias(alias); -- 按别名反查法规（OL-4 解析热点）
CREATE INDEX IF NOT EXISTS idx_law_alias_law ON law_alias(law_id);  -- 按法规列别名

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

-- ============ v11 多维标引（docs/design-taxonomy.md，拍板 2026-11-04） ============

-- dict_subject 部门法词表（拍板项 1：按法域全覆盖；词表可扩展，禁止兜底「其他」——
-- 判不出留空进回填队列，界面如实显示「部门法未标注」）。
CREATE TABLE IF NOT EXISTS dict_subject (
  code         TEXT PRIMARY KEY,   -- 如 CN-CIV；法言语文案与代码一一对应
  name         TEXT NOT NULL,      -- UI 展示名（法言法语）
  jurisdiction TEXT NOT NULL,      -- CN|EU|US
  parent_code  TEXT,               -- 预留二级部门法；当前全部为空
  ordinal      INTEGER NOT NULL    -- 界面固定顺序
);

-- dict_doc_form 文档形态词表（拍板项 2：全自动机器判定，无人工落判）。
CREATE TABLE IF NOT EXISTS dict_doc_form (
  code         TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  jurisdiction TEXT NOT NULL
);

-- subject_log 部门法标引审计（law 级，与 region_log 并列而非复用——
-- 正文/地域/部门法是三种独立判定，不得混同）。
CREATE TABLE IF NOT EXISTS subject_log (
  log_id INTEGER PRIMARY KEY AUTOINCREMENT,
  law_id TEXT NOT NULL REFERENCES law(law_id),
  action TEXT NOT NULL CHECK (action IN ('suggest','confirm','reject','clear','undo')),
  from_code TEXT,
  to_code   TEXT,
  source    TEXT NOT NULL,          -- auto|manual|trial|pack
  rule      TEXT,                   -- 机器：规则号；人工：依据
  actor     TEXT NOT NULL,          -- 机器固定 openlaw-subject；人工必须署名
  reason    TEXT,
  batch_id  TEXT,                   -- 同一次批量写入/撤销的关联号（-undo 依此回滚）
  created_at TEXT NOT NULL
);

-- law_status 时效状态派生视图（拍板项 3/4：纯派生不缓存；「已修改」按版本链判定，
-- relation 不参与时效派生，仅作「关联法规」展示）。判定顺序即优先级：
-- 无版本或施行日期全缺→未知；现行版本前有历史版本→已修改；现行版本→现行有效；
-- 全部版本未到施行日→尚未生效；曾有生效版本但现行区间为空→已废止（含换代空窗）。
-- 日期比较用 ISO-8601 字典序，与既有列格式一致。
CREATE VIEW IF NOT EXISTS law_status AS
SELECT l.law_id,
  CASE
    WHEN NOT EXISTS (SELECT 1 FROM law_version v WHERE v.law_id = l.law_id
                     AND v.effective_date IS NOT NULL) THEN '未知'
    WHEN EXISTS (SELECT 1 FROM law_version cur
                 WHERE cur.law_id = l.law_id
                   AND cur.effective_date <= date('now')
                   AND (cur.expire_date IS NULL OR cur.expire_date > date('now'))
                   AND EXISTS (SELECT 1 FROM law_version prev
                               WHERE prev.law_id = cur.law_id
                                 AND prev.effective_date < cur.effective_date)) THEN '已修改'
    WHEN EXISTS (SELECT 1 FROM law_version v WHERE v.law_id = l.law_id
                 AND v.effective_date <= date('now')
                 AND (v.expire_date IS NULL OR v.expire_date > date('now'))) THEN '现行有效'
    WHEN NOT EXISTS (SELECT 1 FROM law_version v WHERE v.law_id = l.law_id
                     AND v.effective_date <= date('now')) THEN '尚未生效'
    ELSE '已废止'
  END AS timeliness
FROM law l;

CREATE INDEX IF NOT EXISTS idx_law_subject_code ON law(subject_code);
CREATE INDEX IF NOT EXISTS idx_law_doc_form ON law(doc_form);
CREATE INDEX IF NOT EXISTS idx_subject_log_law ON subject_log(law_id);
CREATE INDEX IF NOT EXISTS idx_subject_log_batch ON subject_log(batch_id);

-- 词表种子（幂等：INSERT OR IGNORE，重跑不覆盖既有词条；扩展词条走后续回填任务）。
-- CN：「宪法相关法 + 七个法律部门」官方口径（docs/design-taxonomy.md §8 拍板项 1）。
INSERT OR IGNORE INTO dict_subject(code,name,jurisdiction,parent_code,ordinal) VALUES
  ('CN-CON','宪法相关法','CN',NULL,1),
  ('CN-CIV','民法商法','CN',NULL,2),
  ('CN-CRI','刑法','CN',NULL,3),
  ('CN-ADM','行政法','CN',NULL,4),
  ('CN-ECO','经济法','CN',NULL,5),
  ('CN-SOC','社会法','CN',NULL,6),
  ('CN-PRO','诉讼与非诉讼程序法','CN',NULL,7);
-- EU：按政策领域初版全覆盖词表（回填任务中按条目验证并扩展，缺口如实计入「未标注」）。
INSERT OR IGNORE INTO dict_subject(code,name,jurisdiction,parent_code,ordinal) VALUES
  ('EU-INT','内部市场与货物自由流动','EU',NULL,1),
  ('EU-CMP','竞争与国家援助','EU',NULL,2),
  ('EU-AGR','农业与渔业','EU',NULL,3),
  ('EU-ENV','环境与气候','EU',NULL,4),
  ('EU-COM','贸易政策','EU',NULL,5),
  ('EU-SOC','就业与社会事务','EU',NULL,6),
  ('EU-FIN','经济与金融','EU',NULL,7),
  ('EU-JUS','司法与基本权利','EU',NULL,8),
  ('EU-MIG','移民与庇护','EU',NULL,9),
  ('EU-ETR','能源与交通','EU',NULL,10),
  ('EU-RTP','研究与技术','EU',NULL,11),
  ('EU-EXT','对外关系与发展','EU',NULL,12);
-- US：按主题领域初版全覆盖词表（同上，缺口如实计入「未标注」）。
INSERT OR IGNORE INTO dict_subject(code,name,jurisdiction,parent_code,ordinal) VALUES
  ('US-CRI','刑事','US',NULL,1),
  ('US-CVR','民权','US',NULL,2),
  ('US-GOV','政府与行政','US',NULL,3),
  ('US-TAX','税收','US',NULL,4),
  ('US-COM','商业与贸易','US',NULL,5),
  ('US-DEF','国防','US',NULL,6),
  ('US-SOC','社会福利','US',NULL,7),
  ('US-ENV','环境与能源','US',NULL,8),
  ('US-INT','国际关系','US',NULL,9),
  ('US-IPR','知识产权','US',NULL,10),
  ('US-FIN','金融与银行','US',NULL,11),
  ('US-LAB','劳动','US',NULL,12);

INSERT OR IGNORE INTO dict_doc_form(code,name,jurisdiction) VALUES
  ('CN-LAW','法','CN'),('CN-CODE','法典','CN'),('CN-AMD','修正案','CN'),
  ('CN-DEC','决定','CN'),('CN-INT','解释','CN'),('CN-REG','条例','CN'),
  ('CN-PRO','规定','CN'),('CN-MEA','办法','CN'),('CN-RUL','规则','CN'),
  ('CN-DET','细则','CN'),('CN-REP','批复','CN'),('CN-NOT','通知','CN'),
  ('EU-REG','条例','EU'),('EU-DIR','指令','EU'),('EU-DEC','决定','EU'),
  ('EU-REC','建议','EU'),('EU-OPN','意见','EU'),('EU-TRT','条约','EU'),
  ('US-ACT','法律（Act）','US'),('US-PL','公法（Public Law）','US'),
  ('US-EO','行政命令','US'),('US-CFR','规章（CFR）','US'),('US-TRT','条约','US');
