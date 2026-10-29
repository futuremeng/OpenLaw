# 地域标引写入路径（F3）设计检查点

状态：**已批准（2026-10-29）**——§9 四项按建议拍板，§8 第 1 步已实施（见 §10）。
纪律依据：AGENTS.md「写路径从严」「人工复核纪律」「诚实展示纪律」；需求依据：开发文档 6.4 / FR-12。

## 1. 目标与非目标

**目标**

- 给 `law` 增加可复核的地域标识（`region_code`），让浏览/检索按区划码过滤，替代当前
  「按标题前缀猜地区」的不可靠匹配。
- 机器只出建议、人工落判；全程可追溯（谁 / 何时 / 依据哪条规则），且机器批量写入可撤销。

**非目标**

- 不做行政区划变更史（只维护现行区划，与 `geo` 包一致）。
- 不用 IP 或在线地理编码给法规定地域：法规地域只来自名称、发布机关等语料内证据。
- 不混淆两个概念：`user_region`（本机用户偏好，存 `meta`）≠ `law.region_code`（法规地域）。

## 2. 现状事实（已核对代码，非推测）

| 事实 | 位置 |
|---|---|
| 按地区过滤目前靠标题前缀 `l.name LIKE ?`，无区划码列 | `desktop/region.go:249`（`regionClause`）、`:218`（`regionPatterns`） |
| `law` 无 `region_code`；另有 `is_local`（0/1） | `store/schema.sql:12` |
| 区划匹配能力已具备：`MatchPrefix` / `AliasesOf` / `Nearest` / `Path` | `geo/geo.go:203/185/148/92` |
| 机器补标先例：默认 dry-run 出 CSV 报告，`-apply` 才写库并在 `remark` 留痕规则号 | `cmd/openlaw-level/main.go`、`parse/levelinfer.go` |
| `SchemaVersion="3"`、`SchemaMin="3"`；迁移链 v1→v2→v3 在 `store.Open`（写路径）执行 | `store/db.go:19/27-29/118` |
| **缺口**：`release-process.md` 承诺「数据模型变更先自动备份 db」，代码里没有实现 | `docs/release-process.md:5,56` |
| `review_log` 是 **doc 级**（外键 `doc_id`），承载不了 law 级地域落判 | `store/schema.sql:131` |
| 更新包导出用显式列的 `changes.csv`，已有三种 header 做向后兼容 | `pack/pack.go:74-83,116,123` |

## 3. Schema v4（加法式）

只加列、不加 CHECK、不重建表。v2 之所以要重建表是因为要改 CHECK 约束；v4 刻意避开 CHECK，
把取值约束放到应用层与门禁测试，迁移风险降到最低。

```sql
ALTER TABLE law ADD COLUMN region_code TEXT;         -- 空/NULL = 地域未标注
ALTER TABLE law ADD COLUMN region_source TEXT;       -- auto|manual|trial（与界面三态一致）
ALTER TABLE law ADD COLUMN region_rule TEXT;         -- 依据与规则号，如 name-prefix(R1)
ALTER TABLE law ADD COLUMN region_reviewer TEXT;     -- 人工落判署名；空 = 仅机器建议
ALTER TABLE law ADD COLUMN region_reviewed_at TEXT;  -- 人工落判时间
CREATE INDEX IF NOT EXISTS idx_law_region_code ON law(region_code);
```

审计表（law 级，与 `review_log` 并列而非复用）：

```sql
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
  batch_id  TEXT,                   -- 同一次批量写入/撤销的关联号
  created_at TEXT NOT NULL
);
```

版本协商：`SchemaVersion="4"`、`SchemaMax="4"`，**`SchemaMin` 保持 `"3"`**——只读消费方读
v3 语料时没有 region 列，界面显示「地域未标注」即可，不被迫升级（待批准，见 §9）。

## 4. 备份与回滚（先补机制，再谈写入）

1. 在 `store.Open` 写路径，**判定将要发生结构迁移时**（读到的 `schema_version` < `SchemaVersion`）
   先备份：`VACUUM INTO '<dataDir>/backups/openlaw-v<from>-<UTC时间戳>.db'`
   （`modernc.org/sqlite` 支持，单文件一致快照）。
2. 备份失败 → 迁移中止并报错（fail-closed），绝不「先迁移再补备份」。
3. 版本未变化不备份（避免每次启动都产文件）；`backups/` 加入 `.gitignore` 与 README 数据目录说明。
4. 回滚两条路径，均写入 `docs/release-process.md`：
   - 整库：停应用 → 用备份文件替换 `db/openlaw.db`；
   - 按批次：`openlaw-region -undo <batch_id>`。前提简化：**机器只写 `region_code IS NULL` 的行**
     （同 `openlaw-level` 的 `WHERE` 护栏），故撤销 = 把该批次写过的行清回空，无需记录原值。

