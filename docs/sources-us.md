# 美国官方数据源调研（sources-us）

> 实网核实日期：2026-10-31。本文只记录**已核实**事实；未核实项如实标注，
> 不得据以开工。版权前提：美国联邦政府作品不受版权保护（17 U.S.C. §105），
> govinfo/eCFR 均为官方开放程序化入口——与 flk「禁止自动化」性质相反。

## 1. govinfo.gov（GPO，主通道）

- **robots.txt 已核实**：对 `*` 未禁止 `/content/`、`/bulkdata/` 路径（只禁
  `/app/search/*`、 Drupal 内部路径等），并公布全套 sitemap 索引，含
  `USCODE / STATUTE / CFR / ECFR / FR / PLAW / USREPORTS / USCOURTS` 与
  **bulkdata 子 sitemap**（`/sitemap/bulkdata/{CFR,ECFR,FR,STATUTE,PLAW,…}/sitemapindex.xml`）。
  即：批量数据目录对自动化抓取开放（骨架即以 sitemapindex 为已验证入口）。
- **Bulk Data Repository 已核实**（https://www.govinfo.gov/bulkdata/）：机器可读
  XML 批量仓库，收录（2026-10-31 页面实况）：
  - Code of Federal Regulations（Annual Edition）——联邦规章年度版
  - Electronic Code of Federal Regulations（ECFR）——联邦规章现行版
  - Statutes at Large（xml uslm beta）——法律原始文本（会期法）
  - Statute Compilations（xml uslm beta）——法典化条文（US Code 性质，beta）
  - Public and Private Laws（xml uslm beta）、Federal Register、Public Papers 等
  注意：**USCODE 集合不在批量仓库清单内**（站内可浏览、sitemap 有，但无 bulk 包）；
  联邦法律现行文本以 Statute Compilations（beta）承载，格式稳定性待观察。
- 每个集合目录含 Resources（XML schema、XSLT、用户指南）——解析器实现前必须先
  按 schema 校验样例，不得凭猜解析。

## 2. eCFR（ecfr.gov）

- **实况已核实**：HTML 页面对自动化访问有 CAPTCHA 墙（明示「程序化访问仅限
  开发者 API」）；robots.txt 对 `*` 禁 `/api/versioner/v1/full/` 与
  `/api/renderer/v1/content/`（搜索引擎护栏）。
- 官方立场与 robots 护栏存在张力：API 是官方指定的程序化入口，但全文端点被
  robots 禁抓。本项目按 fail-closed 纪律处理：**规章现行全文改走 govinfo ECFR
  批量包**（robots 开放、无 CAPTCHA）；eCFR API 仅在不触碰被禁路径
  （如 titles/structure 元数据）且遵守官方 API 条款时可用——接入前需人工确认条款。

## 3. uscode.house.gov（OLRC）

- **已核实**：批量下载页对自动抓取返回 403/404（UA/会话限制）——与 flk 同类
  待遇。批量 XML 下载在官方页面对人工浏览开放；自动化通道未证实前**不开采集**，
  联邦法律现行文本以 govinfo Statute Compilations 为主。

## 4. Federal Register（federalregister.gov）

- 官方开发者 API 存在（HTML 有 CAPTCHA 墙、API 为官方程序化入口，同 eCFR）；
  行政命令（Executive Orders）亦有 govinfo FR/PPP 批量旁路。本期未实装，仅登记。

## 5. 通道决策（骨架所依）

1. 联邦规章 → govinfo CFR/ECFR 批量包（robots 开放）。
2. 联邦法律 → govinfo Statute Compilations（beta，格式观察期）；USCODE 批量
   旁路若出现再切换。
3. 行政命令 → govinfo FR/PPP 批量（后续）。
4. 骨架第一步只做**清单拉取与留证**（sitemapindex → 包清单），不碰全文：
   `openlaw-us -sitemap -collection ECFR`；全文解析入库属写路径高风险变更，
   另行按阶段实施（见 design-multi-jurisdiction.md §6）。
5. 限速 ≥2s/请求、robots 前置 fail-closed、原始响应落 `incoming/` 留证、
   每次访问进 import_log——与中国/欧盟通道同一纪律。
