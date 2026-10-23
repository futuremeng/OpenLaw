# 架构文档

只写分层与边界，表结构见 data-dictionary.md，接口见 api.md。

## 1. 定位

OpenLaw 是本地化的可溯源法律知识底座：向上层应用（OpenCourt 等）提供版本解析、引用校验、要件比对、关联查询与断链检测，**不承担最终法律判断**。

## 2. 四层单向数据流（禁止反向写回）

```
原始层  data/pdf + data/provenance   ← 公报等来源的原始 PDF 与溯源文件，只读不改，全系统证据基准
   ↓
文本层  data/md                      ← 解析后的 Markdown 正文；条文携带法规/版本/条号/生效失效日期锚点；
                                        文件头部记录来源 doc_id 与哈希；人工修订另存新版本，不覆盖原文
   ↓
索引层  data/db (SQLite + FTS5)      ← 点查、全文检索、时间回溯、关系查询；
                                        DuckDB 仅用于离线数据体检，不作为主库
   ↓
能力层  sdk/ validate/ search/ …     ← 版本解析、引用校验、要件比对、关联查询、断链检测；
                                        进程内 SDK 提供，不默认暴露 HTTP 服务
```

选型说明：点查场景下事务型数据库（SQLite）比分析型（DuckDB）更合适；进程内调用避免 HTTP 故障面（开发文档 5.1）。

## 3. 组件与代码包对应

| 组件 | 包 | 职责 |
|---|---|---|
| ingest-cli | `cmd/openlaw-ingest` | 导入 PDF、生成溯源文件、写入数据库 |
| parser | `parse` | 解析正文、拆分条文、标准化日期（ParserVersion 版本化） |
| hash | `hashutil` | SHA-256（防篡改）/ MD5（仅判下载损坏） |
| validator | `validate` | 六环节断链检测、manifest 签名、引用校验 |
| pack/signer | `validate/manifest.go` | manifest 生成、Ed25519 签名与验签 |
| search-service | `search` | FTS5 检索 + LIKE 兜底；向量混合检索为后续 |
| element-engine | `element` | 构成要件加载/比对、脱敏 |
| sdk | `sdk` | 上层应用统一调用入口（Result/Trace/错误码） |
| store | `store` | schema.sql（go:embed）、连接与迁移 |
| 运维 | `cmd/openlaw-check` | 每日哈希重算 + 签名校验 + 断链清单 |

## 4. 关键边界

- **上层禁止直连数据库**：OpenCourt 只能 import `sdk`，防止绕过校验逻辑自己拼 SQL
- **断链不抛异常**：统一走结构化 `Result.Status`，方便界面提示；OL-1006 时禁止输出结论
- **机器不裁决**：relation 默认 pending；要件比对只输出风险假设清单
- **代码与数据分离**：`data/` 整体可拷贝迁移；解析器版本入库（article.parser_version）
- **签名私钥独立存储**：不进仓库、不进数据目录（.gitignore 已拦截）

## 5. 关键流程

1. **导入**（6.1）：哈希 → 溯源 JSON → 解析拆分 → 事务入库 → import_log → 断链检测；任一环节失败整批回滚（rolled_back）
2. **引用校验**（6.2）：名称解析 → 时间点取版本（左闭右开）→ 条款号 → 文本相似度 → 效力检查 → 溯源链；溯源失败直接 OL-1006/OL-1007
3. **要件比对**（6.3）：按案由加载要件 → 事实映射 → 缺失要件 + 突破方向；无胜诉结论
4. **每日运维**（9.2）：manifest 重算与验签（失败 exit 2 告警）→ 全量断链清单（有断点 exit 3）

## 6. 升级与回滚

先在新目录导入更新包并跑校验，通过后再切换数据目录，保留上一版本以便回滚（9.4）。数据模型变更必须带迁移脚本并自动备份（16.1）。
