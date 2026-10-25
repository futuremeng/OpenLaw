// openlaw-eu-text：欧盟全文导入 CLI（docs/sources-eu-text.md，M1）。
// 默认离线：本命令不运行则绝不联网。数据源为 Cellar representation 层
// （官方开放程序化入口，四步链 expression→manifestation→item，无 WAF）。
// 流程：CELEX+语言 → FetchText（FORMEX ACT XML 首选）→ ParseFormexACT
// （条文边界来自 <ARTICLE> 结构元素，零猜测）→ Importer 入库
// （document.lang=eng、parser_version=openlaw-parse/0.1.0+formex-eu、confirm_status=pending 人工闸口）。
// 前置：law 已由 openlaw-eu 建立（--celex 能按 celex 列查到 law_id），否则报错。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	var (
		dataDir = flag.String("data", "data", "数据目录")
		celex   = flag.String("celex", "", "CELEX 编号（必填，如 32016R0679）")
		lang    = flag.String("lang", "eng", "正文语言（ISO 639-3，如 eng/deu/fra）")
		format  = flag.String("format", "fmx4", "全文格式：fmx4（FORMEX XML，首选）| xhtml（CONVEX 回退）")
		version = flag.String("version-no", "OJ原文", "版本号（law_version.version_no）")
		name    = flag.String("name", "", "法规名称（留空用库内既有名称；库内为回退名时用 Cellar 官方全称）")
		dryRun  = flag.Bool("dry-run", false, "只获取与解析，不入库")
		timeout = flag.Duration("timeout", 60*time.Second, "单次请求超时")
		delay   = flag.Duration("delay", 2*time.Second, "Cellar 请求间隔（礼貌限速）")
		asJSON  = flag.Bool("json", false, "以 JSON 输出结果")
	)
	flag.Parse()

	if *celex == "" {
		fatal(fmt.Errorf("-celex 必填（CELEX 编号，如 32016R0679）"))
	}
	if err := os.MkdirAll(filepath.Join(*dataDir, "db"), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	// 前置：law 必须已由 openlaw-eu 建立（元数据先行纪律，不自动建占位法）
	var lawID, dbName, shortName string
	err = db.QueryRow(`SELECT law_id, name, COALESCE(short_name,'') FROM law WHERE celex = ?`,
		strings.ToUpper(*celex)).Scan(&lawID, &dbName, &shortName)
	if errors.Is(err, sql.ErrNoRows) {
		fatal(fmt.Errorf("库内无 CELEX %s 对应的法——先运行 openlaw-eu -celex %s 导入元数据", *celex, *celex))
	} else if err != nil {
		fatal(err)
	}

	c := &ingest.Cellar{DataDir: *dataDir, DB: db,
		Policy: ingest.FetchPolicy{Timeout: *timeout}, MinInterval: *delay}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t, err := c.FetchText(ctx, *celex, *lang, *format)
	if err != nil {
		fatal(err)
	}

	// 解析：fmx4 走结构化 FORMEX；xhtml 走文本切分回退
	var arts []parse.Article
	var mdText string
	pv := parse.ParserVersionEU
	if t.Format == "fmx4" {
		fx, perr := parse.ParseFormexACT(t.Bytes)
		if perr != nil {
			fatal(fmt.Errorf("%w（可尝试 -format xhtml 回退）", perr))
		}
		arts, mdText = fx.Articles, fx.FullText
	} else {
		text := strings.TrimSpace(string(t.Bytes))
		arts = parse.SplitArticlesEU(text)
		if len(arts) == 0 {
			fatal(fmt.Errorf("XHTML 正文未识别出任何 Article（转 PDF 归档 + 人工队列）"))
		}
		mdText = text
		pv = parse.ParserVersionEU + "+xhtml"
	}

	// 名称：-name 优先；库内是占位回退名时用 Cellar 官方全称
	lawName := *name
	if lawName == "" {
		if strings.Contains(dbName, "[标题待人工补充]") && t.Title != "" {
			lawName = t.Title
		} else {
			lawName = dbName
		}
	}

	absRaw := filepath.Join(*dataDir, t.RawPath)
	if *dryRun {
		out := map[string]any{
			"celex": t.Celex, "lang": t.Lang, "format": t.Format, "law_id": lawID,
			"title": t.Title, "articles": len(arts),
			"first": arts[0].No, "last": arts[len(arts)-1].No,
			"sha256": t.SHA256, "size": t.Size, "raw": t.RawPath,
			"item_uri": t.ItemURI, "manifestation": t.ManifestationURI,
			"dry_run": true,
		}
		if *asJSON {
			b, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(b))
		} else {
			fmt.Printf("law_id      %s\n", lawID)
			fmt.Printf("标题        %s\n", t.Title)
			fmt.Printf("条文        %d 条（%s … %s）\n", len(arts), arts[0].No, arts[len(arts)-1].No)
			fmt.Printf("正文        %s（%.1f KB, sha256=%s…）\n", t.RawPath, float64(t.Size)/1024, t.SHA256[:16])
			fmt.Printf("item        %s\n", t.ItemURI)
			fmt.Println("dry-run     未入库（去掉 -dry-run 执行导入）")
		}
		return
	}

	// 元数据（日期等）仅入库路径需要——dry-run 绝不多发此请求（省 Cellar 配额，RL 纪律）
	w, err := c.FetchWork(ctx, *celex)
	if err != nil {
		fatal(fmt.Errorf("元数据（日期等）获取失败: %w", err))
	}

	pub := w.Created
	if pub == "" {
		pub = w.Signed
	}
	notes := fmt.Sprintf("Cellar 全文通道：CELEX %s.%s；manifestation %s；item %s；%s",
		t.Celex, t.Lang, t.ManifestationURI, t.ItemURI, t.FetchedAt)
	im := &ingest.Importer{DataDir: *dataDir, DB: db}
	res, err := im.Import(ingest.Request{
		DocID:         "eu_" + strings.ToLower(t.Celex) + "_" + t.Lang,
		LawID:         lawID,
		LawName:       lawName,
		ShortName:     shortName,
		Issuer:        "European Union",
		EffectLevel:   w.EffectLevel(),
		VersionNo:     *version,
		EffectiveDate: w.EffectiveDate(),
		ExpireDate:    w.ExpireDate(),
		PublishDate:   pub,
		SourceURL:     t.ItemURI,
		SourceType:    "cellar",
		ConfirmStatus: "pending",
		Notes:         notes,
		PDFPath:       absRaw,
		Lang:          t.Lang,
		Articles:      arts,
		MDContent:     mdText,
		ParserVersion: pv,
	})
	if err != nil {
		fatal(err)
	}

	out := map[string]any{
		"doc_id": res.DocID, "version_id": res.VersionID, "law_id": lawID,
		"articles": len(res.ArticleIDs), "sha256": res.SHA256,
		"lang": t.Lang, "parser_version": pv, "confirm_status": "pending",
		"chain_issues": len(res.ChainIssues),
	}
	if *asJSON {
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("doc_id      %s\n", res.DocID)
		fmt.Printf("version     %s\n", res.VersionID)
		fmt.Printf("条文        %d 条（结构化 ARTICLE 边界，parser=%s）\n", len(res.ArticleIDs), pv)
		fmt.Printf("sha256      %s\n", res.SHA256)
		fmt.Printf("确认状态    pending（人工闸口：openlaw-confirm sample/apply）\n")
		if len(res.ChainIssues) > 0 {
			fmt.Printf("断链检测    %d 项异常（openlaw-check 复核）\n", len(res.ChainIssues))
		} else {
			fmt.Println("断链检测    通过")
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(2)
}
