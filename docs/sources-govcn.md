# gov.cn 官方法规库（行政法规库 xzfgk + 国家规章库 gjgzk）来源核实（2026-10-25/26）

`https://www.gov.cn/zhengce/xzfgk/`（国家行政法规库，国务院办公厅主办）。收录现行行政法规
（实测 389 条：国令 373 / 国发 4 / 国函 2 / 国办发 7 / 国办函 3）。

`https://www.gov.cn/zhengce/xxgk/gjgzk/`（国家规章库，司法部主办）。收录现行有效
**部门规章**与**地方政府规章**（2023 年上线；规章以「X令第N号」公布）。

## 合规画像（实查，2026-10-25/26）

| 入口 | robots.txt | 结论 |
|---|---|---|
| `www.gov.cn` 主站（详情页 `/zhengce/…`、`/gongbao/…`） | `User-agent: *`；`/zhengce/` 不在 Disallow（仅禁 `/2016zhengce/` 等旧路径）；主动发布 4 个 sitemap | **允许抓取** |
| `sousuo.www.gov.cn`（站内检索/清单 API） | `User-agent: *  Disallow: /` | **禁止自动化**——清单不得由程序发现 |
| `sousuoht.www.gov.cn`（规章库 Athena 检索平台） | robots 为空，但接口 `/athena/forward/<hash>` 带 **RSA appKey 应用层鉴权** | **不得自动化**——逆向鉴权属绕过技术措施，绝不尝试 |
| `bmfw.www.gov.cn`（司法部政策文件库平台） | — | 境外 DNS 不可达，不使用 |

版权定位：法规/规章原文属《著作权法》第 5 条官方文件（公有领域）；但**不镜像站点汇编**——
只取文本本身，自有汇编事实由 corpus manifest 记录，来源 URL 全链路留证（import_log +
provenance + 原始 HTML）。

## 两段式流程（清单人工、获取程序）

```
① 清单（人工浏览）：浏览器打开法规库检索页 → 安装 tools/govcn-xzfgk-export.user.js
   （Tampermonkey）→ 浏览/人工翻页 → 点「导出法规清单 JSON」→ 得 list.json
   （脚本只整理用户已浏览页面的 DOM，绝不代为调用检索接口——那是 Disallow: / 的域）
   清单层等价模式（2026-10-26，真机校准）：**人在环代理浏览**——OpenCourt 内置
   智能体经 BrowserOS neo 驱动用户自己的真实浏览器，只在 www.gov.cn 官方列表页
   （混合清单：部门规章+地方政府规章同列）上点官方分页按钮「下一页」浏览并读
   DOM 收集条目（neutral/govlistassist.go：起点 URL 白名单、翻页间隔下限 2s、
   单会话 ≤200 页、URL 硬校验 https + .gov.cn 域名族、cat 按发布机关逐条推断、
   审计）。这与用户自己浏览+导出等价；仍绝不程序化访问 sousuo/sousuoht 检索域
   或鉴权接口。注意：地方政府规章详情页直链地方政府网站（如 beijing.gov.cn）——
   清单如实记录，但本通道主机锁死 www.gov.cn，此类条目在导入报告中记
   skip_bad_url 转人工（是否为其开设独立合规通道另行评估，见下节）。
   全库恰为 1000 页（真机 end_of_list 探测证实第 1000 页为末页）> 单会话 200 页
   硬上限：分批续接经官方「跳转」控件到达起始页并核验页码（等价人工输入页码）。
   官方清单存在大段真实稀疏空区（如 472–499 连续 28 页空而 500 页恢复数据），
   空页停止阈值可调（CLI -emptystop，稀疏区补采 25–40）。CLI 批量入口
   OpenCourt `cmd/govlistcollect`（-start/-pages/-emptystop），产物多份
   list_agent_*.json 用 `scripts/govmerge.py` 合并去重（或多次直接喂给本 CLI，
   幂等 skip_dup_db 自动去重）。
   真机全量采集（2026-10-27）：11 分片合并共 **1970 条**（部门规章 271 +
   地方政府规章 1699；www.gov.cn 686 条可走本通道抓取，其余 1284 条地方政府
   直链按上述策略记 skip_bad_url 转人工）。
② 获取（程序合规）：openlaw-govcn 按 list.json 抓详情页：
   robots 强制 + 主机锁死 www.gov.cn（重定向逐跳复查）+ 限速（默认 2s/页，不可低于）
   + UA 如实标识 + 每次访问写 import_log + 原始 HTML 留证 data/incoming/govcn/
③ 解析入库：TRS 正文容器（trs_editor_view/TRS_UEDITOR）→ Markdown →
   日期：正文落款保守推断（parse.InferDates）优先，清单 pubtime 兜底，仍无 → skip_no_date 转人工（绝不猜）
   → Importer：effect_level=行政法规、issuer=中华人民共和国国务院、
     confirm_status=pending（人工闸口）、AutoExpirePrior 版本链接续、
     无「第X条」结构（决定/函类）→ 全文即一节显式打标
```

