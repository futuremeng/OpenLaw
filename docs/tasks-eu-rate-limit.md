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

### RL-2 配额探测（P1，小工具，RL-1/RL-4 的前置）——✅ 已实现（2026-10-25）
- **实现**：`Cellar.Probe`（ingest/probe.go）+ `openlaw-eu-text -probe`。与正式抓取同指纹链（policy/Transport/UA/robots 缓存）；robots 拒绝时不发起 expression（合规省配额）；结论行写 import_log `[cellar-probe]`；退出码 0=窗口开 / 3=限流 / 2=其他异常（任务编排可直接判）。
- **用法**：
  ```
  openlaw-eu-text -data data -celex 32016R0679 -lang eng -probe   # 0=可跑 pilot；3=限流勿跑
  ```
- **验收**：单测覆盖 200/429(robots)/429(expr)/404/非法输入五场景（httptest，两层状态码独立可控）——全部通过。
- 原方案要点（保留作背景）：避免盲跑浪费配额；探测须与正式抓取同指纹（背景事实 2）。

### RL-3 抓取内建退避（P1，代码任务，RL-4 前置）——✅ 已实现（2026-11-02）
- **实现**：`getCellar` 包装层（ingest/backoff.go）对 429 做指数退避 60s→120s→240s，
  最多 3 次（第 4 次仍 429 即如实失败，单次等待封顶 10 分钟，绝不无上限等待）；
  出现 `Retry-After` 头即遵从（取较大者）。每次尝试照常写 import_log（getCellarOnce
  职责，退避不重复计数）；退避等待点响应 ctx 取消——任务管理器停止即断。
  探测（RL-2）走 `getCellarOnce` 不退避：探测的意义是立刻如实报告窗口状态。
- **验收**：httptest 覆盖「429,429,200」恢复、「429×4」封顶失败且日志计数正确、
  Retry-After 遵从、退避中取消即时返回、策略表 60→120→240/取大/封顶——全过
  （ingest/backoff_test.go）。
- 原方案要点（保留作背景）：单文档运行遇 429 应自动退避而非立即失败；站点无
  Retry-After，需自定策略。批量与单文档共用。

### RL-4 M2 批量抓取配额规划（P2，依赖 RL-2/RL-3）——✅ 代码骨架已实现（2026-11-02）
- **实现**：`ingest.RunEUBatch`（队列驱动 + 断点续传 + 熔断 + CSV 批次报告）+
  `openlaw-eu-text -batch <队列.json> [-circuit N] [-report 路径] [-dry-run]`。
  队列文件为 JSON 数组 `[{"celex":"32016R0679","lang":"eng"}]`（人工/脚本生成；
  SPARQL 全量枚举生成队列未自动化——如实标注，勿假装已具备）；每处理一部立即
  原子回写队列（done/failed/pending），重跑只处理 pending，done 不再发请求；
  failed 保留待人工处置，不静默重跑；连续 ≥circuit 部（默认 5）退避后仍 429 →
  整批熔断中止，剩余如实留 pending（退出码 3）；语言纪律：队列默认 eng 单语。
- **验收**：httptest 熔断演练（6 部 × circuit=3 → 中止且队列全 pending）与
  中断-恢复演练（限流轮→恢复轮：done 入库 pending 闸口、failed 如实、终轮 done
  零请求）——全过（ingest/eubatch_test.go）。批次报告 CSV 每部一行
  （celex/lang/状态/条数/sha256/错误）。
- **仍待实网**（M2 主体执行期，非代码缺口）：小样本测速估算安全小时速率、
  按速率切天分批窗口。RL-3 已合入，批量启动的纪律前提已满足。
- 原方案要点（保留作背景）：EU in-force 法规全量（估数千部 × 每部 4-6 请求）
  远超单桶日配额；无规划的批量必然长期 429。

### RL-5 限流可观测（P2，与 RL-4 互相支撑）——✅ 已实现（2026-11-02）
- **实现**：`ingest.EUQuotaStats`（库层聚合）+ `openlaw-stats eu [-data 目录]
  [-since 日期] [-json]`（只读，绝不联网）。按日汇总 europa.eu 通道请求：
  次数/成功/失败/429 率/累计字节；另按 CELEX 归因请求计数。数据源就是既有
  import_log 留痕——报表不新增任何请求。
- **验收**：对现有 import_log 出正确报表（2026-10-25 的 429 爆发段如实呈现：
  38 次全失败、5 次带 429 状态码）；合成日志单测（ingest/stats_test.go）通过。
- 原方案要点（保留作背景）：配额决策要有数据；import_log 已含全部要素，缺聚合视图。

### RL-6 指纹/域名/robots 巡检（P3，周期性，季度）
- **动机**：三处会随时间漂移——①`publications.europa.eu`→`op.europa.eu` 域名迁移未完成（双域白名单是当前快照）；②Go 升级可能改变默认 TLS 曲线/新增后量子机制，令 `f8cbfab` 的兼容修正失效或多余；③robots.txt 策略可能变更（当前 `User-agent: * Allow: /`）。
- **步骤**：跑 RL-2 probe → 复核 robots.txt 内容 → `go version` 与曲线设置复核 → 更新本文档「背景事实」日期戳。
- **验收**：巡检记录（日期/结论/是否改码）追加到本文档末尾「巡检日志」。
- **规模**：极小（15 分钟/次），但必须周期性。

## 编排建议

```
今天可做：  RL-2（probe 工具）→ RL-1（pilot，探测到窗口即跑）
下个迭代：  RL-3（退避）✅ → RL-5（报表）✅ → RL-4（批量骨架）✅——
            剩余为 M2 主体实网执行期（小样本测速 + 分批窗口，见 RL-4「仍待实网」）
常驻：      RL-6（季度巡检）
```

- RL-1 不等待任何代码任务——配额窗口开即可跑（代码已就绪）。
- RL-4 严禁在 RL-3 合入前启动（无退避的批量 = 轰炸）。
- 所有任务共同纪律：UA 如实标识、不冒充浏览器指纹、fail-closed、429 后拉长间隔而非缩短。

## 巡检日志

| 日期 | 执行人 | 结论 |
|---|---|---|
| 2026-10-25 | （实现期） | 基线建立：双域白名单、TLS 曲线修正合入；Go 桶冷却小时级 |
