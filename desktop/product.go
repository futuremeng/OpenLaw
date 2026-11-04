// product.go 产品评审落地的绑定层（P0-1 复核兑现 / P0-2 粘贴即核验 / P1-1 发行版分档 /
// P1-4 检索诚实出路 / 度量体系）。
//
// 纪律：本文件**不实现任何判定与写入逻辑**——一律调用与 CLI 相同的库层 runner
// （search.SearchGuided、validate.VerifyCitationText、ingest.ReviewCoverage、
// metrics.ReportFor）。桌面端不是第二写路径，也不做第二套判定（AGENTS.md）。
//
// 本机台账（store.UsageLog）只在**用户动作的出口**记录，且只记种类与结论：
// 不记检索词、不记法名、不记引用内容、不记案件内容；无任何上传路径。
package main

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/metrics"
	"github.com/futuremeng/OpenLaw/search"
	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

// ---- 发行版分档（P1-1）----

const (
	EditionFull   = "full"   // 数据维护版：查法规 + 复核/标引/采集/自检（默认）
	EditionReader = "reader" // 只读查询版：查法规 + 取数（安装/导入更新包），不暴露写路径入口
)

// SetEdition 由启动参数设定档位；未知值一律回落到数据维护版并如实说明。
func (a *App) SetEdition(e string) {
	a.editionWarn = ""
	switch strings.TrimSpace(strings.ToLower(e)) {
	case EditionReader, "read", "readonly", "reader-only":
		a.edition = EditionReader
	case "", EditionFull, "maintainer", "full-edition":
		a.edition = EditionFull
	default:
		a.edition = EditionFull
		a.editionWarn = "启动参数中的发行版档位无法识别，已按数据维护版运行。"
	}
}

// editionInfo 档位标识 + 白话说明（界面必须原样带出，不得只写「专业版/精简版」）。
func (a *App) editionInfo() (string, string) {
	note := ""
	if a.edition == EditionReader {
		note = "本机为只读查询版：可检索、筛选与核验引用，也可安装官方数据包或导入他人分享的更新包；" +
			"不提供人工复核、地域标引与逐条采集——这些是维护法规库数据时才需要的操作。"
	} else {
		note = "本机为数据维护版：除查法规外，还可进行人工复核、地域标引、从官方渠道逐条获取与数据自检。"
	}
	if a.editionWarn != "" {
		note += "（" + a.editionWarn + "）"
	}
	return a.edition, note
}

// AppEdition 当前发行版（前端据此裁剪导航；隐藏的是入口，不是功能开关）。
func (a *App) AppEdition() map[string]any {
	code, note := a.editionInfo()
	return map[string]any{"code": code, "note": note}
}

// ---- 引用核验（P0-2：粘贴即核验）----

// VerifyCitationText 粘贴一段自由文字（判决书/合同/备忘录里的引用），
// 由库层认出要素并走 CheckCitation 同一条六环节链路判定。
// atDate 非空时覆盖文中认出的时点（用户显式指定优先）。
func (a *App) VerifyCitationText(text, atDate string) (validate.VerifyResult, error) {
	if a.db == nil {
		return validate.VerifyResult{}, errors.New(a.openErr)
	}
	today := time.Now().Format("2006-01-02")
	res := validate.VerifyCitationText(a.db, a.dataDir, text, atDate, today)
	a.logUsage(store.EventVerify, res.Verdict, "")
	return res, nil
}

// ---- 检索的诚实出路（P1-4）----

// SearchView 检索结果 + 诚实说明。结果本身仍是 FTS5 原样命中（不重排序、不加工），
// guidance 只解释「命中/为什么没命中/现在能做什么」，不改变任何一条结果。
type SearchView struct {
	Rows     []SearchRow      `json:"rows"`
	Guidance *search.Guidance `json:"guidance"`
}

// SearchGuided 条文检索 + 诚实出路（首页与条文检索面板共用；与 SearchArticles 同一取数路径）。
func (a *App) SearchGuided(query string, topK int) (*SearchView, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	limit := topK
	if limit <= 0 {
		limit = 10
	}
	hits, g, err := search.SearchGuided(a.db, query, limit)
	out := &SearchView{Guidance: g}
	for _, h := range hits {
		r := SearchRow{ArticleID: h.ArticleID, ArticleNo: h.ArticleNo, Snippet: h.Snippet, DocID: h.DocID}
		err := a.db.QueryRow(`SELECT COALESCE(l.name,''), COALESCE(l.effect_level,'效力等级未标注'), COALESCE(v.version_no,''),
			d.confirm_status, COALESCE(d.publish_date,''), v.effective_date, COALESCE(v.expire_date,'')
			FROM document d JOIN law_version v ON v.doc_id=d.doc_id LEFT JOIN law l ON l.law_id=v.law_id
			WHERE d.doc_id=? LIMIT 1`, h.DocID).
			Scan(&r.LawName, &r.Level, &r.VersionNo, &r.Status, &r.Publish, &r.Effective, &r.Expire)
		if errors.Is(err, sql.ErrNoRows) {
			r.LawName, r.Level = "（未挂法规）", ""
		}
		out.Rows = append(out.Rows, r)
	}
	outcome := "ok"
	if g != nil && g.ZeroResult {
		outcome = "zero"
	} else if g != nil && g.Capped {
		outcome = "capped"
	}
	a.logUsage(store.EventSearch, outcome, "")
	return out, err
}

// ---- 复核兑现进度（P0-1）----

// ReviewCoverage 常用法规参考清单的复核兑现 + 全库复核比例。
// limit<=0 只给计数；>0 时列出缺失/未复核清单（最多 limit 条，其余以计数说明）。
func (a *App) ReviewCoverage(limit int) (*ingest.Coverage, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	return ingest.ReviewCoverage(a.db, limit)
}

// ---- 本机度量（评审「建议补的度量」；全部本机统计，不上传）----

// UsageReport 最近 30 天本机留痕聚合（首启漏斗、零命中检索率、人工吞吐、断链率）。
func (a *App) UsageReport() (*metrics.Report, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	return metrics.ReportFor(a.db)
}

// SetupRouteChosen 初始化面板选了哪条获取路线（只记路线名，供首启漏斗聚合）。
func (a *App) SetupRouteChosen(route string) {
	switch route {
	case "pack", "collect", "import":
		a.logUsage(store.EventSetupRoute, route, "")
	}
}

// ---- 台账写入（失败绝不影响主流程）----

func (a *App) logUsage(kind, outcome, detail string) {
	if a.db == nil {
		return
	}
	_ = store.UsageLog(a.db, kind, outcome, detail)
}

// logTaskEvent 任务终态入台账：采集类记 fetch、数据包类记 pack_install、审核类记 review_run。
// detail 只放状态白话，绝不放任务参数（可能含法名/URL）。
func (a *App) logTaskEvent(kind, state string) {
	event := ""
	switch kind {
	case "govcn-import", "govlist-browse", "eu-text":
		event = store.EventFetch
	case "pack-import", "pack-install":
		event = store.EventPackInstall
	case "agent-review":
		event = store.EventReviewRun
	default:
		return
	}
	outcome := "ok"
	switch state {
	case "done":
	case "cancelled":
		outcome = "partial"
	case "failed":
		outcome = "fail"
	default:
		return
	}
	a.logUsage(event, outcome, "")
}
