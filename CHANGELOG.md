# 更新日志

本项目遵循 Semantic Versioning：主版本=数据模型或 SDK 接口不兼容变更（必须附迁移脚本，迁移前自动备份）；次版本=新增能力；修订版本=修复。详见 docs/release-process.md。

## Unreleased（v0.1.0 开发中）

- **界面语言与渐进披露规范（成文）**：新增 `docs/design-ui-language.md`——用户可见文案的
  三条硬规则（目标语言、渐进披露、诚实展示）、主视图禁止清单（SQL/LIKE 通配符、命令行参数
  与环境变量名、URL/端口、ISO UTC 时间戳、schema 版本号与数据目录路径）、术语替换表
  （去复核→查看待复核条目、采集与更新→获取最新法规、抓取→获取、（未标注）→效力等级未标注、
  `成功 0 · 失败 N`→如实说明本轮没有新增内容）与三个页面的信息层级要求。AGENTS.md 增加引用：
  违反它属于设计缺陷，不是文案偏好。据此重做「库总览」「人工复核」「数据采集与更新」三页
  （见后续条目）。
- **界面规范落地 · 库总览页**：按 `docs/design-ui-language.md` 重做。行动条由「36,109 条新条目
  等待人工复核 / 去复核 → / 采集与更新 →」改为处境 + 目标动作：「法规库中有 36,109 条内容
  尚未经人工复核，引用前请以官方原文为准」+「查看待复核条目」「获取最新法规」；统计卡
  「待人工复核」→「待复核」；「覆盖情况（按效力等级）」→「法规构成（按效力等级）」；
  效力等级空值「（未标注）」→「效力等级未标注」（Go 侧四处 SQL 默认值同步）；
  「最近采集记录」→「最近获取记录」，ISO UTC 时间戳改为本机时间显示（原始 UTC 留在 `title`
  供核对），`成功 0 · 失败 1,361` 如实渲染为「本轮没有新增内容 · 失败 1,361 条」；
  两处空态给出下一步指引；「语料目录：data（来源：命令行 -data）」与 schema 版本号移出主视图，
  收进页尾「技术信息（数据存放位置 · 数据格式版本）」折叠；侧栏底部「数据库版本 4」改为
  「本机法规库 · 28,952 部」（启动即取，不再只在进过总览后才显示）。

- **地点定位 · 第 1 段（离线区划层与名称推导）**：新增 `geo` 包——内置 34 个省级
  与 346 个地级行政区（地级市/自治州/盟/地区/直辖县级市）及其**近似中心点**
  （按公开资料人工整理，可能偏差 10~30 km，非官方测绘成果；代码注释与测试如实
  声明精度边界）。`Nearest(lat,lng)` 纯离线就近判定并返回中心点距离作为置信提示
  （不联网、不做 IP 定位）；`MatchPrefix` 名称前缀最长匹配，同级并列取上级。
  新增 `parse.InferRegion`：按法规名称保守推导地域（省级/地级两条规则，判定不出
  一律留空，绝不猜）。测试：区划码与名称唯一性、父级完整性、坐标合理范围、
  就近判定（上海/湘西/喀什/大兴安岭等）、非法坐标如实拒绝、名称推导不误判
  国家层面法规与县级市。

- **地区试判定（定位被拒时的可用通道）**：macOS 裸二进制（无 `Info.plist` 定位声明）
  下 webview 一律回「已拒绝授权」，实测确认。新增 `main.App.LocateFromRegion(code)`：
  把选定行政区的中心点当作位置，跑**与真实定位同一条**离线就近判定链路，来源记为
  `trial` 并在说明中写明「试判定 · 非真实定位」，与 `auto`（真实坐标）严格可区分、
  不得混同；未选地点或未知区划码如实拒绝且不改动已有地区。侧栏加「按选定地点试判定」
  按钮与说明文字。测试：来源标记、说明文案含「非真实位置」、省级试判定落到下辖地级
  （证明同一判定链路）、失败不改偏好、`auto` 与 `trial` 不混同。
  另修前端一处自造函数引用（`reloadViewing` 未定义）——绑定与 DOM 门禁都覆盖不到
  这类错误，已补实现并逐个核对该模块引用的标识符。

- **地点定位 · 第 2 段（桌面接线）**：`main.App` 新增 `RegionOptions`（省→市两级
  列表）、`CurrentRegion`、`SetRegion`（手动选择/取消，非法码拒绝）、
  `LocateFromCoords`（浏览器经纬度 → 本机离线就近判定，误差距离如实回传）、
  `BrowseStats`（命中数 + 其中「地域未标注」数）；`BrowseLaws` 增加 `region` 参数
  （选省级时自动含下辖地级与「××经济特区」别名，可复现的名称前缀过滤，非重排）。
  侧栏新增地区栏（获取我的位置 / 省·市下拉 / 不限地区），浏览页新增「仅本机地区」
  开关与统计行，法规行显示推导地域或「地域未标注」。偏好存 `meta`（经 Go 读写）。
  补「××经济特区」命名别名规则后，真实语料实测覆盖率：law 24,033/28,952（83.0%）、
  地方类 24,030/25,009（96.1%）——其余如实显示未标注。macOS 裸二进制下 webview
  可能拒绝定位授权，此时提示走手动选择（见 docs/deploy.md §9）。
  **尚未做**：全文检索按地域过滤；地点标引写路径（`law.region_code` + 人工落判）。

- **地点定位 · 第 3 段（macOS 真实定位：.app 打包 + 授权 shim + 语料目录解析）**：
  实测确认裸二进制下 webview 一律回「已拒绝授权」，且 Wails v2.17 的 darwin 前端
  不含任何定位授权回调（模块内 grep `CLLocation` 无命中）——光加 plist 不够。补齐两块：
  ①`packaging/macos/`：`Info.plist`（`NSLocationWhenInUseUsageDescription` 等定位用途
  声明，文案如实写明不联网/不上传）+ `build-app.sh`（组装 bundle、ad-hoc 签名、
  `plutil -lint` 与 `codesign --verify` 自检；`TAGS=` 可产出带控制台的实测变体）；
  ②`desktop/location_darwin.m` + `main.App.RequestLocationPermission`：仅在用户点
  「获取我的位置」时申请 WhenInUse 授权（弹窗异步，最多轮询 10 秒），把「已授权/
  已拒绝/受限/未应答/缺 bundle 声明/平台不支持」逐项回传原因与出路，绝不谎报授权。
  新增 `desktop/dataresolve.go`：语料目录按 `-data` > 环境变量 `OPENLAW_DATA` > 随程序
  位置 > 当前目录解析，来源写入启动日志与「库总览」——`.app` 从访达启动时工作目录是
  `/`，不解析就会静默新建空语料库（违反诚实展示纪律）。实测：CWD=`/` 下启动 .app，
  日志显示「语料目录：…（来源：随程序位置）」，`lsof` 确认打开的是真实语料库；
  LaunchServices 身份 `com.github.futuremeng.openlaw`，ad-hoc 签名满足 Designated
  Requirement。**待人工确认**：系统弹窗点「允许」后 webview 是否回传坐标（机器无法代答）。
  新增门禁 `desktop/bundle_test.go`：plist 必备键、用途声明须含「不联网/不上传」、
  `CFBundleExecutable` 与脚本复制的可执行名一致、脚本必须签名自检；
  `TestShellVarFollowedByCJK` 抓「`$VAR` 后紧跟中文」——实测踩过（`$TAGS；` 被 bash
  并入变量名，`set -u` 下脚本直接退出）。另如实记录：CoreLocation/Foundation 带入重复
  `-lobjc`，链接期有无害提示，压制需 `CGO_LDFLAGS_ALLOW`（与「构建不依赖外部环境变量」
  冲突），故保留。
