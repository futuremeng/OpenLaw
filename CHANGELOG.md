# 更新日志

本项目遵循 Semantic Versioning：主版本=数据模型或 SDK 接口不兼容变更（必须附迁移脚本，迁移前自动备份）；次版本=新增能力；修订版本=修复。详见 docs/release-process.md。

## Unreleased（v0.1.0 开发中）

### 新增
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
- 扫描件 OCR 未实现（仅接口就位，无文本层 PDF 会被拒绝导入并提示 OL-1009）；文本层提取存在部首码位映射等保真风险，依赖人工确认兜底
- flk.npc.gov.cn 明确禁止自动化采集（硬禁止），其文本只能人工下载入暂存区；公报批量层只覆盖 PDF 直链（公报文号页为 HTML 全文，未做 HTML→正文通道）
- 中文 FTS5 检索为 unicode61 分词 + LIKE 兜底，术语辅助列与向量混合检索、重排序为后续版本
