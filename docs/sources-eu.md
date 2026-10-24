# 欧盟法律数据源核实报告（2026-10-24 实网验证）

## 一、访问路径实测结论

| 入口 | 实测结果 | 自动化可用性 |
|---|---|---|
| EUR-Lex 网页/ELI 解析器（data.europa.eu/eli/… → eur-lex） | CloudFront WAF JS 挑战（`202 x-amzn-waf-action: challenge`） | ❌ 与 flk 同级：只能人工浏览 |
| Cellar SPARQL `https://publications.europa.eu/webapi/rdf/sparql` | ✅ curl 直连可用（Virtuoso），按 work 的 named graph（cellar UUID）可取完整 CDM 元数据 | ✅ 可自动化，但限流严（连发即 429，需保守限速） |
| Cellar REST `https://publications.europa.eu/resource/celex/<CELEX>?language=eng`（无 Accept 头） | ✅ 303 → `/rdf/object/full`，返回该法完整 CMR 记录（GDPR 为 62 MB，含全部修正链/废止链/判例引用） | ✅ 元数据可自动化 |
| Cellar REST 文本表示（Accept: text/html / application/pdf） | 400 `Invalid content type CONTENT_STREAM for WORK without language`——需按 OP 官方文档的 expression/manifestation 表示路径（文档站当前 429 限流，未能核对确切语法） | ⚠️ 待按官方文档实现 |

## 二、Cellar 元数据与我们的模型映射（以 GDPR 32016R0679 实测样本）

| Cellar CDM 字段 | 实测值示例 | OpenLaw 对应 |
|---|---|---|
| `cdm#resource_legal_eli` | `http://data.europa.eu/eli/reg/2016/679/oj` | `law.source_url` / 新增 `eli` 标识列 |
| `cdm#resource_legal_id_celex` | `32016R0679` | 新增 `celex` 列（或 identifier 表） |
| `owl:sameAs`（cellar/celex/eli/oj 四套 URI） | 同一 work 四种持久 URI | 溯源链天然兼容 |
| `cdm#resource_legal_date_entry-into-force` | 2016-05-24 **与** 2018-05-25（生效与适用是两个日期） | `law_version.effective` + **需新增 application_date** |
| `cdm#resource_legal_date_end-of-validity` / `in-force` | 9999-12-31 / 1 | `law_version` 失效区间 |
| `cdm#resource_legal_repeals_resource_legal` / `implicitly_repeals_…` | 指向被废止法的 cellar URI | **`relation(废止)` 可直接从元数据填充**（比中文源更好：flk 无机器可读废止关系） |
| `cdm#resource_legal_based_on_concept_treaty` | TFEU | `relation(依据)` 可扩展 |
| `cdm#resource_legal_published_in_official-journal` | OJ L 119 | 溯源/出处 |

## 三、兼容性判定

**架构级：兼容。** 四层架构、`law/law_version` 版本区间、`relation`、provenance+SHA-256、pending→confirmed、pack 签名共享全部映射成立，且 EU 元数据的显式废止/修正关系比中文源质量更高。

**字段级差距（接入前需扩展）：**
1. **多语言**：24 个官方语言版本同等作准——`document/article` 需加 `lang` 列（当前隐含单语）；
2. **条文结构**：EU 用 `Article N` / 序言 `Recital (N)` / `Annex`——条文切分器需 EU 样式规则（recital 作独立条类型）；
3. **生效 vs 适用**两个日期（见上表）；
4. **manifest 需 license/attribution 字段**：EU 数据重使用要求署名（© European Union），pack 共享时必须携带。

## 四、接入策略（镜像 flk/gov.cn 三分法）

- **元数据/关系**：Cellar SPARQL + CMR REST，可自动化，保守限速（串行、退避）；
- **全文文本**：EUR-Lex 网页只能人工保存（WAF）；Cellar REST 单文档表示待按官方文档语法实现后自动化；
- **合规**：欧盟法律文本本身公有领域，EUR-Lex/Cellar 重使用需署名与注明出处；SPARQL 端点是官方明确开放的程序化入口，与 flk 的"禁止自动化"性质相反。

## 五、实现状态（2026-10-24）

已实现：`ingest/cellar.go`（SPARQL 客户端：CELEX→named graph→CDM 字段；robots 强制、2s 限速、import_log、原始三元组留证 incoming）+ `cmd/openlaw-eu`（law 主表 upsert + 废止/依据关系 pending 挂载，仅挂已入库目标法）。schema v2 迁移落地（eli/celex/lang/application_date/欧盟效力等级/依据关系）。pack manifest 增加 license/attribution 署名字段。

关键实现细节：Cellar 规范 URI 为 **http scheme**（Linked Data 惯例），SPARQL 按 https 拼 URI 会查不中——`cellarResBase` 固定为 `http://publications.europa.eu/resource`。

待办：Cellar 表示层（HTML/PDF 全文）确切语法按官方文档实现；EUR-Lex 全文人工通道；op.europa.eu robots.txt 限流（429）时整次运行 fail-closed，属正确合规行为，稍后重试即可。

