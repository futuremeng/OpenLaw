# 法规多维标引（分类模型）设计检查点

状态：**已批准（2026-11-04）**——§8 四项拍板已定（词表按法域全覆盖并回填至全覆盖；
文档形态全自动；时效纯派生不缓存；「已修改」按版本链判定）。
**第 1、2、3 步已实施**（schema v11：law 六列 + dict_subject/dict_doc_form/subject_log +
law_status 视图 + 词表种子 + 迁移链与门禁测试，见 store/taxonomy_v11_test.go；
补标工具 parse/subjectinfer + ingest.SubjectMarker + cmd/openlaw-subject，
默认 dry-run、-apply 只写规则命中与形态补判、-undo 按批次撤销，
见 ingest/subject_test.go；智能体自审扩展 agent.RunReview →
ingest.WriteAgentSuggestion——classify JSON 增 subject_code/subject_confidence/doc_form，
达门槛写 auto 建议（署名留空），与裁决无关、人工结论不顶回，
见 agent/review_test.go）；**桌面「部门法标引」面板已实施**（ingest/subjectreview.go 人工落判 +
desktop/subjectreview.go 绑定 + panel-subject 面板：复刻地域面板交互，批量先干跑，
文档形态只读）。
纪律依据：AGENTS.md「架构纪律（写路径从严）」「人工复核纪律」「诚实展示纪律」；
范式先例：`docs/design-region-write.md`（v4 地域标引）、`docs/design-multi-jurisdiction.md`（v7 法域）。
外部参照：lawtext/laws（https://lawtext.github.io/ 源码，Hugo+Docsy，2026-11 分析）。

## 1. 目标与非目标

**目标**

- 把法规标引从单一 `effect_level` 扩展为**多维正交**模型：法域 × 效力层级 × 地域 ×
  部门法 × 文档形态 × 时效状态，每一维独立可空、可过滤、可分面计数。
- 部门法（subject）走与地域同款的「机器建议 → 人工落判 → 全程留痕 → 批次可撤销」路径；
  机器不裁决，界面区分「机器建议 · 待复核」与「已人工复核」。
- **部门法全覆盖**：词表按法域全覆盖（CN/EU/US 各有词表），回填以**语料全覆盖**为目标——
  「未标注」是待办积压而非终态，规则判不出的转智能体建议与人工落判直至清零。
- 时效状态（尚未生效/现行有效/已修改/已废止/未知）**派生而非落列**——单一事实来源是
  `law_version` 区间与已确认的 `relation`，杜绝状态值漂移。
- 受控词表：分类取值一律命中词表（`dict_*`），界面与导出不得出现自由文本分类。

**非目标**

- 不建自由关键词表。lawtext 的 keywords 为抽取式（「当事人」264 次、「问题」200 次），
  噪音无治理；本库关键词检索由 FTS5 + LIKE 兜底承担，不额外落列。
- 不把多维混装进一个标签字段。lawtext 的 tags 同时装制定机关、效力状态、文种、主题，
  交叉筛选语义混乱——本库每一维独立成列/表。
- 一期不做制定机关规范化（`issuer` 仍为自由文本；机关权威表列二期，见 §7）。
- 不引入 lawtext 语料本身（其数据来自 flk 站内采集，robots 禁止抓取纪律不变，见
  `ingest/downloader.go`）；只借鉴其分类学。

## 2. 现状事实（已核对代码，非推测）

| 事实 | 位置 |
|---|---|
| 分类仅一维：`law.effect_level` CHECK 枚举（宪法/法律/…/欧盟条例/美国联邦法律等） | `store/schema.sql:19` |
| 法域列 v7 起存在，刻意不加 CHECK，合法性在应用层与门禁测试 | `store/schema.sql:27`、`docs/design-multi-jurisdiction.md` |
| 地域标引五列范式（code/source/rule/reviewer/reviewed_at）+ law 级审计表 `region_log` + 批次撤销 | `store/schema.sql:31-35,196`、`docs/design-region-write.md` |
| 复核闸口在 document 级：新导入一律 pending，确认必须署名+依据+`review_log` | `store/schema.sql:56-59`、`ingest/confirm.go` |
| 双智能体自审的 `agent_review.classify` 已承载分类建议 JSON（含 effect_level） | `store/schema.sql:172`、`agent/` |
| 时效区间左闭右开 `[effective_date, expire_date)`，未失效留 NULL | `store/schema.sql:63-75` |
| `relation`（修改/废止/解释/配套/依据）默认 pending，人工确认后才作数 | `store/schema.sql:118-124` |
| 效力等级保守名称规则补标先例：默认 dry-run，`-apply` 写库留痕 | `cmd/openlaw-level/`、`parse/levelinfer.go` |
| `SchemaVersion="10"`；加法式先例：v8/v9 新表不加 CHECK、纯 ALTER/IF NOT EXISTS | `store/db.go:44` |
| 浏览分面已有等级/机关/地域/复核态过滤与同条件计数 | `desktop/browse.go`、`wiring_smoke.mjs` |