- **修复（定位链路：三处根因逐条由系统日志查实，非推测）**：
  ① `.app` 未注册进 LaunchServices——locationd 报「The given bundleId or bundlePath
  is not a plugin or an app」+「identity unverified」，身份解析不了就不弹授权窗；
  `build-app.sh` 现执行 `lsregister -f` 注册产物。
  ② 绑定调用里不得阻塞——绑定路径占住主线程，而 `CLLocationManager` 只能在主队列
  工作，在 Go 侧 sleep 轮询等弹窗会把主队列饿死（弹窗与回调都不出现）。改为
  「发起即返回 + 前端轮询」，新增只读绑定 `LocationPermissionStatus`。
  ③ **WKWebView 不转交坐标**——Wails v2.17 未实现
  `webView:requestGeolocationPermissionForFrame:decisionHandler:`：日志显示系统侧已
  `AuthorizedAlways`，前端 `navigator.geolocation` 仍收到 `PERMISSION_DENIED`。
  因此坐标改由原生 CoreLocation 取得（`location_darwin.m` 单次定位 + 状态缓存，
  新增绑定 `StartLocationFix` / `LocationFixStatus`），webview 位置接口降级为
  非 macOS 平台的后备路径。
  另：实测包与正式包不得共用 bundle id（实测 `lsregister -dump` 同一 id 命中两个
  `.app`，TCC 身份随之含糊），`build-app.sh` 现支持 `BUNDLE_ID` 覆盖并写回 plist，
  已门禁化。
- **数据模型 v4 · 地域标引第 1 步（列 + 迁移前自动备份）**
  **原因**：F3 需要可复核的法规地域标识；而 `docs/release-process.md` 承诺的「数据模型变更先
  自动备份 db」此前**只有承诺没有实现**（全仓 grep `backup` 仅命中私钥提示）——先补机制再改结构。
  **影响范围**：`store`——`SchemaVersion` 3→4；`law` 加法式 5 列（`region_code`/`region_source`/
  `region_rule`/`region_reviewer`/`region_reviewed_at`，刻意不加 CHECK、不重建表）+ 新表
  `region_log`（law 级审计，与 doc 级 `review_log` 并列不复用）+ 3 个索引；新增 `store/backup.go`：
  `Open` 写路径在结构迁移前 `VACUUM INTO` 落 `data/backups/openlaw-v<旧版>-<UTC>.db`，
  **备份失败即中止迁移**（原库不动）；版本一致或空库不产备份。`SchemaMin` 保持 `"3"`：
  只读消费方读 v3 语料不被迫升级，缺地域列即显示「地域未标注」。
  真实语料已升级并核对：28,952 法规 / 36,109 文档 / 1,367,161 条文 / 55,930 导入日志 /
  36,115 校验日志逐表计数与迁移前快照**完全一致**，FTS 1,367,161 条完好；`region_code`
  已标注 0 条（本步只让列就位，不写任何值）。2.0 GB 库实测：备份 + 迁移 1 分钟内完成，
  备份落盘后结构变更 1 秒内完成（加法式 `ALTER` 只改元数据）。
  **回滚方式**：停应用 → 用 `data/backups/openlaw-v3-20261029T121439Z.db` 替换 `db/openlaw.db`
  （已验证该快照 `schema_version=3`、无 `region_code` 列、无 `region_log` 表、28,952 法规可读）。
  测试：`store/backup_test.go`（备份确为迁移前快照 / 备份目录不可写时中止迁移且原库分毫未动 /
  未迁移不产备份 / 路径含单引号仍安全 / 转义单元断言）与 `TestMigrateV3ToV4`（旧数据保留、
  新列可写且机器建议不带署名、`region_log` 外键与 action 枚举生效、重复打开幂等）。
- **文档（地域标引写入路径 F3 设计检查点）**：新增 `docs/design-region-write.md`（状态：待批准）
  ——schema v4 加法式列 + law 级 `region_log` 审计表、机器建议与人工落判边界、`openlaw-region`
  默认试采 + `-apply` + `-undo`、桌面复核面板、浏览过滤从标题前缀切到区划码、包格式扩展、
  验收门禁与实施顺序。检查时查实一处缺口：`docs/release-process.md` 承诺「数据模型变更先
  自动备份 db」而代码无实现（全仓 grep `backup` 仅命中私钥提示）——列为实施第 1 步（独立有价值）。
  覆盖率基线一并写入验收标准：现行名称前缀法 24,033/28,952（83.0%）。
- **地域标引 · 第 2 步（写侧建议层）**：`parse.SuggestRegion` 在既有读侧 `InferRegion`
  （行为不变，界面显示仍走它）之上给出可写/不可写判定——名称前缀 R1 省级、R2 地级
  （含「深圳经济特区」等别名，`geo` 已内置）算**高置信**；新增 R3 发布机关名命中
  （新函数 `geo.MatchContained`）只算**中置信**，按拍板只进报告不写库；名称与机关指向
  **互不隶属**的两个行政区算**冲突**（同链如「石家庄市…」+「河北省人大常委会」不算冲突），
  转人工；判不出一律留空。`geo.MatchPrefix` 与 `MatchContained` 共用一份匹配实现，
  口径不会分叉。测试：`geo.TestMatchContained`、`parse.TestSuggestRegion`（冲突/中置信/
  直辖市路径名不重复/依据文案非空）。
- **地域标引 · 第 3 步（库层 runner + CLI）**：新增 `ingest.RegionMarker`（`Scan` 只读分类 /
  `Apply` 只写高置信且 `region_code` 为空、`region_reviewer` 一律留空=机器建议 · 待复核、
  逐笔写 `region_log` 并带 `batch_id` / `Undo` 按批次撤销且**不回滚人工落判过的行**，
  默认也是先干跑计数）与 CLI `openlaw-region`（默认 dry-run 出全量分类 CSV，含已标注行）。
  真实语料 28,952 条试采：高置信 **24,033**（与既有名称前缀基线 24,033 逐条一致，读写同口径）、
  中置信 90、冲突 0、无法判定 4,829；试采后 `region_code` 仍 0 条、`region_log` 仍 0 行（只读为实）。
  如实记录两点：`law.issuer` 仅 5,214/28,952（18%）有值，故 R3 通道能补的量有限；
  「兰州经济技术开发区条例」这类名称省略「市」字的仍判不出（留空，不猜）。
  **尚未 `-apply`**：按设计须人工核对报告后才谈写入。
  测试：`ingest/region_test.go`（扫描不改库、只写高置信、审计 actor/batch_id、
  重复执行写 0 条、撤销先干跑后执行、人工落判不被回滚）。
