// openlaw-us：美国官方通道（骨架，docs/sources-us.md、docs/design-multi-jurisdiction.md §6）。
// 默认离线：本命令不运行则绝不联网。数据源为 govinfo（GPO）批量数据清单——
// robots.txt 开放批量目录并显式公布 sitemap 入口（与 flk 的「禁止自动化」性质相反）。
// 本骨架只做「批量清单拉取与留证」：sitemapindex → incoming/ 留证 + import_log 留痕，
// 供发版流水线核对可采范围。全文解析入库（USLM/USC 结构化、规章切分、生效废止关系）
// 尚未开通——如实拒绝而非假装可采：那是写路径高风险变更，另行按阶段实施。
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
	if *collection == "" {
		fatal(fmt.Errorf("-collection 必填（-list 可查收录集合）；全文采集通道尚未开通，本命令只做清单拉取与留证"))
	}
	if err := os.MkdirAll(filepath.Join(*dataDir, "db"), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()

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
		fmt.Println("说明：本通道为骨架——清单已留证可核对；全文解析入库尚未开通（docs/design-multi-jurisdiction.md §6）。")
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "openlaw-us:", err)
	os.Exit(1)
}
