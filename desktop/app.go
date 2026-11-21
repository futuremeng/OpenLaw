// app.go 桌面端应用层：全部走与 CLI 相同的 store/ingest/validate 代码路径。
// 桌面端属于 OpenLaw 自身的写入路径（生产者 UI，store.Open）；消费方（OpenCourt
// 等）必须用 sdk.OpenReadOnly——两者边界见 docs/tasks-opencourt-bridge.md OL-7。
package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/futuremeng/OpenLaw/browser"
	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/pack"
	"github.com/futuremeng/OpenLaw/search"
	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

type App struct {
	ctx     context.Context
	dataDir string
	dataSrc string // 数据目录来源（命令行/环境变量/随程序位置/当前目录），必须可展示
	db      *store.DB
	openErr string

	// 发行版分档（产品评审 P1-1）：full=数据维护版（默认，含复核/标引/采集）；
	// reader=只读查询版（查法规 + 取数，不暴露复核与采集控件）。档位由启动参数决定，
	// 前端据此裁剪导航——不是隐藏功能，是**不给只读用户写路径的入口**。
	edition       string
	editionWarn   string // 启动参数里档位无法识别等异常（必须随档位说明一起给出）
	migrationNote string // 本机数据被升级过的如实说明（meta.migration_report）

	// 风险标记缓存：LoadRiskSetFromProvenance 要扫数千 provenance JSON，
	// 不能每次刷队列都扫。TTL 5 分钟；只影响读侧展示，不影响写纪律。
	riskMu   sync.Mutex
	risk     ingest.RiskSet
	riskAt   time.Time
	riskLoad string // 最近一次加载错误（如实展示用）

	tm *TaskManager // 采集任务管理器（单槽互斥，见 tasks.go）

	// 远程数据包目录缓存（packs.go）：仅由用户显式刷新，安装任务据此校验所选包。
	packsMu      sync.Mutex
	packsCatalog *pack.Catalog
}

func NewApp(dataDir string) *App {
	a := &App{dataDir: dataDir, edition: EditionFull}
	a.tm = NewTaskManager(func(ev string, data ...interface{}) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, ev, data...)
		}
	})
	// 任务终态写本机台账（度量体系；只记种类与结论，不记参数内容）。
	a.tm.onDone = a.logTaskEvent
	return a
}

func (a *App) Open() error {
	if err := pack.RegisterOfficialKeys(); err != nil {
		a.openErr = err.Error()
		return err
	}
	if err := os.MkdirAll(filepath.Join(a.dataDir, "db"), 0o755); err != nil {
		a.openErr = err.Error()
		return err
	}
	db, err := store.Open(filepath.Join(a.dataDir, "db", "openlaw.db"))
	if err != nil {
		a.openErr = err.Error()
		return err
	}
	a.db = db
	// 迁移留痕如实带出（产品评审 P1-6：数据被升级过必须让用户看见，不只是内部人知道）。
	if mr := store.LoadMigrationReport(db.DB); mr.OK {
		a.migrationNote = fmt.Sprintf("本机数据已从格式版本 %s 升级到 %s（%s）；升级前已自动备份：%s",
			mr.From, mr.To, mr.At, mr.Backup)
	}
	a.logUsage(store.EventAppOpen, "ok", "")
	return nil
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }
func (a *App) shutdown(ctx context.Context) {
	if a.db != nil {
		a.db.Close()
		a.db = nil
	}
}

// ---- 法规库状态面板 ----

type LevelCount struct {
	Level     string `json:"level"`
	Juris     string `json:"juris"` // 效力等级所属法域（CN/EU/US，空=法域未标注）——库总览构成按法域分组联动
	Pending   int    `json:"pending"`
	Confirmed int    `json:"confirmed"`
	Rejected  int    `json:"rejected"`
}

// JurisCount 法域计数（v7 多法域，docs/design-multi-jurisdiction.md）。
// Code 为技术标识 CN/EU/US（界面文案由前端映射为「中国/欧盟/美国」）；
// 空 Code = 法域未标注。诚实展示：CN/EU/US 三行恒常给出，0 也要如实显示。
type JurisCount struct {
	Code      string `json:"code"`
	Laws      int    `json:"laws"`
	Documents int    `json:"documents"` // 该法域留证条文数（0 时界面不得说「均已复核」，须如实「尚无留证条文」）
	Pending   int    `json:"pending"`   // 该法域待复核条文数（与「待复核」总数同口径，按法域联动）
}

