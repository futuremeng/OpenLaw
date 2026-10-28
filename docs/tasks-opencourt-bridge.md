# 任务清单：OpenCourt 对接桥（opencourt-bridge）

OpenCourt 侧盘点（2026-10-28）发现：本仓库已具备的能力大多够用，缺的是**六个契约点**
——其中三个是 OpenCourt 侧越界做了本应由本仓库负责的事（需要本仓库提供正确出口才能
修掉），三个是数据/接口缺口。

边界原则（与 OpenCourt `docs/architecture.md` §跨项目边界 一致）：
**谁拥有语料，谁拥有该语料的索引、检索与语料层校验。** 本仓库回答「这个引用真不真、
当时哪个版本生效、溯源断没断、要件确认了没有」；OpenCourt 回答「在本案里能不能用、
怎么呈现」。本清单不含任何 OpenCourt 侧改动。

## 优先级速览

| # | 任务 | 优先级 | 阻塞什么 |
|---|---|---|---|
| OL-7 | sdk 只读入口 `OpenReadOnly` | **P0**（✅ 2026-10-28 完成） | 一并安装时的数据完整性（隐患 H1） |
| OL-8 | schema 版本协商（不匹配即拒） | **P0**（✅ 2026-10-28 完成） | schema 版本被消费方回写降级（隐患 H2） |
| OL-2 | 构成要件导入 CLI（`openlaw-element`） | **P0** | OpenCourt v0.2 M4 全部（`element` 表当前 **0 行**） |
| OL-1 | 显式 `effect_status` 字段 | **P0** | OpenCourt 停止自行推导法条效力（违例 V1） |
| OL-3 | 要件名封闭候选集查询 | P1 | OpenCourt 阅卷抽取的要件归属（违例 V3） |
| OL-4 | 法名规范化/别名解析 + 候选列表 | P1 | 文书引用核验可用性（简称引用极常见） |
| OL-5 | 程序期限规则查询（带法条引用） | P2 | OpenCourt 程序守护的规则溯源（违例 V4，v0.3） |
| OL-6 | sdk 接口稳定性承诺 | 持续 | OpenCourt 进程内 import 是编译期耦合 |
| OL-9 | 清单采集通道内聚本仓库（browser 包 + `openlaw-govlist`） | **P0**（✅ 2026-10-28 完成） | 独立分发时清单层曾依赖 OpenCourt（违例 V5） |
| OL-10 | 本仓库桌面客户端（Wails v2，对齐 OpenCourt 技术栈；语料状态/确认闸口 UI/采集任务面板） | P1（🚧 M1 完成 2026-10-28：状态+报告分诊/检索/确认增强；M2 采集任务、M3 运维待建） | 「自带管理界面与客户端」的边界前提（integration-model.md §1）当前只有 CLI |

> 边界前提（OpenCourt `docs/integration-model.md`）：**代码各自分发，语料只有一份；
> 谁生产语料谁独占写权，消费方一律只读。** OL-7/OL-8 就是把这条纪律写进本仓库的
> 打开路径——现状是任何调用方一打开库就会跑迁移并回写 `schema_version`。

---

## OL-7 sdk 只读入口（P0）

> ✅ 2026-10-28 已实现：`store.OpenReadOnly` + `sdk.OpenReadOnly`（query_only、不
> 迁移、版本协商见 OL-8）；验收单测见 `store/version_test.go`；契约写入 docs/api.md §1。

**现状**：`store.Open()` 的 DSN 是
`file:...?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`
——**读写打开**，且 `Open()` 无条件执行 `migrate()`。任何调用方（含 OpenCourt）一打开
语料就会写库：这是「消费方修改生产者数据」，与边界纪律冲突（隐患 H1）。

**要求**

- 新增 `sdk.OpenReadOnly(dataDir) (*OpenLaw, error)`：
  - DSN 追加 `_pragma=query_only(1)`；
  - **不执行 migrate**、不写 `meta`；
  - `schema_version` 高于支持范围时直接返回错误（见 OL-8）。
- `sdk.New` 保留给本仓库自己的写入路径（ingest / confirm / pack import / check）。
- 在 `docs/api.md` 明确写：**上层应用必须用 `OpenReadOnly`**；用 `New` 打开他人语料
  属于越权。

> 为什么不用 `mode=ro`：WAL 模式库只读打开仍需 `-shm`/`-wal` 可写，容易失败；
> `query_only` 是按连接生效的 pragma，与 WAL 兼容，且能挡住误写的 SQL。

