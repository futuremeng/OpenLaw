# 接口文档（SDK）

调用方式：**进程内 SDK**（`import github.com/futuremeng/OpenLaw/sdk`），不启动任何网络服务（开发文档 5.1）。如需跨进程，后续版本提供本机 IPC/HTTP 且仅监听回环地址。

## 1. 初始化

```go
law, err := sdk.New("./data") // data 目录：db/ pdf/ md/ provenance/ manifest/
defer law.Close()
```

**上层应用（OpenCourt 等消费方）必须用只读入口**——`sdk.New` 是写入路径（会跑
schema 迁移链），用它打开他人语料属于越权（边界纪律：谁生产语料谁独占写权）：

```go
law, err := sdk.OpenReadOnly("./data") // query_only、不迁移、不写 meta
```

只读入口对 `meta.schema_version` 做版本协商：不在 `[sdk.SchemaMin, sdk.SchemaMax]`
范围内直接报错（错误文案给出两条出路：升级应用 / 换兼容语料包）。

## 2. 通用返回结构

```go
type Result struct {
    Status    Status         // 五态，禁止字符串模糊匹配
    Data      map[string]any // 各接口数据体
    Trace     *Trace         // doc_id + pdf_path + chain_ok + break_reason
    Warnings  []string
    ErrorCode string         // OL-xxxx
}
```

`Status`：`ok` / `expired` / `broken_chain` / `not_found` / `need_manual`。
所有对外输出必须携带 doc_id（Trace），无法携带的禁止展示。

## 3. 接口清单

### ResolveVersion(lawName, atDate string) Result
按时间点解析当时生效版本（左闭右开）。
Data：version_id、version_no、effective_date、expire_date、confirm_status。
错误：OL-1001（名称不存在）、OL-1002（该日期无生效版本）；断链 → broken_chain。
confirm_status=pending → ok + 人工确认提示（仅提示不报错）。

### CheckCitation(lawName, articleNo, quote, atDate string) Result
入参校验链：名称 → 时间点版本 → 条款号 → 文本相似度 → 效力 → 溯源链。
Data：hit、similarity（0~1，互相包含=1.0，否则字符二元组 Dice）、expired、content（原文）、version_no、current_version、confirm_status。
错误：OL-1001/1002/1003；OL-1004（相似度 < 0.60，返回原文供人工比对）；OL-1005（expired + current_version）；溯源失败 → broken_chain + OL-1006/OL-1007（禁止输出判错结论）。

### AnalyzeElements(caseType string, facts []string) Result
Data：covered[]、missing_elements[]（name + how_to_attack）。
输出为风险假设清单，附「不构成胜诉/败诉结论」警告。案由无要件 → not_found（提示先人工导入）。

### FindRelation(lawID string) Result
Data：relations[]（src/dst/type/based_on_doc_id/confirm_status）。
pending 关系附人工确认警告。

### DetectBreak(articleID string) Result
Data：chain_ok、breaks[]（step/reason/error_code）。
环节：version → document → file → hash → manifest → signature。

### Desensitize(text string, extraNames ...string) Result
Data：text（脱敏后）、restore_map（本地还原映射，**不得写入日志**）。
覆盖：显式名单姓名、身份证号、案号、企业名称、角色上下文人名。

### Search(query string, topK int) Result
Data：query、hits[]（article_id/article_no/doc_id/law_name/version_id/version_no/
effective_date/expire_date/confirm_status/lang/snippet）。
FTS5 优先，零命中 LIKE 兜底；中文多词（空格分词）走多词 AND 路径
（unicode61 不切 CJK，FTS 短语必失败）；topK 上限 50。
零命中不是错误：status=ok + warning。pending 命中附人工确认警告（12.3）。
OpenCourt LawSourcePlugin.SearchLaw 对接面（expire_date 非空 → AMENDED，
绝不自动判 REPEALED）。

### GetArticle(articleID string) Result
Data：article_id/article_no/content（全文）/doc_id/law_name/version_id/version_no/
effective_date/expire_date/confirm_status/lang；Trace 携带 doc_id/pdf_path。
断链（OL-1006）时 status=broken_chain 但 data 照常完整返回供人工核对。
OpenCourt LawSourcePlugin.GetLawDetail 对接面（lawID = article_id 约定）。

## 4. 错误码（5.4）