type StatusView struct {
	DataDir         string            `json:"data_dir"`
	DataSrc         string            `json:"data_src,omitempty"` // 目录来源（如实展示）
	OpenErr         string            `json:"open_err,omitempty"`
	SchemaVersion   string            `json:"schema_version"`
	SchemaMin       string            `json:"schema_min"`
	SchemaMax       string            `json:"schema_max"`
	AppVersion      string            `json:"app_version,omitempty"` // 程序版本（发布流水线注入；见 main.go var version）
	ProducerVersion string            `json:"producer_version"`
	License         string            `json:"license,omitempty"`
	ManifestAt      string            `json:"manifest_at,omitempty"`
	Laws            int               `json:"laws"`
	Versions        int               `json:"versions"`
	Articles        int               `json:"articles"`
	Documents       int               `json:"documents"`
	Pending         int               `json:"pending"`
	Confirmed       int               `json:"confirmed"`
	Rejected        int               `json:"rejected"`
	Govcn           int               `json:"govcn"`
	Govlocal        int               `json:"govlocal"`
	ByLevel         []LevelCount      `json:"by_level"`
	Juris           []JurisCount      `json:"juris"`                // 分法域计数（CN/EU/US 恒常三行，0 也如实显示）
	LastFetch       map[string]string `json:"last_fetch,omitempty"` // 各渠道最近抓取报告时间（数据源状态卡）
	// 产品评审落地三项（随状态一并给出，避免面板各自多跑一次查询而口径不一）：
	Edition          string `json:"edition"`                  // full=数据维护版 / reader=只读查询版
	EditionNote      string `json:"edition_note"`             // 该档位的白话说明（含能做什么、不能做什么）
	MigrationNote    string `json:"migration_note,omitempty"` // 本机数据被升级过的如实说明（无迁移则空）
	CoverageHeadline string `json:"coverage_headline"`        // 复核兑现一行白话（P0-1，缺失必须直说）
}

func (a *App) count(q string, args ...any) int {
	var n int
	if a.db == nil {
		return 0
	}
	if err := a.db.QueryRow(q, args...).Scan(&n); err != nil {
		return 0
	}
	return n
}

