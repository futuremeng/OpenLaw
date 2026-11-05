# AGENTS.md

本文件约束和指导参与 OpenLaw（开源法律法规数据库）的 AI 代理与协作者。
需求与编号依据见开发文档章节引用（代码注释中的 6.4、16.2、FR-xx 等）；
架构与流程见 docs/architecture.md、docs/release-process.md、docs/tasks-opencourt-bridge.md。

## 产品与术语

- 产品名：**开源法律法规数据库**（侧栏短名「法律法规库」）。公众模式为默认，
  专业模式为可切换的完整形态。
- 用户可见文案一律使用法言法语，禁止技术黑话进入 UI 与 CLI 提示：
  语料→法规库；pending→待复核；confirmed→已复核确认；风险标记→来源提示；
  审计流水→复核记录；落判→复核落判（UI 用「确认/驳回」）；dry-run→试采（不写入）；
  reviewer/reason→复核人/结论依据；doc_id→条目编号。技术标识（JSON 字段、
  列名、错误码 OL-100x）不受此限。
- 界面文案与渐进披露的成文规范见 **docs/design-ui-language.md**（目标语言、渐进披露、
  主视图禁止项、术语替换表、三个页面的信息层级）。新增或改动面板、按钮、提示文案前
  先对齐它；违反它属于设计缺陷，不是文案偏好。

## 诚实展示纪律（最高优先）

- 数据缺失必须如实显示「缺失/未知」并给出补救指引（如无 manifest → 报告缺失 +
  CLI 指引），禁止渲染占位内容或假数据。
- 中断、部分成功、未验证项必须如实保留与标注（partial 报告、已采条目、
  「尚未复核」徽章）；严禁声称"已完整验证"或"不影响现有数据"，除非有对应验证。
- FTS5 检索结果不重排序、不加工，保持语料可复现。

## 架构纪律

- **同一代码路径**：CLI 与桌面端必须调用同一库层 runner（`ingest.RunGovcnBatch`、
  `browser.RunGovListAgent`、`pack.*`、`validate.*`）。桌面端不是第二写路径，
  CLI 只是薄壳。前端不直连数据库、不发网络请求，一切经 `window.go.main.App.*`。
- **写路径从严**：ingest / confirm / pack import / schema 迁移属核心链路；
  机制或配置变化按高风险变更处理，必须验证旧数据到新版本的真实链路，
  不能只验证新版本自身。
- 采集任务经单槽 TaskManager（互斥、context 取消贯穿限速点、历史环形 20 条）。
- 数据库变更：`store/schema.sql` 加法式修改，schema_version 协商（min/max），
  不兼容变更属主版本且必须附迁移脚本（迁移前自动备份，见 release-process.md）。

## 人工复核纪律

- 机器不裁决：新导入条目一律 `confirm_status=pending`；确认/驳回必须由人工
  在库层强制署名（复核人）+ 结论依据，全程写 review_log。
- 批量流转必须先干跑（BulkPreview 计数）再执行。

## 采集与离线纪律

- 默认离线：不主动联网；采集必须由用户显式触发。govlist 通道需 BrowserOS
  环境（未配置时给出指引并拒绝任务）。
- 限速护栏不可绕过（govcn ≥2s/条，govlist ≥2000ms/页）；中断时已获取部分
  如实保留并产出 partial 报告。

## 信任与签名（16.2）

- 签名私钥只存仓库与数据目录之外，只经路径引用，内容不进 UI/日志/包体。
- 导入默认强制来源信任：包公钥必须命中内置受信发布者名单
  （`pack.RegisterTrustedPubkey`，发版时内置，轮换用新 key ID）。
  完整性通过但公钥未受信 = 拒绝导入；`ImportAllowUntrusted` 仅限本地自验。

## 设计技能与外部工具（联网与安装纪律）

默认离线纪律同样约束开发期设计工具，不只是产品运行时：

- 设计判断的唯一依据是 **docs/design-ui-language.md**。外部设计技能的规则集
  服务于营销页，与本产品（数据密集型桌面应用）冲突时**以文档为准**，
  并把立场写进该文档的「设计体检」一节；不得被工具驱动着改界面。
- **Hallmark**：只使用它的静态检查器，且只经用户级全局技能 design-lint
  （`~/.claude/skills/design-lint/`，vendored MIT 检查器，不联网）调用；
  各项目自己的基线放在该项目 `.design-lint/baseline.json`。
  禁止 `npx skills add nutlope/hallmark`（会装进整套 build/redesign 流程）
  与 `/hallmark redesign`——后者替换整套视觉系统，
  `desktop/ui_contract_test.go` 的结构门禁会成片红，且其 Floor 与本产品
  信息密度直接矛盾。
- **Impeccable**：只跑只读命令（`audit` / `critique` / `document` / `extract`）。
  不得跑 `init` / `live` / `generate`：前者启动时向 impeccable.style 拉取
  视觉方向（**关掉 telemetry 也不会停止该请求**），后者驱动浏览器与实时改写。
  安装一律带 `--no-hooks`（其 hook 绕过模型审批执行命令，本仓库有签名私钥纪律）；
  运行环境带 `DO_NOT_TRACK=1`，项目内 `.impeccable/config.json` 固定
  `"telemetry": false`。引擎二进制下载失败时，按该技能自身的降级路径
  直接读取现有上下文继续，**不得反复重试下载**。
- 本仓库**不另立** `PRODUCT.md` / `DESIGN.md`：产品上下文即 AGENTS.md，
  设计上下文即 docs/design-ui-language.md。外部技能要求读这两个文件时，
  直接读这两份，不得新建第三份设计文档与之并存。
- 任何新装的外部技能都要在此处登记它是否联网、是否装 hook、是否会写仓库文件。

## 验证门禁（提交前全过）

```sh
gofmt -l . && go build ./... && go vet ./... && go test ./... -count=1
node --check desktop/frontend/dist/assets/app.js
go build -tags desktop,production -o dist/openlaw-desktop ./desktop
go build -tags desktop,production,devtools -o dist/openlaw-desktop-devtools ./desktop
```

缺 `-tags desktop,production` 会链接 stub 绑定，属构建错误而非可跳过项。
新行为必须带同包测试；测试离线可跑（临时目录语料），不得依赖真实网络或真实语料。
前端 ↔ Go 绑定契约由 `desktop/binding_test.go` 门禁：改动前端 `app().Method(...)`
调用或 `main.App` 导出方法签名后，`go test ./desktop/` 必须仍绿——Wails 在实参个数
不符时只在运行期拒绝调用，而面板加载是一整段 try/catch，一处不符即整块空白。

## 提交与文档

- 提交遵循仓库现有 Conventional Commits 风格（feat/fix/chore/docs + scope），
  原子提交；每个功能提交同步 CHANGELOG.md。
- 阶段进展同步 docs/tasks-opencourt-bridge.md；README.md 的目录说明与桌面描述
  保持最新。文档主语言 zh-CN。
- 基于 Issue 的工作在提交信息中关联 Issue。
