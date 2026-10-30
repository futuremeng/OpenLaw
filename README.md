# OpenLaw

本地化的**可溯源法律知识底座**。向上层应用（出版审校、文本纠错、智能庭审模拟、案件推演、证据缺口分析等）提供版本解析、条文校验、要件比对与溯源能力，**不承担最终法律判断**。

> ⚖️ 输出为辅助参考，不构成法律意见。详见 [DISCLAIMER.md](DISCLAIMER.md)。

## 核心原则

- **可溯源**：任一输出结论必须能反查到原始 PDF；溯源链任一环节失败显式返回断链（OL-1006），绝不静默通过
- **默认离线**：不联网、不上传案件材料；数据目录整体可拷贝迁移，不依赖云服务
  （定位功能例外：坐标取自系统定位服务，需系统授权，macOS 可能经由其 Wi-Fi 定位
  数据库；把坐标判定成地区与全部存储都在本机）
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

三项目各自可独立分发、通常一并安装，因此语料纪律是：**代码各自分发，语料只有一份；
本仓库是法规语料的唯一写入者（导入/确认/关系判定/schema 迁移），消费方一律只读打开**。
消费方不得触发迁移、不得改写 `meta`（详见 OpenCourt `docs/integration-model.md`；
本仓库侧待办 OL-7 只读入口、OL-8 版本协商见 [docs/tasks-opencourt-bridge.md](docs/tasks-opencourt-bridge.md)）。

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
  parse/        # 条文拆分、日期标准化（左闭右开区间）、效力等级与地域保守名称推导
  geo/          # 内置行政区划词表（34 省级 + 346 地级 + 近似中心点）与离线就近判定
  validate/     # 六环节断链检测、manifest 签名、引用校验
  search/       # FTS5 检索（向量混合检索为后续）
  element/      # 构成要件加载/比对、脱敏
  cmd/
    openlaw-ingest/   # CLI：PDF → 哈希 → 溯源 → 文本层提取/拆分 → 入库 → 断链检测
    openlaw-fetch/    # CLI：显式触发的抓取（robots/白名单/大小限制 → 暂存区 → 人工确认；含公报批量两阶段模式）
    openlaw-compare/  # CLI：跨来源同一法律文件一致性核验（字节级 + 文本级双重复核）
    openlaw-pack/     # CLI：离线更新包 keygen / build / verify / import（完整性 + 来源信任双门禁，失败保留旧数据）
    openlaw-scan/     # CLI：存量批量下载包盘点（zip/GBK 解码/SHA-256/文本探测/CSV 报告）
    openlaw-batch/    # CLI：从盘点报告批量导入存量（docx 正文提取/三层去重/版本链自动接续）
    openlaw-confirm/  # CLI：pending 语料人工确认（风险抽样/清单落判/条件批量，全程 review_log 审计）
    openlaw-level/    # CLI：flk 存量效力等级保守名称规则补标（默认 dry-run，-apply 写库留痕）
    openlaw-region/   # CLI：法规地域标引（名称/机关保守推导；默认 dry-run 出分类报告，-apply 只写高置信，-undo 按批次撤销，-check 一致性自检只报告，-stats/-review/-bulk 人工落判）
    openlaw-eu-text/  # CLI：欧盟全文导入（Cellar representation 层 FORMEX XML 结构化条文，默认 dry-run）
    openlaw-govcn/    # CLI：gov.cn 官方法规库导入（行政法规库 + 国家规章库；人工清单 + 详情页合规抓取 + TRS 正文解析，默认离线）
    openlaw-govlist/  # CLI：国家规章库清单浏览采集（经本机 BrowserOS neo 人在环代理浏览官方列表页；默认离线、回环硬校验、限速护栏）
    openlaw-check/    # 每日哈希重算 + 签名校验 + 断链清单
  desktop/      # 桌面客户端「开源法律法规数据库」（Wails v2；公众模式：查法规/
                #   按目录浏览；专业模式：库总览/法规检索+修订沿革+引用核验/
                #   人工复核/地域标引/获取最新法规（原「数据采集与更新」；文案、渐进披露与
                #   界面结构遵守 docs/design-ui-language.md：每屏一个页头（处境一句话 + 主行动）、
                #   侧栏导航挂实时计数、法规构成用占比条给形状、深浅主题同一套令牌，
                #   主视图不出现 LIKE 通配符、命令行参数、ISO UTC 时间戳与 schema 版本号）；
                #   「地域标引」面板把机器建议落判为
                #   人工结论（确认/改选地区/驳回/清除，一律署名+结论依据，批量先干跑计数）；
                #   地点定位为离线就近判定 + 手动选择 +
                #   试判定，真实定位需打包 .app（见 packaging/），法规按库中区划码过滤与显示
                #   （未标注的按名称推定并如实显示「地域未标注（名称推定…）」）；
                #   与 CLI 同一代码路径；仅此壳需 CGO，
                #   核心库仍纯 Go。
                #   go build -tags desktop,production -o dist/openlaw-desktop ./desktop）
  packaging/    # macOS .app 打包：Info.plist（定位用途声明等）+ build-app.sh
                #   （组装 bundle、ad-hoc 签名、plutil/codesign 自检）
  data/         # pdf/ md/ db/ backups/ provenance/ logs/ manifest/ incoming/（gitignore）
  testdata/     # 样例法规与测试数据
  tests/acceptance/  # 端到端验收：导入 → 校验 → 篡改 → 断链