// Status 法规库状态汇总（计数/来源版本/schema/许可）。
func (a *App) Status() (StatusView, error) {
	// 程序版本与数据状态无关：即使库没打开成功也必须如实报出（诚实展示）。
	v := StatusView{DataDir: a.dataDir, DataSrc: a.dataSrc, OpenErr: a.openErr, SchemaMin: store.SchemaMin, SchemaMax: store.SchemaMax, AppVersion: version}
	if a.db == nil {
		return v, errors.New(a.openErr)
	}
	_ = a.db.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&v.SchemaVersion)
	v.Laws = a.count(`SELECT COUNT(*) FROM law`)
	v.Versions = a.count(`SELECT COUNT(*) FROM law_version`)
	v.Articles = a.count(`SELECT COUNT(*) FROM article`)
	v.Documents = a.count(`SELECT COUNT(*) FROM document`)
	v.Pending = a.count(`SELECT COUNT(*) FROM document WHERE confirm_status='pending'`)
	v.Confirmed = a.count(`SELECT COUNT(*) FROM document WHERE confirm_status='confirmed'`)
	v.Rejected = a.count(`SELECT COUNT(*) FROM document WHERE confirm_status='rejected'`)
	v.Govcn = a.count(`SELECT COUNT(*) FROM document WHERE doc_id LIKE 'govcn_%'`)
	v.Govlocal = a.count(`SELECT COUNT(*) FROM document WHERE doc_id LIKE 'govlocal_%'`)

	rows, err := a.db.Query(`SELECT COALESCE(l.effect_level,'效力等级未标注'), COALESCE(l.jurisdiction,''),
		SUM(d.confirm_status='pending'), SUM(d.confirm_status='confirmed'), SUM(d.confirm_status='rejected')
		FROM document d LEFT JOIN law_version v ON v.doc_id=d.doc_id LEFT JOIN law l ON l.law_id=v.law_id
		GROUP BY 1,2 ORDER BY 3 DESC`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var lc LevelCount
			if err := rows.Scan(&lc.Level, &lc.Juris, &lc.Pending, &lc.Confirmed, &lc.Rejected); err == nil {
				v.ByLevel = append(v.ByLevel, lc)
			}
		}
	}
	// 分法域计数：CN/EU/US 三行恒常给出（缺失必须如实显示为 0，前端据此给补救指引）；
	// 法域未标注仅在非 0 时追加一行（诚实展示纪律）。待复核按法域联动（与总待复核同口径）。
	juris := map[string]int{}
	jd := map[string]int{}
	jp := map[string]int{}
	if jrows, err := a.db.Query(`SELECT COALESCE(jurisdiction,''), COUNT(*) FROM law GROUP BY 1`); err == nil {
		defer jrows.Close()
		for jrows.Next() {
			var code string
			var n int
			if err := jrows.Scan(&code, &n); err == nil {
				juris[code] = n
			}
		}
	}
	if prows, err := a.db.Query(`SELECT COALESCE(l.jurisdiction,''), COUNT(*), SUM(d.confirm_status='pending')
		FROM document d JOIN law_version v ON v.doc_id=d.doc_id JOIN law l ON l.law_id=v.law_id GROUP BY 1`); err == nil {
		defer prows.Close()
		for prows.Next() {
			var code string
			var n, p int
			if err := prows.Scan(&code, &n, &p); err == nil {
				jd[code] = n
				jp[code] = p
			}
		}
	}
	v.Juris = []JurisCount{{"CN", juris["CN"], jd["CN"], jp["CN"]}, {"EU", juris["EU"], jd["EU"], jp["EU"]}, {"US", juris["US"], jd["US"], jp["US"]}}
	if n := juris[""]; n > 0 {
		v.Juris = append(v.Juris, JurisCount{"", n, jd[""], jp[""]})
	}
	if m, _, err := validate.LoadManifest(a.dataDir); err == nil {
		v.ProducerVersion = m.ProducerVersion
		if v.ProducerVersion == "" {
			v.ProducerVersion = "来源未知（较早的数据包未记录来源版本）"
		}
		v.License = m.License
		v.ManifestAt = m.GeneratedAt
	} else {
		v.ProducerVersion = "无 manifest（尚未 openlaw-pack build / check）"
	}
	// 各官方渠道最近抓取时间（数据源状态卡；来自 incoming/ 报告文件 mtime）。
	v.LastFetch = map[string]string{}
	for _, r := range ScanReports(a.dataDir, 1) {
		if cur, ok := v.LastFetch[r.Source]; !ok || r.ModifiedAt > cur {
			v.LastFetch[r.Source] = r.ModifiedAt
		}
	}
	// 发行版档位与迁移留痕（界面据此如实说明本机是哪一档、数据是否被升级过）。
	v.Edition, v.EditionNote = a.editionInfo()
	v.MigrationNote = a.migrationNote
	// 复核兑现一行白话（P0-1）：读不到时如实说明，不得留空让界面渲染成中性数字。
	if cov, err := ingest.ReviewCoverage(a.db, 0); err == nil {
		v.CoverageHeadline = cov.Headline()
	} else {
		v.CoverageHeadline = "本机暂时读不到复核进度：" + err.Error()
	}
	return v, nil
}

// ---- 人工确认闸口面板 ----

type PendingItem struct {
	DocID      string   `json:"doc_id"`
	Title      string   `json:"title"`
	Level      string   `json:"level"`
	Issuer     string   `json:"issuer"`
	VersionNo  string   `json:"version_no"`
	Effective  string   `json:"effective_date"`
	Publish    string   `json:"publish_date"`
	SourceType string   `json:"source_type"`
	SourceURL  string   `json:"source_url"`
	Articles   int      `json:"articles"`
	Flags      []string `json:"flags,omitempty"` // inferred_date/whole_text/no_publish_date
}

