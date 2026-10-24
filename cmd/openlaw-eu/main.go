// openlaw-eu：欧盟法律元数据导入 CLI（docs/sources-eu.md）。
// 默认离线：本命令不运行则绝不联网。数据源为欧盟出版局 Cellar SPARQL（官方开放程序化入口，
// robots Allow:/webapi；与 flk 的「禁止自动化」性质相反）。
// 流程：CELEX → work 级元数据（ELI/生效日/适用日/废止关系）→ 原始 JSON 落暂存区留证
// → law 主表 upsert（eli/celex/欧盟效力等级）→ 废止/依据关系仅挂已入库目标法（pending 人工确认）。
// 全文文本不在本命令范围：EUR-Lex 网页有 WAF 挑战，须人工保存后走 openlaw-ingest 闭环。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	var (
		dataDir = flag.String("data", "data", "数据目录")
		celex   = flag.String("celex", "", "CELEX 编号（必填，如 32016R0679）")
		name    = flag.String("name", "", "法规名称（人工提供；留空用回退名并备注待补充）")
		short   = flag.String("short-name", "", "法规简称（如 GDPR）")
		issuer  = flag.String("issuer", "European Union", "制定机关")
		timeout = flag.Duration("timeout", 30*time.Second, "单次请求超时")
		delay   = flag.Duration("delay", 2*time.Second, "SPARQL 查询间隔（礼貌限速，不建议低于 2s）")
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

	c := &ingest.Cellar{
		DataDir:     *dataDir,
		DB:          db,
		Policy:      ingest.FetchPolicy{Timeout: *timeout},
		MinInterval: *delay,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	w, err := c.FetchWork(ctx, *celex)
	if err != nil {
		fatal(err)
	}
	lawID, err := c.UpsertLaw(w, *name, *short, *issuer)
	if err != nil {
		fatal(err)
	}
	linked, skipped, err := c.LinkRelations(ctx, lawID, w)
	if err != nil {
		fatal(err)
	}

	out := map[string]any{
		"law_id": lawID, "celex": w.Celex, "eli": w.ELI,
		"effect_level": w.EffectLevel(), "type": w.TypeLetter,
		"in_force": w.InForce, "entry_into_force": w.EntryIntoForce,
		"application_date": w.ApplicationDate(), "expire_date": w.ExpireDate(),
		"official_journal": w.OJRef, "graph": w.GraphURI,
		"repeals_total":            len(w.Repeals) + len(w.ImplicitRepeal),
		"relations_total":          len(w.Repeals) + len(w.ImplicitRepeal) + len(w.BasedOnLegal),
		"relations_linked_pending": linked, "relations_skipped_no_dst": skipped,
		"treaties": len(w.Treaties),
		"note":     "仅元数据入库（law + pending 关系）；全文须人工保存 EUR-Lex 后走 openlaw-ingest",
	}
	if *asJSON {
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("law_id        %s\n", lawID)
		fmt.Printf("CELEX / ELI   %s / %s\n", w.Celex, w.ELI)
		fmt.Printf("效力等级      %s（in force=%v）\n", w.EffectLevel(), w.InForce)
		fmt.Printf("生效日        %s\n", w.EntryIntoForce)
		if a := w.ApplicationDate(); a != "" {
			fmt.Printf("适用日        %s（欧盟特有：生效≠适用）\n", a)
		}
		fmt.Printf("官方公报      %s\n", w.OJRef)
		fmt.Printf("关系        元数据共 %d 条（废止+依据）：已挂 pending %d，目标未入库跳过 %d\n",
			len(w.Repeals)+len(w.ImplicitRepeal)+len(w.BasedOnLegal), linked, skipped)
		fmt.Printf("留证          data/incoming/eu_%s_work.json（查询已进 import_log）\n", w.Celex)
		fmt.Println("下一步        全文人工保存（EUR-Lex 有 WAF）→ openlaw-ingest 建版本与条文")
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(2)
}
