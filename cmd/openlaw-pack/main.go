// openlaw-pack：离线更新包工具（开发文档 6.4、16.2、FR-06）。
// build  : 数据目录 → pack 目录（manifest + Ed25519 签名 + changes.csv）
// verify : 校验包（签名/哈希/多余文件），失败退出码 2
// import : 校验通过后导入目标数据目录；失败保留旧数据
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/futuremeng/OpenLaw/pack"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	fs := flag.NewFlagSet("openlaw-pack", flag.ExitOnError)
	var (
		dataDir        = fs.String("data", "data", "数据目录（build 的源 / import 的目标）")
		packDir        = fs.String("pack", "", "更新包目录（必填）")
		keyPath        = fs.String("key", "", "签名私钥路径（build 必填；不进仓库不进数据目录）")
		license        = fs.String("license", "", "共享许可声明（build 可选，默认公有领域+署名要求）")
		attrib         = fs.String("attribution", "", "来源署名声明（build 可选；欧盟等来源要求重使用署名）")
		allowUntrusted = fs.Bool("allow-untrusted", false, "import 允许导入未列入受信发布者名单的包（仅本地自验）")
		packID         = fs.String("pack-id", "", "build：包标识（台账主键，基础包/增量包必填）")
		kind           = fs.String("kind", "", "build：包种类 base=基础包 | delta=增量包（留空=旧整包）")
		regions        = fs.String("regions", "", "build：地域过滤，逗号分隔区划码前缀；CN=国家级（如 CN,44）")
		since          = fs.String("since", "", "build：发布日期窗口起（含，如 2026-01-01）")
		until          = fs.String("until", "", "build：发布日期窗口止（含）")
		indexDir       = fs.String("index", "", "index：目录输出目录（index.json + index.sig）")
		urlBase        = fs.String("url-base", "", "index：下载直链前缀（与归档文件名拼接，如 GitHub Release 资产目录）")
		outPath        = fs.String("out", "", "archive：归档输出路径（默认 <pack 目录>.zip）")
	)
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(1)
	}
	if cmd != "keygen" && cmd != "index" && cmd != "index-verify" && *packDir == "" {
		fmt.Fprintln(os.Stderr, "必须提供 -pack")
		os.Exit(1)
	}

	switch cmd {
	case "build":
		if *keyPath == "" {
			fatal("build 必须指定 -key 私钥路径")
		}
		db := openDB(*dataDir)
		defer db.Close()
		opts := pack.BuildOptions{PackID: *packID, Kind: *kind, Since: *since, Until: *until}
		if *regions != "" {
			for _, r := range strings.Split(*regions, ",") {
				if r = strings.TrimSpace(r); r != "" {
					opts.Regions = append(opts.Regions, r)
				}
			}
		}
		if err := pack.BuildWithOptions(db, *dataDir, *packDir, *keyPath, *license, *attrib, opts); err != nil {
			fatal(err)
		}
		fmt.Println("更新包已生成并签名:", *packDir)
	case "verify":
		if err := pack.Verify(*packDir); err != nil {
			fmt.Fprintln(os.Stderr, "更新包校验失败:", err)
			os.Exit(2)
		}
		fmt.Println("更新包校验通过（签名有效、哈希一致、无未收录文件）")
		if keyID, err := pack.TrustOf(*packDir); err == nil {
			fmt.Println("来源可信：受信发布者", keyID)
		} else {
			fmt.Fprintln(os.Stderr, "警告：签名自洽但公钥不在受信发布者名单，导入将被拒绝（本地自验可用 -allow-untrusted）")
		}
	case "keygen":
		if *keyPath == "" {
			fatal("keygen 必须指定 -key 输出路径（该文件必须保存在仓库与数据目录之外）")
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fatal(err)
		}
		if err := os.WriteFile(*keyPath, []byte(hex.EncodeToString(priv.Seed())+"\n"), 0o600); err != nil {
			fatal(err)
		}
		fmt.Println("私钥已写入:", *keyPath, "（0600，务必备份且不出仓库）")
		fmt.Println("公钥（发版时内置为受信发布者）:", hex.EncodeToString(pub))
	case "import":
		db := openDB(*dataDir)
		defer db.Close()
		var rep pack.ImportReport
		var err error
		if *allowUntrusted {
			fmt.Fprintln(os.Stderr, "注意：-allow-untrusted 跳过来源信任检查，仅适用于本地自验。")
			rep, err = pack.ImportPackAllowUntrusted(db, *dataDir, *packDir)
		} else {
			rep, err = pack.ImportPack(db, *dataDir, *packDir)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "导入中止（旧数据未受影响）: %v\n", err)
			fmt.Fprintf(os.Stderr, "已导入 %d 条、跳过 %d 条（均为新增，可经 import_log 追溯；台账记 partial）\n",
				rep.Imported, rep.Skipped)
			os.Exit(1)
		}
		status := "imported"
		if rep.AlreadyApplied {
			status = "already_applied"
		}
		out, _ := json.MarshalIndent(map[string]any{
			"status": status, "pack_id": rep.PackID, "kind": rep.Kind,
			"total": rep.Total, "imported": rep.Imported, "skipped": rep.Skipped,
		}, "", "  ")
		fmt.Println(string(out))
		if rep.AlreadyApplied {
			fmt.Println("该包此前已导入（台账 status=applied），本次未改动任何数据")
			return
		}
		if rep.Imported == 0 && rep.Skipped > 0 {
			fmt.Println("全部条目已在库（内容一致），本次无新增")
			return
		}
		fmt.Println("新导入条目 confirm_status=pending，请执行 openlaw-check 并转人工确认")
	case "archive":
		out := *outPath
		if out == "" {
			out = *packDir + ".zip"
		}
		if err := pack.Archive(*packDir, out); err != nil {
			fatal(err)
		}
		fmt.Println("更新包归档已生成:", out)
	case "index":
		if *indexDir == "" || *keyPath == "" {
			fatal("index 必须指定 -index 输出目录与 -key 私钥路径（参数为各包的 zip 归档）")
		}
		if *urlBase == "" {
			fatal("index 必须指定 -url-base 下载直链前缀（如 https://github.com/<owner>/<repo>/releases/download/<tag>）")
		}
		var metas []pack.CatalogPack
		for _, z := range fs.Args() {
			cp, err := pack.ReadPackMeta(z, strings.TrimSuffix(*urlBase, "/")+"/"+filepath.Base(z))
			if err != nil {
				fatal(err)
			}
			metas = append(metas, cp)
		}
		if err := pack.BuildCatalog(metas, *indexDir, *keyPath); err != nil {
			fatal(err)
		}
		fmt.Printf("更新包目录已生成并签名: %s（%d 个包）\n", *indexDir, len(metas))
	case "index-verify":
		if *indexDir == "" {
			fatal("index-verify 必须指定 -index 目录")
		}
		c, err := pack.ParseCatalog(*indexDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "更新包目录校验失败:", err)
			os.Exit(2)
		}
		fmt.Printf("更新包目录校验通过（签名有效、来源可信）: %d 个包\n", len(c.Packs))
		for _, p := range c.Packs {
			fmt.Printf("  %-32s %-5s %6d 条 %12d 字节\n", p.PackID, p.Kind, p.DocCount, p.Size)
		}
	default:
		usage()
	}
}

func openDB(dataDir string) *store.DB {
	if err := os.MkdirAll(filepath.Join(dataDir, "db"), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(filepath.Join(dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	return db
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: openlaw-pack keygen|build|verify|import|archive|index|index-verify [-pack <dir>] [-data <dir>] [-key <path>] [-allow-untrusted]")
	fmt.Fprintln(os.Stderr, "  archive: -pack <包目录> [-out <zip>]；index: -index <目录> -key <私钥> -url-base <直链前缀> <zip>…；index-verify: -index <目录>")
	os.Exit(1)
}

func fatal(msg any) {
	err, _ := msg.(error)
	if err != nil && errors.Is(err, pack.ErrBadChange) {
		msg = "changes.csv 非法: " + err.Error()
	}
	fmt.Fprintln(os.Stderr, "错误:", msg)
	os.Exit(1)
}
