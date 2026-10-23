// openlaw-pack：离线更新包工具（开发文档 6.4、16.2、FR-06）。
// build  : 数据目录 → pack 目录（manifest + Ed25519 签名 + changes.csv）
// verify : 校验包（签名/哈希/多余文件），失败退出码 2
// import : 校验通过后导入目标数据目录；失败保留旧数据
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/futuremeng/OpenLaw/pack"
	"github.com/futuremeng/OpenLaw/store"
)

func main() {
	fs := flag.NewFlagSet("openlaw-pack", flag.ExitOnError)
	var (
		dataDir = fs.String("data", "data", "数据目录（build 的源 / import 的目标）")
		packDir = fs.String("pack", "", "更新包目录（必填）")
		keyPath = fs.String("key", "", "签名私钥路径（build 必填；不进仓库不进数据目录）")
	)
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(1)
	}
	if *packDir == "" {
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
		if err := pack.Build(db, *dataDir, *packDir, *keyPath); err != nil {
			fatal(err)
		}
		fmt.Println("更新包已生成并签名:", *packDir)
	case "verify":
		if err := pack.Verify(*packDir); err != nil {
			fmt.Fprintln(os.Stderr, "更新包校验失败:", err)
			os.Exit(2)
		}
		fmt.Println("更新包校验通过（签名有效、哈希一致、无未收录文件）")
	case "import":
		db := openDB(*dataDir)
		defer db.Close()
		results, err := pack.Import(db, *dataDir, *packDir)
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
	fmt.Fprintln(os.Stderr, "用法: openlaw-pack build|verify|import -pack <dir> [-data <dir>] [-key <path>]")
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