## 5. 机器建议 vs 人工落判（边界）

- 机器（`openlaw-region -apply`）：只写 `region_code IS NULL` 的行；写 `region_source='auto'`、
  `region_rule=R<n>`，`region_reviewer` 留空；每行写
  `region_log(action='suggest', actor='openlaw-region', batch_id=…)`。
- 人工（面板 / CLI）：确认、驳回、改选一律必须署名 + 结论依据，写
  `region_log(action='confirm'|'reject'|'clear')` 并填 `region_reviewer/reviewed_at`。
- 界面诚实：`region_reviewer` 为空的条目一律显示「机器建议 · 待复核」，不得显示成已确认；
  未标注显示「地域未标注」，不得留白或渲染占位。
- 不混用 doc 级 `confirm_status`：法规正文已复核 ≠ 其地域已复核。

## 6. 判定规则（`parse.SuggestRegion`，读侧 `InferRegion` 行为不变）

`parse/regioninfer.go` **早已存在**（读侧 `InferRegion`，界面显示走它），本步是在其上加写侧建议层，
不是新建文件。写侧与读侧共用 `geo` 的同一份匹配实现，口径不会分叉。

| 规则 | 依据 | 置信 | 处置 | 实现 |
|---|---|---|---|---|
| R1 | 名称以省级行政区全称开头 | 高 | 可写 | `RegionRuleProvince`（既有） |
| R2 | 名称以地级行政区全称开头（含自治州/盟/地区全称） | 高 | 可写 | `RegionRuleCity`（既有） |
| R2a | 别名前缀（「深圳经济特区」→440300、「海南经济特区」→460000 等 5 条） | 高 | 可写 | `geo.regionAliases` **已内置**并参与匹配，无需新增 |
| R3 | 发布机关 `law.issuer` 含行政区名 | 中 | 不写，仅进报告 | 新增 `geo.MatchContained` + `RegionRuleIssuer` |
| R4 | 名称与发布机关指向**互不隶属**的两个行政区 | — | 不写，转人工 | 新增冲突判定；同链（石家庄市… + 河北省人大常委会）不算冲突 |
| R5 | 无法判定 | — | 留空，绝不猜 | — |

与原设计的两处出入（如实记录，不是遗漏）：

1. **名称前缀同级并列时取上级**：「吉林省吉林市…」判省级 220000，沿用 `geo.MatchPrefix`
   既有口径（与 `levelinfer` 先强后弱一致），未改成「取最长前缀到市」。改口径会动界面
   既有显示与历史报告数字，属独立变更，不在本步夹带。
2. **冲突只查「名称 vs 机关」**：名称内部出现两个地区名时由最长匹配决定（见上条），
   不额外判为冲突——真实语料 28,952 条试采冲突为 0，未见需要更严的证据。

一致性自检（只报告、不自动改）：`effect_level='地方性法规'` 却无地域；非地方性法规却有地域；
`is_local` 与地域矛盾。计数如实进报告（尚未实现，随第 4 步面板一起做）。

## 7. CLI、界面、检索与包

**写入路径在库层 `ingest.RegionMarker`**（CLI 只是薄壳，第 4 步桌面面板必须调同一 runner）

- `Scan`：只读，逐条给出 `confidence ∈ {marked, high, medium, conflict, none}` 与依据文案。
- `Apply`：只写 `high` 且 `region_code` 为空的行；`region_source='auto'`、`region_rule`=依据、
  **`region_reviewer` 一律留空**；逐笔写 `region_log(action='suggest')` 并带 `batch_id`。
- `Undo(batch_id, apply)`：默认干跑计数；执行时只清「仍是该批写入值且无人工署名」的行，
  已人工落判的绝不回滚，并写 `region_log(action='undo')`。

**`cmd/openlaw-region`**（仿 `openlaw-level`）

- 默认 dry-run：CSV 报告（`law_id,name,issuer,confidence,region_code,region_path,rule,note`，
  **已标注的行也列出**，便于核对覆盖率而非只看命中数）+ 分类计数；`-json` 供脚本消费。
- `-apply`：仅写高置信；`-undo <batch_id>`（加 `-apply` 才执行）。
- 文案沿用：`dry-run：未写库。核对报告后加 -apply 执行（只写高置信）`。

**桌面「地域标引」面板**

- 待复核列表（机器建议优先）：名称 / 建议地区 / 规则号 / 来源徽章；
- 操作：确认 / 驳回 / 改选地区，署名与依据必填（沿用现有复核署名约定）；
- 批量流转先干跑计数（`BulkPreview` 模式）再执行；
- 新绑定方法纳入 `desktop/binding_test.go` 契约门禁。

**检索与包**

- `regionClause`：有 `region_code` 时按码前缀过滤；语料未标满时回退标题前缀，并在界面如实
  标注「按标题前缀匹配（该语料未标地域码）」。
