// openlaw-fetch：显式触发的抓取 CLI（开发文档 6.1、18 章）。
// 默认离线：本命令不运行则程序绝不联网；不做静默自动抓取更新（6.4）。
// 流程：策略检查（HTTPS/白名单/大小/Content-Type）→ 下载到 data/incoming/ 暂存区
// → 哈希 → import_log（source_url/http_status/content_type/抓取时间）→
// 若提供 -md 则继续走 openlaw-ingest 同一导入闭环（confirm_status 默认 pending 转人工确认）。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	var (
		dataDir    = flag.String("data", "data", "数据目录")
		url_       = flag.String("url", "", "PDF 直链（必填，须为官方公报或官方发布渠道）")
		docID      = flag.String("doc-id", "", "文档唯一标识（必填）")
		allowHosts = flag.String("allow-hosts", "", "来源域名白名单，逗号分隔（如 www.npc.gov.cn）；留空=允许全部（不推荐）")
		maxSize    = flag.Int64("max-size", 64<<20, "响应体大小上限（字节）")
		timeout    = flag.Duration("timeout", 30*time.Second, "请求超时")

		// 以下与 openlaw-ingest 相同：提供 -md 时抓取后继续导入
		md      = flag.String("md", "", "解析后的 Markdown 正文路径（可选；提供则抓取后继续导入）")
		lawID   = flag.String("law-id", "", "法规标识（-md 时必填）")
		lawName = flag.String("law-name", "", "法规名称（-md 时必填）")
		short   = flag.String("short-name", "", "法规简称")
		issuer  = flag.String("issuer", "", "制定机关")
		level   = flag.String("level", "", "效力等级")
		ver     = flag.String("version", "", "版本号（-md 时必填）")
		eff     = flag.String("effective", "", "生效日期（-md 时必填）")
		exp     = flag.String("expire", "", "失效日期（留空=未失效）")
		pub     = flag.String("publish", "", "公布日期")
		srcType = flag.String("source-type", "官方网站", "来源类型：公报/官方网站/司法解释库等")
		gongbao = flag.Bool("gongbao", false, "是否公报文本")
		confirm = flag.String("confirm", "pending", "确认状态：confirmed/pending（默认 pending 转人工）")
	)
	flag.Parse()

	if *url_ == "" || *docID == "" {
		fmt.Fprintln(os.Stderr, "必须提供 -url 与 -doc-id")
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(*dataDir, "db"), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	fetcher := &ingest.Fetcher{
		DataDir: *dataDir, DB: db,
		Policy: ingest.FetchPolicy{
			AllowHosts: splitHosts(*allowHosts),
			MaxSize:    *maxSize,
			Timeout:    *timeout,
		},
	}
	fetched, err := fetcher.Fetch(context.Background(), *url_, *docID)
	if err != nil {
		fatal(err)
	}
	out, _ := json.MarshalIndent(map[string]any{
		"status":       "fetched",
		"doc_id":       *docID,
		"staged_path":  filepath.Join(*dataDir, filepath.FromSlash(fetched.Path)),
		"final_url":    fetched.FinalURL,
		"http_status":  fetched.HTTPStatus,
		"content_type": fetched.ContentType,
		"sha256":       fetched.SHA256,
		"size":         fetched.Size,
		"fetched_at":   fetched.FetchedAt,
	}, "", "  ")
	fmt.Println(string(out))

	if *md == "" {
		fmt.Println("下一步：解析正文（人工或解析器），再运行 openlaw-ingest 导入并转人工确认。")
		return
	}

	// 显式提供正文时，继续走与 openlaw-ingest 完全相同的导入闭环
	im := &ingest.Importer{DataDir: *dataDir, DB: db}
	res, err := im.Import(ingest.Request{
		DocID: *docID, LawID: *lawID, LawName: *lawName, ShortName: *short,
		Issuer: *issuer, EffectLevel: *level, VersionNo: *ver,
		EffectiveDate: *eff, ExpireDate: *exp,
		SourceURL: fetched.FinalURL, SourceType: *srcType, IsGongbao: *gongbao,
		PublishDate: *pub, ConfirmStatus: *confirm,
		PDFPath: filepath.Join(*dataDir, filepath.FromSlash(fetched.Path)),
		MDPath:  *md,
	})
	if err != nil {
		fatal(err)
	}
	out2, _ := json.MarshalIndent(map[string]any{
		"status": "imported", "doc_id": res.DocID, "version_id": res.VersionID,
		"articles": len(res.ArticleIDs), "chain_issues": res.ChainIssues,
	}, "", "  ")
	fmt.Println(string(out2))
}

func splitHosts(s string) []string {
	var hosts []string
	for _, h := range strings.Split(s, ",") {
		if h = strings.TrimSpace(h); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "抓取失败:", err)
	os.Exit(1)
}
