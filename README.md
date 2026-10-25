# OpenLaw

本地化的**可溯源法律知识底座**。向上层应用（出版审校、文本纠错、智能庭审模拟、案件推演、证据缺口分析等）提供版本解析、条文校验、要件比对与溯源能力，**不承担最终法律判断**。

> ⚖️ 输出为辅助参考，不构成法律意见。详见 [DISCLAIMER.md](DISCLAIMER.md)。

## 核心原则

- **可溯源**：任一输出结论必须能反查到原始 PDF；溯源链任一环节失败显式返回断链（OL-1006），绝不静默通过
- **默认离线**：不联网、不上传案件材料；数据目录整体可拷贝迁移，不依赖云服务
- **机器不裁决**：不自动判定法规废止与修改关系，一律标记待人工确认；不输出胜诉概率
- **代码与数据分离**：`data/` 可整体迁移，永不提交仓库

## 技术栈（对齐 OpenCourt）

| 项 | 说明 |
|---|---|
| 语言 | Go 1.27，纯 Go 无 CGO |
| 存储 | SQLite（`modernc.org/sqlite`，内置 FTS5），点查/全文检索/时间回溯/关系查询 |
| 形态 | **进程内 SDK**（上层直接 import，不默认暴露 HTTP 服务）+ 命令行工具 |
| 防篡改 | SHA-256 + 每日 manifest + Ed25519 签名（私钥独立存储，不进仓库） |

OpenCourt 通过 `import github.com/futuremeng/OpenLaw/sdk` 进程内调用，禁止直连数据库或让模型自行生成法条（开发文档 12.1）。

## 四层架构（单向数据流，禁止反向写回）

```
原始层  data/pdf + data/provenance   ← 只读证据基准
  ↓
文本层  data/md                      ← 解析正文，条文携带版本/日期锚点
  ↓
索引层  data/db (SQLite + FTS5)      ← 点查、检索、时间回溯、关系
  ↓
能力层  sdk/ validate/ search/ …     ← 版本解析、引用校验、断链检测
```

## 目录结构

```text
OpenLaw/
  sdk/          # 进程内 SDK（OpenCourt 对接面：Result/Trace/错误码）
  store/        # schema.sql（全量 DDL + FTS5）、db、migrate
  ingest/       # 哈希、溯源 JSON、导入器（事务 + 回滚）
  parse/        # 条文拆分、日期标准化（左闭右开区间）
  validate/     # 六环节断链检测、manifest 签名、引用校验
  search/       # FTS5 检索（向量混合检索为后续）
  element/      # 构成要件加载/比对、脱敏
  cmd/
    openlaw-ingest/   # CLI：PDF → 哈希 → 溯源 → 文本层提取/拆分 → 入库 → 断链检测
    openlaw-fetch/    # CLI：显式触发的抓取（robots/白名单/大小限制 → 暂存区 → 人工确认；含公报批量两阶段模式）
    openlaw-compare/  # CLI：跨来源同一法律文件一致性核验（字节级 + 文本级双重复核）
    openlaw-pack/     # CLI：离线更新包 build / verify / import（失败保留旧数据）
    openlaw-scan/     # CLI：存量批量下载包盘点（zip/GBK 解码/SHA-256/文本探测/CSV 报告）
    openlaw-batch/    # CLI：从盘点报告批量导入存量（docx 正文提取/三层去重/版本链自动接续）
    openlaw-confirm/  # CLI：pending 语料人工确认（风险抽样/清单落判/条件批量，全程 review_log 审计）
    openlaw-level/    # CLI：flk 存量效力等级保守名称规则补标（默认 dry-run，-apply 写库留痕）
    openlaw-check/    # 每日哈希重算 + 签名校验 + 断链清单
  data/         # pdf/ md/ db/ provenance/ logs/ manifest/ incoming/（gitignore）
  testdata/     # 样例法规与测试数据
  tests/acceptance/  # 端到端验收：导入 → 校验 → 篡改 → 断链
```

## 快速开始

