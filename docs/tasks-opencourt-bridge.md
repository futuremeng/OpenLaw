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
| OL-2 | 构成要件导入 CLI（`openlaw-element`） | **P0** | OpenCourt v0.2 M4 全部（`element` 表当前 **0 行**） |
| OL-1 | 显式 `effect_status` 字段 | **P0** | OpenCourt 停止自行推导法条效力（违例 V1） |
| OL-3 | 要件名封闭候选集查询 | P1 | OpenCourt 阅卷抽取的要件归属（违例 V3） |
| OL-4 | 法名规范化/别名解析 + 候选列表 | P1 | 文书引用核验可用性（简称引用极常见） |
| OL-5 | 程序期限规则查询（带法条引用） | P2 | OpenCourt 程序守护的规则溯源（违例 V4，v0.3） |
| OL-6 | sdk 接口稳定性承诺 | 持续 | OpenCourt 进程内 import 是编译期耦合 |

---

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

## 明确不在本清单内（不扩散）

- 类案检索、裁判文书语料：归 LocalCases，本仓库不参与。
- 案件管理、事实五态、门禁、脱敏强制关口、审计、推演：归 OpenCourt。
- 相关性重排、语义扩展检索：本仓库与 LocalCases 均不做（语料层保持可复现）。
- 脱敏：OpenCourt 的 `core/redact` 是唯一强制关口，本仓库 `Desensitize` 只是自身语料
  的可选预处理，不承担上层合规责任。