- `pack`：`changes.csv` 增 `region_code`/`region_source` 列，新增 header 版本，旧包按现有
  三种 header 机制保持兼容；包内 `schema_version` 协商不变。

## 8. 验收门禁与实施顺序

**测试**（离线可跑，临时目录语料，不依赖网络或真实语料）

- `store`：v3→v4 真实链路（旧库数据保留、新列存在、重复迁移幂等）；**迁移前备份文件存在且
  可打开**；备份失败 → 迁移中止。
- `parse/regioninfer`：R1–R5、冲突不猜、自治州/经济特区别名命中。
- CLI：dry-run 后断言库未变；`-apply` 写库并留 `region_log`；`-undo` 复原。
- `desktop`：绑定契约、DOM 元素契约、未标注与「机器建议 · 待复核」的显示断言。
- 真实语料 dry-run 报告：在 28,952 条总量下给出 已标注 / 本次建议 / 冲突 / 无法判定 计数，
  如实写入 `docs/tasks-opencourt-bridge.md` 与 CHANGELOG。
  **基线**：现行「名称前缀」匹配在真实语料上的覆盖率为 24,033/28,952（83.0%，已实测记录）。
  F3 落码后必须报告同一口径的覆盖率，并给出「区划码覆盖 ≥ 前缀覆盖」的核对；若更低，
  必须逐条列出差异原因，不得只报总数。

**实施顺序**（每步一个原子提交；任一步门禁不过就停，不带病推进）

1. `store`：自动备份机制 + v4 迁移 + 测试（高风险，先做；顺带补 `release-process.md` 已承诺
   却未实现的备份）
2. `parse/regioninfer.go` + 测试
3. `cmd/openlaw-region`（先只 dry-run）→ 真实语料报告，**人工看过报告再谈 `-apply`**
4. 桌面「地域标引」复核面板 + 绑定 + 门禁
5. 浏览过滤切区划码 + `pack` 列扩展 + 兼容测试
6. 文档：`deploy.md` / `data-dictionary.md` / `tasks-opencourt-bridge.md` / README / CHANGELOG

## 9. 拍板结论（2026-10-29 已批准）

1. `SchemaMin` **保持 `"3"`**——只读方不被迫升级，代价是看不到地域列（已实现）。
2. 中置信 R3（机关名）**默认不写**，只进报告。
3. 复核面板首版**只做逐条 + 小批**，批量确认必须先干跑计数。
4. 机器建议**随更新包分发**，但 `region_reviewer` 保持为空 → 接收方界面仍显示待复核。

## 10. 实施进度

- **第 1 步已完成（2026-10-29）**：`store/backup.go` 自动备份 + schema v4（`law` 5 列 +
  `region_log` + 索引）+ 测试。真实语料 v3→v4 已跑通并核对：备份快照
  `data/backups/openlaw-v3-20261029T121439Z.db`（2,118,131,712 字节，`schema_version=3`、
  无 `region_code` 列、28,952 法规可读）；升级后逐表计数与 FTS 条目数与快照完全一致，
  `region_code` 已标注 0 条（列就位、不写值）。备份目录不可写时迁移中止且原库未动，
  该行为有测试覆盖。
- **第 2 步已完成（2026-10-29）**：写侧建议层 `parse.SuggestRegion` + `geo.MatchContained`
  （见 §6 口径与两处出入）。读侧 `InferRegion` 未改，界面显示不受影响。
- **第 3 步已完成（2026-10-29，仅到试采）**：`ingest.RegionMarker` + `cmd/openlaw-region`。
  真实语料 28,952 条 dry-run：

  | 分类 | 条数 |
  |---|---|
  | 已标注 | 0 |
  | 高置信（名称前缀 R1/R2，可写） | **24,033** |
  | 中置信（仅发布机关 R3，不写） | 90 |
  | 冲突（转人工） | 0 |
  | 无法判定（留空） | 4,829 |

  覆盖率核对（§8 要求的同口径比较）：高置信 24,033 与既有名称前缀基线 **24,033/28,952（83.0%）
  逐条一致**——写侧未引入新口径，也不存在「区划码覆盖 < 前缀覆盖」的差异需要解释。
  试采后 `region_code` 仍 0 条、`region_log` 仍 0 行，只读为实。
  如实记录：`law.issuer` 仅 5,214/28,952（18%）有值，R3 通道能补的量有限（90 条）；
  「兰州经济技术开发区条例」这类名称省略「市」字的仍判不出（留空，不猜）。
  **`-apply` 尚未执行**——按 §8 须人工核对报告后再谈。
- 第 4 步起（桌面复核面板等）尚未开始。

---

原 §9 的四项待决事项已在 2026-10-29 全部按建议拍板（见上）。第 1–3 步已落地（第 3 步止于试采）；
第 4 步起须按 §8 顺序逐步推进，每步过门禁再提交。