- **地域标引 · 第 3 步之写入（2026-10-29，人工核对报告后 `-apply`）**：写入前先取快照
  `data/backups/openlaw-v4-preapply-20261029T151600Z.db`（`integrity_check=ok`、v4、28,952 法规、
  `region_code` 0 条）。首次写入 24,033 条后逐表计数与基线完全一致（law 28,952 / document 36,109 /
  law_version 36,109 / article 1,367,161 / import_log 55,930 / validate_log 36,115 / FTS 1,367,161），
  审计 24,033 条全为 `suggest`/`actor=openlaw-region`、`from_code` 全空、署名全空。
  **一致性自检抓出一条误标**：「台湾渔船停泊点边防治安管理办法」（issuer=公安部，
  `effect_level=部门规章`）按名称前缀被判 710000 台湾省——名称里的地名不等于法规的地域归属。
  据此在库层加护栏 `nationalEffectLevels`（宪法/法律/行政法规/监察法规/部门规章/司法解释/欧盟三类
  不按名称标地域；「其他」不在列内，因其既可能国家级也可能地方级），并**整批撤销后按新口径重跑**：
  批次 `region-20261029T151705Z` 撤销 24,033 条 → 新批次 `region-20261029T155448Z` 写入 **24,032** 条。
  最终分类：高置信 24,032（83.0%）| 中置信 90 | 冲突 0 | **国家级不按名称标 2,820** |
  无法判定 2,010。护栏只从「高置信」移走 1 条（即误标那条），其余 2,819 条原属「无法判定」——
  于是「留空 4,920」被如实拆成「本就不该有地域 2,820」与「真的判不出 2,010」。
  另如实记录语料缺陷：`is_local=0` 却有地域的 3,612 条（如「山东省地震群测群防办法」是地方政府规章），
  `is_local` 本身不可靠，只报告、不当护栏用。
  测试：`TestRegionNationalEffectGuard`（部门规章不误标、「其他」不被否决）。
- **地域标引 · 第 4 步（桌面「地域标引」复核面板）**：把已写入的 24,032 条机器建议接上人工闭环。
  库层新增落判层 `ingest/regionreview.go`：`Review`（`confirm` 确认或改选地区 / `reject` 驳回 /
  `clear` 清除标注；复核人与结论依据在库层强制，写 `region_log(action=…, source='manual',
  actor=复核人)`）、`BulkReviewMatch` 干跑 + `BulkReview` 执行（**执行必须带回预览数**，
  预览与执行之间队列有变即中止；单次上限 500 条，首版只做逐条 + 小批）、
  `ReviewStats`（待复核 / 已确认 / 已驳回留空 / 未标注）、`ReviewLog`、`SuggestOne`；
  `Scan` 的分类抽成 `classify`，全量扫描与单条预览共用同一口径。
  **顺带修一条写路径漏洞**：人工驳回后 `region_code` 留空但署名仍在，原 `Apply` 会把机器建议
  重新填回去、把人工结论顶掉——现 `Apply` 的 WHERE 增加 `region_reviewer IS NULL`，
  `Scan` 把这类行分类为 `rejected`（不再当待判定）。
  桌面新增 `desktop/regionreview.go` 六个绑定（`RegionReviewStats` / `RegionReviewList` /
  `RegionReviewDetail` / `JudgeRegion` / `RegionBulkPreview` / `RegionBulkApply`），一律经
  `ingest.RegionMarker`——桌面不是第二条写路径；纳入绑定契约门禁（42 方法 / 51 处调用核对通过）。
  前端专业模式加「地域标引」面板：统计条、队列（机器建议优先）、预览（当前标注 / 机器判定 /
  判定依据 / 复核记录）、确认·改选·驳回·清除、按省批量（先干跑后执行）。状态一律用
  「机器建议 · 待复核」「地域未标注」「已人工复核」，未标注显示「缺失/未知」。
  开发中抓到一处诚实性错误：详情用机器建议码填 `region_code`，会把未标注条目显示成已标注——
  现 `region_code` 一律取库中实际值。测试：`ingest/regionreview_test.go`（署名强制、
  驳回不被机器顶回、改选留痕、批量预览数与上限）、`desktop/regionreview_test.go`
  （队列排序与状态口径、详情不得虚标、按省批量与预览数不符即中止）。
- **地域标引 · 第 5 步之一（浏览过滤切区划码）**：桌面浏览的地域过滤从「仅名称前缀」改为
  **已标注行按 `law.region_code` 前缀 + 未标注行回退名称前缀**（省级取 2 位、天然含下辖地级；
  地级取 4 位）。已标注行**不再回退名称**——码优先，人工落判不被名称推翻；代价如实写进测试
  （标题写「石家庄市…」而人工落判为省级 130000 的条目，按市过滤不再命中）。
  真实语料只读逐省核对（34 个省级单位）：旧口径 24,033 = 新口径 24,033，**净增 0、净失 0**，
  仅按码 24,032；差的 1 条正是护栏故意不写码的「台湾渔船停泊点边防治安管理办法」（部门规章），
  只有名称回退口径查得到它。如实说明：现有码本就由名称前缀口径写出，故切码当下命中集合不变，
  价值在人工改选之后（过滤随复核走）——不得声称切码即提升覆盖。
  显示以库中值为准：`LawRow` 新增 `region_code`/`region_source`，前端按来源区别显示
  「××（机器建议 · 待复核 / 已人工复核 / 随更新包携带 · 待复核）」「地域未标注（名称推定：××）」
  「区划码 …（不在内置区划表）」；`BrowseStats` 的未标注数改按库中码统计（与过滤子句同口径），
  统计条如实说明「这些按标题前缀匹配，其余按区划码匹配」。
  测试：`desktop/region_test.go` 种子扩至 LR1–LR9，断言按码命中、未标注回退、码优先于名称、
  `unknown` 码显示、未标注数按码统计。
- **地域标引 · 第 5 步之二（更新包携带地域）**：`changes.csv` 升 v3 表头，增
  `region_code`/`region_source` 两列；v0/v1/v2 旧包照常读取，缺的列一律留空（留空就是
  「上游未标注」，不得用默认值伪造），陌生表头仍拒绝。导入按 §9.4 只写码：
  `region_source` **一律改写为 `pack`**、`region_reviewer` 一律留空——接收方界面显示
  「随包携带 · 待复核」，不得显示成已人工复核；上游来源只写进 `region_log.reason`，
  `batch_id` 记 `pack:<generated_at>` 便于按包追溯。守卫：已有码或已人工落判的行不被包推翻
  （既不写码也不留审计）。前端队列徽章区分「机器建议 · 待复核」与「随包携带 · 待复核」，
  预览显示标注来源。顺带更正 `release-process.md` 里「待补：changes.csv」的过期说法。
  测试：`ingest.TestImportRegionCarried`、`pack.TestImportPackRegion`、
  `pack.TestReadChangesLegacyHeaders`。
- **地域标引 · 第 5 步之三（§6 一致性自检报告）**：新增库层只读 `ingest.RegionMarker.Audit()`
  与 CLI `openlaw-region -check [-check-report <csv>] [-json]`——逐项计数 + 逐条 CSV，
  **只报告、不自动改**：矛盾项一律交人工在桌面「地域标引」面板落判。真实语料实测（只读，
  跑完 `region_log` 仍 72,098 条、库文件未变）：28,952 部中矛盾项 4,590 条——地方类法规未标地域
  979（地方性法规 877 + 地方政府规章 102）、国家级效力却标地域 0（R0 护栏有效）、
  效力等级未标注却标地域 2（两条「台湾省出席全国人大代表协商选举方案」，效力等级为空）、
  `is_local=1` 未标地域 877、效力等级属地方级但 `is_local` 未标记 3,711、
  已标地域但无地方效力依据 2。口径按实测修正并如实记录：设计稿原写的「`is_local=0` 却有地域」
  命中 3,611 条，其中 3,609 条是地方政府规章——它们本就是地方级，只是 `is_local` 从未置位，
  照原口径报会把 `is_local` 自己的毛病算成地域的毛病，故拆成两条。`is_local` 只作线索、不改数。
  测试：`ingest/regionaudit_test.go`（逐项计数、地方政府规章不得虚报、自检前后指纹一致 = 只读）。
  至此设计 §8 的第 5 步三块（浏览过滤切码 / `pack` 列扩展 / 一致性自检）全部完成。
