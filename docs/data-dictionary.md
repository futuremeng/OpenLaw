# 数据字典

表结构定义的唯一事实来源是 `store/schema.sql`（go:embed 入库），本文为字段说明与追溯矩阵。

## 1. 实体关系

law 1─n law_version 1─n article 1─n element；law 1─n relation；document 与 law_version/article 关联（溯源锚点）。导入与校验过程分别记录 import_log、validate_log；多源不一致记录 conflict。

## 2. 表与关键字段

### law 法规主表
law_id（PK）、name、short_name、issuer、effect_level（CHECK 枚举：宪法/法律/行政法规/监察法规/地方性法规/部门规章/地方政府规章/司法解释/其他/欧盟条例/欧盟指令/欧盟决定）、is_local、remark。

**地域（地点定位，OL-12 / F3）**：schema v4 起 `law` 具备地域标引列——`region_code`
（GB/T 2260 区划码，与 `geo` 包一致；**空 = 地域未标注**）、`region_source`
（auto=机器建议（名称/机关推导）/ manual=人工落判 / trial=试判定）、
`region_rule`（依据与规则号，如 `name-prefix(R1)`、`issuer(R3)`）、
`region_reviewer`（人工落判署名；**空 = 仅机器建议**）、`region_reviewed_at`。
列刻意不加 CHECK、不重建表（v2/v3 改 CHECK 需 12 步重建，风险高），取值合法性由应用层与门禁测试保证。
**当前状态（如实）**：真实语料 28,952 条法规中 `region_code` 已填 **24,032 条（83.0%）**，
全部为 `region_source='auto'` 的**机器建议**（`region_reviewer` 全空 = 待复核，界面必须显示
「机器建议 · 待复核」），批次号 `region-20261029T155448Z`（`openlaw-region -undo <批次> -apply`
可整批回滚；上一批次 `region-20261029T151705Z` 已因国家级护栏整批撤销，`region_log` 保留
suggest/undo 完整痕迹）。仍留空 4,920 条，分类如实：**2,820 条属国家级效力**
（法律/行政法规/部门规章/司法解释/宪法/欧盟条例——本就不该有地域）、**2,010 条判不出**
（县级市、名称省略「市」字等）、90 条仅机关命中的中置信（按拍板不写）。
**桌面浏览过滤仍按标题前缀**（`desktop/region.go` 的 `regionPatterns`），尚未切到区划码——
本批写入属「数据就位」，界面切换与如实提示属实施第 5 步。
在此之前界面仍按**查询期推导**显示地域：`parse.InferRegion` 按名称前缀匹配内置 `geo` 区划表
（省级/地级两条规则 + 「××经济特区」别名），判不出一律留空——UI 必须显示「地域未标注」，
不得假装覆盖。真实语料实测：law 24,033/28,952（83.0%）可判，其中地方类 24,030/25,009（96.1%）；
剩余部分（如「城市民族工作条例」「××自治县…单行条例」）如实留空。
地区偏好存 `meta` 表（经 Go 读写，前端不私存）：`user_region`（区划码，空=不限）、
`user_region_source`（auto=就近判定 / manual=手动选择）、`user_region_at`（RFC3339 UTC）、
`user_region_note`（就近判定误差等如实说明）。

### document 原始文档（溯源锚点）
doc_id（PK）、file_path（**相对数据目录**，如 `pdf/<doc_id>.pdf`）、file_size、sha256（防篡改依据）、hash_algo（必须注明算法类型）、hash_computed_at、source_url/source_type、is_gongbao、publish_date、ocr_used/ocr_confidence、confirm_status（confirmed/pending/rejected）、lang（ISO 639-3，默认 zho；EU 通道 eng 等——24 官方语言同等作准，多语言=多 document）。

### law_version 版本表
version_id（= `law_id + "_" + version_no`）、effective_date、expire_date、lang（版本正文语言，默认 zho）。
**时效约定：左闭右开 `[effective, expire)`；未失效留 NULL，不用 9999 占位。**
生效当天命中、失效当天不命中。导入器在事务内做区间重叠检查，防止同日双版本命中。

### article 条文表
article_id（= `version_id + "_" + article_no`）、content、md_path、doc_id、**parser_version（强制版本化）**。

**全文即一节（whole-text）**：无「第X条」结构的文档（决定/决议/公告/名单类）以单条记录承载全文——`article_no = "全文"`（约定值，不与任何真实条号冲突）、`parser_version` 带 `+whole` 后缀（如 `openlaw-parse/0.1.0+whole`）。该记录可全文检索，但**语义上是整份文档而非条文**：引用校验/要件分析等条文级功能不得将其当作条文使用；按 `parser_version LIKE '%+whole'` 或 `article_no='全文'` 即可整体排除。