**lawtext 参照结论**（`/tmp/lawtext-laws` 实测）：五维正交（目录=效力层级、
categories=部门法七大类、group=文档形态、tags=混装、years/keywords）；status 五态
（有效 1529 / 已修改 420 / 已废止 276 / 未知 269 / 尚未生效 8）靠 `statusClassMap`
事后映射同义值（「有效/生效」「已修改/已修订」并存）——这是要用派生+词表避免的反例；
文件名=flk 条目 id、`urls` 挂官方详情页——与本库 doc_id 前缀纪律（`govcn_` 等）同构。

## 3. 维度模型

| 维度 | 语义 | 载体 | 状态 |
|---|---|---|---|
| 法域 | CN/EU/US | `law.jurisdiction` | 已有（v7） |
| 效力层级 | 宪法>法律>行政法规>… | `law.effect_level` | 已有 |
| 地域 | GB/T 2260 | `law.region_*` 五列 | 已有（v4） |
| **部门法** | 宪法相关/民商/行政/经济/社会/刑法/诉讼程序 | `law.subject_*` 五列 + `dict_subject` | **新增（v11）** |
| **文档形态** | 法典/修正案/决定/解释/条例/规定/办法/批复/命令… | `law.doc_form` + `dict_doc_form` | **新增（v11）** |
| 时效状态 | 尚未生效/现行有效/已修改/已废止/未知 | 派生视图 `law_status` | **新增（v11，派生）** |
| 年份 | 公布年代 | `document.publish_date` 派生 | 已有数据 |
| 制定机关 | 机关层级树 | `law.issuer`（自由文本） | 二期（§7） |

设计要点：

- **部门法与效力层级正交**：「法律」是层级（谁制定的），「行政法」是部门法（管什么的）。
  lawtext 把两者都塞进 categories（「法律」294 次与「行政法」139 次并存即混淆证据），
  本库分列存放，界面并列展示。
- **部门法词表按法域全覆盖**（拍板项 1）：CN 用「宪法 + 七个部门法」官方口径，EU/US 各建
  全覆盖词表；全部法域的条目都应有归属，回填目标为全覆盖（§5），期间未标注如实显示。
- **文档形态全自动**（拍板项 2）：文种是标题可判的客观事实，机器判定即落，不设人工落判；
  判不出留空并计入待办，规则迭代直至收敛。部门法允许判不准，走人工落判。
- **时效状态派生、不缓存**（拍板项 3），规则（顺序即优先级）：
  1. 无任何版本或施行日期全缺 → `未知`（诚实显示，不猜）；
  2. 存在现行版本（`effective_date ≤ 今日 < expire_date`）：
     - 该版本之前同 law 还有历史版本（版本链 > 1，即被修正案/修改决定更新过）→ `已修改`
       （拍板项 4：**按版本链判定**，现行文本即修改后文本，原版本仍可溯源；不再以
       「修改」relation 作时效判据，relation 只用于「关联法规」展示）；
     - 否则 → `现行有效`；
  3. 无现行版本且全部版本未到施行日 → `尚未生效`；
  4. 曾有生效版本但现行区间为空 → `已废止`。
     边界：旧版本已废止且新版本尚未生效（换代空窗）→ 按 4 判 `已废止`，界面并列显示
     「新版本 X 将于 Y 施行」。

## 4. Schema v11（加法式）

延续 v4/v7 策略：纯 ALTER、不加 CHECK、不重建表；取值合法性放应用层与门禁测试。

```sql
-- law：部门法标引五列（与 region_* 完全同构，语义一致）
ALTER TABLE law ADD COLUMN subject_code TEXT;          -- 空 = 部门法未标注
ALTER TABLE law ADD COLUMN subject_source TEXT;        -- auto|manual|trial|pack
ALTER TABLE law ADD COLUMN subject_rule TEXT;          -- 依据与规则号，如 title-keyword(S2)
ALTER TABLE law ADD COLUMN subject_reviewer TEXT;      -- 人工落判署名；空 = 仅机器建议
ALTER TABLE law ADD COLUMN subject_reviewed_at TEXT;
-- law：文档形态（机器判定，单列；判不出留空，不设人工落判——见 §3）
ALTER TABLE law ADD COLUMN doc_form TEXT;
CREATE INDEX IF NOT EXISTS idx_law_subject_code ON law(subject_code);
CREATE INDEX IF NOT EXISTS idx_law_doc_form ON law(doc_form);
```

新表（`IF NOT EXISTS` 自动补齐，不进迁移链）：

