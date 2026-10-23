# 数据字典

表结构定义的唯一事实来源是 `store/schema.sql`（go:embed 入库），本文为字段说明与追溯矩阵。

## 1. 实体关系

law 1─n law_version 1─n article 1─n element；law 1─n relation；document 与 law_version/article 关联（溯源锚点）。导入与校验过程分别记录 import_log、validate_log；多源不一致记录 conflict。

## 2. 表与关键字段

### law 法规主表
law_id（PK）、name、short_name、issuer、effect_level（CHECK 枚举）、is_local、remark。

### document 原始文档（溯源锚点）
doc_id（PK）、file_path（**相对数据目录**，如 `pdf/<doc_id>.pdf`）、file_size、sha256（防篡改依据）、hash_algo（必须注明算法类型）、hash_computed_at、source_url/source_type、is_gongbao、publish_date、ocr_used/ocr_confidence、confirm_status（confirmed/pending/rejected）。

### law_version 版本表
version_id（= `law_id + "_" + version_no`）、effective_date、expire_date。
**时效约定：左闭右开 `[effective, expire)`；未失效留 NULL，不用 9999 占位。**
生效当天命中、失效当天不命中。导入器在事务内做区间重叠检查，防止同日双版本命中。

### article 条文表
article_id（= `version_id + "_" + article_no`）、content、md_path、doc_id、**parser_version（强制版本化）**。

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
| FR-04 关联查询 | relation 表、sdk.FindRelation | 待补 | ⏳ |
| FR-05 要件比对 | element.loader/matcher、sdk.AnalyzeElements | 待补 | ⏳ |
| FR-06 离线更新包 | validate.manifest（签名先行） | TestManifestTampered | 部分（包体 build/verify 待补） |
| FR-07 混合检索 | search.fts（FTS5+LIKE） | TestSearchChineseFallback | 部分（向量待后续） |
| FR-08 脱敏 | element.desensitize | TestDesensitizeCoverage | ✅ |
