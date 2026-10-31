# 发布与运维流程

## 1. 语义化版本（16.1）

- **主版本**：数据模型或 SDK 接口不兼容变更——必须提供迁移脚本（在 `store/db.go`：
  `migrate` + `upgradeToV2/V3/V4`；仓库内没有独立的 `store/migrate.go`），迁移前自动备份数据库
  （已实现：`store/backup.go`，`VACUUM INTO` 落 `data/backups/`，备份失败即中止迁移）
- **次版本**：新增能力（向后兼容）
- **修订版本**：修复

CHANGELOG 每条必须写**原因、影响范围、回滚方式**，只写结果不写原因的记录等于没记。

## 2. 发布检查清单（16.3）

提交前必过（CI 同步执行）：

```bash
make lint    # go vet + gofmt 门禁，不通过禁止打包发布
make test    # 含四类必测：解析、日期边界、断链、哈希失败 + 脱敏覆盖率
```

发布说明必须写明：新增法规数量、失效法规数量、待人工确认数量、已知冲突数量。

## 3. 离线更新包（16.2，后续版本实现完整包体）

```text
pack-YYYYMMDD/
  manifest.json   # 每个文件的相对路径、大小、SHA-256、生成时间、打包工具版本
  manifest.sig    # Ed25519 签名（私钥独立存储，不进仓库、不进数据目录）
  pdf/ md/ provenance/ changes.csv
```

导入时校验哈希与清单一致性，成功后写日志；失败保留旧数据，不覆盖（6.4）。不做静默自动抓取更新。

当前已实装：manifest 生成 + Ed25519 签名/验签（`validate/manifest.go`）+ 包体打包与
`changes.csv`（`pack/pack.go`；当前 v3 表头含 `region_code`/`region_source`，v0/v1/v2 旧包向后兼容）。

**包模型 v2（基础包/增量包）**：manifest 可选身份字段 `pack_id / kind(base|delta) /
regions / since / until / doc_count`（随签名保护；缺字段 = 旧整包）。切分打包：
`openlaw-pack build -kind base -regions CN,44 -pack-id base-cn-2026 …` 或
`-kind delta -since 2026-01-01 -until 2026-03-31`；包内只含清单引用到的文件。
导入幂等：`pack_log` 台账（schema v6）记录包级安装事实，`status=applied` 的包再导入
整包跳过；重复条目与库内哈希一致则跳过、不一致中止转人工。分发渠道（GitHub Releases、
签名 index 与下载通道）见 docs/tasks-opencourt-bridge.md 对应阶段记录，尚未实装。

## 4. 日常运维（9.2）

| 频率 | 动作 | 命令 |
|---|---|---|
| 每日 | 哈希重算 + manifest 签名校验 | `openlaw-check -build-manifest -key <独立存储路径>` |
| 每周 | 断链清单 | `openlaw-check`（exit 3 = 有断点） |
| 每月 | 待人工确认清单（confirm_status=pending、relation pending、conflict open） | SQL 查询 |

告警处置：
- **文件被替换（OL-1007）**：立即隔离该 PDF 并告警，不自动恢复
- **验签失败（OL-1008）**：停止使用整个数据目录（`openlaw-check` exit 2）

## 5. 备份策略（9.3）

PDF、溯源文件、数据库、签名文件**分别备份**；签名文件（私钥/公钥/签名）独立存储，避免与数据同时丢失或被同时篡改。

## 6. 升级与回滚（9.4）

1. 新建 `data-v2/` 目录，导入更新包
2. `openlaw-check` 全量校验通过
3. 切换应用指向 `data-v2/`，保留 `data-v1/` 以便回滚
4. 数据模型变更：先自动备份 db，再执行迁移脚本（自动完成，见 `store/backup.go`；
   快照在 `data/backups/`，回滚 = 停应用后用快照替换 `db/openlaw.db`）

## 7. 部署形态（9.1）

桌面程序 + 数据目录（`pdf/ md/ db/ provenance/ logs/ manifest/`），支持完全离线运行。整个数据目录可拷贝迁移，不依赖云服务。
