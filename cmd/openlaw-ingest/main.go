// openlaw-ingest：导入 CLI（开发文档 3.5、6.1）。
// 流程：PDF → 哈希 → 溯源 JSON → 解析拆分 → 事务入库 → import_log → 断链检测。
// 任一环节失败整批标记失败并回滚写入。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	var (
		dataDir = flag.String("data", "data", "数据目录")
		docID   = flag.String("doc-id", "", "文档唯一标识（必填）")
		lawID   = flag.String("law-id", "", "法规标识（必填）")
		lawName = flag.String("law-name", "", "法规名称（必填）")
		short   = flag.String("short-name", "", "法规简称")
		issuer  = flag.String("issuer", "", "制定机关")
		level   = flag.String("level", "", "效力等级：法律/行政法规/地方性法规/司法解释等")
		ver     = flag.String("version", "", "版本号，如 2023修正（必填）")
		eff     = flag.String("effective", "", "生效日期 YYYY-MM-DD 或 YYYY年M月D日（必填）")
		exp     = flag.String("expire", "", "失效日期（留空表示未失效，左闭右开）")
		url     = flag.String("source-url", "", "来源 URL（本地导入可留空）")
		srcType = flag.String("source-type", "本地导入", "来源类型")
		gongbao = flag.Bool("gongbao", false, "是否公报文本")
		pub     = flag.String("publish", "", "公布日期")
		confirm = flag.String("confirm", "pending", "确认状态：confirmed/pending")
		pdf     = flag.String("pdf", "", "PDF 路径（必填）")
		md      = flag.String("md", "", "Markdown 正文路径（必填）")
	)
	flag.Parse()

	if err := os.MkdirAll(filepath.Join(*dataDir, "db"), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	im := &ingest.Importer{DataDir: *dataDir, DB: db}
	res, err := im.Import(ingest.Request{
		DocID: *docID, LawID: *lawID, LawName: *lawName, ShortName: *short,
		Issuer: *issuer, EffectLevel: *level, VersionNo: *ver,
		EffectiveDate: *eff, ExpireDate: *exp,
		SourceURL: *url, SourceType: *srcType, IsGongbao: *gongbao,
		PublishDate: *pub, ConfirmStatus: *confirm,
		PDFPath: *pdf, MDPath: *md,
	})
	if err != nil {
		fatal(err)
	}

	out, _ := json.MarshalIndent(map[string]any{
		"status":       "success",
		"doc_id":       res.DocID,
		"version_id":   res.VersionID,
		"sha256":       res.SHA256,
		"articles":     len(res.ArticleIDs),
		"chain_issues": res.ChainIssues,
	}, "", "  ")
	fmt.Println(string(out))
	if len(res.ChainIssues) > 0 {
		fmt.Fprintln(os.Stderr, "警告：导入成功但溯源链存在断点（如 manifest 尚未生成），请执行 openlaw-check")
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "导入失败:", err)
	os.Exit(1)
}