**验收**：只读入口打开语料后 `md5`/`mtime` 不变；对只读连接执行 `INSERT` 报错；
单测覆盖「只读入口不跑迁移」。

## OL-8 schema 版本协商（P0）

> ✅ 2026-10-28 已实现：`SchemaMin/SchemaMax` 导出、两入口先读后判、超上限拒开且
> 不回写、manifest +producer_version/schema_version（签名覆盖）；验收见
> `store/version_test.go`（v99 标记保持不变）。

**现状**：`migrate()` 末尾执行

```sql
INSERT INTO meta(key, value) VALUES('schema_version', ?)
  ON CONFLICT(key) DO UPDATE SET value = excluded.value
```

把 `meta.schema_version` 无条件写成**内嵌 SDK 的** `SchemaVersion`（当前 `"3"`）。
若语料由更新的 OpenLaw 产出（schema v4），旧 SDK 一打开就把标记**回写成 v3**：
结构仍是 v4，声称却是 v3，后续新版 CLI 会重跑迁移（隐患 H2，数据完整性事故）。

**要求**

- 导出 `SchemaMin` / `SchemaMax`（本 SDK 可读写与可只读的范围）。
- 打开流程改为：先读 `meta.schema_version`（不写）→
  - `> SchemaMax` ⇒ **返回错误**，不迁移、不部分读；
  - `< SchemaMin` ⇒ 返回错误（提示用 OpenLaw 升级语料）；
  - 范围内 ⇒ 写入路径才跑 `migrate()`；只读路径跳过。
- `manifest.json` 增加 `producer_version`（产出该语料的 OpenLaw 版本）与
  `schema_version`，受 Ed25519 签名覆盖；消费方可据此显示来源。
- 错误文案包含两条出路：升级消费方应用 / 用兼容语料包。

**验收**：构造 `schema_version='99'` 的库，旧 SDK 两种入口均报错且**不改动该值**；
`producer_version` 缺失时按「来源未知」处理而非报错。

## OL-1 显式 `effect_status`（P0）

**现状**：`Search` / `GetArticle` / `ResolveVersion` 只返回 `effective_date` 与
`expire_date`，由上层自行推断效力。OpenCourt 目前的做法（`plugins/law_openlaw.go`
的 `openlawStatus()`）是「`expire_date` 非空 → `AMENDED`，否则 `EFFECTIVE`」——
**效力判定发生在语料层之外**，这是越界，且判不出废止（OpenLaw 10 章纪律：机器不自动
判定废止，须 `confirmed` 的 relation 证据）。

**要求**

- `Search` 的每条 hit、`GetArticle` / `ResolveVersion` 的 `Data` 增加
  `effect_status` 字段，取值：

  | 值 | 判定依据 |
  |---|---|
  | `EFFECTIVE` | 版本区间覆盖当下，且无 `confirmed` 的废止/接续关系 |
  | `AMENDED` | 被后续版本接续（`expire_date` 非空）且有 `confirmed` relation |
  | `REPEALED_PENDING` | 存在废止/接续 relation 但 `confirm_status != confirmed` |
  | `UNKNOWN` | 版本链不完整或 relation 缺失 |

- 判定只依据 `law_version` 链 + `relation`（**仅 `confirm_status = confirmed` 可判
  `REPEALED`**），与现有 10 章纪律一致；不确定就返回 `UNKNOWN`，不猜。
- 保留 `effective_date` / `expire_date` 原字段不变（向后兼容）。

**验收**：表驱动单测覆盖四种取值；`relation` 为 `pending` 时绝不出现 `REPEALED`；
`docs/api.md` §3 字段表同步。

## OL-2 构成要件导入 CLI（P0）

**现状**：`element` 表 **0 行**（对比 `law` 27955 / `article` 1334070）。`cmd/` 下
无任何 element 导入工具。`AnalyzeElements` 因此永远返回
「案由无已录入构成要件，请先人工确认并导入要件」——上层要件匹配失去数据基础。

**要求**

- 新增 `cmd/openlaw-element/`：CSV/JSON 导入构成要件到 `element` 表
  （`element_id / article_id / case_type / name / description / how_to_attack`）。
- 默认 `confirm_status = pending`，人工确认后才 `confirmed`；全程 `review_log` 留痕
  （复用 `openlaw-confirm` 的判定与审计语义）。