## 用法

```bash
# 预览（抓取+解析+留证，不入库）
go run ./cmd/openlaw-govcn -data data -list data/incoming/govcn/list.json -dry-run

# 正式导入（分批防压力：-limit）
go run ./cmd/openlaw-govcn -data data -list data/incoming/govcn/list.json -limit 100

# 国家规章库（部门规章 + 地方政府规章）：浏览器脚本 tools/govgz-export.user.js
# 在国家规章库页浏览导出 govgz-list.json（含分类页签 cat 与发布机关 puborg），然后：
go run ./cmd/openlaw-govcn -data data -list data/incoming/govgz/list.json -source gzk -dry-run
go run ./cmd/openlaw-govcn -data data -list data/incoming/govgz/list.json -source gzk -limit 100

# 复核（与全部语料同一闸口）
openlaw-confirm sample -data data -out queue.csv -n 200
openlaw-confirm apply  -data data -in queue.csv
```

- 幂等续跑：已入库条目 `skip_dup_db`（不重复联网）；已留证 HTML 直接复用（断点续传）。
- 报告：`data/incoming/<govcn|govgz>/report_<ts>.csv`（url,doc_id,status,articles,reason），`status=failed` 退出码 1。
- 增量更新：重跑同一清单即可发现新法规（清单在浏览器重新导出）；修订版靠
  `AutoExpirePrior` 自动接续版本链（旧版 expire 前移 + import_log 留痕）。

## 数据模型映射

| 字段 | 取值 |
|---|---|
| `doc_id` | `govcn_<content_id>`（详情页 URL `content_<id>.htm[l]` 稳定标识；content_id 站点全局唯一，两库共用前缀天然去重） |
| `law_id` | `govcn_<sha256(标题)[:12]>` |
| `effect_level` | xzfgk：`行政法规`（本库性质保证）；gzk：`部门规章`/`地方政府规章`（清单分类页签直取；缺分类按发布机关保守归类——含「人民政府」→ 地方，否则部门；两者皆缺 `skip_no_meta` 转人工，不猜） |
| `issuer` | xzfgk：`中华人民共和国国务院`；gzk：清单发布机关（DOM 提取） |
| `source_type` | `gov.cn 国家行政法规库（官方详情页抓取）` / `gov.cn 国家规章库（官方详情页抓取）` |
| `effective_date_inferred` | 落款/pubtime 推断一律 true，人工 confirm 前非已证事实 |

注：规章库详情页 URL 由站点 Athena 返回（可能挂 `/zhengce/zhengceku/` 或 `/gongbao/…`
公报路径）——清单校验只锁主机与 `content_<id>` 形态。规章库结果页不渲染发布日期，
清单 pubtime 常为空 → 日期依赖正文落款推断（规章落款含公布/施行日期，命中率高），
推不出即 `skip_no_date` 转人工。

## 与其他源的关系

- **国务院公报**（www.gov.cn/gongbao/，docs 已有批量通道）：公报刊载载体，发现层是静态目录页；
  本库是现行有效版本的事实库。两者并存，按 sha256 与版本链去重衔接。国家规章库的
  docurl 本身就可能指向公报页（同一 content_id → 同一 doc_id，跨库天然去重）。
- **flk**（硬禁止名单）：行政法规/部门规章/地方政府规章在 flk 存量里也有；gov.cn 两库
  提供合规的「现行有效」核对源。
- 效力核对（失效/修订）仍走人工比对（浏览器浏览合规），本通道不自动判废止。
