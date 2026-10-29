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

	"github.com/futuremeng/OpenLaw/ingest"
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
	)
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(1)
	}
	if cmd != "keygen" && *packDir == "" {
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
		if err := pack.Build(db, *dataDir, *packDir, *keyPath, *license, *attrib); err != nil {
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
		var results []ingest.Result
		var err error
		if *allowUntrusted {
			fmt.Fprintln(os.Stderr, "注意：-allow-untrusted 跳过来源信任检查，仅适用于本地自验。")
			results, err = pack.ImportAllowUntrusted(db, *dataDir, *packDir)
		} else {
			results, err = pack.Import(db, *dataDir, *packDir)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "导入中止（旧数据未受影响）: %v\n", err)
			fmt.Fprintf(os.Stderr, "已成功导入 %d 条（均为新增，可经 import_log 追溯）\n", len(results))
			os.Exit(1)
		}
		out, _ := json.MarshalIndent(map[string]any{
			"status": "imported", "count": len(results),
		}, "", "  ")
		fmt.Println(string(out))
		fmt.Println("新导入条目 confirm_status=pending，请执行 openlaw-check 并转人工确认")
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
	fmt.Fprintln(os.Stderr, "用法: openlaw-pack keygen|build|verify|import -pack <dir> [-data <dir>] [-key <path>] [-allow-untrusted]")
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