**EU 通道**：`article_no` 为英文条号（`Article 1` 等）、`parser_version = openlaw-parse/0.1.0+formex-eu`（FORMEX ACT XML 结构边界，XHTML 回退带 `+xhtml` 后缀）。条文级中文语义功能（要件抽取等）须按 `parser_version LIKE '%+formex-eu'` 或 `lang` 区分语言。

### element 构成要件表
element_id、article_id、case_type（案由）、name、description、how_to_attack（对方突破方向）。

### case_fact 案件事实表
status：supported（已支撑）/ unsupported（缺证据）/ conflict（相互矛盾）。

### relation 关系表
relation_type：修改/废止/解释/配套；confirm_status 默认 pending——**机器不自动判定废止关系**。

### conflict 冲突表
多源差异并列记录，不自动采信任一方；handle_status：open/resolved/ignored。

### import_log / validate_log
导入结果：success/failed/rolled_back；校验结果：pass/fail + 原因。
注意：import_log.doc_id 有外键，document 未入库的失败场景 doc_id 置 NULL，标识保留在 reason 中。

### review_log 人工确认审计
confirm 流程（openlaw-confirm）的状态流转留痕：doc_id、action（confirm/reject/reset）、from_status→to_status、reviewer（必须署名）、reason（确认依据；批量流转必含过滤条件与依据）、created_at。**绝不允许无审计痕迹的状态变更**；provenance JSON 的 confirm_status 随流转同步。

### region_log 地域标引审计（v4）
law 级地域判定留痕：law_id、action（suggest/confirm/reject/clear/undo）、from_code→to_code、
source（auto/manual/trial）、rule（机器规则号或人工依据）、actor（机器固定 `openlaw-region`，
人工必须署名）、reason、batch_id（同一次批量写入/撤销的关联号，`openlaw-region -undo` 依此回滚）、
created_at。**与 review_log 并列而非复用**：review_log 外键是 doc_id（文档级正文复核），
region_log 是 law 级（地域复核）——正文已复核 ≠ 地域已复核，两者不得混同。
机器写入必须 `law.region_reviewer` 留空，界面据此显示「机器建议 · 待复核」；
人工落判（confirm/reject/clear）一律署名 + 结论依据。

## 3. 溯源文件（provenance/<doc_id>.json）

必填：doc_id、file_name、file_size、sha256、hash_algo_used、hash_computed_at、hash_computed_by、source_url、source_type、is_gongbao、publish_date、effective_date、expire_date、effective_date_inferred、md_path、confirm_status。
可选：md5、sm3、ocr_used、ocr_confidence、pdf_has_signature、signature_valid、timestamp_token、notes。
MD5 仅判下载损坏；防篡改用 SHA-256/SHA-512/SM3。

## 4. 全文检索

`article_fts`（FTS5，unicode61）由 article 触发器同步。已知限制：unicode61 对连续中文整段切词，短语召回差，`search.Search` 以 LIKE 兜底；术语辅助列与向量混合检索为后续版本。

## 5. 需求追溯矩阵（17.2）

| 需求编号 | 实现模块 | 测试用例 | 状态 |
|---|---|---|---|
| FR-01 版本解析 | store、validate.ResolveVersion | TestActiveAtBoundary、TestVersionExpiryFlow | ✅ |
| FR-02 引用校验 | validate.citation | TestClosedLoop（命中/OL-1004） | ✅ |
| FR-03 反查 PDF | sdk.Trace（doc_id + pdf_path） | TestClosedLoop（Trace 断言） | ✅（界面定位按钮由上层实现） |
| FR-04 关联查询 | relation 表、sdk.FindRelation | TestFR04FindRelation（pending 告警/confirmed 不告警/双向查询） | ✅ |
| FR-05 要件比对 | element.loader/matcher、sdk.AnalyzeElements | TestFR05AnalyzeElements（covered/missing 分组、突破方向、风险假设告警） | ✅ |
| FR-06 离线更新包 | pack（build/verify/import + trust 来源信任）、cmd/openlaw-pack | TestBuildVerifyRoundtrip、TestVerifyDetectsTamper、TestImportPackFailNoOverwrite、TestImportPackSuccess、TestImportRejectsUntrustedKey、TestImportTrustedAfterRegistration、TestImportAllowUntrusted、TestRegisterTrustedPubkeyGuards | ✅ |
| FR-07 混合检索 | search.fts（FTS5+LIKE） | TestSearchChineseFallback | 部分（向量待后续） |
| FR-08 脱敏 | element.desensitize | TestDesensitizeCoverage | ✅ |
