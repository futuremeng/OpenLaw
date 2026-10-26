# 国家行政法规库（gov.cn xzfgk）来源核实（2026-10-25）

`https://www.gov.cn/zhengce/xzfgk/`（国家行政法规库，国务院办公厅主办）。收录现行行政法规
（实测 389 条：国令 373 / 国发 4 / 国函 2 / 国办发 7 / 国办函 3）。

## 合规画像（实查，2026-10-25）

| 入口 | robots.txt | 结论 |
|---|---|---|
| `www.gov.cn` 主站（详情页 `/zhengce/…`） | `User-agent: *`；`/zhengce/` 不在 Disallow（仅禁 `/2016zhengce/` 等旧路径）；主动发布 4 个 sitemap | **允许抓取** |
| `sousuo.www.gov.cn`（站内检索/清单 API） | `User-agent: *  Disallow: /` | **禁止自动化**——清单不得由程序发现 |
| `bmfw.www.gov.cn`（司法部政策文件库平台） | — | 境外 DNS 不可达，不使用 |

版权定位：行政法规原文属《著作权法》第 5 条官方文件（公有领域）；但**不镜像站点汇编**——
只取法规文本本身，自有汇编事实由 corpus manifest 记录，来源 URL 全链路留证（import_log +
provenance + 原始 HTML）。

## 两段式流程（清单人工、获取程序）

```
① 清单（人工浏览）：浏览器打开法规库检索页 → 安装 tools/govcn-xzfgk-export.user.js
   （Tampermonkey）→ 浏览/人工翻页 → 点「导出法规清单 JSON」→ 得 list.json
   （脚本只整理用户已浏览页面的 DOM，绝不代为调用检索接口——那是 Disallow: / 的域）
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

# 复核（与全部语料同一闸口）
openlaw-confirm sample -data data -out queue.csv -n 200
openlaw-confirm apply  -data data -in queue.csv
```

- 幂等续跑：已入库条目 `skip_dup_db`（不重复联网）；已留证 HTML 直接复用（断点续传）。
- 报告：`data/incoming/govcn/report_<ts>.csv`（url,doc_id,status,articles,reason），`status=failed` 退出码 1。
- 增量更新：重跑同一清单即可发现新法规（清单在浏览器重新导出）；修订版靠
  `AutoExpirePrior` 自动接续版本链（旧版 expire 前移 + import_log 留痕）。

## 数据模型映射

| 字段 | 取值 |
|---|---|
| `doc_id` | `govcn_<content_id>`（详情页 URL `content_<id>.htm` 稳定标识） |
| `law_id` | `govcn_<sha256(标题)[:12]>` |
| `effect_level` | `行政法规`（本库性质保证；勿与国务院公报的「其他」混层） |
| `source_type` | `gov.cn 国家行政法规库（官方详情页抓取）` |
| `effective_date_inferred` | 落款/pubtime 推断一律 true，人工 confirm 前非已证事实 |

## 与其他源的关系

- **国务院公报**（www.gov.cn/gongbao/，docs 已有批量通道）：公报刊载载体，发现层是静态目录页；
  本库是现行有效版本的事实库。两者并存，按 sha256 与版本链去重衔接。
- **flk**（硬禁止名单）：行政法规在 flk 存量里也有；govcn 通道提供合规的「现行有效」核对源。
- 效力核对（失效/修订）仍走人工比对（浏览器浏览合规），本通道不自动判废止。