// PendingList 分页列出 pending 文档（filter 匹配 doc_id/source_type/法规名）。
func (a *App) PendingList(filter string, limit, offset int) ([]PendingItem, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	like := "%" + filter + "%"
	rows, err := a.db.Query(`SELECT d.doc_id, COALESCE(l.name,''), COALESCE(l.effect_level,'效力等级未标注'),
		COALESCE(l.issuer,''), COALESCE(v.version_no,''), COALESCE(v.effective_date,''),
		COALESCE(d.publish_date,''), COALESCE(d.source_type,''), COALESCE(d.source_url,''),
		(SELECT COUNT(*) FROM article a WHERE a.doc_id=d.doc_id)
		FROM document d LEFT JOIN law_version v ON v.doc_id=d.doc_id LEFT JOIN law l ON l.law_id=v.law_id
		WHERE d.confirm_status='pending' AND (d.doc_id LIKE ? OR d.source_type LIKE ? OR l.name LIKE ?)
		ORDER BY d.doc_id LIMIT ? OFFSET ?`, like, like, like, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingItem
	for rows.Next() {
		var p PendingItem
		if err := rows.Scan(&p.DocID, &p.Title, &p.Level, &p.Issuer, &p.VersionNo,
			&p.Effective, &p.Publish, &p.SourceType, &p.SourceURL, &p.Articles); err != nil {
			return nil, err
		}
		if fl := a.riskSet()[p.DocID]; len(fl) > 0 {
			p.Flags = append([]string(nil), fl...)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type PreviewView struct {
	DocID      string `json:"doc_id"`
	FilePath   string `json:"file_path"`
	SourceURL  string `json:"source_url"`
	SourceType string `json:"source_type"`
	Publish    string `json:"publish_date"`
	SHA256     string `json:"sha256"`
	Status     string `json:"confirm_status"`
	// ConfirmSrc 确认来源（agent=智能体确认 / human=人工确认 / 空=历史数据或未经确认）。
	// 徽章必须按它区分，历史确认条目不得渲染成智能体确认（诚实展示纪律）。
	ConfirmSrc string   `json:"confirm_source,omitempty"`
	Content    string   `json:"content"`
	Truncated  bool     `json:"truncated"`
	Missing    string   `json:"missing,omitempty"`
	ReviewLog  []string `json:"review_log"`
	// 面向律师的法规要素（文档所挂法规与版本；未挂接则为空）。
	LawID     string `json:"law_id,omitempty"`
	LawName   string `json:"law_name,omitempty"`
	Issuer    string `json:"issuer,omitempty"`
	Level     string `json:"level,omitempty"`
	VersionNo string `json:"version_no,omitempty"`
	Effective string `json:"effective_date,omitempty"`
	Expire    string `json:"expire_date,omitempty"`
}

// Preview 文档留证正文（file_path 指向的 md/txt，只读展示）+ 审计历史。
func (a *App) Preview(docID string) (PreviewView, error) {
	var p PreviewView
	if a.db == nil {
		return p, errors.New(a.openErr)
	}
	err := a.db.QueryRow(`SELECT doc_id, file_path, COALESCE(source_url,''), COALESCE(source_type,''),
		COALESCE(publish_date,''), sha256, confirm_status, COALESCE(confirm_source,'') FROM document WHERE doc_id=?`, docID).
		Scan(&p.DocID, &p.FilePath, &p.SourceURL, &p.SourceType, &p.Publish, &p.SHA256, &p.Status, &p.ConfirmSrc)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("文档不存在: %s", docID)
	} else if err != nil {
		return p, err
	}
	_ = a.db.QueryRow(`SELECT l.law_id, l.name, COALESCE(l.issuer,''), COALESCE(l.effect_level,''),
		COALESCE(v.version_no,''), v.effective_date, COALESCE(v.expire_date,'')
		FROM law_version v LEFT JOIN law l ON l.law_id=v.law_id WHERE v.doc_id=? LIMIT 1`, docID).
		Scan(&p.LawID, &p.LawName, &p.Issuer, &p.Level, &p.VersionNo, &p.Effective, &p.Expire)
	if b, err := os.ReadFile(filepath.Join(a.dataDir, filepath.FromSlash(p.FilePath))); err == nil {
		s := string(b)
		if len(s) > 60000 {
			cut := strings.LastIndexByte(s[:60000], '\n')
			if cut <= 0 {
				cut = 60000
				for cut > 0 && !utf8.RuneStart(s[cut]) {
					cut--
				}
			}
			s = s[:cut]
			p.Truncated = true
		}
		p.Content = s
	} else {
		p.Missing = "留证文件不可读: " + p.FilePath
	}
	rows, err := a.db.Query(`SELECT action||' '||from_status||'→'||to_status||' by '||reviewer||'：'||COALESCE(reason,'')||' @'||created_at
		FROM review_log WHERE doc_id=? ORDER BY review_id`, docID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var s string
			if rows.Scan(&s) == nil {
				p.ReviewLog = append(p.ReviewLog, s)
			}
		}
	}
	return p, nil
}

// Judge 落判（confirm/reject/reset）：署名与依据必填；复用 ingest.Confirmer.Apply
// 的 CSV 落判与 review_log 审计语义——UI 不另开写路径（机器不裁决，人判必留痕）。
func (a *App) Judge(docIDs []string, verdict, reviewer, reason string) (*ingest.ApplySummary, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	if len(docIDs) == 0 {
		return nil, errors.New("请先选择要复核的条目")
	}
	switch verdict {
	case "confirm", "reject", "reset":
	default:
		return nil, fmt.Errorf("verdict 只能为 confirm/reject/reset: %q", verdict)
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(reason) == "" {
		return nil, errors.New("复核须由人工完成：请填写复核人与结论依据")
	}
	tmp, err := os.CreateTemp("", "openlaw-judge-*.csv")
	if err != nil {
		return nil, err
	}
	path := tmp.Name()
	defer os.Remove(path)
	w := csv.NewWriter(tmp)
	_ = w.Write([]string{"doc_id", "verdict", "reviewer", "note"})
	for _, d := range docIDs {
		_ = w.Write([]string{d, verdict, reviewer, reason})
	}
	w.Flush()
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	c := &ingest.Confirmer{DataDir: a.dataDir, DB: a.db}
	return c.Apply(path)
}

// ---- 风险标记缓存 ----

var riskTTL = 5 * time.Minute

// riskSet 带 TTL 的风险标记集（读侧展示用；加载失败保留旧值并记录错误）。
func (a *App) riskSet() ingest.RiskSet {
	a.riskMu.Lock()
	defer a.riskMu.Unlock()
	if a.risk != nil && time.Since(a.riskAt) < riskTTL {
		return a.risk
	}
	rs, err := ingest.LoadRiskSetFromProvenance(a.dataDir)
	if err != nil {
		a.riskLoad = "风险标记加载失败: " + err.Error()
		if a.risk == nil {
			a.risk = ingest.RiskSet{}
		}
	} else {
		a.risk, a.riskLoad = rs, ""
	}
	a.riskAt = time.Now()
	return a.risk
}

// ---- 总览增强 ----

// RecentReports 最近导入/采集报告分诊（读 incoming/ 报告文件，不碰 DB）。
// limit = 面板展示条数上限：跨来源合并后按修改时间倒序取前 limit 份
// （ScanReports 已按修改时间倒序返回）。limit ≤0 取默认 5，>50 钳制到 50。
// 参数个数必须与前端 app().RecentReports(n) 调用一致——不符时 Wails 运行期
// 直接报 "received N arguments ... expected M"，见 binding_test.go 契约门禁。
func (a *App) RecentReports(limit int) []ReportSummary {
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}
	reps := ScanReports(a.dataDir, limit) // 每来源最多取 limit 份，足够填满全局前 limit
	if len(reps) > limit {
		reps = reps[:limit]
	}
	return reps
}

type ReviewRow struct {
	DocID     string `json:"doc_id"`
	LawName   string `json:"law_name"`
	Juris     string `json:"juris"` // 条目所属法域（CN/EU/US，空=法域未标注/未知）——库总览按法域归列
	Action    string `json:"action"`
	From      string `json:"from_status"`
	To        string `json:"to_status"`
	Reviewer  string `json:"reviewer"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

// RecentReviews 最近人工确认流转（review_log 审计流水）。
func (a *App) RecentReviews(limit int) ([]ReviewRow, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := a.db.Query(`SELECT r.doc_id, COALESCE(l.name,''), COALESCE(l.jurisdiction,''), r.action, r.from_status, r.to_status,
		r.reviewer, COALESCE(r.reason,''), r.created_at
		FROM review_log r LEFT JOIN law_version v ON v.doc_id=r.doc_id LEFT JOIN law l ON l.law_id=v.law_id
		ORDER BY r.review_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewRow
	for rows.Next() {
		var r ReviewRow
		if err := rows.Scan(&r.DocID, &r.LawName, &r.Juris, &r.Action, &r.From, &r.To,
			&r.Reviewer, &r.Reason, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- 检索面板 ----

type SearchRow struct {
	ArticleID string `json:"article_id"`
	ArticleNo string `json:"article_no"`
	Snippet   string `json:"snippet"`
	DocID     string `json:"doc_id"`
	LawName   string `json:"law_name"`
	Level     string `json:"level"`
	VersionNo string `json:"version_no"`
	// 信任层要素：复核状态与时效（公众/律师都需要）。
	Status    string `json:"confirm_status"`
	Publish   string `json:"publish_date"`
	Effective string `json:"effective_date"`
	Expire    string `json:"expire_date"`
}

// SearchArticles FTS5 条文检索 + 法规名/效力等级补全（原样透出，无重排——
// 语料层可复现纪律）。每条结果必带 doc_id。
func (a *App) SearchArticles(query string, topK int) ([]SearchRow, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	hits, err := search.Search(a.db, query, topK)
	if err != nil {
		return nil, err
	}
	var out []SearchRow
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
		out = append(out, r)
	}
	return out, nil
}

// CheckCitation 引用核验（validate 六环节原样透出：命中/相似度/失效版本提示/
// 断链问题/待确认状态，五态语义不粉饰）。
func (a *App) CheckCitation(lawName, articleNo, quote, atDate string) (validate.CitationResult, error) {
	if a.db == nil {
		return validate.CitationResult{}, errors.New(a.openErr)
	}
	today := time.Now().Format("2006-01-02")
	return validate.CheckCitation(a.db, a.dataDir, lawName, articleNo, quote, atDate, today)
}

// ---- 确认面板增强：统计与批量 ----

// QueueStats 确认进度与风险面统计（ingest.Confirmer.Stats 原样）。
func (a *App) QueueStats() (*ingest.ConfirmStats, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	c := &ingest.Confirmer{DataDir: a.dataDir, DB: a.db}
	return c.Stats(a.riskSet())
}

// SourceFacet 待复核条目的来源分布（批量复核的来源下拉用）。
type SourceFacet struct {
	Source  string `json:"source"`
	Pending int    `json:"pending"`
	Total   int    `json:"total"`
}

// ConfirmSources 真实来源名与各来源的待复核条数。界面据此出下拉：用户只选来源名，
// 不需要也不应该手写 gov.cn% 这类通配符（docs/design-ui-language.md 主视图禁止项）。
// Source 为空串的是「未记录来源」的条目，前端不列进下拉（无法按名选择，请逐条处理）。
func (a *App) ConfirmSources() []SourceFacet {
	if a.db == nil {
		return nil
	}
	rows, err := a.db.Query(`SELECT COALESCE(source_type,''), SUM(confirm_status='pending'), COUNT(*)
		FROM document GROUP BY 1 HAVING SUM(confirm_status='pending')>0 ORDER BY 2 DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []SourceFacet
	for rows.Next() {
		var s SourceFacet
		if err := rows.Scan(&s.Source, &s.Pending, &s.Total); err == nil {
			out = append(out, s)
		}
	}
	return out
}

// BulkPreview 批量流转干跑：只计数不写库（先看清楚再动手）。
// source 是来源名精确匹配（来自 ConfirmSources 下拉），不是 LIKE 模式串。
func (a *App) BulkPreview(source, level, verdict string) (int, error) {
	if a.db == nil {
		return 0, errors.New(a.openErr)
	}
	c := &ingest.Confirmer{DataDir: a.dataDir, DB: a.db}
	return c.BulkMatch(ingest.BulkFilter{Source: source, Level: level}, verdict)
}

// BulkApply 批量流转执行：署名与依据必填（库层同样强制，双保险）。
func (a *App) BulkApply(source, level, verdict, reviewer, reason string) (*ingest.ApplySummary, error) {
	if a.db == nil {
		return nil, errors.New(a.openErr)
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(reason) == "" {
		return nil, errors.New("批量复核须填写复核人与结论依据")
	}
	c := &ingest.Confirmer{DataDir: a.dataDir, DB: a.db}
	return c.Bulk(ingest.BulkFilter{Source: source, Level: level}, verdict, reviewer, reason)
}

// ---- 采集任务面板（M2）----

// TaskList 当前任务（在前）+ 历史（最近在前）。
func (a *App) TaskList() []TaskInfo { return a.tm.Tasks() }

// CancelTask 请求取消当前采集任务。
func (a *App) CancelTask() bool { return a.tm.Cancel() }

// BrowserStatus 辅助浏览通道状态（只读环境配置，不主动联网探测）。
// guidance 是白话下一步做法；technical 是端点形态与参数名，界面只放「技术细节」折叠。
func (a *App) BrowserStatus() map[string]any {
	b := browser.NewBrowserOS(os.Getenv("OPENLAW_BROWSEROS_ENDPOINT"))
	guide := ""
	if !b.Enabled() {
		guide = browser.Guidance
	}
	return map[string]any{"enabled": b.Enabled(), "guidance": guide, "technical": browser.GuidanceTech}
}

// GovcnLists 可选的已导出清单（incoming/*/list*.json，最近在前）。
func (a *App) GovcnLists() []map[string]string {
	var out []map[string]string
	roots, _ := filepath.Glob(filepath.Join(a.dataDir, "incoming", "*", "*.json"))
	for _, p := range roots {
		base := filepath.Base(p)
		if !strings.HasPrefix(base, "list") {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		out = append(out, map[string]string{
			"path":  p,
			"name":  filepath.Base(filepath.Dir(p)) + "/" + base,
			"mtime": fi.ModTime().UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["mtime"] > out[j]["mtime"] })
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}

// GovcnTaskOptions govcn 详情导入任务参数（与 openlaw-govcn CLI flag 同语义）。
type GovcnTaskOptions struct {
	Source   string `json:"source"` // xzfgk | gzk
	ListPath string `json:"list_path"`
	DelaySec int    `json:"delay_sec"`
	Limit    int    `json:"limit"`
	DryRun   bool   `json:"dry_run"`
	Local    bool   `json:"local"`
}

// StartGovcnTask 提交 gov.cn 清单抓取导入任务（ingest.RunGovcnBatch——与 CLI 同路径）。
func (a *App) StartGovcnTask(o GovcnTaskOptions) (TaskInfo, error) {
	if o.Source != "xzfgk" && o.Source != "gzk" {
		return TaskInfo{}, errors.New("抓取通道只能选：国家行政法规库 / 国家规章库")
	}
	if o.ListPath == "" {
		return TaskInfo{}, errors.New("请先提供官方目录清单文件（由浏览器脚本人工导出；程序不代为检索）")
	}
	if !filepath.IsAbs(o.ListPath) {
		o.ListPath = filepath.Join(a.dataDir, o.ListPath)
	}
	if o.DelaySec < 2 {
		o.DelaySec = 2 // 礼貌限速纪律，与 CLI 同样钳制
	}
	entries, badURLs, err := ingest.LoadGovcnList(o.ListPath, o.Local)
	if err != nil {
		return TaskInfo{}, err
	}
	if o.Limit > 0 && o.Limit < len(entries) {
		entries = entries[:o.Limit]
	}
	subdir := "govcn"
	if o.Source == "gzk" {
		subdir = "govgz"
	}
	params := fmt.Sprintf("source=%s list=%s limit=%d dry_run=%v local=%v delay=%ds",
		o.Source, filepath.Base(o.ListPath), o.Limit, o.DryRun, o.Local, o.DelaySec)
	return a.tm.Submit("govcn-import", params, func(ctx context.Context, progress func(string)) error {
		db, err := store.Open(filepath.Join(a.dataDir, "db", "openlaw.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		g := &ingest.Govcn{DataDir: a.dataDir, DB: db, MinInterval: time.Duration(o.DelaySec) * time.Second,
			DryRun: o.DryRun, Kind: o.Source, LocalGov: o.Local}
		im := &ingest.Importer{DataDir: a.dataDir, DB: db}
		res, err := ingest.RunGovcnBatch(ctx, g, im, entries, badURLs, ingest.DefaultGovcnReportPath(a.dataDir, subdir),
			func(i, total int, it ingest.GovcnItem) {
				progress(fmt.Sprintf("%d/%d %s %s", i, total, it.Status, it.DocID))
			})
		if res != nil {
			progress(fmt.Sprintf("完成 %d/%d %v（报告 %s）", res.Total, len(entries), res.Counts, filepath.Base(res.ReportPath)))
		}
		return err
	})
}

// EUTextTaskOptions 欧盟全文导入任务参数（与 openlaw-eu-text CLI flag 同语义）。
type EUTextTaskOptions struct {
	Celex    string `json:"celex"`
	Lang     string `json:"lang"` // 空 = eng
	DelaySec int    `json:"delay_sec"`
	DryRun   bool   `json:"dry_run"`
}

// StartEUTextTask 提交欧盟全文导入任务（ingest.RunEUText——与 CLI 同路径）。
// 元数据先行纪律：CELEX 对应的法必须已由欧盟元数据通道建立，否则任务如实失败。
func (a *App) StartEUTextTask(o EUTextTaskOptions) (TaskInfo, error) {
	celex := strings.ToUpper(strings.TrimSpace(o.Celex))
	if celex == "" {
		return TaskInfo{}, errors.New("请先提供 CELEX 编号（如 32016R0679；须先经欧盟元数据通道建立条目）")
	}
	if o.Lang == "" {
		o.Lang = "eng"
	}
	if o.DelaySec < 2 {
		o.DelaySec = 2 // 礼貌限速纪律，与 CLI 同样钳制
	}
	params := fmt.Sprintf("celex=%s lang=%s dry_run=%v delay=%ds", celex, o.Lang, o.DryRun, o.DelaySec)
	return a.tm.Submit("eu-text", params, func(ctx context.Context, progress func(string)) error {
		db, err := store.Open(filepath.Join(a.dataDir, "db", "openlaw.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		progress("获取全文（官方出版局通道，礼貌限速）…")
		c := &ingest.Cellar{DataDir: a.dataDir, DB: db,
			Policy: ingest.FetchPolicy{Timeout: 60 * time.Second}, MinInterval: time.Duration(o.DelaySec) * time.Second}
		res, err := ingest.RunEUText(ctx, a.dataDir, db, c, ingest.EUTextOptions{
			Celex: celex, Lang: o.Lang, DryRun: o.DryRun,
		})
		if res != nil {
			if res.DryRun {
				progress(fmt.Sprintf("试采完成：%d 条（未入库）", res.Articles))
			} else {
				progress(fmt.Sprintf("已入库待复核：%d 条（%s）", res.Articles, res.DocID))
			}
		}
		return err
	})
}

// GovListTaskOptions govlist 清单浏览任务参数（与 openlaw-govlist CLI flag 同语义）。
type GovListTaskOptions struct {
	Cat       string `json:"cat"` // 空 | 部门规章 | 地方政府规章
	MaxPages  int    `json:"max_pages"`
	StartPage int    `json:"start_page"`
	DelayMs   int    `json:"delay_ms"`
	EmptyStop int    `json:"empty_stop"`
}

// StartGovListTask 提交国家规章库清单浏览任务（browser.RunGovListAgent——与 CLI 同路径）。
// 默认离线纪律不变：BrowserOS 端点未配置直接拒绝，绝不降级到其他抓取方式。
func (a *App) StartGovListTask(o GovListTaskOptions) (TaskInfo, error) {
	b := browser.NewBrowserOS(os.Getenv("OPENLAW_BROWSEROS_ENDPOINT"))
	if !b.Enabled() {
		return TaskInfo{}, errors.New(browser.Guidance)
	}
	if o.DelayMs < 2000 {
		o.DelayMs = 2500 // 与 CLI 一致：低于下限钳回默认
	}
	params := fmt.Sprintf("cat=%s pages=%d start=%d delay=%dms empty_stop=%d",
		o.Cat, o.MaxPages, o.StartPage, o.DelayMs, o.EmptyStop)
	return a.tm.Submit("govlist-browse", params, func(ctx context.Context, progress func(string)) error {
		_, err := browser.RunGovListAgent(ctx, b, browser.GovListOptions{
			Cat: o.Cat, MaxPages: o.MaxPages, Delay: time.Duration(o.DelayMs) * time.Millisecond,
			StartPage: o.StartPage, EmptyStop: o.EmptyStop,
		}, browser.DefaultGovListOutPath(a.dataDir), func(pages, entries int) {
			progress(fmt.Sprintf("页 %d 条 %d", pages, entries))
		})
		return err
	})
}