| 错误码 | 含义 | 处理建议 |
|---|---|---|
| OL-1001 | 未找到法规 | 提示核对名称与简称 |
| OL-1002 | 指定日期无生效版本 | 提示确认时间范围 |
| OL-1003 | 条款号不存在 | 提示核对条款号与版本 |
| OL-1004 | 引用内容与原文差异大 | 展示原文供人工比对 |
| OL-1005 | 版本已失效 | 返回当前有效版本 |
| OL-1006 | 溯源断链 | 禁止输出结论，提示人工核对 |
| OL-1007 | 哈希校验失败 | 立即告警并隔离文件 |
| OL-1008 | manifest 签名验证失败 | 停止使用数据目录 |
| OL-1009 | OCR 置信度过低 | 转人工确认（OCR 待接入） |

提示文案常量见 `sdk.ErrorHints`。

## 5. OpenCourt 结果处理规则（12.3）

- 命中且一致 → 标记通过
- 命中但差异大 → 提示可能引用错误并展示原文
- 版本失效 → 提示并给出当前有效版本
- 断链/无溯源 → 提示无法核实，**禁止输出判错结论**
- 确认状态待人工 → 仅提示不报错
- 模型输出若包含法条，必须由 CheckCitation 校验存在，不通过直接丢弃并重新生成

## 6. 命令行工具

| 命令 | 用途 | 退出码 |
|---|---|---|
| `openlaw-fetch` | 显式触发抓取：策略检查（HTTPS/robots/白名单/禁止名单/大小/Content-Type）→ 暂存区 → import_log；提供 `-md` 或 `-law-id` 时继续导入闭环 | 0 成功 / 1 失败 |
| `openlaw-fetch -list <目录页URL>` | 批量①：抓公报目录页 → `data/incoming/candidates.csv` 候选清单（不下载 PDF，供人工审阅） | 0 / 1 |
| `openlaw-fetch -batch <candidates.csv>` | 批量②：逐条限速下载进暂存区（默认 2s/条），单条失败不中断，全部写 import_log | 0 全部成功 / 1 有失败 |
| `openlaw-compare -a FILE -b FILE` | 跨来源同一法律一致性核验：字节级（SHA-256）+ 文本级（去空白/部首折叠/正文容器提取/句段覆盖）；支持 .pdf/.html/.htm/.md/.txt，`-json` 输出报告；扫描版 PDF（无文本层）报错并给出替代路径（flk 在线浏览页手动存 .txt / 公报单文档 HTML / OCR） | 0 文本一致 / 1 实质差异 / 2 错误 |
| `openlaw-eu -celex <CELEX>` | 欧盟元数据导入（Cellar SPARQL，官方开放入口）：ELI/CELEX/生效+适用日/公报出处/废止与依据关系 → law 主表 upsert + pending 关系（仅挂已入库目标）；原始三元组留证 incoming，限速 2s、import_log 全记 | 0 成功 / 2 错误 |

**数据源策略（18/19 章，2026-10 复核）**：
- `flk.npc.gov.cn`（国家法律法规数据库）robots.txt 全文 `Disallow: /` 且明确「禁止使用任何自动化工具、脚本、爬虫程序采集或复制网站数据」→ **硬禁止名单**，任何配置下不得自动抓取；需要其文本时人工下载放入 `data/incoming/`
- `www.gov.cn/gongbao/`（国务院公报）robots 允许 → 可批量源：目录页 → candidates.csv → 限速下载；整期 PDF 较大（约 30MB），建议 `-timeout 180s`
- `publications.europa.eu`（欧盟出版局 Cellar）robots `Allow: /` → **官方开放的程序化入口**（SPARQL/REST，含 representation 层全文下载，见 `docs/sources-eu-text.md`），可自动化但限流较严（连发即 429，默认 2s 间隔；429 时 fail-closed，稍后重试）；EUR-Lex 网页本身有 WAF 挑战 → 该域名只能人工浏览。详见 `docs/sources-eu.md`
- robots 判定：404/403=无限制；网络错误/5xx=fail-closed 拒绝；最长前缀匹配，同长度 Allow 优先
| `openlaw-ingest` | 导入 PDF+MD → 哈希 → 溯源 → 入库 → 断链检测 | 0 成功 / 1 失败（已回滚） |
| `openlaw-check -gen-key -key <path>` | 生成签名私钥（勿入仓库/数据目录） | 0 / 1 |
| `openlaw-check -build-manifest -key <path>` | 重算哈希生成 manifest 并签名 | 0 / 1 |
| `openlaw-check` | 签名校验 + 断链清单 | 0 完好 / 2 验签失败 / 3 有断链 |
| `openlaw-pack build -pack <dir> -key <path>` | 生成离线更新包（manifest + 签名 + changes.csv） | 0 / 1 |
| `openlaw-pack verify -pack <dir>` | 校验包（签名/哈希/未收录文件） | 0 / 2 校验失败 |
| `openlaw-pack import -pack <dir> -data <dir>` | 先校验后导入；失败拒绝整包保留旧数据 | 0 / 1 |