```

## 快速开始

> 部署与运维（二进制构建、数据目录迁移、签名私钥、OpenCourt 对接）见 docs/deploy.md。

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

> ⚠️ 上层应用打开语料**必须走只读入口**（OL-7 `sdk.OpenReadOnly`，尚未实现）。
> 下例的 `sdk.New` 是写入路径——它会执行 schema 迁移并回写 `meta.schema_version`，
> 只供本仓库自己的 CLI（ingest/confirm/pack/check）使用；消费方用它属于越权（隐患 H1/H2）。

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
- **人在环代理浏览（仅限 gov.cn 官方列表页）**：OpenCourt 内置智能体经 BrowserOS neo 驱动用户自己的真实浏览器，在 www.gov.cn 主站 robots 允许的官方列表页（行政法规库/国家规章库）点官方 UI 翻页浏览并收集清单（起点白名单、限速 ≥2s/页、审计留痕；见 OpenCourt `neutral/govlistassist.go`）——与人工浏览+导出等价，适用于本条；**绝不**程序化访问 sousuo/sousuoht 检索域或逆向 Athena 鉴权；**flk 等硬禁止站不适用本条**（其 robots 禁止一切自动化，含浏览器代理）。
- 礼貌约束：即便手动，也应按类别/年份分批、控制频次，不要连续高频连点。
- **共享**：法律法规官方文本为公有领域作品，下载后共享/分发合法（前提：获取方式正当）。推荐以签名更新包（`openlaw-pack build/verify/import`）形式共享，接收方可独立验证哈希与签名，防篡改防版本混乱；共享自有 schema 的客观事实字段（法名/日期/效力状态）无碍，但勿整套镜像来源方的数据库编排结构（汇编权益）。
- **欧盟（EUR-Lex/Cellar）**：EUR-Lex 网页有 WAF 挑战，只能人工浏览；但 Cellar（publications.europa.eu）是官方开放的程序化入口——SPARQL 元数据 + representation 层全文（expression→manifestation→item 四步链，GDPR 全文 XML 实测可下载，2026-10-25）均可自动化，须保守限速。EU 数据重使用须署名（© European Union）。详见 `docs/sources-eu.md`（元数据核实）与 `docs/sources-eu-text.md`（全文通道设计草案）。
- **国家行政法规库（www.gov.cn/zhengce/xzfgk/）与国家规章库（www.gov.cn/zhengce/xxgk/gjgzk/）**：主站 robots 允许抓详情页（`/zhengce/` 不在 Disallow，站点并以 sitemap 欢迎机器索引），但检索/清单入口不得程序发现——sousuo.www.gov.cn robots `Disallow: /`，规章库 Athena 接口（sousuoht）另带 RSA 鉴权（**不得逆向**）。清单由用户浏览器浏览时经 `tools/govcn-xzfgk-export.user.js` / `tools/govgz-export.user.js` 导出（只整理已浏览页面），`openlaw-govcn -source xzfgk|gzk` 按清单合规抓取（限速 2s/页起、主机锁、import_log、HTML 留证、pending 人工闸口）。规章库条目按清单分类页签定 `部门规章`/`地方政府规章`（缺分类按发布机关保守归类，皆缺转人工）。法规/规章原文公有领域（著作权法第 5 条），不镜像站点汇编。详见 `docs/sources-govcn.md`。

## 项目边界（不做的事）

不自动判定法规废止与修改关系 · 不输出胜诉概率 · 不生成未经核验的案例 · 不提供云端多租户服务 · 不内置任何未授权抓取的法规全文（数据由用户自行导入）· 不替代律师和编辑的专业判断

## 许可证与文档

- 代码：Apache-2.0（见 [LICENSE](LICENSE)）
- 语料数据：[DATA-LICENSE.md](DATA-LICENSE.md)（法条原文公有领域；本项目汇编产物 CC BY 4.0；欧盟部分保留 © European Union 署名）；每个 lawdata 发布包内附副本
- 第三方依赖清单：[THIRD-PARTY-NOTICES.txt](THIRD-PARTY-NOTICES.txt)
- 免责声明：[DISCLAIMER.md](DISCLAIMER.md) · 贡献指南：[CONTRIBUTING.md](CONTRIBUTING.md) · 协作者与 AI 代理约束：[AGENTS.md](AGENTS.md)
- 架构：[docs/architecture.md](docs/architecture.md) · 数据字典：[docs/data-dictionary.md](docs/data-dictionary.md) · 接口：[docs/api.md](docs/api.md) · 界面语言与渐进披露：[docs/design-ui-language.md](docs/design-ui-language.md) · 发布：[docs/release-process.md](docs/release-process.md) · OpenCourt 对接桥待办：[docs/tasks-opencourt-bridge.md](docs/tasks-opencourt-bridge.md) · 地域标引写入设计（已批准 · 第 1–4 步已实施）：[docs/design-region-write.md](docs/design-region-write.md)
