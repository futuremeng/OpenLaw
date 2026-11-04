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
按时间点解析当时生效版本（左闭右开）。法名支持全称、简称与别名（OL-4）。
Data：version_id、version_no、effective_date、expire_date、confirm_status、effect_status、
law_id、law_name、resolved_from（命中依据：full_name/short_name/alias/name_rule）。
错误：OL-1001（名称不存在）、OL-1002（该日期无生效版本）；断链 → broken_chain。
confirm_status=pending → ok + 人工确认提示（仅提示不报错）。
多候选（同名多法、别名指向多法）且时间点无法唯一确定 → `need_manual` + OL-1010 +
`Data.candidates[]`（并列交人工，绝不自动采信任何一条）；若同名多法在所给日期只有一部
有生效版本，则按时间点收敛为一条并在 warnings 明示「同名若干部，已按日期唯一确定」。

### ResolveLawName(lawName string) Result
法名 → 语料内法规解析（OL-4），供上层做名称规范化与消歧。解析顺序（逐级放宽，命中即停）：
`law.name` 精确 → `law.short_name` 精确 → `law_alias` 别名表 → 国名省略式名称规则
（`中华人民共和国X` ↔ `X`，规则号 `name-prefix(R1)`）。
Data：query、normalized、candidates[]（law_id/name/issuer/resolved_from/alias_source/
based_on_article_id）、alias_table_missing（语料早于 v9 时为 true）。
唯一命中 → ok + 依据进 warnings；多候选 → need_manual + OL-1010；未命中 → OL-1001。
别名只来自语料内可溯源信息（简称列登记、官方文本「以下简称」条款并附出处条文号），
**不由模型生成**。

### CheckCitation(lawName, articleNo, quote, atDate string) Result
入参校验链：名称（含简称/别名，OL-4）→ 时间点版本 → 条款号 → 文本相似度 → 效力 → 溯源链。
Data：hit、similarity（0~1，互相包含=1.0，否则字符二元组 Dice）、expired、content（原文）、version_no、current_version、confirm_status；按简称/别名命中时另附 resolved_from、alias_source、alias_article_id（凭什么认定这部法，必须可见）。
错误：OL-1001/1002/1003；OL-1004（相似度 < 0.60，返回原文供人工比对）；OL-1005（expired + current_version）；OL-1010（法名多候选，`Data.candidates[]` 交人工）；溯源失败 → broken_chain + OL-1006/OL-1007（禁止输出判错结论）。

### ListElements(caseType string) Result
按案由列出**已复核确认**的构成要件（OL-3），供上层作为封闭候选集（如 OpenCourt 的
`ListCaseElements`）。只读、不裁决：待复核（pending）要件一律不进入候选。
Data：case_type、elements[]（element_id/name）。
语义区分：案由未在要件库登记 → `not_found`；已登记但无已确认要件 → `ok` + 空表 +
「尚未复核确认」提示（旧版本语料未记录确认状态时提示为「全部按待复核处理」）。


### AnalyzeElements(caseType string, facts []string) Result
Data：covered[]、missing_elements[]（name + how_to_attack + confirm_status）。
输出为风险假设清单，附「不构成胜诉/败诉结论」警告。案由无要件 → not_found（提示先人工导入）。
未确认要件（confirm_status≠confirmed）另附「待人工确认」警告（OL-2：导入要件一律
pending，人工确认后才视为定论；导入经 `openlaw-element`，见 §6）。

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
effective_date/expire_date/confirm_status/effect_status/lang/snippet）。
FTS5 优先，零命中 LIKE 兜底；中文多词（空格分词）走多词 AND 路径
（unicode61 不切 CJK，FTS 短语必失败）；topK 上限 50。
零命中不是错误：status=ok + warning。pending 命中附人工确认警告（12.3）。
OpenCourt LawSourcePlugin.SearchLaw 对接面。**效力判定已在语料层完成**（OL-1）：
上层直接消费 effect_status，禁止再由 expire_date 自行推导。

### GetArticle(articleID string) Result
Data：article_id/article_no/content（全文）/doc_id/law_name/version_id/version_no/
effective_date/expire_date/confirm_status/effect_status/lang；Trace 携带 doc_id/pdf_path。
断链（OL-1006）时 status=broken_chain 但 data 照常完整返回供人工核对。
OpenCourt LawSourcePlugin.GetLawDetail 对接面（lawID = article_id 约定）。

### effect_status（OL-1 效力状态字段）
`Search` 每条命中、`GetArticle`、`ResolveVersion` 的 Data 均携带，取值：

| 值 | 判定依据 |
|---|---|
| `EFFECTIVE` | 版本区间覆盖当下，且无废止/接续关系证据 |
| `AMENDED` | 被后续版本接续（expire_date 已到期）且有 `confirmed` 的修改关系 |
| `REPEALED` | 存在 `confirmed` 的废止关系（relation 为 pending 时绝不出现） |
| `REPEALED_PENDING` | 存在废止/接续关系但未经人工确认 |
| `UNKNOWN` | 版本链不完整或关系缺失（判不出不猜） |

判定只依据 `law_version` 链 + `relation`，且仅 `confirm_status=confirmed` 的关系参与
裁决（10 章纪律：机器不自动判定废止）。effective_date/expire_date 原字段保留不变。

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
| OL-1010 | 法名对应多部法规且时间点无法唯一确定 | 并列 `candidates[]` 交人工选择，禁止自动采信 |

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
| `openlaw-element import -in <csv\|json> [-apply]` | 构成要件导入（OL-2）：默认 dry-run 计数；article_id 关联不到条文的行如实拒绝并报告；导入一律 pending（机器不裁决） | 0 / 1 |
| `openlaw-element confirm -ids <id,…> -verdict confirm\|reject -reviewer <署名> -reason <依据>` | 要件人工落判：署名 + 依据必填，流转与复核记录原子写入 | 0 / 1 |
| `openlaw-element stats` | 按案由统计要件数与确认状态 | 0 / 1 |
| `openlaw-alias [-apply] [-csv 报告.csv] [-stats] [-lookup 法名]` | 法名别名表构建与巡检（OL-4）：默认 dry-run 只出「将会」计数；`-apply` 才写库；只收录语料内可溯源别名（简称列登记 + 官方「以下简称」条款，附出处条文），不生成别名 | 0 / 1 |

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
| `openlaw-pack keygen -key <path>` | 生成 ed25519 签名密钥对，打印待内置的受信公钥（私钥勿入仓库/数据目录） | 0 / 1 |
| `openlaw-pack verify -pack <dir>` | 校验包（签名/哈希/未收录文件），并输出来源信任结论 | 0 / 2 校验失败 |
| `openlaw-pack import -pack <dir> -data <dir>` | 先校验（完整性 + 公钥命中受信发布者名单）后导入；任一失败拒绝整包保留旧数据 | 0 / 1 |