- `article_id` 必须存在且可溯源；关联不到条文的行**如实拒绝并报告**，不静默丢弃。
- 幂等：重复导入同一 `element_id` 更新而非新增；`-dry-run` 默认，`-apply` 才写库。
- 至少打通一个民事案由（建议民间借贷）作为样板，供 OpenCourt 端到端验证。

**验收**：`make test` 通过；导入 → `AnalyzeElements("民间借贷", facts)` 返回非空
`covered` / `missing_elements`；未确认要件在 `Warnings` 中提示待人工确认。

## OL-3 要件名封闭候选集（P1）

**现状**：OpenCourt 阅卷 prompt 让模型自由填 `related_element`（"可选要件名"），
生成的要件名与 `element.name` 对不上，要件匹配形同虚设。

**要求**：sdk 增加 `ListElements(caseType) Result`，返回该案由下**已确认**要件的
`element_id / name` 列表。用途是作为上层抽取时的封闭候选集（模型只能选其一或留空），
不是让上层自由生成。

**验收**：单测（案由不存在 → `not_found`；无已确认要件 → `ok` + 空列表 + warning）；
`docs/api.md` 接口清单同步。

## OL-4 法名规范化与别名解析（P1）

**现状**：`CheckCitation(lawName, ...)` / `ResolveVersion(lawName, ...)` 按法规名精确
查找。文书里大量使用简称（《民法典》↔《中华人民共和国民法典》），解析失败直接
OL-1001，核验可用性差。

**要求**

- 维护法名规范化表（全称 ↔ 简称/别名），来源必须是**语料内可溯源**的信息
  （如 `law.name` + 官方文本中的「以下简称」条款），**不得由模型生成别名**。
- 解析不确定时返回**候选列表**（`Data.candidates[]`）而非单值，让上层并列展示交人工，
  不自动采信。
- 解析成功时在 `Data` 标明 `resolved_from`（命中的是全称还是别名），供 UI 显示依据。

**验收**：表驱动单测（全称、简称、带书名号、错字、多候选）；多候选时不返回单一结果。

## OL-5 程序期限规则查询（P2，v0.3）

**现状**：OpenCourt 的 `DeadlineRule{Name, Days}` 由调用方手填，无出处——程序性规则
（举证期限、上诉期限、送达回证）本质是**法条内容**，属语料层数据。

**要求**：新增期限规则查询能力（表或 `element` 复用均可），每条规则携带
`article_id`（法条出处）、`days`、`warn_days` 建议值、`confirm_status`。
本仓库只提供数据与出处，**不做日期计算与预警**（那是 OpenCourt 的引擎职责）。

**验收**：规则查询返回带 `article_id` 且可 `DetectBreak` 溯源；未确认规则显式标注。

## OL-6 sdk 稳定性承诺（持续）

OpenCourt 通过 `import github.com/futuremeng/OpenLaw/sdk` 进程内调用（开发期
`replace => ../OpenLaw`），这是**编译期耦合**。按 OpenLaw 10.4 版本语义执行：

- 破坏性变更（字段改名/删除、`Result` 结构变化）⇒ 主版本 + 迁移脚本 + 迁移前自动备份；
- 新增能力 ⇒ 次版本；`sdk.Result` / `Status` 五种 / `OL-100x` 错误码视为稳定契约；
- 上层禁止直连 `data/db`，禁止绕过 sdk。

---

## OL-9 清单采集通道内聚（P0，✅ 2026-10-28 完成）

**违例 V5**：本仓库语料管线的清单层曾依赖 OpenCourt 代码（`cmd/govlistcollect` +
`neutral/govlistassist.go` 驱动 BrowserOS）——独立分发时 OpenLaw 无法自行完成
清单采集，与「各自独立分发」前提冲突；且按「谁拥有语料，谁拥有该语料的索引、
检索与语料层校验」，清单作为本仓库语料输入，采集工具本就归属本仓库。

**修法（已落地）**：`browser` 包（最小 MCP Streamable HTTP 客户端 + BrowserOS neo
客户端，端点回环硬校验、默认离线）+ `cmd/openlaw-govlist`（护栏与 OpenCourt 同源，
真机 1000 页验证过的形态）。OpenCourt 侧入口保留为历史通道不动（同一合规画像），
后续是否退役由 OpenCourt 侧自行决定，不在本清单范围。

## OL-10 桌面客户端（P1，规划）

