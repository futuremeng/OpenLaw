// eutext.go 欧盟全文通道 runner（docs/sources-eu-text.md）：CELEX+语言 → Cellar
// representation 层取全文 → FORMEX/XHTML 解析 → Importer 入库。
// CLI（openlaw-eu-text）与桌面端「获取最新法规」共用本 runner——同一代码路径纪律。
// 人工闸口：新条目一律 confirm_status=pending；dry-run 绝不发元数据请求（省 Cellar 配额）。
package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
)

// EUTextOptions 全文导入参数（与 openlaw-eu-text flag 同语义）。
type EUTextOptions struct {
	Celex     string // CELEX 编号（必填；law 必须已由元数据通道建立）
	Lang      string // 正文语言 ISO 639-3（默认 eng）
	Format    string // fmx4（FORMEX XML，首选）| xhtml（CONVEX 回退）；默认 fmx4
	VersionNo string // 版本号（默认「OJ原文」）
	Name      string // 法规名称（留空用库内既有名；库内为回退占位名时用 Cellar 官方全称）
	DryRun    bool   // 只获取与解析，不入库
}

// EUTextResult 全文导入结果（dry-run 时 DocID 为空、DryRun=true）。
type EUTextResult struct {
	Celex         string `json:"celex"`
	Lang          string `json:"lang"`
	Format        string `json:"format"`
	LawID         string `json:"law_id"`
	Title         string `json:"title"`
	Articles      int    `json:"articles"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	RawPath       string `json:"raw"`
	ItemURI       string `json:"item_uri,omitempty"`
	Manifestation string `json:"manifestation,omitempty"`
	ParserVersion string `json:"parser_version"`
	DocID         string `json:"doc_id,omitempty"`
	VersionID     string `json:"version_id,omitempty"`
	ChainIssues   int    `json:"chain_issues"`
	DryRun        bool   `json:"dry_run,omitempty"`
}

// RunEUText 欧盟全文导入。law 必须已由元数据通道（openlaw-eu）建立——
// 元数据先行纪律，不自动建占位法。
func RunEUText(ctx context.Context, dataDir string, db *store.DB, c *Cellar, o EUTextOptions) (*EUTextResult, error) {
	if strings.TrimSpace(o.Celex) == "" {
		return nil, errors.New("CELEX 编号必填")
	}
	if o.Lang == "" {
		o.Lang = "eng"
	}
	if o.Format == "" {
		o.Format = "fmx4"
	}
	if o.VersionNo == "" {
		o.VersionNo = "OJ原文"
	}
	var lawID, dbName, shortName string
	err := db.QueryRow(`SELECT law_id, name, COALESCE(short_name,'') FROM law WHERE celex = ?`,
		strings.ToUpper(o.Celex)).Scan(&lawID, &dbName, &shortName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("库内无 CELEX %s 对应的法——先经欧盟元数据通道导入（openlaw-eu -celex %s）", o.Celex, o.Celex)
	} else if err != nil {
		return nil, err
	}

	t, err := c.FetchText(ctx, o.Celex, o.Lang, o.Format)
	if err != nil {
		return nil, err
	}
	res := &EUTextResult{Celex: t.Celex, Lang: t.Lang, Format: t.Format, LawID: lawID,
		Title: t.Title, SHA256: t.SHA256, Size: t.Size, RawPath: t.RawPath,
		ItemURI: t.ItemURI, Manifestation: t.ManifestationURI, DryRun: o.DryRun}

	var arts []parse.Article
	var mdText string
	res.ParserVersion = parse.ParserVersionEU
	if t.Format == "fmx4" {
		fx, perr := parse.ParseFormexACT(t.Bytes)
		if perr != nil {
			return nil, fmt.Errorf("%w（可尝试 xhtml 格式回退）", perr)
		}
		arts, mdText = fx.Articles, fx.FullText
	} else {
		text := strings.TrimSpace(string(t.Bytes))
		arts = parse.SplitArticlesEU(text)
		if len(arts) == 0 {
			return nil, errors.New("XHTML 正文未识别出任何 Article（转 PDF 归档 + 人工队列）")
		}
		mdText = text
		res.ParserVersion = parse.ParserVersionEU + "+xhtml"
	}
	res.Articles = len(arts)
	if o.DryRun {
		return res, nil
	}

	// 名称：-name 优先；库内是占位回退名时用 Cellar 官方全称。
	lawName := o.Name
	if lawName == "" {
		if strings.Contains(dbName, "[标题待人工补充]") && t.Title != "" {
			lawName = t.Title
		} else {
			lawName = dbName
		}
	}
	// 元数据（日期等）仅入库路径需要——dry-run 绝不多发此请求（省 Cellar 配额）。
	w, err := c.FetchWork(ctx, o.Celex)
	if err != nil {
		return nil, fmt.Errorf("元数据（日期等）获取失败: %w", err)
	}
	pub := w.Created
	if pub == "" {
		pub = w.Signed
	}
	notes := fmt.Sprintf("Cellar 全文通道：CELEX %s.%s；manifestation %s；item %s；%s",
		t.Celex, t.Lang, t.ManifestationURI, t.ItemURI, t.FetchedAt)
	im := &Importer{DataDir: dataDir, DB: db}
	ir, err := im.Import(Request{
		DocID: "eu_" + strings.ToLower(t.Celex) + "_" + t.Lang, LawID: lawID,
		LawName: lawName, ShortName: shortName, Issuer: "European Union",
		EffectLevel: w.EffectLevel(), VersionNo: o.VersionNo,
		EffectiveDate: w.EffectiveDate(), ExpireDate: w.ExpireDate(), PublishDate: pub,
		SourceURL: t.ItemURI, SourceType: "cellar", ConfirmStatus: "pending",
		Notes: notes, PDFPath: filepath.Join(dataDir, t.RawPath), Lang: t.Lang,
		Articles: arts, MDContent: mdText, ParserVersion: res.ParserVersion,
	})
	if err != nil {
		return nil, err
	}
	res.DocID, res.VersionID, res.ChainIssues = ir.DocID, ir.VersionID, len(ir.ChainIssues)
	return res, nil
}