```sql
CREATE TABLE IF NOT EXISTS dict_subject (
  code        TEXT PRIMARY KEY,     -- 如 CN-ADM（行政法）、CN-CIV（民法商法）
  name        TEXT NOT NULL,        -- 法言语文案：行政法
  jurisdiction TEXT NOT NULL,       -- CN|EU|US
  parent_code TEXT,                 -- 预留二级部门法；一期全部为空
  ordinal     INTEGER NOT NULL      -- 界面固定顺序（宪法相关→民商→刑→行政→经济→社会→程序）
);
CREATE TABLE IF NOT EXISTS dict_doc_form (
  code        TEXT PRIMARY KEY,     -- FA(法典)/XZ(修正案)/JD(决定)/JS(解释)/TL(条例)/…
  name        TEXT NOT NULL,        -- 条例/规定/办法/批复/规则/细则/通知/命令/条约…
  jurisdiction TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS subject_log (               -- 与 region_log 并列不复用
  log_id INTEGER PRIMARY KEY AUTOINCREMENT,
  law_id TEXT NOT NULL REFERENCES law(law_id),
  action TEXT NOT NULL CHECK (action IN ('suggest','confirm','reject','clear','undo')),
  from_code TEXT,
  to_code   TEXT,
  source    TEXT NOT NULL,          -- auto|manual|trial|pack
  rule      TEXT,                   -- 机器：规则号；人工：依据
  actor     TEXT NOT NULL,          -- 机器固定 openlaw-subject；人工必须署名
  reason    TEXT,
  batch_id  TEXT,
  created_at TEXT NOT NULL
);
```

派生视图（时效状态唯一出口；只读、不缓存——拍板项 3/4）。判定顺序即优先级，按 law
聚合（每 law 恰一行；「已修改」以**版本链**为判据，`relation` 不参与时效派生）：

```sql
CREATE VIEW IF NOT EXISTS law_status AS
SELECT l.law_id,
  CASE
    -- 1. 无任何版本、或有版本但施行日期全缺 → 未知（诚实显示，不猜）
    WHEN NOT EXISTS (SELECT 1 FROM law_version v WHERE v.law_id = l.law_id
                     AND v.effective_date IS NOT NULL) THEN '未知'
    -- 2. 存在现行版本 → 已修改（版本链 > 1，现行版本前有历史版本）或现行有效
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
    -- 3. 全部版本都还没到施行日 → 尚未生效
    WHEN NOT EXISTS (SELECT 1 FROM law_version v WHERE v.law_id = l.law_id
                     AND v.effective_date <= date('now')) THEN '尚未生效'
    -- 4. 有过生效版本但现行区间为空 → 已废止（含换代空窗，界面并列提示新版本施行日）
    ELSE '已废止'
  END AS timeliness
FROM law l;
```

> 「已修改」= 现行版本之前同 law 还有历史版本（修正案/修改决定已并入版本链，拍板项 4）；
> 首个版本即现行版本的单一版本法规 → `现行有效`。`relation`（修改/废止/解释/配套/依据）
> 只服务「关联法规」展示与复核流程，不再作为时效判据——版本链是入库时已确认的事实，
> 比 pending/confirmed 的关系标记更稳定。日期比较用 ISO-8601 字典序，与既有列格式一致。
> 同一 law 多个现行版本并存属数据异常，视图按 2 处理，但必须另有巡检写入 `validate_log`
> 并在界面如实提示（不静默）。


## 5. 写入路径与工具

- **`openlaw-subject` CLI**（与 `desktop` 采集面板共用 `ingest`/`parse` 层 runner——
  同一代码路径纪律）：
  - 默认 dry-run：CSV 报告（`law_id,name,jurisdiction,subject_code,confidence,rule,note`），
    不写库；`-apply` 仅写**高置信**（≥0.85，与智能体门槛一致）且
    `subject_reviewer IS NULL` 的行（人工结论不被机器顶回，同 region `Apply` 纪律）；
    `-undo <batch_id>` 整批回滚，`subject_log` 全程留痕。
  - 规则保守枚举（顺序即置信度）：名称含「刑法/刑事」→刑；「民事/商事/公司/合同」→民商；
    「劳动/社会保险/未成年人」→社会法；「税/反垄断/价格/审计」→经济法；
    「诉讼/仲裁/程序」→程序法；处罚/治安/环境管理/许可→行政法；判不出留空转下一级。
  - **全覆盖收敛路径**（拍板项 1）：规则 → 智能体建议（classify 出 subject+confidence，
    达门槛写 auto 建议）→ 例外队列人工落判；每轮批次后「未标注」计数必须下降并在报告
    中如实列出残余原因分布，直至清零。EU/US 词表同口径建设。
  - **文档形态全自动**（拍板项 2）：`openlaw-subject` 同批产出 `doc_form`（标题后缀/
    文种规则，如「…修正案」→修正案、「…批复」→批复），无人工队列；判不出计入
    「形态未判定」待办并随规则迭代收敛。
