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

### docx 批次（scan_report.csv → batch_report_docx.csv，终态）

| 项 | 数值 |
|---|---|
| 入库文档 | 23,722（文件名日期 20,597 + 正文推断 ok_inferred 3,125） |
| 报告内 sha 重复跳过 | 601 |
| docx 无文本跳过 | 73 |
| 日期无法推断跳过 | 44 |
| .doc 跳过（转转换批） | 5,944 |
| 无条文标记失败 | 1,231（决定/决议/名单类，无「第X条」结构） |

### .doc 转换批（converted_report.csv → batch_report_converted.csv，终态）

| 项 | 数值 |
|---|---|
| 入库文档 | 5,686（文件名日期 5,100 + 正文推断 ok_inferred 586） |
| 日期无法推断跳过 | 19 |
| 无条文标记失败 | 222 |

### 数据库合计（flk 语料，全部 pending，2026-10-24 终态）

| 指标 | 数值 |
|---|---|
| law | 22,345 |
| document | 29,408 |
| article | 1,183,918（FTS5 已同步） |
| 已闭合历史版本（版本链） | 7,063 |

完整性审计：全库 0 区间重叠、0 空区间、无未来生效日期异常。

版本冲突消解：.doc 与 .docx 分两批到达导致 536 个「中间版缺失」区间冲突，由 **区间前移重链**（AutoExpirePrior，expire 前移 + import_log 留痕）自动消解；同生效日真冲突仍拒绝转人工。

### 日期推断通道（parse.InferDates，2026-10-24）

文件名无日期段（`名称_.docx`）的存量，从正文落款保守推断公布日/生效日：

- 生效日：日期后紧跟「(起)施行/生效/执行」；公布日：日期前后窗口内「通过/批准/公布/发布/签署/修订/修正」；均取文末命中（落款位置）；
- 推断值写入溯源 JSON `effective_date_inferred=true`，batch 报告状态 `ok_inferred`；**批量通道日期一律 inferred**（文件名日期同样打标）；
- 无命中 → 仍 skip_no_date 转人工，绝不猜。

结果：docx 批 3,936 无日期 → 3,125 推断入库（44 仍无命中）；转换批 624 → 586 推断入库（19 仍无命中）。

### 人工队列终态（绝不静默假入库）

| 队列 | 数量 | 处置建议 |
|---|---|---|
| 无条文标记（决定/决议/名单类） | 1,453 | 定夺入库形态（全文即一节 or 维持人工） |
| 日期无法推断 | 63 | 人工补元数据 |
| docx 无文本 | 73 | 转 OCR 或人工 |

报告：`/Volumes/HDD/law/batch_report_docx.csv`、`batch_report_converted.csv`（doc_id,law_id,name,date,status,articles,detail），按 `status=fail` 过滤即得人工队列。
