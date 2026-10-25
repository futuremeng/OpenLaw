# 欧盟全文通道设计草案（Cellar representation 层，2026-10-25）

状态：**草案（未实现）**。本文所有网络事实均为 2026-10-25 实网验证（GDPR CELEX `32016R0679` 样本），非推测。

## 一、关键突破：全文可自动化，推翻"只能人工"旧结论

此前结论（sources-eu.md）：EUR-Lex 网页被 CloudFront WAF 拦截 → 全文只能人工保存。**该结论只适用于 eur-lex.europa.eu 域名**。本次实测证明 Cellar（publications.europa.eu）的 representation 层可直接程序化取得全文，全程无 WAF：

| 步骤 | 实测 | 结果 |
|---|---|---|
| 1. expression 定位 | `GET /resource/celex/32016R0679.ENG`（无 Accept） | 303 → `cellar/<uuid>.0006/rdf/object/full`（expression CMR；`.0006` 为 expression 序号） |
| 2. manifestation 列表 | expression CMR 内 `cdm:expression_manifested_by_manifestation` | 三种：`.fmx4`（XML）、`.xhtml`、`.pdfa1a`（PDF），URI 形如 `oj/JOL_2016_119_R_0001.ENG.<fmt>` |
| 3. item 发现 | `GET <manifestation URI>/rdf/object/full` | item 列表：`cdm:item_identifier`（`DOC_1`、`DOC_2`…）+ `cdm:manifestationMimeType` |
| 4. 全文下载 | `GET <cellar manifestation URI>/<item_id>` | ✅ 200 直接给字节，无 WAF |

GDPR 实测样本：
- `<uuid>.0006.03/DOC_1` → CONVEX XHTML 全文，806 KB（`application/xhtml+xml`）
- `<uuid>.0006.02/DOC_1` → FORMEX wrapper（1.6 KB 元数据，指向 PHYS 文件）
- `<uuid>.0006.02/DOC_2` → **FORMEX ACT 正文 XML，428 KB**——含 99 个结构化条文元素

合规性不变项：robots.txt `User-agent: * Allow: /`，`/webapi/rdf/sparql` 与 `/resource` 均官方开放程序化访问（与 flk 的"禁止自动化"性质相反）；重使用须署名（© European Union，注明出处与修改）。限速沿用 `ingest/cellar.go` 机制（串行 + 2s + 429 退避 + import_log + robots 强制）。

## 二、格式选型

| 格式 | 内容 | 条文结构 | 用途 |
|---|---|---|---|
| **FORMEX ACT XML**（fmx4 manifestation 的 DOC_2） | 正文 | ✅ `<ARTICLE IDENTIFIER="001"><TI.ART>Article 1</TI.ART><STI.ART>Subject-matter and objectives</STI.ART><PARAG IDENTIFIER="001.001"><NO.PARAG>1.</NO.PARAG><ALINEA>正文</ALINEA></PARAG>…`（GDPR 实测 99 个 ARTICLE 元素，1–99 全量） | **首选**：条文边界来自结构，零正则、零猜测 |
| CONVEX XHTML（xhtml manifestation 的 DOC_1） | 正文 | ❌ 表格布局，`Article N` 为纯文本 | 备选：FORMEX 缺失时按 `Article N` 文本规则切分（低置信，confirm 门槛更高） |
| PDF（pdfa1a） | 正文 | — | 仅归档留证（raw 层），不参与解析 |

FORMEX wrapper（DOC_1）的 `REF.PHYS FILE=` 与 `sameAs` 均指向 doc.xml，但直接取 uriserv doc.xml 会 303 回 wrapper——**正文一律取 manifestation 的 `DOC_2`**（或 RDF 中 `item_identifier` 对应 mimeType=`application/xml;type=fmx4` 且体积最大者，wrapper 固定 ~1.6 KB 可据此区分）。

## 三、模型映射（复用现有 schema v3，无新迁移）

