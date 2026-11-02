// openlaw-us：美国官方通道（docs/sources-us.md、docs/design-multi-jurisdiction.md §6）。
// 默认离线：本命令不运行则绝不联网。数据源为 govinfo（GPO）批量数据——
// robots.txt 开放批量目录并显式公布 sitemap 入口（与 flk 的「禁止自动化」性质相反）。
// 两种模式：
//   - 清单模式（默认）：sitemapindex → incoming/ 留证 + import_log 留痕，供发版流水线核对可采范围；
//   - 全文模式（-ingest）：解析批量包解出的 USLM XML 入库（law.jurisdiction=US、
//     confirm_status=pending 人工闸口、原始 XML 归档 xml/ 层）——与桌面端共用
//     ingest.RunUSLMImport。批量包本体的下载仍由用户经官方渠道自行完成（体积与
//     配额纪律），本命令不代下载。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	var (
		dataDir    = flag.String("data", "data", "数据目录")
		collection = flag.String("collection", "", "govinfo 批量集合名（如 ECFR/CFR/STATUTE/PLAW/FR）")
		list       = flag.Bool("list", false, "列出收录的集合名后退出")
		timeout    = flag.Duration("timeout", 30*time.Second, "单次请求超时")
		delay      = flag.Duration("delay", 2*time.Second, "请求间隔（礼貌限速，不得低于 2s）")
		asJSON     = flag.Bool("json", false, "以 JSON 输出结果")
		// 全文模式（USLM 解析入库）。
		ingestPath  = flag.String("ingest", "", "全文模式：批量包解出的 USLM XML 文件路径")
		docID       = flag.String("doc-id", "", "全文模式：条目编号（如 us_ecfr_title12_2025）")
		lawName     = flag.String("name", "", "全文模式：法规名称（留空用文件标题）")
		lawID       = flag.String("law-id", "", "全文模式：法规标识（留空同条目编号）")
		issuer      = flag.String("issuer", "", "全文模式：发布机关")
		level       = flag.String("effect-level", "联邦规章", "全文模式：效力等级（决定法域推导 US）")
		versionNo   = flag.String("version-no", "", "全文模式：版本号（如 2025 annual edition；留空=生效日期版）")
		effective   = flag.String("effective", "", "全文模式：版本生效日期 ISO（必填，来自批量包元数据，不得猜）")
		publishDate = flag.String("publish-date", "", "全文模式：公布日期 ISO（可空=未知）")
		sourceURL   = flag.String("source-url", "", "全文模式：批量包来源地址")
		dryRun      = flag.Bool("dry-run", false, "全文模式：只解析不入库")
	)
	flag.Parse()

	if *list {
		names := make([]string, 0, len(ingest.USBulkCollections))
		for k := range ingest.USBulkCollections {
			names = append(names, k)
		}
		sort.Strings(names)
		fmt.Println("govinfo 批量集合（收录清单见 docs/sources-us.md）：\n  " + strings.Join(names, "\n  "))
		fmt.Println("注意：USCODE 无批量包；联邦法律现行文本以 Statute Compilations（beta）承载。")
		return
	}
	if err := os.MkdirAll(filepath.Join(*dataDir, "db"), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	if *ingestPath != "" {
		if *docID == "" {
			fatal(fmt.Errorf("-ingest 必须配 -doc-id（条目编号，如 us_ecfr_title12_2025）"))
		}
		res, err := ingest.RunUSLMImport(*dataDir, db, ingest.USLMImportOptions{
			XMLPath: *ingestPath, DocID: *docID, LawID: *lawID, Name: *lawName,
			Issuer: *issuer, EffectLevel: *level, VersionNo: *versionNo,
			EditionDate: *effective, PublishDate: *publishDate,
			SourceURL: *sourceURL, DryRun: *dryRun,
		})
		if err != nil {
			fatal(err)
		}
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(res); err != nil {
				fatal(err)
			}
			return
		}
		fmt.Printf("标题        %s\n", res.Title)
		fmt.Printf("引注        %s\n", res.Citation)
		fmt.Printf("条文        %d 条（解析器 %s）\n", res.Articles, res.ParserVersion)
		if res.DryRun {
			fmt.Println("dry-run     未入库（去掉 -dry-run 执行导入）")
			return
		}
		fmt.Printf("条目        %s（law_id %s，待复核）\n", res.DocID, res.LawID)
		fmt.Printf("断链检查    %d 项问题\n", res.ChainIssues)
		fmt.Println("说明：新条目一律待复核——请在界面或 openlaw confirm 复核落判。")
		return
	}

	if *collection == "" {
		fatal(fmt.Errorf("-collection 必填（-list 可查收录集合）；全文导入用 -ingest <USLM XML>（批量包文件需自行解出）"))
	}

	u := &ingest.USGovinfo{
		DataDir:     *dataDir,
		DB:          db,
		Policy:      ingest.FetchPolicy{Timeout: *timeout},
		MinInterval: *delay,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	idx, err := u.FetchSitemapIndex(ctx, *collection)
	if err != nil {
		fatal(err)
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(idx); err != nil {
			fatal(err)
		}
	} else {
		fmt.Printf("集合 %s：批量清单 %d 项\n", idx.Collection, len(idx.Locs))
		fmt.Printf("留证：%s（SHA-256 %s）\n", filepath.Join(*dataDir, idx.RawPath), idx.SHA256)
		shown := idx.Locs
		if len(shown) > 5 {
			shown = shown[:5]
		}
		for _, l := range shown {
			fmt.Println("  ", l)
		}
		if len(idx.Locs) > len(shown) {
			fmt.Printf("   …其余 %d 项见留证文件\n", len(idx.Locs)-len(shown))
		}
		fmt.Println("说明：清单已留证可核对；全文导入用 -ingest 指向批量包解出的 USLM 文件。")
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "openlaw-us:", err)
	os.Exit(1)
}
