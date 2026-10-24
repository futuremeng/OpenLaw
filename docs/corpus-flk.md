# flk 存量语料盘点（2026-10-24）

## 获取方式与合规边界

- 语料由**用户自行从 flk 获取**后作为本地存量提供（浏览器批量下载为主，部分批次有脚本辅助）。
- **OpenLaw 仓库内工具对 flk 永不联网**：`flk.npc.gov.cn` 在硬禁止名单（robots 禁止任何自动化采集），本项目所有自动抓取仅针对 robots 允许的官方入口（如欧盟 Cellar）。用户侧获取方式由用户自担责任；法律文本本身公有领域，持有与分享合法。
- 本仓库对存量的处理全部是**离线**的：盘点（openlaw-scan）→ 哈希固定 → 导入（openlaw-ingest）。

## 存量规模（/Volumes/HDD/law，openlaw-scan 实测）

| 指标 | 数值 |
|---|---|
| zip 包 | 317（flk 站内批量下载，每包约 100 条） |
| 文件总数 | 31,615 |
| 格式 | .docx 25,671 / .doc 5,943 / .docm 1 |
| 文本可用（docx 正文探测） | 25,598 |
| 无文本层/空 | 0 |
| 未识别（.doc/.docm 遗留二进制格式） | 6,017 |
| SHA-256 跨包重复 | 601（同名法规出现在多个包，导入时按哈希去重） |
| 写盘错误 | 0（超长法规名已在 rune 边界截断修复） |

报告：`/Volumes/HDD/law/scan_report.csv`（zip,entry,out,ext,size,sha256,text,dup_of）；解压：`/Volumes/HDD/law/extracted/`。

## 站点侧确认缺失（任何方式均 404，非下载遗漏）

1. 深圳市人大常委会关于修改《深圳市建设工程质量管理条例》的决定（p103）
2. 广州市人大常委会关于清理与民法典不一致条款的决定（p108）
3. 西安市周丰镐、秦阿房宫、汉长安城和唐大明宫遗址保护管理条例（p191）

个别 zip 为 99/94 文件即上述条目所致，属正常。

## 后续处理路线

- **.docx（25,671）**：✅ 已批量导入（openlaw-batch，见下）。
- **.doc（5,943）**：✅ 经 macOS `textutil -convert docx` 离线转换（5,927 个成功，1 个同名合并；`/Volumes/HDD/law/converted/`）→ openlaw-scan 散文件盘点 → 批量导入。溯源线索：converted_report.csv 的 zip 列=`(散文件)`；原始 .doc 仍保留于 extracted/ 供追溯。
- **601 重复**：导入时按 scan_report 的 sha256 去重（skip_dup_report 601 条）。
- 全部条目 `confirm_status=pending`，人工确认后生效（保真度要求不变）。

## 批量导入结果（openlaw-batch 全量实跑，2026-10-24）

### docx 批次（scan_report.csv → batch_report_docx.csv）

| 项 | 数值 |
|---|---|
| 入库文档 | 20,597（首轮 20,518 + 区间前移重链修复后 79） |
| 报告内 sha 重复跳过 | 601 |
| docx 无文本跳过 | 73 |
| 文件名无日期跳过 | 3,312（flk 文件名 `名称_.docx` 无日期段，须人工补元数据） |
| .doc 跳过（转转换批） | 5,944 |
| 无条文标记失败 | 1,088（决定/决议/名单类，无「第X条」结构） |

### .doc 转换批（converted_report.csv → batch_report_converted.csv）

| 项 | 数值 |
|---|---|
| 入库文档 | 5,100（首轮 4,643 + 重链修复后 457） |
| 文件名无日期跳过 | 624 |
| 无条文标记失败 | 203 |

### 数据库合计（flk 语料，全部 pending）

| 指标 | 数值 |
|---|---|
| law | 18,634 |
| document | 25,697 |
| article | 1,042,266（FTS5 已同步） |
| 已闭合历史版本（版本链） | 7,063 |

版本冲突消解：.doc 与 .docx 分两批到达导致 536 个「中间版缺失」区间冲突，由 **区间前移重链**（AutoExpirePrior，expire 前移 + import_log 留痕）自动消解；同生效日真冲突仍拒绝转人工。

失败清单（转人工，绝不静默假入库）：无条文标记 1,291 / 文件名无日期 3,936 / docx 无文本 73。
报告：`/Volumes/HDD/law/batch_report_docx.csv`、`batch_report_converted.csv`（doc_id,law_id,name,date,status,articles,detail），按 `status=fail` 过滤即得人工队列。
