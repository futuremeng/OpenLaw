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
    openlaw-ingest/   # CLI：PDF → 哈希 → 溯源 → 入库 → 断链检测
    openlaw-fetch/    # CLI：显式触发的抓取（白名单/HTTPS/大小限制 → 暂存区 → 人工确认）
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

## 项目边界（不做的事）

不自动判定法规废止与修改关系 · 不输出胜诉概率 · 不生成未经核验的案例 · 不提供云端多租户服务 · 不内置任何未授权抓取的法规全文（数据由用户自行导入）· 不替代律师和编辑的专业判断

## 许可证与文档

- 代码：Apache-2.0（见 [LICENSE](LICENSE)）
- 免责声明：[DISCLAIMER.md](DISCLAIMER.md) · 贡献指南：[CONTRIBUTING.md](CONTRIBUTING.md)
- 架构：[docs/architecture.md](docs/architecture.md) · 数据字典：[docs/data-dictionary.md](docs/data-dictionary.md) · 接口：[docs/api.md](docs/api.md) · 发布：[docs/release-process.md](docs/release-process.md)