| 字段 | 取值 | 说明 |
|---|---|---|
| `law` | 已由 `openlaw-eu` 建立（eli/celex 列已填充） | 全文通道只补 document/article，不重复建法 |
| `document.doc_id` | `eu_<celex小写>_<lang>`（如 `eu_32016r0679_eng`） | 幂等键；多语言=多 document |
| `document.lang` | ISO 639-3（`eng`…） | schema v2 已备列 |
| `document.source_type` | `cellar` | 区别于 `flk`/`govcn` |
| `document.parser_version` | `formex-eu-v1`（XHTML 回退用 `convex-eu-v1`） | 沿用 `+whole` 惯例：解析器可追溯；回退通道特征查询须排除 |
| `article.article_no` | `Article 1`…（与 `TI.ART` 一致） | 中文语料是「第X条」，EU 保留英文标记原样 |
| `law_version` | 原 OJ 版区间（元数据已由 openlaw-eu 建）；合并版（conslex，CELEX 前导 `0`，如 `02016R0679-20240625`）M2 再接 | 版本链语义与 flk 通道一致 |
| provenance JSON | eli/celex/manifestation URI/item URL/格式/sha256/抓取时间 | 与 flk provenance 同目录同规范 |
| raw 层 | `data/incoming/eu_<celex>_<lang>_<fmt>.<ext>` 留证（M2 起迁 `data/raw/eu/`） | sha256 复检可重放 |
| `confirm_status` | **pending**（与 flk 一致） | EU 文本虽权威，仍走人工确认闸口，`openlaw-confirm` 直接可用 |

语言与权威性：EU 24 官方语言同等作准；**中文不是 EU 官方语言**——任何中文译本只能作非正式参考，入库须独立 provenance 且不得标 confirmed（文档级纪律，M3 前不收）。

## 四、实现设计（最小改动原则）

1. **`ingest/cellar.go` 扩展 `FetchText(ctx, celex, lang, format) (*TextDoc, error)`**：
   四步链（expression 303 → manifestation RDF → item 挑选 → 下载），复用现有 robots/限速/import_log/大小上限机制；原始字节落 incoming 留证；返回 `{Bytes, SHA256, ManifestationURI, ItemURL, Format, MimeType}`。
2. **`parse/formex.go` 新增 `ParseFormexACT(xml []byte) ([]Article, error)`**：`encoding/xml` 解析（FORMEX 是 well-formed XML，不用正则）；输出 `{No:"Article 1", Title:"Subject-matter and objectives", Text:…}`（PARAG/ALINEA 按 `1.` 序号拼接）；PREAMBLE/ANNEX/TITLE 章结构 M1 归入文档级 `全文` 兜底或跳过，M3 细化。解析失败 → 整体回退 `article_no='全文'`（whole 通道先例，parser_version 加 `+whole` 后缀）。
3. **`ingest.Request` 扩展 `Articles []ArticleSpec`**（非空时跳过 MDPath+SplitArticles，直接按给定条文列表入库）：复用 Importer 的版本链、FTS、provenance、confirm 全部现有逻辑，不复制第二条入库路径。
4. **`cmd/openlaw-eu-text`**：`-celex 32016R0679 -lang eng [-format fmx4|xhtml] [-dry-run]`；dry-run 打印条文计数/首尾样本，正式运行写库。批量（M2）：`-list` 从 SPARQL 枚举 in-force 法规/指令/决定 CELEX 清单，串行下载。
5. **回退策略**：FORMEX 缺失 → XHTML + `Article N` 文本切分（`parse.SplitArticlesEU`，低置信）→ 全缺 → PDF 仅归档 + 入人工队列（对齐 flk 的 73 条无文本 docx 纪律）。

## 五、验收标准（M1，GDPR 试点）

- `document`：`eu_32016r0679_eng`，lang=eng，parser_version=formex-eu-v1，confirm_status=pending
- `article`：99 行 `Article 1`–`Article 99`，计数 == XML `<ARTICLE>` 元素数；FTS 可搜「personal data」命中条文级
- provenance JSON + incoming 原始文件 + sha256 复检一致；import_log 记录全部四步 URL
- pack 导出/回灌往返一致（manifest license/attribution 已备）
- `openlaw-confirm stats` 出现 EU 分层；人工复核后可 confirmed

## 六、里程碑

