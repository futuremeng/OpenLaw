# 部署（OpenLaw）

OpenLaw 无常驻服务、无端口、无守护进程。部署形态 = **CLI 单文件二进制 + 可迁移数据目录 + 进程内 SDK**。纯 Go 实现（`modernc.org/sqlite`，`CGO_ENABLED=0`），单文件跨平台即开即用。

## 1. 三种使用形态

| 形态 | 谁用 | 需要什么 |
|---|---|---|
| 独立 CLI | 数据运维（导入/确认/校验/打包） | `dist/` 二进制 + 数据目录 |
| 应用内嵌（主形态） | 最终用户（经 OpenCourt） | 仅需数据目录；SDK 已编译进 OpenCourt 二进制，**不需要** OpenLaw CLI |
| 库模式 | 其他 Go 程序 | `import github.com/futuremeng/OpenLaw/sdk` |

## 2. 构建

```bash
make build       # 当前平台 → dist/（11 个 CLI，3~11MB/个）
make build-all   # darwin/arm64 darwin/amd64 linux/{amd64,arm64} windows/amd64
```

前置 Go 版本以 `go.mod` 为准；国内网络建议 `export GOPROXY=https://goproxy.cn,direct`。

## 3. 数据目录（真正的部署物）

```text
data/
  db/openlaw.db     # SQLite 单文件（schema 自动迁移到当前版本）
  pdf/ docx/        # 原始层（字节级证据，哈希锚定）
  md/               # 文本层
  provenance/       # 溯源 JSON（每文档）
  manifest/         # 防篡改清单 + Ed25519 签名（见 §5）
  incoming/ logs/   # 抓取暂存区 / 导入日志
```

- **整体可迁移**：所有 CLI 用 `-data <路径>` 指目录，内部全相对路径——拷贝整个 `data/` 到新机器即完成数据部署（当前约 3.3G：2.5 万文档 / 104 万条文）
- 数据目录严禁提交仓库（已 gitignore）；对外共享推荐签名更新包（§7）

## 4. 装后验证（新机器必做）

```bash
openlaw-check -data <数据目录>
# 六环节断链清单 + manifest 签名校验；exit 3 = 有断点（转人工），0 = 干净
```

## 5. 完整性与签名（首次部署必做）

```bash
# ① 生成 Ed25519 签名私钥——独立存储（U 盘/密码管理器），
#    严禁进入仓库或数据目录；私钥丢失 = 历史 manifest 全部无法验签
openlaw-check -data <数据目录> -gen-key -key <独立存储路径>/openlaw.key

# ② 生成 manifest（重算全部哈希 + 签名）。未生成前，SDK 的
#    GetArticle 一律如实报 OL-1006 断链（UNVERIFIED）——这是防篡改
#    闸口正常工作的表现，不是 bug
openlaw-check -data <数据目录> -build-manifest -key <独立存储路径>/openlaw.key
```

## 6. 日常运维（docs/release-process.md §4）

| 频率 | 动作 | 命令 |
|---|---|---|
| 每日 | 哈希重算 + manifest 签名校验 | `openlaw-check -data data -build-manifest -key <独立存储>` |
| 每周 | 断链清单 | `openlaw-check -data data`（exit 3 = 有断点） |
| 每日 | 备份数据目录 | `db/` 用 SQLite 安全拷贝；原始层增量备份；备份加密存储 |

## 7. 更新分发（签名更新包）

```bash
openlaw-pack keygen  # 首次发版：生成签名密钥对，公钥内置为受信发布者（16.2）
openlaw-pack build   # 产出 pack 包（manifest + 签名 + 变更文件）
openlaw-pack verify  # 接收方独立验哈希与签名，并给出来源信任结论
openlaw-pack import  # 完整性 + 来源信任都通过才入库；失败保留旧数据，不覆盖（6.4）
```

导入默认要求包签名公钥命中内置受信发布者名单（`pack/trust.go`）：签名自洽但来源未受信
的包等同损坏包，一律拒绝且零写入。密钥轮换用新 key ID 登记，吊销即删除旧 key ID。
`-allow-untrusted` 只跳过来源信任、仍强制完整性校验，仅限本地自验，不用于分发。

不做静默自动抓取更新；抓取类 CLI（`openlaw-fetch` / `openlaw-eu` / `openlaw-eu-text`）均为显式触发 + robots fail-closed + 礼貌限速。

## 8. 与 OpenCourt 对接

- OpenCourt 编译期携带 OpenLaw SDK（`go.mod` require；**正式发版前给 OpenLaw 打 tag 并移除本地 `replace`**）
- 最终用户机器只需：拷贝 OpenLaw 数据目录 + 在 OpenCourt 配置 `openlaw.data_dir`（或环境变量 `OPENCOURT_OPENLAW_DATA_DIR`）指向它；配置位置见 OpenCourt `deploy/README.md`
- 数据运维机器才需要 OpenLaw CLI 二进制

## 9. 安全与合规基线

- **地点定位（OL-12，默认离线）**：浏览器只交经纬度，判定用内置区划表在本机完成
  （`geo` 包：34 省级 + 346 地级 + 近似中心点）；不联网、不做 IP 定位、不调用任何
  地理编码服务。就近判定必然带误差（中心点为人工整理的近似值，10~30 km），UI 必须
  显示距离并允许一键更正。**实测（2026-10-29，macOS）**：裸二进制（无 `Info.plist`
  定位声明）下 webview 一律返回「已拒绝授权」——要真正启用需产出带
  `NSLocationWhenInUseUsageDescription` 的 `.app` 包（尚未做）。此期间走两条可用通道：
  ①下拉手动选省/市；②「按选定地点试判定」（`LocateFromRegion`，以所选地点中心点跑同一条
  判定链路，来源记 `trial` 并明示「非真实定位」，不冒充真实位置）。

- 数据不出域：无遥测、无后台请求；一切联网为显式 CLI 触发
- 私钥（`*.key`）与数据备份均严禁入库，独立加密存储
- 待人工确认（pending）语料在 SDK 输出中带显式 warnings，界面不得当作已确认事实展示（12.3）