- **地域标引 · 复核动作对齐 CLI**：`openlaw-region` 新增 `-stats`（复核面统计，与桌面面板同口径）、
  `-review <条目编号[,编号…]> -verdict confirm|reject|clear [-code <区划码>] -reviewer <复核人>
  -reason <结论依据>`（逐条落判）、`-bulk [-province <码前缀>]`（批量落判：缺省只计数，
  执行必须 `-apply -expected <上一步计数>`，计数变了即中止）。全部走 `ingest.RegionMarker`
  同一入口——桌面与 CLI 都不是第二条写路径；批量只挑「机器建议 · 待复核」，人工已落判的行绝不被推翻。
  顺带修正读侧口径：`region_reviewer` 空串与 NULL 同义，没有署名的行不得算「已人工复核」
  （新增 `ingest.SQLReviewerSet`/`SQLReviewerBlank`，统计、复核队列、批量范围统一使用）。
  真实语料实测空署名行 0 条：这条是防御性口径修正，现有数据计数不变，不谎称修掉了脏数据。
  真实语料只读实测：待复核 24,032 / 已人工确认 0 / 已复核留空 0 / 未标注且未复核 4,920；
  按 `13`（河北省）前缀计数 870 条待流转（未写入，跑完 `region_log` 仍 72,098 条）。
  测试：`cmd/openlaw-region/review_test.go`（未计数即拒绝执行、计数不符即中止、人工落判行不被
  批量推翻、缺复核人即拒绝、驳回落空但已复核并留复核记录）+
  `ingest.TestRegionReviewBlankReviewerIsNotReviewed` + `desktop.TestRegionReviewBlankReviewerNotReviewed`。
- **修复（直辖市路径重复）**：`geo.PathName` 对省市同名的直辖市会拼成「北京市 · 北京市」
  （实测界面上就这么显示）。连续同名只保留一次，现为「北京市」；`geo_test.go` 覆盖
  110000/110100/120100/310100/500100。
- **诚实措辞修正（定位）**：坐标来自系统定位服务（macOS 可能经由其 Wi-Fi 定位数据库），
  因此 plist 用途声明、界面提示与 README 一律不再声称「不联网」，改为如实说明坐标来源、
  不上传、不做 IP 定位、判定在本机离线完成；`TestMacOSBundleContract` 现禁止「不联网」措辞。

- **修复（库总览面板空白）+ 前端绑定契约门禁**：前端 `app().RecentReports(5)` 与
  Go 侧 `RecentReports()` 实参个数不符，Wails 运行期拒绝该调用
  （`received 1 arguments to method 'main.App.RecentReports', expected 0`），
  而面板加载是一整段 try/catch——结果整块「库总览」落入 catch，只剩「读取失败」
  一行，覆盖表/采集记录/复核记录全部空白。`RecentReports(limit)` 现按面板展示条数
  取跨来源最近 limit 份（limit ≤0 取默认 5，>50 钳制）；新增 `desktop/binding_test.go`
  契约门禁：扫描 app.js 中全部 `app().Method(...)` 调用，逐一核对实参个数与
  `main.App` 导出方法一致（调用不存在的方法同样报错），把这类编译期无感的前端
  漂移变成测试期失败。测试：limit 语义（跨来源按修改时间取最近）、默认值与钳制、
  无 incoming 目录如实返回空、门禁当前覆盖 31 个方法 / 40 处调用。
  同一文件再加 **DOM 元素契约门禁**：app.js 引用的每个 `$("#id")` 必须在
  index.html（或 app.js 自己插入的模板）中声明——缺失元素与参数漂移是同一类故障
  （`$()` 返回 null → 整段面板落入 catch 而空白）；已用临时探针确认该门禁真的会
  失败，不是摆设。当前覆盖 91 个元素 id。

- **AGENTS.md（仓库内 AI 代理与协作者约束）**：根目录新增条文，把既有纪律提炼为
  可执行约束——诚实展示（缺失/中断如实标注，禁假数据）、CLI 与桌面端共用同一库层
  runner、写路径从严并验证旧数据真实链路、复核强制署名 + 批量先干跑、默认离线与
  限速护栏不可绕过、签名私钥仓库外 + 导入默认来源信任、提交前验证门禁清单、
  用户可见文案法言法语。

- **来源信任门禁（借鉴 edgeever 内置公钥模型）**：更新包导入
  默认要求签名公钥命中内置受信发布者名单（`pack/trust.go`：名单 + key ID
  轮换 + `VerifyTrusted`/`TrustOf`）；完整性通过但来源未受信 = 拒绝导入且
  零写入；`ImportAllowUntrusted` 与 CLI `-allow-untrusted` 仅供本地自验；
  `openlaw-pack keygen` 生成密钥对并打印待内置公钥；verify/桌面校验输出
  来源信任结论。测试：未受信拒绝且零写入、登记后成功、本地通道、名单守门。

- **语料台 UI 改版（OL-10 UI 阶段，产品名「开源法律法规数据库」）**：渐进披露
  双模式——公众模式（默认）：大搜索框首页 + 覆盖卡片 + 卡片流结果 + 类别→法规→
  条款层级浏览（修订沿革时间线）；专业模式：库总览（三层信息架构）/法规检索
  （等级过滤、仅已复核、复制引用格式、引用核对）/人工复核/数据采集与更新
  （渠道状态卡 + 任务区 + 采集表单 + 折叠高级设置）。信任层贯穿：每条结果与
  预览恒显效力等级、复核状态、现行有效/已失效、官方原文链接，pending 条目
  醒目警示；全部术语法言法语化（复核人/结论依据/来源提示/试采等）。
  Go 读侧新增 BrowseLaws/BrowseArticles/LawVersions + Preview/检索补全法规
  要素与时效 + StatusView.last_fetch。关于区块与免责说明内置。测试：浏览/
  沿革/信任字段。

- **语料台 M3（OL-10 运维面板 + 设置面板，六面板全部转正）**：全库体检
  （validate.VerifyManifest + CheckAllArticles，openlaw-check 同路径只读，
  断链明细前 100 条如实标注）；manifest 六要素 + 签名状态（缺失如实报告并给
  CLI 指引）；离线更新包 build/verify/import（pack.* 同路径，build/import
  经单槽任务队列，私钥只经路径引用，import 失败保留旧数据、新条目全 pending）;
  设置面板（语料/schema/通道状态只读 + 风险缓存重载）。测试：体检如实、
  pack 守门、缓存重载。OL-10 M1–M3 主体完成。

