# 多法域设计（中国 / 欧盟 / 美国）

> 状态：第一、二步已实施（schema v7 + 界面如实展示）；第三步（美国采集通道）
> 清单骨架（`ingest/usgovinfo.go`）+ 全文解析入库已开通（`parse.ParseUSLM` +
> `ingest.RunUSLMImport`，`openlaw-us -ingest`，人工闸口 pending）；欧盟全文通道
> 已接入桌面「获取最新法规」（`ingest.RunEUText` + `StartEUTextTask`）；
> 基础包按法域切分已开通（`BuildOptions.Jurisdictions`）。
> 美国官方数据源实况见 docs/sources-us.md（2026-10-31 实网核实）。

## 1. 为什么要法域维度

v6 及以前，中国/欧盟全靠 `effect_level` 取值隐式区分，美国无处表达；库总览的
「法规构成」只有中国九级，欧盟/美国既无条目也无「此处为空」的如实交代——违反
诚实展示纪律（缺失必须显示为缺失并给补救指引）。法域是法规的属法身份，与
「地域」（中国境内的区划归属）是两个正交概念，不得混用一列。

## 2. 数据模型（v7）

- `law.jurisdiction TEXT`：`CN`=中国、`EU`=欧盟、`US`=美国；**空 = 法域未标注**
  （界面必须如实显示，禁止渲染成任何法域）。刻意不加 CHECK（同 v4 地域列策略：
  取值合法性放应用层与门禁测试），加列走纯 ALTER。
- `effect_level` CHECK 扩展美国五级（改 CHECK 须十二步重建表，v7 一并完成；
  迁移前自动备份）：

  | 法域 | 效力层级（法言法语） | 对应官方概念 |
  |---|---|---|
  | 中国 | 宪法/法律/行政法规/监察法规/地方性法规/部门规章/地方政府规章/司法解释/其他 | 现有九级不变 |
  | 欧盟 | 欧盟条例/欧盟指令/欧盟决定 | Regulation / Directive / Decision（Treaties 暂并入条例类，待有真实条目再扩列） |
  | 美国 | 美国宪法/联邦法律/联邦规章/行政命令/联邦条约 | U.S. Constitution / U.S. Code（Acts of Congress）/ CFR / Executive Orders / Treaties |

  美国州法不在本期范围：如接入，另立「州法律/州规章」层级并在本表登记，不得
  借用联邦层级。
- 存量回填（迁移内完成，口径如实注释）：欧盟三类效力或带 `eli/celex` → EU；
  其余 → CN。依据：v6 及以前语料全部来自中国官方渠道与欧盟 Cellar 通道，无美国条目。
- 版本协商：`SchemaVersion=7`，`SchemaMin` 保持 3——只读消费方读 v3–v6 语料时
  显示「法域未标注」即可，不被迫升级；要写美国条目必须升到 v7。

## 3. 写路径纪律

- 法域由效力等级推导（`ingest.DeriveJurisdiction`）：「其他」/空判不出 → 留空，
  **禁止瞎猜**；upsert 只在新值可判定处覆盖（判不出不清空既有法域）。
- 欧盟 Cellar 通道显式落 EU（效力等级本身就是欧盟三类，双保险）。
- 美国联邦各级纳入 `nationalEffectLevels` 护栏：中国区划码的「名称开头行政区」
  规则对其不适用，不得据名称前缀给美国/欧盟条目标地域。
- 新法域条目同样走人工复核闸口：一律 `confirm_status=pending`，确认/驳回必须
  署名 + 结论依据（不因法域新增而放宽）。

## 4. 界面口径（诚实展示）

- 库总览「法规库分法域」：中国/欧盟/美国三行**恒常给出**；0 必须显示
  「尚未收录」+ 可达补救指引（欧盟：通道现为命令行 `openlaw-eu`；美国：通道
  尚在建设中）；法域未标注仅在非 0 时追加一行。
- 按条件筛选新增「法域」分面：未标注单独成桶（不并入任何法域），计数与列表
  同源（browseWhere 一处口径），选中后其余维度照常联动。
- 效力等级分面/占比条数据驱动：欧盟/美国条目入库后其层级自然出现，不预设假数据。

## 5. 更新包与发版（后续）

- 基础包按法域切分已开通：`BuildOptions.Jurisdictions`（CN/EU/US/none=法域未标注，
  名单外取值拒绝）+ manifest `jurisdictions` 字段（随签名保护）+
  `openlaw-pack build -jurisdictions EU`；`pack_id`（如 `base-eu-2026`）承载包身份，
  `regions` 字段仍用中国 GB/T 2260 区划码，欧盟/美国包留空。
- 签名目录、受信名单、幂等台账对多法域包原样适用，无需改动。

## 6. 美国通道（第三步）

- 清单：`ingest/usgovinfo.go`（govinfo 批量清单拉取：robots 前置 fail-closed、
  ≥2s 礼貌限速、原始留证落 `incoming/`、import_log 留痕、partial 如实报告）+
  `cmd/openlaw-us`（显式触发；未配置/未联网时如实拒绝）。
- 全文解析入库已开通：`parse.ParseUSLM`（USLM XML 结构即证据：编号 div
  SECTION/PART 为条文边界，零猜测；零条文如实失败）+ `ingest.RunUSLMImport`
  （law.jurisdiction=US 由效力等级推导、confirm_status=pending 人工闸口、
  原始 XML 归档 xml/ 层、生效日期必须由批量包元数据提供不得猜）+
  `openlaw-us -ingest <USLM XML>`（与桌面共用同一 runner）。批量包本体下载
  仍由用户经官方渠道自行完成（体积与配额纪律），本命令不代下载。
- 未做（如实交代）：生效/废止关系（Statute ↔ CFR）建立、批量包自动下载、
  行政命令（Executive Orders）FR/PPP 旁路。这些按阶段推进并各自带真实链路测试。