```bash
# 前置：Go 1.22+（国内网络建议 GOPROXY=https://goproxy.cn,direct）
make test      # 全部测试（解析、日期边界、断链、哈希失败、脱敏、抓取策略）
make lint      # go vet + gofmt 门禁

# 抓取（显式触发，默认离线；只进暂存区，转人工确认）
go run ./cmd/openlaw-fetch -data data \
  -url https://www.npc.gov.cn/.../law.pdf -doc-id law_company_2018 \
  -allow-hosts www.npc.gov.cn

# 导入一份样例法规（跑通最小闭环）
mkdir -p data
go run ./cmd/openlaw-ingest -data data \
  -pdf testdata/fake_law.pdf -md testdata/law_company_2018.md \
  -doc-id law_company_2018 -law-id law_company -law-name 中华人民共和国公司法 \
  -version 2018修正 -effective 2018-10-26 -source-type 测试样例

# 每日校验：manifest 签名 + 断链清单
go run ./cmd/openlaw-check -data data -build-manifest -key ./keys/openlaw.key

# 欧盟法律元数据（Cellar SPARQL 官方开放入口；全文须人工保存 EUR-Lex）
go run ./cmd/openlaw-eu -data data -celex 32016R0679 -name 欧盟通用数据保护条例 -short-name GDPR
```

## SDK 调用示例（OpenCourt 侧）

```go
import "github.com/futuremeng/OpenLaw/sdk"

law, _ := sdk.New("./data")
res := law.CheckCitation("中华人民共和国公司法", "第二十六条",
    "公司股东滥用公司法人独立地位和股东有限责任……", "2018-05-01")

switch res.Status {
case sdk.StatusOK:        // 命中且一致
case sdk.StatusExpired:   // 版本已失效，给出当前有效版本
case sdk.StatusBrokenChain: // 无法溯源，禁止输出判错结论
}
```

## 来源合规边界

- **flk.npc.gov.cn（国家法律法规数据库）**：robots.txt 明确「禁止使用任何自动化工具、脚本、爬虫程序采集或复制网站数据」，代码层已列入硬禁止名单，**任何工具/脚本/浏览器自动化（含 RPA、宏、computer-use）驱动均不得访问该站**。
- **人工浏览器操作不在此限**：用户本人在浏览器中使用该站**自带的**检索与批量下载功能（检索页勾选下载、翻页），属网站设计用途内的手动使用；法律法规本身为公有领域作品（著作权法第 5 条），本地建库使用合法。下载文件放入 `data/incoming/` 后走正常导入闭环（溯源必填、pending 转人工、扫描件 OL-1009 转 OCR）。
- 礼貌约束：即便手动，也应按类别/年份分批、控制频次，不要连续高频连点。
- **共享**：法律法规官方文本为公有领域作品，下载后共享/分发合法（前提：获取方式正当）。推荐以签名更新包（`openlaw-pack build/verify/import`）形式共享，接收方可独立验证哈希与签名，防篡改防版本混乱；共享自有 schema 的客观事实字段（法名/日期/效力状态）无碍，但勿整套镜像来源方的数据库编排结构（汇编权益）。
- **欧盟（EUR-Lex/Cellar）**：EUR-Lex 网页有 WAF 挑战，只能人工浏览；但 Cellar（publications.europa.eu）是官方开放的程序化入口——SPARQL 元数据 + representation 层全文（expression→manifestation→item 四步链，GDPR 全文 XML 实测可下载，2026-10-25）均可自动化，须保守限速。EU 数据重使用须署名（© European Union）。详见 `docs/sources-eu.md`（元数据核实）与 `docs/sources-eu-text.md`（全文通道设计草案）。

## 项目边界（不做的事）

不自动判定法规废止与修改关系 · 不输出胜诉概率 · 不生成未经核验的案例 · 不提供云端多租户服务 · 不内置任何未授权抓取的法规全文（数据由用户自行导入）· 不替代律师和编辑的专业判断

## 许可证与文档

- 代码：Apache-2.0（见 [LICENSE](LICENSE)）
- 免责声明：[DISCLAIMER.md](DISCLAIMER.md) · 贡献指南：[CONTRIBUTING.md](CONTRIBUTING.md)
- 架构：[docs/architecture.md](docs/architecture.md) · 数据字典：[docs/data-dictionary.md](docs/data-dictionary.md) · 接口：[docs/api.md](docs/api.md) · 发布：[docs/release-process.md](docs/release-process.md)