| 里程碑 | 范围 |
|---|---|
| M1 | `FetchText` + `ParseFormexACT` + `Request.Articles` + `openlaw-eu-text` 单文档，GDPR 试点 |
| M2 | SPARQL 批量清单（regulation/directive/decision，in-force）+ XHTML 回退 + PDF 归档 + raw/eu/ 目录规范 |
| M3 | Recital（序言段）/Annex 切分、conslex 合并版版本链、多语言版本策略 |

## 八、实现状态（M1 已完成，2026-10-25）

已实现：`Cellar.FetchText`（四步链：expression CMR → manifestation 选择 → item 发现 → 下载取最大者；robots 强制/2s 限速/import_log/大小上限/留证 incoming）+ `parse.ParseFormexACT`（`encoding/xml` 状态机，ARTICLE/TI.ART/STI.ART/PARAG/NO.PARAG/ALINEA 最小元素集，序言/结语/章节标题进 md 层）+ `parse.SplitArticlesEU`（英文 `Article N` 切分，XHTML 回退与 pack 回灌共用）+ `Importer` 扩展（`Request.Articles/Lang/ParserVersion`，document.lang/law_version.lang 写入）+ `cmd/openlaw-eu-text`（默认 dry-run；前置 openlaw-eu 元数据，不自动建占位法）+ pack `changes.csv` 新增 `lang` 列（v0/v1/v2 三代表头向后兼容）与 xml 原始层。单测覆盖：四步链 mock（httptest 复刻 GDPR 真实结构）、FORMEX 解析与 FullText↔SplitArticlesEU 往返、EU 入库（lang/parser_version/FTS）、pack lang 往返。

**GDPR 实网 pilot 状态**：代码链在实网验证到 expression 请求步（TLS 修复后 robots 通过），其后被 EU 站点按客户端指纹分桶的 IP 级 429 冷却挡住（探测阶段请求过量所致；curl 桶已恢复 200，Go 桶冷却更长）。pilot 待配额恢复后执行：

```
openlaw-eu-text -data data -celex 32016R0679 -lang eng -dry-run   # 先验证 99 条
openlaw-eu-text -data data -celex 32016R0679 -lang eng            # 入库（pending 闸口）
```

实网新发现（实现期修正设计文档）：
1. **expression URI 语言后缀大写**（`…32016R0679.ENG`，非 ISO 639-3 小写）——lang 字段入库保持小写，URI 构造时 ToUpper；
2. **域名迁移**：`publications.europa.eu` 资源 URI 会 301/303 到 `op.europa.eu`（同一出版局服务，robots.txt 本就由 op.europa.eu 提供）——重定向白名单锁双域；
3. **429 双层限流**：AFD（op.europa.eu）与 nginx（publications.europa.eu）都会限流；fail-closed 拒绝属正确行为，长退避（≥5 分钟）重试，勿轰炸；
4. **TLS 指纹误伤**：Go 1.24+ ClientHello 默认带 X25519MLKEM768 后量子 key share，JA3 异常被 AFD 按机器人持续 429（curl 同 IP 200）——`Cellar.policy()` 用自定义 Transport 限定曲线偏好（X25519/P256）即恢复；UA 仍如实标识，不冒充浏览器。

## 九、风险与开放问题

1. FORMEX schema 随 CONVEX converter 版本演化——解析器只依赖 `ARTICLE/TI.ART/STI.ART/PARAG/NO.PARAG/ALINEA` 最小元素集，未知结构走 whole 兜底；
2. 2000 年前纸质 OJ 数字化文档部分只有 PDF/TIFF 扫描——OCR 队列（同 flk 无文本 docx 纪律）；
3. 部分法（如指令）跨 OJ 页多段、manifestation item 可能多个 DOC_N——M2 批量时按 RDF mimeType+体积挑选，逐条验证；
4. Cellar 429 限流严（既有经验）：批量必须串行+退避，单文档四步链已含 3 次请求，全库级批量（数万件）需评估配额与时间预算；
5. 合并版（conslex）与原文并存的版本链语义（原文永存 + 合并版接续）M2 前须与 `law_version` 区间模型对齐设计，防止区间重叠。
