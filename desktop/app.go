// app.go 语料台应用层：全部走与 CLI 相同的 store/ingest/validate 代码路径。
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
	"strings"
	"unicode/utf8"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

type App struct {
	ctx     context.Context
	dataDir string
	db      *store.DB
	openErr string
}

func NewApp(dataDir string) *App { return &App{dataDir: dataDir} }

func (a *App) Open() error {
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
	return nil
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }
func (a *App) shutdown(ctx context.Context) {
	if a.db != nil {
		a.db.Close()
		a.db = nil
	}
}

// ---- 语料状态面板 ----

type LevelCount struct {
	Level     string `json:"level"`
	Pending   int    `json:"pending"`
	Confirmed int    `json:"confirmed"`
	Rejected  int    `json:"rejected"`
}

type StatusView struct {
	DataDir         string       `json:"data_dir"`
	OpenErr         string       `json:"open_err,omitempty"`
	SchemaVersion   string       `json:"schema_version"`
	SchemaMin       string       `json:"schema_min"`
	SchemaMax       string       `json:"schema_max"`
	ProducerVersion string       `json:"producer_version"`
	License         string       `json:"license,omitempty"`
	ManifestAt      string       `json:"manifest_at,omitempty"`
	Laws            int          `json:"laws"`
	Versions        int          `json:"versions"`
	Articles        int          `json:"articles"`
	Documents       int          `json:"documents"`
	Pending         int          `json:"pending"`
	Confirmed       int          `json:"confirmed"`
	Rejected        int          `json:"rejected"`
	Govcn           int          `json:"govcn"`
	Govlocal        int          `json:"govlocal"`
	ByLevel         []LevelCount `json:"by_level"`
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

// Status 语料状态汇总（计数/来源版本/schema/许可）。
func (a *App) Status() (StatusView, error) {
	v := StatusView{DataDir: a.dataDir, OpenErr: a.openErr, SchemaMin: store.SchemaMin, SchemaMax: store.SchemaMax}
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

	rows, err := a.db.Query(`SELECT COALESCE(l.effect_level,'（未标注）'),
		SUM(d.confirm_status='pending'), SUM(d.confirm_status='confirmed'), SUM(d.confirm_status='rejected')
		FROM document d LEFT JOIN law_version v ON v.doc_id=d.doc_id LEFT JOIN law l ON l.law_id=v.law_id
		GROUP BY 1 ORDER BY 2 DESC`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var lc LevelCount
			if err := rows.Scan(&lc.Level, &lc.Pending, &lc.Confirmed, &lc.Rejected); err == nil {
				v.ByLevel = append(v.ByLevel, lc)
			}
		}
	}
	if m, _, err := validate.LoadManifest(a.dataDir); err == nil {
		v.ProducerVersion = m.ProducerVersion
		if v.ProducerVersion == "" {
			v.ProducerVersion = "来源未知（旧语料包无 producer_version）"
		}
		v.License = m.License
		v.ManifestAt = m.GeneratedAt
	} else {
		v.ProducerVersion = "无 manifest（尚未 openlaw-pack build / check）"
	}
	return v, nil
}

// ---- 人工确认闸口面板 ----

type PendingItem struct {
	DocID      string `json:"doc_id"`
	Title      string `json:"title"`
	Level      string `json:"level"`
	Issuer     string `json:"issuer"`
	VersionNo  string `json:"version_no"`
	Effective  string `json:"effective_date"`
	Publish    string `json:"publish_date"`
	SourceType string `json:"source_type"`
	SourceURL  string `json:"source_url"`
	Articles   int    `json:"articles"`
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
	rows, err := a.db.Query(`SELECT d.doc_id, COALESCE(l.name,''), COALESCE(l.effect_level,'（未标注）'),
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
		out = append(out, p)
	}
	return out, rows.Err()
}

type PreviewView struct {
	DocID      string   `json:"doc_id"`
	FilePath   string   `json:"file_path"`
	SourceURL  string   `json:"source_url"`
	SourceType string   `json:"source_type"`
	Publish    string   `json:"publish_date"`
	SHA256     string   `json:"sha256"`
	Status     string   `json:"confirm_status"`
	Content    string   `json:"content"`
	Truncated  bool     `json:"truncated"`
	Missing    string   `json:"missing,omitempty"`
	ReviewLog  []string `json:"review_log"`
}

// Preview 文档留证正文（file_path 指向的 md/txt，只读展示）+ 审计历史。
func (a *App) Preview(docID string) (PreviewView, error) {
	var p PreviewView
	if a.db == nil {
		return p, errors.New(a.openErr)
	}
	err := a.db.QueryRow(`SELECT doc_id, file_path, COALESCE(source_url,''), COALESCE(source_type,''),
		COALESCE(publish_date,''), sha256, confirm_status FROM document WHERE doc_id=?`, docID).
		Scan(&p.DocID, &p.FilePath, &p.SourceURL, &p.SourceType, &p.Publish, &p.SHA256, &p.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("文档不存在: %s", docID)
	} else if err != nil {
		return p, err
	}
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
		return nil, errors.New("未选择文档")
	}
	switch verdict {
	case "confirm", "reject", "reset":
	default:
		return nil, fmt.Errorf("verdict 只能为 confirm/reject/reset: %q", verdict)
	}
	if strings.TrimSpace(reviewer) == "" || strings.TrimSpace(reason) == "" {
		return nil, errors.New("机器不裁决：落判必须署名（reviewer）并写明依据（reason）")
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
