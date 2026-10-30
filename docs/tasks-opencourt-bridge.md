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
| OL-10 | 本仓库桌面客户端（Wails v2，对齐 OpenCourt 技术栈；语料状态/确认闸口 UI/采集任务面板） | P1（✅ 2026-10-28 M1–M3 完成：状态+分诊/检索/确认/采集任务/运维 check·pack/设置） | 「自带管理界面与客户端」的边界前提（integration-model.md §1）当前只有 CLI |

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

**M2 采集任务面板（2026-10-28）**：

- 共享 runner 提取（消除第二写路径）：`ingest.RunGovcnBatch`（清单→抓取→入库→
  CSV 报告逐行落盘，ctx 取消保留已写部分）与 `browser.RunGovListAgent`
  （浏览护栏 + list.json/govlist_report 原子落盘，中断也出 partial 报告）；
  openlaw-govcn / openlaw-govlist CLI 改为薄壳调用同一 runner，
  `RunGovListBrowse` 增加每页 progress 回调（可 nil）。
- `desktop/tasks.go` TaskManager：单槽互斥（运行中第二提交拒绝）、context 取消
  （贯穿到限速点）、状态机 running/done/failed/cancelled、历史环形 20 条、
  每次变化 Wails 事件 "task" 推送前端。
- 采集面板：BrowserOS 通道状态（未配置如实给指引，绝不降级抓取）、清单发现
  （incoming/*/list*.json 下拉）、govcn 导入任务（通道/限速≥2s 钳制/上限/
  dry-run/local）、govlist 浏览任务（分类/页数/起始页续接/间隔≥2000ms/真空页）。
- 测试：TaskManager（互斥/进度/取消/失败详情/环形历史）+ 面板校验
  （非法 source/缺清单/端点未配置拒绝/清单发现排除报告文件）。
- 下一期 M3：运维面板（check/pack/manifest/serve 状态）+ 设置面板。

**M3 运维面板 + 设置面板（2026-10-28）**：六面板全部转正。

- 运维面板（`desktop/ops.go`）：全库体检 = `validate.VerifyManifest` +
  `validate.CheckAllArticles`（openlaw-check 同路径，只读；断链明细前 100 条
  超出如实标注）；manifest 六要素 + 签名状态展示（`LoadManifest`，缺失如实
  给 CLI 指引——当前真实语料即无 manifest，面板如实报告）；
  pack build/verify/import（`pack.Build/Verify/Import` 同路径；build/import
  经任务队列单槽互斥；私钥只经路径引用，内容不进 UI/日志；import 失败保留
  旧数据、成功条目全 pending）。
- 设置面板：语料/schema/产出者只读展示、BrowserOS 通道状态、风险缓存重载。
- 测试：体检无 manifest 如实报告、pack 守门（缺目录/缺私钥/私钥不可读拒绝）、
  风险缓存重载。
- OL-10 主体完成（M1 状态/检索/确认 + M2 采集 + M3 运维/设置）；后续为增强
  （如 serve 状态、更多通道 M4）。
- **UI 改版（2026-10-29，产品名「开源法律法规数据库」）**：面向律师与公众的
  渐进披露双模式。公众模式（默认）：查法规（大搜索框 + 卡片流结果）与按目录
  浏览（类别→法规→条款，修订沿革时间线）；专业模式：库总览（核心数字→行动
  条→覆盖表三层）/法规检索（等级与仅已复核过滤、复制引用格式、引用核对）/
  人工复核/数据采集与更新（渠道状态卡、任务区、采集表单、高级设置折叠）。
  信任层贯穿所有结果与预览（效力等级、复核状态、现行有效/已失效、官方链接、
  pending 警示）。Go 读侧新增 `BrowseLaws/BrowseArticles/LawVersions`、
  Preview/检索补全法规要素与时效、`StatusView.last_fetch`；desktop 层用户
  可见文案全面法言法语化。白话摘要、场景导航、引证追踪、术语提示等依赖内容
  层数据，归 OL-11（当前库无此数据，UI 不渲染假内容）。
- **更新包来源信任门禁（2026-10-29）**：`Verify` 用包内 pubkey.hex 验签属自签自验，
  只能证明内容完整、不能证明签署者。新增 `pack/trust.go`：内置受信发布者名单
  （key ID → 公钥 hex）、`RegisterTrustedPubkey`（发版内置/下游装配/测试）、
  `TrustOf`、`VerifyTrusted`。`Import` 默认走 `VerifyTrusted`——完整性通过但来源
  未受信 = 拒绝且零写入；`ImportAllowUntrusted` 与 CLI `-allow-untrusted` 仅供本地
  自验；`openlaw-pack keygen` 生成密钥对并打印待内置公钥（私钥 0600，仓库外）。
  verify 与桌面校验输出来源信任结论。测试：未受信拒绝且零写入、登记后成功、
  本地通道、名单守门（非法 hex/重复 key ID）。
- **AGENTS.md（2026-10-29）**：根目录新增 AI 代理与协作者约束条文（诚实展示、
  共享 runner、写路径从严、复核署名、离线限速、签名信任、验证门禁、法言法语术语）。
- **绑定契约门禁（2026-10-29）**：修复 RecentReports 参数个数漂移导致库总览整段
  空白（Wails 运行期拒绝调用 + 前端单段 try/catch = 一处失败全屏空白）；新增
  `desktop/binding_test.go` 扫描 app.js 全部 `app().Method(...)` 调用核对实参个数
  与 main.App 导出方法一致——前端↔Go 绑定漂移自此在 `go test` 阶段暴露。

- **地点定位 · 第 1+2 段（2026-10-29）**：`geo` 包（内置 34 省级 + 346 地级行政区与
  近似中心点，离线就近判定，精度边界在注释与测试中如实声明）+ `parse.InferRegion`
  （名称前缀保守推导：省级/地级 + 「××经济特区」别名，判不出留空）；桌面端
  `RegionOptions/CurrentRegion/SetRegion/LocateFromCoords/BrowseStats` 与
  `BrowseLaws(+region)`，侧栏地区栏（定位 / 省·市下拉 / 不限）、浏览页「仅本机地区」
  与未标注计数，法规行显示推导地域；地区偏好存 `meta`。真实语料实测：law 可判
  24,033/28,952（83.0%），地方类 24,030/25,009（96.1%），其余如实显示「地域未标注」。
  **下一段（未做，写路径按高风险变更处理）**：地点标引——`law.region_code`
  （schema v4 加法式 + 迁移前自动备份）、`openlaw-region` CLI（默认试采报告 →
  `-apply` 写库留痕规则号）、桌面「地域标引」面板（BulkPreview 干跑计数 → 人工落判
  署名）；自治县、直辖县级、区级等层级另行对齐。全文检索按地域过滤同样未做
  （FTS5 结果不重排序，只能做过滤，需单独对齐口径）。

- **地点定位 · 第 3 段（2026-10-29，macOS 真实定位链路）**：定位被拒的根因逐条由系统
  日志查实共四处——①裸二进制无 bundle 身份（TCC 不认）；②`.app` 未注册进
  LaunchServices（locationd「not a plugin or an app」+「identity unverified」→ 不弹窗）；
  ③绑定调用占住主线程时 `CLLocationManager` 排不到主队列（Go 侧阻塞轮询会饿死弹窗与
  回调）；④Wails v2.17 的 WKWebView 无定位授权回调，系统侧已 `AuthorizedAlways` 而
  `navigator.geolocation` 仍 `PERMISSION_DENIED`。
  已补：`packaging/macos/`（.app + 定位用途声明 + ad-hoc 签名 + `lsregister -f` 注册 +
  自检，`BUNDLE_ID` 可覆盖以免实测包与正式包共用身份）；`desktop/location_darwin.m`
  的申请授权与单次定位均在主队列发起、结果缓存由前端轮询（`RequestLocationPermission`
  / `LocationPermissionStatus` / `StartLocationFix` / `LocationFixStatus`）；坐标改走原生
  CoreLocation，webview 位置接口只作非 macOS 平台后备。语料目录改为
  `-data` > `OPENLAW_DATA` > 随程序位置 > 当前目录（`dataresolve.go`），来源在日志与
  「库总览」显示——防 `.app` 从访达启动时静默打开空语料库。
  实测已过：CWD=`/` 启动 .app → 目录来源「随程序位置」、`lsof` 打开真实库、bundle id 与
  ad-hoc 签名有效；用户点「允许」后系统侧状态到 `AuthorizedAlways`（日志证实）。
  门禁：`bundle_test.go`（plist 键 / 声明文案如实性 / 可执行名一致性 / 签名自检 /
  shell 中文插值 / bundle id 可覆盖）+ 定位状态映射与非阻塞契约测试。
  **实测已过（2026-10-29）**：点「获取我的位置」→ 允许 → 原生 `didUpdateLocations` 回传坐标
  （精度约 76 m）→ 界面「已判定：北京市（按最近行政中心判定，距中心约 17 km，请核对）」；
  `meta` 记 `user_region=110100`、`user_region_source=auto`、时间戳与日志吻合。

- **地域标引写入路径（F3）· 设计检查点（2026-10-29，待批准）**：方案已成文
  [docs/design-region-write.md](design-region-write.md)——schema v4 加法式（`law.region_code`
  等 5 列 + law 级 `region_log` 审计表）、机器建议与人工落判边界、`openlaw-region` 默认试采
  （dry-run）+ `-apply` + `-undo`、桌面复核面板、浏览过滤由标题前缀切到区划码、包格式扩展。
  检查过程中查实一处缺口：`release-process.md` 承诺「数据模型变更先自动备份 db」，但代码中
  无实现（全仓 grep `backup` 仅命中私钥提示）——故实施第 1 步先补自动备份（`VACUUM INTO`，
  失败即中止迁移），这一步独立有价值。待拍板项见该文档 §9。
- **F3 第 1 步 · 已实施（2026-10-29）**：`SchemaVersion` 3→4——`law` 加法式 5 列
  （`region_code`/`region_source`/`region_rule`/`region_reviewer`/`region_reviewed_at`）+
  law 级审计表 `region_log` + 3 索引；新增 `store/backup.go`：结构迁移前 `VACUUM INTO`
  自动快照到 `data/backups/`，**备份失败即中止迁移**（该缺口此前只有文档承诺、无实现）。
  `SchemaMin` 保持 3（只读方不被迫升级）。真实语料 v3→v4 已实跑核对：快照
  `openlaw-v3-20261029T121439Z.db`（2.0 GB，`schema_version=3`、无地域列）与升级后库逐表计数
  完全一致（law 28,952 / document 36,109 / article 1,367,161 / import_log 55,930 /
  validate_log 36,115，FTS 1,367,161）；`region_code` 已标注 0 条——本步只让列就位，不写值。
  备份 + 迁移 1 分钟内完成，备份落盘后结构变更 1 秒内完成（加法式 `ALTER` 只改元数据）。
  测试：`store/backup_test.go` + `TestMigrateV3ToV4`。**下一步**：`parse/regioninfer.go`
  （R1–R5）与 `openlaw-region` 试采报告（默认 dry-run，人工看过报告再谈 `-apply`）。
- **F3 第 2、3 步 · 已实施（2026-10-29，第 3 步止于试采）**：写侧建议层 `parse.SuggestRegion`
  （读侧 `InferRegion` 未改，界面显示不受影响）+ 新函数 `geo.MatchContained`（机关名命中，
  与 `MatchPrefix` 共用一份匹配实现）。口径：名称前缀 R1/R2 = 高置信可写；机关名 R3 = 中置信，
  **只进报告不写库**；名称与机关互不隶属 = 冲突转人工；判不出留空。库层 runner
  `ingest.RegionMarker`（`Scan` 只读 / `Apply` 只写高置信且未标注、`region_reviewer` 一律留空、
  逐笔 `region_log` + `batch_id` / `Undo` 按批次撤销且不回滚人工落判）+ CLI `openlaw-region`。
  真实语料 28,952 条 dry-run：**高置信 24,033**（与既有名称前缀基线 24,033/28,952=83.0% 逐条一致，
  读写同口径）、中置信 90、冲突 0、无法判定 4,829；试采后 `region_code` 仍 0 条、`region_log`
  仍 0 行。如实记录：`law.issuer` 仅 5,214/28,952（18%）有值，R3 能补的量有限；名称省略「市」字
  （如「兰州经济技术开发区条例」）仍判不出，留空不猜。测试：`parse.TestSuggestRegion`、
  `geo.TestMatchContained`、`ingest/region_test.go`。
  **`-apply` 已执行**（人工核对报告后）：写入前先取快照
  `data/backups/openlaw-v4-preapply-20261029T151600Z.db`；首次写入 24,033 条后一致性自检抓出
  误标——「台湾渔船停泊点边防治安管理办法」（公安部，部门规章）按名称前缀被判 710000 台湾省；
  据此加库层护栏 `nationalEffectLevels`（国家级效力不按名称标地域），**整批撤销
  `region-20261029T151705Z`（24,033 条）后重跑为 `region-20261029T155448Z`（24,032 条）**，
  `region_log` 保留 suggest+undo 完整痕迹。最终：已标注 24,032（83.0%）| 中置信 90 | 冲突 0 |
  国家级不按名称标 2,820 | 无法判定 2,010；逐表计数与快照一致，`region_reviewer` 全空（待复核）。
  另记录语料缺陷：`is_local=0` 却有地域 3,612 条，`is_local` 不可靠，只报告不当护栏。
  **界面尚未切换**：浏览过滤仍走标题前缀，切区划码属第 5 步。
  测试另加 `TestRegionNationalEffectGuard`。
  **第 4 步已落地（2026-10-29）· 桌面「地域标引」复核面板**：
- 库层落判（`ingest/regionreview.go`）：`Review`（confirm 确认/改选、reject 驳回、clear 清除，
  复核人与结论依据必填，写 `region_log(action=…, source='manual', actor=复核人)`）、
  `BulkReviewMatch` 干跑 + `BulkReview` 执行（**执行必须带回预览数**，数变了即中止；
  单次上限 500 条）、`ReviewStats`（待复核/已确认/已驳回留空/未标注）、`ReviewLog`、
  `SuggestOne`。`Scan` 的分类抽成 `classify`，全量与单条共用同一口径。
- **补上一条写路径漏洞**：人工驳回后 `region_code` 留空但署名仍在，原 `Apply` 会把机器建议
  重新填回去、把人工结论顶掉。现 `Apply` 的 WHERE 增加 `region_reviewer IS NULL`，
  `Scan` 把这类行分类为 `rejected` 不再当待判定。测试 `TestRegionRejectedNotRewrittenByMachine`。
- 桌面绑定（`desktop/regionreview.go`）：`RegionReviewStats` / `RegionReviewList` /
  `RegionReviewDetail` / `JudgeRegion` / `RegionBulkPreview` / `RegionBulkApply`，
  全部经 `ingest.RegionMarker`（桌面不是第二写路径）；纳入绑定契约门禁（42 方法 / 51 处调用）。
- 前端面板（专业模式「地域标引」）：统计条 + 队列（机器建议优先）+ 预览（当前标注 / 机器判定 /
  判定依据 / 复核记录）+ 确认·改选·驳回·清除 + 按省批量（先干跑后执行）。状态一律用
  「机器建议 · 待复核」「地域未标注」「已人工复核」，未标注显示「缺失/未知」。
  开发中抓到一处会把未标注显示成已标注的错（详情用机器建议码填 `region_code`），已修并有测试。
- 测试：`ingest/regionreview_test.go`、`desktop/regionreview_test.go`；门禁全过
  （gofmt/build/vet/test、`node --check`、生产与 devtools 桌面构建）。
  **第 5 步之一已落地（2026-10-30）· 浏览过滤切区划码**：
- 过滤：已标注行按 `region_code` 前缀（省级取 2 位、天然含下辖地级；地级取 4 位），
  未标注行回退名称前缀；**已标注行不再回退名称**——码优先，人工落判不被名称推翻
  （代价写入测试：标题写「石家庄市…」而码为 130000 的条目，按市过滤不再命中）。
- 真实语料只读逐省核对（34 个省级单位）：旧口径 24,033 = 新口径 24,033，净增 0、净失 0；
  仅按码 24,032。差的 1 条正是护栏故意不写码的「台湾渔船停泊点边防治安管理办法」（部门规章），
  只有名称回退口径查得到它。如实结论：现有码全部由名称前缀写出，切码当下**等价**；
  价值在人工改选之后——过滤随复核走，且不再依赖名称。
- 显示：`LawRow` 增 `region_code`/`region_source`，前端按来源区别显示——已标注显示
  「××（机器建议 · 待复核 / 已人工复核 / 随更新包携带 · 待复核）」，未标注显示
  「地域未标注（名称推定：××）」，码不在内置区划表显示「区划码 …（不在内置区划表）」。
  `BrowseStats` 的未标注数改按库中码统计（与过滤子句同一口径），统计条如实说明两类匹配各覆盖多少。
- 测试：`desktop/region_test.go` 种子扩到 LR1–LR9（已落码但标题无地区名 / 未落码但标题有地区名 /
  人工落判省级 / 码名不一致 / 码不在表内），断言按码命中、回退命中、码优先于名称、
  `unknown` 码显示、未标注数按码统计。
  **第 5 步之二已落地（2026-10-30）· 更新包携带地域**：
- `changes.csv` v3 表头增 `region_code`/`region_source`；v0/v1/v2 旧表头照常读取，
  缺的列一律留空（= 上游未标注，不用默认值伪造）；陌生表头仍拒绝。
- 导入按 §9.4 只写码：`region_source` 一律改写为 `pack`、`region_reviewer` 留空
  → 接收方界面显示「随包携带 · 待复核」；上游来源（auto/manual）只进 `region_log.reason`，
  `batch_id`=`pack:<generated_at>` 便于按包追溯。守卫：已有码或已人工落判的行不被包推翻
  （既不写码也不留审计）。
- 前端同步：队列徽章区分「机器建议 · 待复核」与「随包携带 · 待复核」，预览显示标注来源。
- 测试：`ingest.TestImportRegionCarried`（署名留空、审计内容、无地域导入不动列、
  人工结论不被包顶掉）、`pack.TestImportPackRegion`（往返 + 来源改写 + 审计）、
  `pack.TestReadChangesLegacyHeaders`（v0–v3 四代表头 + 陌生表头拒绝）。
  **第 5 步之三已落地（2026-10-30）· §6 一致性自检报告**：
- 库层 `ingest.RegionMarker.Audit()`（`ingest/regionaudit.go`）只读扫描全库矛盾项，逐项计数 +
  逐条列出；CLI `openlaw-region -check [-check-report <csv>] [-json]` 出报告，绝不写库、
  绝不自动改数——矛盾项交人工在桌面「地域标引」面板落判。
- 真实语料实测（只读；跑完 `region_log` 仍 72,098 条、库文件未变）：28,952 部中地方类未标地域
  **979**（地方性法规 877 + 地方政府规章 102）、国家级效力却标地域 **0**（R0 护栏有效）、
  效力等级未标注却标地域 **2**（两条「台湾省出席全国人大代表协商选举方案」）、`is_local=1`
  未标地域 877、效力等级属地方级但 `is_local` 未标记 3,711、已标地域但无地方效力依据 2；
  共 4,590 条逐条出 CSV。
- 口径按实测修正（如实记录）：设计稿原写的「`is_local=0` 却有地域」命中 3,611 条，其中 3,609 条
  是地方政府规章——它们本就是地方级，只是 `is_local` 从未置位；照原口径报等于把 `is_local`
  自己的毛病算成地域的毛病，故拆成「效力等级属地方级但 is_local 未标记」与「已标地域但无地方
  效力依据」两条。`is_local` 只作线索，绝不据此改数。
- 测试：`ingest/regionaudit_test.go`（逐项计数、地方政府规章不得虚报为「无地方效力依据」、
  自检前后数据指纹一致 = 只读）。
  **第 5 步三块全部完成。**
  **复核动作已对齐 CLI（2026-10-30）**：`openlaw-region` 增 `-stats`（复核面统计，与面板同口径）、
  `-review <条目编号[,编号…]> -verdict confirm|reject|clear [-code] -reviewer -reason`（逐条落判）、
  `-bulk [-province <码前缀>]`（缺省只计数，执行必须 `-apply -expected <计数>`，计数变了即中止）——
  全部走 `ingest.RegionMarker` 同一入口，桌面与 CLI 都不是第二条写路径；批量只挑「机器建议 · 待复核」，
  人工已落判的行绝不批量推翻。读侧口径同时修正：`region_reviewer` 空串与 NULL 同义，没有署名的行
  不得算「已人工复核」（`ingest.SQLReviewerSet`/`SQLReviewerBlank` 统一统计、队列与批量范围）。
  真实语料实测空署名 0 条，属防御性口径统一，现有计数不变。
  真实语料只读实测：待复核 24,032 / 已人工确认 0 / 已复核留空 0 / 未标注且未复核 4,920；
  按 `13` 前缀计数 870 条待流转（未写入，跑完 `region_log` 仍 72,098 条）。测试：
  `cmd/openlaw-region/review_test.go`、`ingest.TestRegionReviewBlankReviewerIsNotReviewed`、
  `desktop.TestRegionReviewBlankReviewerNotReviewed`。
  **下一步**：面板在打包 `.app` 里的手工冒烟；多法域（美国数据）设计文档。
- **界面语言与渐进披露（2026-10-30，规范成文 + 三页重做）**：新增 `docs/design-ui-language.md`——
  三条硬规则（目标语言、渐进披露、诚实展示）、主视图禁止清单（LIKE 通配符、命令行参数与环境变量名、
  URL/端口、ISO UTC 时间戳、schema 版本号与数据目录路径）、术语替换表与三个页面的信息层级；
  AGENTS.md 引用该规范（违反属设计缺陷）。据此重做三页：
  ① 库总览：行动条改「查看待复核条目」「获取最新法规」，语料目录/启动参数/schema 版本收进
  「技术信息」折叠，获取记录显示本机时间并把 `成功 0` 如实写成「本轮没有新增内容」；
  ② 人工复核：三步层级（找到条目 → 看原文与复核记录 → 署名落判），批量条件改真实来源名下拉
  （新增读侧 `main.App.ConfirmSources()`；`ingest.BulkFilter` 新增 `Source` 精确匹配，
  桌面端不再走 LIKE），未先干跑不得执行、干跑 0 条不得执行；
  ③ 获取最新法规（原「数据采集与更新」）：导航与标题改目标语言，常用参数在前，
  限速/翻页/续接参数进折叠并写明护栏下限（≥2 秒、≥2000 毫秒不得调低），
  BrowserOS 指引拆成白话 `browser.Guidance` 与技术写法 `browser.GuidanceTech`
  （后者只在「技术细节」折叠显示，`BrowserStatus` 新增 `technical` 字段）。
  测试：`browser.TestGuidancePlainLanguage`（指引不得含端点地址/参数名/环境变量名）、
  `ingest.TestConfirmBulkSourceExact`（来源名精确匹配、通配符按字面命中 0）、
  `desktop.TestBrowserStatusAndGovcnLists` 增断言 `technical` 非空。
  ④ 收尾：地域标引页措辞同步（「批量落判」→「批量处理」等）；产品自称与内部说法清理——
  窗口标题与打包显示名统一为「开源法律法规数据库」（开发期旧称退场，新增 `desktop/title_test.go`
  门禁断言两者一致且用户可见文件无旧称），`store`/`sdk`/CLI 提示里的「语料」改
  「法规库数据 / 数据包」，`docs/design-ui-language.md` 新增「产品自称」条。
- **界面重构（2026-10-30，按 `docs/design-ui-language.md` 重做视觉与结构）**：样式层重写为
  设计系统（语义令牌 + 深浅两套主题 + 手动外观切换）；每个面板加页头（处境一句话 + 主行动）；
  侧栏导航挂实时计数（待复核 `#cnt-confirm`、机器建议 `#cnt-region`、运行任务 `#dot-collect`）；
  库总览法规构成改堆叠占比条（精确数字进折叠）；地域标引页按「先选条目 → 再署名结论」重排，
  批量处理进折叠。新增 `desktop/ui_contract_test.go` 四条门禁（类名有样式、面板有页头、
  技术字样只在 `<details>` 内、深色令牌存在）与无浏览器接线冒烟
  `desktop/frontend/wiring_smoke.mjs`（`TestFrontendWiringSmoke` 调用）；`id` 与绑定调用未变（117 个 id 仍全部核对）。
  `docs/design-ui-language.md` 新增「界面结构基线」节。**下一步**：打包 `.app` 内逐页手工冒烟
  （深浅主题、占比条、侧栏计数），以及多法域（美国数据）设计文档。
- **筛选归位（2026-10-30，地区从侧栏挪回筛选层）**：侧栏地区控件被指出「莫名其妙」——
  地区是定位法规的筛选维度，且只服务公众便利，专业用户需要的是多维度指标。据此：
  ① 侧栏不再放筛选控件，只留一行状态；② 「按目录浏览」→「按条件筛选」，两模式共用；
  ③ Go 层新增 `LawFilter` + `BrowseLawList`/`BrowseFacets`（分面计数走 SQL，口径「其余维度
  已生效、本维度不限」，列表与计数共用 `browseWhere`），旧签名委托新方法；④ 前端筛选栏
  芯片挂计数、0 计数禁点、当前筛选回显白话、关键词即时联动；地区作为其中一个维度
  （芯片选省、下拉到地级、「按我的位置」直接套用）；⑤ 检索页对命中就地收窄
  （等级/复核状态，计数按命中统计并声明不重排序）；首页类别卡成为带筛选的入口。
  新增测试 `TestBrowseFacetsLinkage`/`TestBrowseFacetsRegionAndSource`，门禁新增
  `TestBrowseFilterRailIsPrimary`，接线冒烟加 5 项联动断言并修掉「绑定桩丢弃实参」这一
  使联动从未真正被验证的桩缺陷。**下一步**：打包 `.app` 内对筛选栏逐维度手工冒烟
  （窄窗口下三栏布局是否需改为筛选栏可折叠），以及多法域（美国数据）设计文档。


---

## 明确不在本清单内（不扩散）

- 类案检索、裁判文书语料：归 LocalCases，本仓库不参与。
- 案件管理、事实五态、门禁、脱敏强制关口、审计、推演：归 OpenCourt。
- 相关性重排、语义扩展检索：本仓库与 LocalCases 均不做（语料层保持可复现）。
- 脱敏：OpenCourt 的 `core/redact` 是唯一强制关口，本仓库 `Desensitize` 只是自身语料
  的可选预处理，不承担上层合规责任。