- **语料台 M2（OL-10 采集任务面板）**：新增**任务管理器**（单槽互斥、context
  取消贯穿限速点、状态机 + 历史环形 20 条、Wails 事件推进度）。采集面板：
  BrowserOS 通道状态、清单发现（incoming/*/list*.json）、govcn 详情导入任务
  （通道/限速≥2s/上限/dry-run/local）、govlist 清单浏览任务（分类/页数/
  起始页续接/间隔≥2000ms）。共享 runner 提取到库层——`ingest.RunGovcnBatch`
  与 `browser.RunGovListAgent`（中断保留已采 + partial 报告），两个采集 CLI
  改为薄壳调用同一 runner（桌面端不是第二写路径）；`RunGovListBrowse`
  增加每页进度回调。测试：互斥/取消/失败详情/环形历史 + 面板离线守门。

- **语料台 M1（OL-10 全功能控制面板第一期）**：六面板导航（状态/检索/确认 可用，
  采集/运维/设置 置灰待建）。状态面板新增**报告分诊**（读 incoming/ 既有
  report_*.csv/batch_report.csv/govlist_report_*.json：ok/failed/skip 计数 +
  失败原因 top5 直方图，缺列如实标注）与**审计流水**；新增**条文检索面板**
  （FTS5 原样透出 + 法规名/等级补全 + 留证预览 + validate 引用核验五态如实）;
  确认面板增强（风险标记列 TTL 缓存、统计条、批量 dry-run→执行，署名+依据
  库层强制）。前端拆 api.js/app.js。测试：报告分诊（合成 CSV/JSON/坏格式容错）+
  面板方法（临时语料：检索 join/风险标记/统计/批量 dry-run/落判审计/引用核验）。

- **边界纪律进代码（OL-7/OL-8）**：新增 `sdk.OpenReadOnly`（消费方唯一合法入口：
  `_pragma=query_only(1)`、不跑迁移、不写 meta；不用 `mode=ro`——WAL 只读仍需
  -shm/-wal 可写）。版本协商：`Open`/`OpenReadOnly` 先只读 `meta.schema_version`，
  高于 `SchemaMax` 一律拒开且**绝不回写降级**（隐患 H2 根除）；只读入口另设
  `SchemaMin` 下限（消费方不代跑迁移，指引升级语料）。导出 `SchemaMin/SchemaMax`；
  `manifest.json` 新增 `producer_version`/`schema_version`（Ed25519 签名覆盖，旧包
  缺字段按来源未知处理）。错误文案给出两条出路。测试：v99 拒开且标记不动、
  只读不迁移（v2 语料只读报错、写入路径正常升级）、query_only 挡 INSERT、空库拒开。
- **桌面客户端首版（OL-10）：`desktop/`「OpenLaw 语料台」**（Wails v2.17.0 对齐
  OpenCourt；仅此壳需 CGO，核心库仍纯 Go；内置 `-framework UniformTypeIdentifiers`
  补齐 wails v2.17 darwin 生产构建链接缺口，构建标签 `desktop,production` 已写入
  源码注释）。面板：语料状态（计数/效力分布/schema/producer_version/许可）+
  人工确认闸口 UI（pending 队列/过滤、留证正文与 review_log 对照、确认/驳回/重置
  必须署名+依据，落判复用 `ingest.Confirmer.Apply` 同一审计路径，不新增写路径）。
  5250 条 pending 的确认瓶颈从 CSV 流转升级为界面流转。

- **浏览器能力内聚（OL-9）：清单采集通道落位本仓库**。新增 `browser` 包：自实现
  最小 MCP Streamable HTTP 客户端（initialize/tools/list/tools/call，SSE 帧与
  会话头处理；**端点回环硬校验**——非 127.0.0.1/localhost/::1 直接拒绝）+
  BrowserOS neo 客户端（懒握手、最小工具面校验 tabs/read/grep/evaluate、
  UNTRUSTED_PAGE_CONTENT 防注入包裹剥离、默认离线：端点未配置调用即报错给指引，
  绝不降级其他抓取）。新增 `cmd/openlaw-govlist`：国家规章库清单人在环代理浏览
  （官方列表页唯一起点、跳转控件等价人工跳页并核验选中页码、≥2s 限速、≤200 页/
  会话、URL 硬校验 http(s)+.gov.cn 族+.htm(l)/.shtml、cat 按发布机关推断、
  产物 list_agent_<ts>.json + sha256 报告落盘）。护栏与 OpenCourt
  neutral/govlistassist.go 同源（真机 1000 页验证过的形态），按「谁拥有语料谁
  拥有采集」边界落位——本仓库独立分发不再依赖 OpenCourt 完成清单层。
  测试：fake 浏览器跳转驱动多页/去重/分类过滤/起始页续接/end_of_list/限速下限
  （虚拟时钟）/伪装域过滤 + MCP 回环拒绝 + httptest 握手全链路。

- **地方政府子域抓取通道（-local）**：`openlaw-govcn -source gzk -local` 放行
  混合清单 2629 条地方政府规章直链（*.gov.cn 子域）抓取。主机策略后缀族匹配
  （防伪装域 gov.cn.evil.example）、重定向逐跳复查、**每站 robots 独立强制
  fail-closed**（真机已有站点 Disallow: / 被正确拒抓）、限速/UA/import_log/
  留证纪律与主站通道全同；doc_id=`govlocal_<url_hash>`、留证独立
  `incoming/govlocal/`、source_type 标注地方政府官网；正文容器真机扩展
  zfxxgk_content（政务公开模板）/wz_content/gzkwz_con（规章库模板；RE2 无
  负向前瞻——首轮 MustCompile panic 教训，精确 class 备选防误配外层
  container）；清单层 `LoadGovcnList(path, allowLocal)` 默认关闭（地方直链
  维持 skip_bad_url 如实交回，显式 -local 才抓——产品决策显式化）；测试：
  hostAllowed 表驱动（含伪装域拒绝）+ 清单双模式 + httptest govlocal 端到端。
  真机三轮终态（2629 条地方直链）：**1305 条入库 pending**（留证
  `incoming/govlocal/`）；解析失败 745（JS 渲染骨架/长尾模板转人工）；
  robots 拒绝 374（DNS 失效、412/413 反爬、Disallow: /——fail-closed，
  重试证实确定性拒绝）；单页多规章冲突 87、HTTP 错误 85、其他 70、
  skip_no_date 46；容器两轮扩展使入库 554→1305（留证复用离线重解析，
  不重复联网）。
- **gzk 清单全量真机采集落地 + skip_bad_url 导入语义**：OpenCourt 跳转驱动批量采集（`cmd/govlistcollect` + 缺口补采）覆盖官方清单全部 1000 页（真机探测证实第 1000 页为末页、每页 7 条），http 过滤 bug 修复后 5 分片合并去重共 **6667 条**（部门规章 1594 / 地方政府规章 5073；www.gov.cn 4026 条走本通道，2641 条地方政府直链转人工）；`LoadGovcnList` 不再对不合规 URL 整单拒绝——如实交回并在导入报告记 `skip_bad_url`（保留原 URL 供人工处置，不静默丢弃、不越主机策略抓取）；主站 `http://` 详情页 URL 归一化 `https://`（去重前，两形态并一条；真机证实官方清单 docurl 大量 http 形态，301→https）；新增 `scripts/govmerge.py`（分片合并去重 + 区段覆盖分析）；dry-run 小样本验证：主站条目解析正常、地方政府直链全部正确记 skip_bad_url。
  真实导入完成（2026-10-27）：主站 4026 条 → **3945 条入库**（部门规章 1522 +
  地方政府规章 2423，均 pending 待人工确认）；超时失败经幂等重试全部恢复；
  终态 failed 72 = 60 真版本冲突（同一规章官网双 URL，canonical 版本已入库，
  裁定为重复无需补录）+ 11 单页多规章（官网一页合并发布多部规章、各有"第一条"
  → UNIQUE 冲突如实回滚转人工）+ 1 正文形态异常（29 字页面转人工核查）；
  另 skip_no_date 11 条留证转人工

- **人在环代理浏览模式真机校准**：规章库默认列表为混合清单（无分类页签）——cat 按发布机关逐条推断（与本仓库 `gvgzResolveLevel` 同规则）；翻页为官方分页按钮 DOM 点击；清单 URL 校验放宽至 `.gov.cn` 域名族并如实记录地方政府规章直链（本通道主机锁 www.gov.cn，此类条目报告记 skip_bad_url 转人工，独立通道与否另行评估）；文档边界已同步（docs/sources-govcn.md）

- **清单层新增「人在环代理浏览」模式（gov.cn 两库通用）**：OpenCourt 内置智能体经 BrowserOS neo 驱动用户真实浏览器在官方列表页翻页浏览收集清单（起点 URL 白名单、官方 UI「下一页」两段式点击、限速 ≥2s/页、单会话 ≤200 页、URL 硬校验、审计），产物 list.json 与本仓库清单契约完全一致——合规定位与「人工浏览+导出」等价（主站 robots 允许 /zhengce/）；仍绝不程序化访问 sousuo/sousuoht 检索域、绝不逆向 Athena 鉴权；flk 等硬禁止站不适用（其 robots 禁止一切自动化）。OpenLaw 侧零代码改动，文档边界已更新（README 来源合规边界、docs/sources-govcn.md）

- **国家行政法规库来源（gov.cn xzfgk，新源）**：两段式合规接入——清单由浏览器脚本 `tools/govcn-xzfgk-export.user.js` 人工导出（sousuo 检索域 robots `Disallow: /`，不得程序发现），`openlaw-govcn` CLI 按清单抓详情页（主站 robots 允许；主机锁 www.gov.cn、限速 ≥2s、UA 如实、import_log、原始 HTML 留证 incoming/govcn/）；TRS 正文容器解析（div 深度配对）→ Markdown → 落款日期保守推断（InferDates 优先、清单 pubtime 兜底、无则转人工）→ effect_level=行政法规、pending 人工闸口、AutoExpirePrior 版本链、无条文结构走全文即一节打标（修改/废止决定防误拆 WholeTextForce）；doc_id=govcn_<content_id> 幂等续跑。合规画像与用法见 docs/sources-govcn.md
- **国家规章库来源（gov.cn gjgzk，同一通道扩展）**：`openlaw-govcn -source gzk` + 浏览器脚本 `tools/govgz-export.user.js`（规章库 SPA 结果 DOM 导出：标题/详情 URL/分类页签 cat/发布机关 puborg；其 Athena 检索接口带 RSA appKey 鉴权——绝不逆向）。effect_level=部门规章/地方政府规章（清单分类直取，缺分类按发布机关保守归类，皆缺 skip_no_meta 转人工）、issuer=清单发布机关；详情页 URL 放宽至任意 gov.cn 路径（公报页可挂），留证分离 incoming/govgz/；清单标题优先于公报页脏 <title>。实网验证：《网络反不正当竞争暂行规定》（市场监管总局令第91号）公报页抓取入库，落款推断 pub=2024-05-06/eff=2024-09-01、43 条正确切分

- **语料 manifest 许可声明（v2 字段）**：`manifest.json` 新增机器可读 `license`/`attribution`（DATA-LICENSE.md 的签名内形态，默认措辞与 pack 更新包统一为 `validate.DefaultLicense/DefaultAttribution` 单一出处）；`openlaw-check -build-manifest` 支持 `-license/-attribution` 覆盖。字段受 Ed25519 签名覆盖——重新打包换许可即验签失败。EU 语料重使用署名义务（© European Union）自此随 core 包分发闭环；OpenCourt 状态面板透出「数据许可」

- **部署面（make build / build-all + docs/deploy.md）**：OpenLaw 部署形态明确为「CLI 单文件 + 可迁移数据目录 + 进程内 SDK」（无常驻服务/端口）。Makefile 新增 `build`（当前平台 dist/ 11 个 CLI）与 `build-all`（darwin/linux/windows 交叉编译——纯 Go modernc sqlite，CGO_ENABLED=0 即开即用）。docs/deploy.md：三种使用形态（独立 CLI / OpenCourt 内嵌——最终用户不需要 OpenLaw 二进制 / 库模式）、数据目录整体迁移、装后验证、签名私钥与 manifest 首次部署流程（未生成 manifest 时 UNVERIFIED 是闸口正常表现）、日常运维与备份、签名更新包分发、与 OpenCourt 配置对接

- **SDK 检索面（Search + GetArticle，OpenCourt M4 对接）**：`Search` 暴露 search/ FTS5+LIKE 能力并补版本上下文——每条命中携带 law_name/version 区间/expire_date/confirm_status/lang/snippet（上层据此映射生效状态：expire 非空→AMENDED，绝不自动判 REPEALED）；中文多词空格分词走多词 AND（unicode61 不切 CJK，FTS 短语必失败的实网修复）；零命中 ok+warning 不是错误；pending 命中附 12.3 提示。`GetArticle` 按 article_id 取全文详情 + trace；断链 OL-1006 数据照常完整返回供人工核对。docs/api.md 同步。OpenCourt 侧 `plugins/law_openlaw.go` 实现 LawSourcePlugin（进程内 sdk，禁直连 DB；go.mod replace 本地联调；config openlaw.data_dir + OPENCOURT_OPENLAW_DATA_DIR；未配置 nil 降级；断链命中透传 UNVERIFIED），真实语料端到端验证通过

- **RL-2 配额探测（openlaw-eu-text -probe + Cellar.Probe）**：EU 站点按客户端指纹分桶限流，curl 探到 200 不代表 Go 桶可用——探测必须同指纹链。`Probe` 走与正式抓取完全相同的 policy/Transport/UA/robots 缓存：robots.txt + 一次 expression RDF GET（robots 拒绝则不发起，合规省配额），结构化判定窗口开/限流/输入错误，结论写 import_log `[cellar-probe]`；退出码 0=窗口开 / 3=限流中 / 2=其他，任务编排可直接判。附带 `HTTPStatusErr` typed 错误（getCellar 非 200 携带状态码——RL-3 退避判据）与 robots 缓存 `LastStatus` 状态暴露

- **EU M1 代码 review 修复**：①`openlaw-eu-text` dry-run 不再多发 FetchWork 元数据请求（省 Cellar 配额，RL 纪律）；②`SplitArticlesEU` 补条号递增过滤（与中文通道交叉引用误拆同型防御；前向引用局限显式钉测试）；③`Cellar.policy()` 惰性缓存——TLS Transport 全生命周期复用（原每请求新建，连接池不复用）；④`FetchText` item 下载失败原因收集入最终错误；⑤文档 parser_version 统一为实际值 `openlaw-parse/0.1.0+formex-eu`（原 formex-eu-v1 系设计稿旧命名）

- **TLS 指纹兼容修复**：Go 1.24+ 默认 ClientHello 带 X25519MLKEM768 后量子 key share，JA3 指纹异常被 EU 出版局前置 Azure Front Door WAF 按机器人持续 429（连 robots.txt 都取不到，curl 同 IP 却 200）；`FetchPolicy.Transport` + Cellar 默认限定 TLS 曲线偏好（X25519/P-256）修复——UA 仍如实标识，不冒充浏览器
- **EU 全文通道 M1（openlaw-eu-text + Cellar.FetchText + parse.ParseFormexACT）**：推翻「EU 全文只能人工」旧结论——Cellar representation 层四步链（expression→manifestation→item）全程无 WAF 可程序化下载全文；FORMEX ACT XML 条文边界来自 `<ARTICLE IDENTIFIER>` 结构元素（GDPR 实测 99 条），零正则零猜测；`Importer` 支持预置 Articles+Lang+ParserVersion（document.lang/law_version.lang 落库，parser_version=openlaw-parse/0.1.0+formex-eu 可追溯），`SplitArticlesEU`（英文 Article N）供 XHTML 回退与 pack 回灌，pack changes.csv +lang 列（v0/v1/v2 表头向后兼容）+xml 原始层；confirm 仍 pending 人工闸口。实网发现：expression URI 语言后缀大写（.ENG）；publications.europa.eu→op.europa.eu 域名迁移（重定向白名单锁双域）；op.europa.eu 429 限流极严（fail-closed 长退避）。限流/配额后续工作独立为任务清单 docs/tasks-eu-rate-limit.md（RL-1 pilot 重试 / RL-2 配额探测 / RL-3 抓取退避 / RL-4 批量配额规划 / RL-5 限流报表 / RL-6 指纹巡检）
- **效力等级补标（parse.InferEffectLevel + openlaw-level）**：flk 存量 effect_level 全空（zip 名无分类）——高置信名称规则（两高→司法解释、国务院开头→行政法规、全国人大/常委会开头→法律、34 省级全称开头/自治州县旗/经济特区/`^1-7字市`开头→地方性法规、「中华人民共和国X法」精确结尾→法律、宪法开头→宪法），无法判定一律留空绝不猜；默认 dry-run 出报告，`-apply` 写库 + `law.remark` 留痕规则号 + 按名称置 is_local，人工 confirm 可纠正。实跑 22,616/23,739（95.3%）补标，留空 1,123 转人工。附带：**schema v3**（effect_level CHECK 增「宪法」，v2 库自动 12 步重建升级）；存量 `law.name` 文件名产物尾巴清理（3,854 条）+ batch 通道源头 TrimRight
- **人工确认流程（openlaw-confirm + review_log 审计）**：pending 语料的抽样→复核→落判→审计闭环。`stats`（进度与风险面，风险标记取自 provenance 持久事实：日期推断/全文即一节/缺公布日）、`sample`（复核清单 CSV：高风险优先+效力等级轮转，附 sha256/原文路径/正文首尾摘要供与官方页比对，可直接填 verdict/reviewer/note 回填）、`apply`（清单落判：署名必填、未填行跳过、单行失败不中断、幂等）、`bulk`（条件批量：默认 dry-run，执行必须 -yes+署名+依据，起始状态按裁决限定——confirm/reject 从 pending、reset 从 confirmed/rejected）。一切流转写 review_log（谁/何时/依据什么）并同步 provenance JSON confirm_status；绝不允许无审计痕迹的确认。实跑风险面：日期推断 5,162、全文即一节 1,451 强制进队列最前。附带性能修复：`article(doc_id)`/`document(confirm_status)` 索引（SQLite 不为外键自动建索引，复核清单相关子查询曾全扫 118 万行）
- **全文即一节通道（whole-text ingest）**：无「第X条」结构的文档（决定/决议/公告/名单类）以单条 article 承载全文——`article_no="全文"`（约定值）、`parser_version` 带 `+whole` 后缀、import_log 留痕；可全文检索但语义为整份文档而非条文，条文级功能（引用校验/要件分析）按后缀整体排除（docs/data-dictionary.md 已定义）。`ingest.Request.WholeTextMode`（单文件导入保持严格默认关闭）；pack changes.csv 增 `whole` 列（旧格式向后兼容）；batch 报告状态 `ok_whole`/`ok_whole_inferred`。实跑：1,451 个决定/决议类入库，批量通道 fail 清零（仅剩 2 个同名同日不同文本真冲突转人工）
- **日期推断通道（parse.InferDates + batch 兜底）**：文件名无日期段的存量（flk `名称_.docx`）从正文落款保守推断——生效日=日期后紧跟「(起)施行/生效/执行」、公布日=窗口内「通过/批准/公布/发布/签署/修订/修正」，均取文末命中；无命中仍转人工绝不猜。推断写溯源 `effective_date_inferred=true`（批量通道日期一律 inferred，含文件名日期），报告状态 `ok_inferred`。实跑：4,560 个无日期存量 → 3,711 推断入库，82 仍转人工
- **pack docx 层闭环测试**：manifest 收录/包内复制/verify/导入空目录按 docx 提取入库全链路单测
- **docx 导入路线（parse/docx + importer 泛化 + cmd/openlaw-batch）**：flk 存量主格式为 .docx——`parse.ExtractDocxText`（zip 容器 `word/document.xml` 提取，实体解码）；importer 按扩展名分流（docx/pdf/txt 自动提取，.doc 遗留格式报错转 `textutil -convert docx`），原始层按真实格式落位 `<kind>/<doc_id>.<ext>`（manifest/pack 同步支持 docx/txt 层）；**条号递增序列过滤**修复交叉引用误拆（「违反本规定第七条」不再成为条文起点——实网 UNIQUE 冲突根因）；**孤儿层恢复**（失败运行回滚遗留的落盘文件，库内无行时允许覆盖重跑并留痕）；**版本链自动接续**（AutoExpirePrior：同法开放旧版自动闭合到新版生效日、回填历史版本自动设 expire；同生效日真冲突仍拒绝——批量专用，单文件导入保持严格）。`openlaw-batch -report <scan_report.csv>` 三层去重（报告内 sha 重复/库内同 sha/doc_id 冲突），逐条失败不中断，产出 batch_report.csv。全量实跑：31,615 存量 + 5,927 个 .doc 经 `textutil` 离线转换产物 → **25,697 文档 / 1,042,266 条文入库**（版本链闭合 7,063 个历史版本；转人工=1,291 无条文标记的决定/决议类 + 3,936 文件名无日期 + 73 docx 无文本）。**区间前移重链**（AutoExpirePrior）：既有闭合区间跨过待导入生效日（中间版分批到达，如 .doc 与 .docx 分两批）→ 旧版 expire 自动前移到新版生效日并留痕 import_log，消解 536 个分批到达版本冲突；同生效日真冲突仍拒绝
- **存量盘点（cmd/openlaw-scan）**：批量下载 zip 盘点与分类——GBK 文件名解码（flk 等中文站点 zip 条目名非 UTF-8）、可选解压（防路径穿越、防覆盖）、逐文件 SHA-256（导入时复用判篡改）+ 重复检测、文本可用性探测（docx 取 `word/document.xml` 的 `<w:t>` 正文；pdf 走文本层提取；扫描/空/未识别分类计数）；产出 CSV 报告（zip,entry,out,ext,size,sha256,text,dup_of）供人工审阅与后续批量导入。盘点层与导入层分离：不写 SQLite。支持顶层散文件盘点（转换产物目录等，zip 列=`(散文件)`，out 即原路径）。实网存量实测：317 个 flk 批量下载 zip（848MB，.docx 为主）
- **欧盟法律元数据接入（ingest/cellar + cmd/openlaw-eu）**：欧盟出版局 Cellar SPARQL（官方开放程序化入口，robots `Allow: /`，与 flk 的「禁止自动化」性质相反）——CELEX → work named graph → ELI/CELEX/类型/生效日+适用日（欧盟特有双日期）/官方公报出处/**显式废止与依据关系**；`openlaw-eu -celex 32016R0679` 一条命令完成 law 主表 upsert + 关系挂载（仅挂已入库目标法，一律 pending 人工确认；目标未入库计数跳过、可重跑补齐）；原始三元组 JSON 落 `data/incoming/eu_<celex>_work.json` 留证，每次 SPARQL 访问进 import_log（默认 2s 礼貌限速；fail-closed）。数据模型 v2：`law +eli/celex`、`effect_level +欧盟条例/指令/决定`、`law_version +application_date/lang`、`document +lang`、`relation_type +依据`——v1 库自动原地迁移（重建表保数据）。全文文本不在本通道：EUR-Lex 网页有 CloudFront WAF 挑战（等同 flk 只能人工保存），已实网核实（docs/sources-eu.md）
- **更新包共享合规（pack）**：manifest 新增 `license/attribution` 字段（随 Ed25519 签名保护，verify 无需改动）——欧盟等来源要求重使用署名；`openlaw-pack build -license/-attribution` 可覆写，默认「公有领域+署名出处」声明
- **跨源一致性核验（compare/ + cmd/openlaw-compare）**：同一法律在不同官方来源的双重复核——字节级（SHA-256，仅作记录）+ 文本级（去全部空白、Kangxi 部首 214 码位折叠、TRS 正文容器 `UCAP-CONTENT` 提取）；判定 bytes_identical / identical / contained（单行法 ⊂ 整期）/ covered（句段全覆盖，容忍分页页眉页码插入噪声）/ different（给出首个差异位点、上下文与未命中句段样例）。退出码 0=一致 / 1=实质差异 / 2=错误
- **重要发现（实网核验）**：国务院公报**整期 PDF 为扫描图像版**（无文本层，触发 OL-1009 拒绝）——公报正文入库应走**单文档 HTML 页**（`gongbao/<年>/issue_<id>/<月>/content_<id>.html`，正文在 UCAP-CONTENT 容器）；整期 PDF 仅作原始出处存档，如需其文本须走 OCR。另据用户确认：**flk 的单行 PDF 亦有一部分为扫描版**——文本核验替代路径：flk 在线浏览页手动复制全文存 `.txt`（浏览器手动操作，合规）、或改用两侧 HTML 文字源；两侧皆扫描时只能转 OCR（暂未实现）。`openlaw-compare` 遇无文本层 PDF 会报错并输出上述替代路径指引
- **更新包（pack/ + cmd/openlaw-pack）**：FR-06 闭环——build（复制 pdf/md/provenance + changes.csv + manifest + Ed25519 签名）、verify（签名/逐文件哈希/拒绝未收录文件）、import（先校验后导入，失败拒绝整包保留旧数据；条目 confirm_status=pending 转人工）
- **PDF 文本层提取（parse/pdftext）**：基于 ledongthuc/pdf（纯 Go，与 OpenCourt 同款依赖）——`openlaw-ingest` 的 `-md` 转为可选，留空自动提取文本层→去碎片化→条文锚点重插→拆分入库；无文本层（扫描件）明确拒绝并提示转 OCR（OL-1009），绝不静默入库；部首码位映射（⼆≠二）等保真问题由 confirm_status=pending 人工确认兜底
- **批量抓取（ingest/harvest + robots + openlaw-fetch -list/-batch）**：robots.txt 强制执行（404/403=无限制、5xx/网络错误=fail-closed 拒绝、最长前缀匹配、同长度 Allow 优先）；`flk.npc.gov.cn` 因 robots 明确禁止任何自动化采集列入**硬禁止名单**；国务院公报 `www.gov.cn/gongbao/` 为可批量源——两阶段：目录页→candidates.csv（人工审阅）→限速下载（默认 2s/条，单条失败不中断，全部写 import_log）。实网验证：当期公报整期 PDF（约 30MB）成功入暂存区
- **抓取（ingest/downloader + cmd/openlaw-fetch）**：显式触发的 PDF 抓取——默认离线不自动联网；HTTPS-only + 来源域名白名单 + 重定向逐跳复核 + 大小上限 + Content-Type 必须为 PDF；只进 `data/incoming/` 暂存区，`source_url/http_status/content_type/抓取时间/SHA-256` 全部写 import_log；提供 `-md` 时继续走同一导入闭环（confirm_status 默认 pending 转人工）
- **存储层（store/）**：SQLite 全量 DDL（law / document / law_version / article / element / case_fact / relation / conflict / import_log / validate_log）+ FTS5 虚拟表与触发器 + 版本时效区间重叠检查（左闭右开）
- **导入链路（ingest/ + cmd/openlaw-ingest）**：PDF → SHA-256/MD5 → 溯源 JSON（必填字段齐全）→ 条文拆分 → 事务入库 → import_log；任一环节失败整批回滚并记 rolled_back
- **校验链路（validate/）**：六环节断链检测（版本→文档→文件→哈希→manifest→签名），失败写 validate_log 并返回 OL-1006/OL-1007/OL-1008，不抛异常；manifest 生成与 Ed25519 签名/验签；引用校验（时间点回溯版本 + 条款号 + 文本相似度）
- **SDK（sdk/）**：Result/Trace 结构体 + Status 五态（ok/expired/broken_chain/not_found/need_manual）+ OL-1001~OL-1009 错误码；ResolveVersion / CheckCitation / AnalyzeElements / FindRelation / DetectBreak / Desensitize
- **脱敏（element/desensitize）**：姓名（自定义名单 + 角色上下文）、身份证号、案号、企业名称四类替换，返回本地还原映射
- **运维（cmd/openlaw-check）**：每日哈希重算 + manifest 签名校验 + 断链清单
- **测试**：条文拆分、日期边界（生效当天命中/失效当天不命中）、导入失败回滚、断链六环节、哈希篡改 OL-1007、manifest 篡改 OL-1008、脱敏覆盖率、端到端验收（导入 → 引用校验命中 → 篡改 PDF → 断链）

### 已知限制
- 扫描件 OCR 未实现（仅接口就位，无文本层 PDF 会被拒绝导入并提示 OL-1009）；文本层文本提取存在部首码位映射等保真风险，依赖人工确认兜底
- 欧盟通道目前仅元数据+关系（law 主表级）；全文须人工保存 EUR-Lex（WAF 挑战）后走 openlaw-ingest；Cellar 表示层（HTML/PDF 全文直取）语法待按官方文档实现；op.europa.eu 对 robots.txt 有较严限流（429 时 fail-closed 拒绝整个运行，稍后重试即可）
- flk.npc.gov.cn 明确禁止自动化采集（硬禁止），其文本只能人工下载入暂存区；公报批量层只覆盖 PDF 直链（公报文号页为 HTML 全文，未做 HTML→正文通道）
- 中文 FTS5 检索为 unicode61 分词 + LIKE 兜底，术语辅助列与向量混合检索、重排序为后续版本
