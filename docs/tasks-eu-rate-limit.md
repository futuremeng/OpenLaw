# EU 通道限流与配额任务清单（独立编排）

> 从 EU 全文通道（docs/sources-eu-text.md）拆出的限流相关任务，供单独排期执行。
> 各任务相互独立处已标注；依赖关系见「编排建议」。

## 背景事实（2026-10-25 实网取证，勿凭记忆改动）

1. **双层限流**：Azure Front Door（`op.europa.eu`，robots.txt 所在域，响应头 `X-Azure-Ref`）与 nginx（`publications.europa.eu`，资源 URI）各自独立限流；429 响应**不带 Retry-After**。
2. **配额按客户端指纹分桶**：同 IP 上 curl 桶与 Go 桶行为不同（curl 间歇 200 时 Go 持续 429，反之亦然）。探测窗口时**必须用与正式抓取相同的客户端指纹**，否则误判。
3. **TLS 指纹误伤已修复**（commit `f8cbfab`）：Go 1.24+ ClientHello 默认带 X25519MLKEM768 后量子 key share → JA3 异常 → AFD 按机器人持续 429；`Cellar.policy()` 已限定曲线偏好（X25519/P-256），UA 保持如实标识。
4. **当日冷却实测**：探测阶段大量请求后，Go 指纹桶 6 轮 ×300s 退避未恢复（小时级冷却）；SPARQL 通道 10-24 全通、10-25 起 robots 步即 429——配额按天波动。
5. 全部访问已留痕 `import_log`（`[cellar]`/`[cellar-text]` 前缀 + http_status/content_type），是配额统计的现成数据源。

## 任务卡

### RL-1 GDPR M1 pilot 重试（P0，短窗口可完成）
- **依赖**：仅配额恢复（RL-2 判定）。
- **步骤**：
  ```
  openlaw-eu-text -data data -celex 32016R0679 -lang eng -dry-run   # 期望 99 条 Article
  openlaw-eu-text -data data -celex 32016R0679 -lang eng            # 入库
  ```
- **验收**（docs/sources-eu-text.md 第五节）：`doc_id=eu_32016r0679_eng`、99 条 `Article N`、`document.lang=eng`、`parser_version=openlaw-parse/0.1.0+formex-eu`、`confirm_status=pending`、FTS 英文条文命中、`incoming/eu_32016R0679_eng_fmx4.xml` 留证 + sha256 复检、import_log 四步链齐全、断链检测 0 项。
- **纪律**：dry-run 与 apply 之间不插入其他 Cellar 请求（省配额）；失败后勿连续重试，回到 RL-2 探测。

### RL-2 配额探测（P1，小工具，RL-1/RL-4 的前置）
- **动机**：避免盲跑浪费配额；探测须与正式抓取同指纹（背景事实 2）。
- **方案**（最小改动）：`cmd/openlaw-eu-text` 加 `-probe` 子模式——只做 robots.txt 获取 + 一次 expression RDF HEAD/GET，打印桶状态与耗时；结果写 import_log（`[cellar-probe]`）。不建 DB 写入路径。
- **验收**：单测（httptest 模拟 200/429）；`-probe` 输出明确「窗口开/关 + 建议下次探测时间」；退出码区分（0=通，3=限流）。
- **规模**：小（半天内）。

### RL-3 抓取内建退避（P1，代码任务，RL-4 前置）
- **动机**：单文档运行遇 429 应自动退避而非立即失败；站点无 Retry-After，需自定策略。
- **方案**：`getCellar` 对 429 做有限次指数退避（60s→120s→240s，最多 3 次；尊重未来可能出现的 Retry-After 头），每次尝试写 import_log；退避期间响应 ctx 取消。批量与单文档共用。
- **验收**：httptest 模拟「429,429,200」序列成功、「429×4」最终失败且日志计数正确；退避可被 context 中断；不引入无上限等待。
- **规模**：小-中（1 天内，含测试）。

### RL-4 M2 批量抓取配额规划（P2，依赖 RL-2/RL-3）
- **动机**：EU in-force 法规全量（估数千部 × 每部 4-6 请求）远超单桶日配额；无规划的批量必然长期 429。
- **方案要点**：
  1. 小样本测速：10 部法规串行跑，记录成功间隔与 429 率 → 估算「安全小时速率」；
  2. 分批窗口：按估算速率切天（如每日 ≤N 部），清单驱动（SPARQL 枚举 CELEX → 本地队列文件）；
  3. **断点续传**：队列文件记录每部状态（done/failed/pending），重跑只处理 pending——doc_id 幂等已由 importer 保证；
  4. **熔断**：连续 ≥5 个 429（退避后仍失败）→ 整批中止，报告剩余 pending，次日再排；
  5. 语言纪律：批量默认 eng 单语，其余语言另行排期（多语言=多 document，配额×24 不可同日完成）。
- **验收**：中断-恢复演练（kill 后重跑不重复下载、不重复入库）；熔断触发演练；批次报告 CSV（每部：celex/状态/条数/sha256/HTTP 计数）。
- **规模**：中（2-4 天）。

### RL-5 限流可观测（P2，与 RL-4 互相支撑）
- **动机**：配额决策要有数据；import_log 已含全部要素，缺聚合视图。
- **方案**：`openlaw-stats eu`（或现有 stats 子命令扩展）：按日 × 主机 × result 聚合 import_log（429 率、成功率、累计字节、各 CELEX 请求数），JSON/表格输出。
- **验收**：对现有 import_log 出正确报表（含 2026-10-25 的 429 爆发段）；单测用合成日志行。
- **规模**：小（半天）。

### RL-6 指纹/域名/robots 巡检（P3，周期性，季度）
- **动机**：三处会随时间漂移——①`publications.europa.eu`→`op.europa.eu` 域名迁移未完成（双域白名单是当前快照）；②Go 升级可能改变默认 TLS 曲线/新增后量子机制，令 `f8cbfab` 的兼容修正失效或多余；③robots.txt 策略可能变更（当前 `User-agent: * Allow: /`）。
- **步骤**：跑 RL-2 probe → 复核 robots.txt 内容 → `go version` 与曲线设置复核 → 更新本文档「背景事实」日期戳。
- **验收**：巡检记录（日期/结论/是否改码）追加到本文档末尾「巡检日志」。
- **规模**：极小（15 分钟/次），但必须周期性。

## 编排建议

```
今天可做：  RL-2（probe 工具）→ RL-1（pilot，探测到窗口即跑）
下个迭代：  RL-3（退避）→ RL-5（报表）→ RL-4（批量规划，M2 主体之前）
常驻：      RL-6（季度巡检）
```

- RL-1 不等待任何代码任务——配额窗口开即可跑（代码已就绪）。
- RL-4 严禁在 RL-3 合入前启动（无退避的批量 = 轰炸）。
- 所有任务共同纪律：UA 如实标识、不冒充浏览器指纹、fail-closed、429 后拉长间隔而非缩短。

## 巡检日志

| 日期 | 执行人 | 结论 |
|---|---|---|
| 2026-10-25 | （实现期） | 基线建立：双域白名单、TLS 曲线修正合入；Go 桶冷却小时级 |