边界前提（OpenCourt `docs/integration-model.md` §1/§6）：三项目**自带管理界面与
客户端**，OpenLaw 界面负责语料导入、人工确认、关系判定、schema 迁移、更新包生成。
现状只有 CLI——5250 条 pending 的人工确认闸口以 CSV 队列流转，是语料管线最大瓶颈。

定案要点：

- **技术栈统一 Wails v2**（对齐 OpenCourt；本仓库纯 Go 无 CGO，进程内直调
  `ingest/confirm/pack` 同一代码路径，桌面端只是 UI 壳，不新增写入逻辑）。
- 「不暴露监听端口」是 **OpenCourt** 的安全模型约束，不约束本仓库：桌面端可选
  内建回环 serve（per-run token + 发现文件，参照 LocalCases 模式）供远程/无头场景，
  默认仍进程内调用，不默认起服务。
- 面板范围（§6 职责）：语料状态（计数/manifest/producer_version/schema/许可）、
  确认闸口 UI（原文留证对照 + 逐条/批量判定 + review_log）、采集/抓取任务面板
  （含 openlaw-govlist / openlaw-govcn 进度与报告）、pack/check 运维视图。
- 浏览器能力复用 OL-9 的 `browser` 包；确认闸口与采集面板都不得绕过 pending 纪律。

**首版落地（2026-10-28）**：`desktop/`（Wails v2.17.0，与 OpenCourt 同版本）。

- 面板一「语料状态」：计数（法规/版本/条文/文档/三态/govcn/govlocal）、按效力等级
  分布、schema_version 与支持范围、manifest 的 producer_version/许可/生成时间。
- 面板二「人工确认」：pending 队列分页 + 过滤；点选看留证正文（file_path 只读）与
  review_log 审计历史；确认/驳回/重置——署名与依据必填，落判复用
  `ingest.Confirmer.Apply` 的 CSV 语义与事务审计，UI 不另开写路径（pending 纪律
  在代码层而非仅文案）。
- 写入路径 = `store.Open`（桌面端是生产者 UI）；消费方仍必须走 `sdk.OpenReadOnly`。
- 构建：`go build -tags desktop,production -o dist/openlaw-desktop ./desktop`；
  桌面壳需 CGO（webview），已内置 `-framework UniformTypeIdentifiers` 链接指令
  （wails v2.17 darwin 生产构建缺该链接）；核心库保持纯 Go。
- 待建：采集/抓取任务面板（govlist/govcn 进度与报告）、pack/check 运维视图、
  可选回环 serve（per-run token）。

**M1 全功能控制面板改造（2026-10-28）**：六面板导航定型（状态/检索/确认 可用，
采集 M2、运维 M3、设置 M3 置灰）。

- 状态面板增强：`desktop/reports.go` 报告分诊——读 incoming/ 既有报告
  （report_*.csv / batch_report.csv / govlist_report_*.json，只读不发明新格式），
  ok/failed/skip 计数 + 失败原因 top5 直方图（awk 分诊套路 Go 化，缺列如实
  malformed）；最近审计流水（review_log）。
- 检索面板：`search.Search` FTS5 原样透出（无重排）+ 法规名/效力等级补全 +
  留证预览复用 + `validate.CheckCitation` 引用核验（五态语义不粉饰）。
- 确认面板增强：风险标记列（provenance inferred_date 等，TTL 5min 缓存）、
  QueueStats 统计条、批量 dry-run 计数（BulkMatch）→ 署名+依据执行（Bulk），
  前端二次确认。
- 前端拆 `api.js`（绑定层）/`app.js`（面板逻辑）；纪律不变：写操作仅
  Judge/BulkApply 且库层强制审计，前端无直连 DB/网络路径。
- 下一期 M2：任务管理器（单槽互斥/取消/进度事件）+ 采集面板（govcn/govlist
  任务化）；M3 运维（check/pack/manifest）。

---

## 明确不在本清单内（不扩散）

- 类案检索、裁判文书语料：归 LocalCases，本仓库不参与。
- 案件管理、事实五态、门禁、脱敏强制关口、审计、推演：归 OpenCourt。
- 相关性重排、语义扩展检索：本仓库与 LocalCases 均不做（语料层保持可复现）。
- 脱敏：OpenCourt 的 `core/redact` 是唯一强制关口，本仓库 `Desensitize` 只是自身语料
  的可选预处理，不承担上层合规责任。