- **智能体自审扩展**：`agent_review.classify` JSON 增加 `subject`（code+confidence）与
  `doc_form` 字段；两项均只是建议，达门槛自动写仅 `subject_source='auto'` 且
  `subject_reviewer` 留空——**永不代签人工**。
- **人工落判**：桌面「部门法标引」面板（专业模式）复刻地域面板交互：确认/改选/驳回/清除，
  一律署名+结论依据，写 `subject_log`；驳回后留空但署名在，机器不再覆盖。
- **批量流转先干跑**：BulkPreview 计数后才执行（人工复核纪律）。

## 6. 浏览侧（分面与渐进披露）

- 分面顺序固定：**法域 → 效力层级 → 部门法 → 地域 → 年份 → 时效状态 → 复核状态**；
  每级计数与列表同条件计算（既有冒烟纪律，`wiring_smoke.mjs` 扩展）。
- 分类列表页头部折叠统计（法言语文案，先例：lawtext `_index` 的 `<details>` 摘要）：
  「本部门法共 X 条：现行有效 Y、已废止 Z、待复核 W；部门法未标注 N 条
  （可用『部门法标引』工具补标）」。N 必须如实显示，禁止并入任何分类。
- 条目页法规信息卡字段序：制定机关｜公布日期｜施行日期｜时效状态徽章｜效力层级｜
  部门法（含「机器建议 · 待复核」/「已人工复核」标记）｜文档形态｜来源链接。
  缺字段显示「缺失/未标注」+ 补救指引（诚实展示纪律）。
- 时效状态徽章文案与配色映射集中一处（对应 lawtext `statusClassMap` 的教训）：
  枚举五态由视图产生，UI 不再各自映射同义词。
- 文案遵守 `docs/design-ui-language.md`：subject→「部门法」、doc_form→「文档形态」、
  timeliness→「时效状态」；技术标识（subject_code 等）不进 UI。
- 实施状态（2026-11-04）：分面顺序、时效徽章集中映射、条目页信息卡字段序已落地，
  并由 `desktop/ui_contract_test.go` 与 `wiring_smoke.mjs` 门禁；「分类列表页头部折叠
  统计」未实施——当前浏览形态是分面平列而非逐类列表页，登记为后续项。

## 7. 二期：制定机关规范化（登记不实施）

`dict_authority`（机关权威表，含层级 parent 与法域）+ `law.authority_code` 五列范式；
`issuer` 自由文本保留为原文照录，规范化码另行落列。lawtext 用 tags 承载机关
（「国务院」867 次）证明该维度检索价值高，但必须独立成维。别名归一复用 `law_alias`
纪律：只成语料内可溯源映射，禁止模型生成。

## 8. 拍板记录（2026-11-04 已确认）

1. **部门法词表全覆盖**：按法域建全覆盖词表（CN「宪法 + 七个部门法」官方口径，EU/US
   各自全覆盖词表）；回填以语料全覆盖为目标，「未标注」视为待办积压而非终态。
2. **文档形态全自动**：机器判定即落，不设人工落判；判不出计入待办，规则迭代收敛。
3. **时效状态纯派生、不缓存**：`law_status` 视图为唯一出口；性能不达标另议物化
   （届时属写路径变更，高风险评审）。
4. **「已修改」按版本链判定**：现行版本之前同 law 存在历史版本即判「已修改」；
   `relation` 不参与时效派生，仅作「关联法规」展示；「废止」以 `law_version.expire_date`
   为准，由废止确认流程维护。

## 9. 迁移与兼容

- v10 → v11：纯 ALTER + 新表 + 视图，加法式；`SchemaVersion="11"`、`SchemaMin` 保持 10
  （旧库读新代码不降级、新库旧代码按「未标注」如实处理——同 v4/v8 先例）。
- 只读消费方读 v10 语料：`subject_*`/`doc_form` 列缺失 → 标引层按「未标注」如实显示并
  给出补标指引；`law_status` 视图不存在 → 时效状态显示「未知（语料版本不含时效视图）」。
- 更新包：`changes.csv` v4 表头追加 `subject_code`/`subject_source` 两列；旧包照常被读，
  缺列留空；导入侧 `subject_source` 一律改写 `pack`、reviewer 留空（同 region 携带纪律）。
- 存量回填：`openlaw-subject` 全库干跑 → 审阅 CSV → `-apply` 分批（批次号
  `subject-<UTC>`），界面「部门法未标注」计数随批次下降，可 `openlaw-subject -undo` 撤销。

