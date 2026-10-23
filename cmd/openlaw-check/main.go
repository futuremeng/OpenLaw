// openlaw-check：每日运维校验（开发文档 9.2）。
// 哈希重算 + manifest 签名校验 + 断链清单。发现文件被替换立即隔离并告警（由上层处置）。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

func main() {
	var (
		dataDir       = flag.String("data", "data", "数据目录")
		keyPath       = flag.String("key", "", "manifest 签名私钥路径（-build-manifest 时必填）")
		buildManifest = flag.Bool("build-manifest", false, "重算哈希并生成/更新 manifest 与签名")
		genKey        = flag.Bool("gen-key", false, "生成新的 Ed25519 签名私钥（配合 -key）")
	)
	flag.Parse()

	if *genKey {
		if *keyPath == "" {
			fatal("生成私钥必须指定 -key 路径；私钥不得进入仓库或数据目录（16.2）")
		}
		if err := validate.GenerateKey(*keyPath); err != nil {
			fatal(err)
		}
		fmt.Println("已生成签名私钥:", *keyPath)
		return
	}

	db, err := store.Open(filepath.Join(*dataDir, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	if *buildManifest {
		if *keyPath == "" {
			fatal("-build-manifest 必须指定 -key 私钥路径")
		}
		if err := validate.BuildManifest(*dataDir, *keyPath); err != nil {
			fatal(err)
		}
		fmt.Println("manifest 已生成并完成签名:", filepath.Join(*dataDir, "manifest", "manifest.json"))
	}

	// 每日：manifest 签名校验（OL-1008 失败则停止使用数据目录）
	if err := validate.VerifyManifest(*dataDir); err != nil {
		fmt.Fprintln(os.Stderr, "签名校验失败:", err)
		os.Exit(2)
	}
	fmt.Println("manifest 签名校验通过")

	// 断链清单（9.2 每周输出，此处每次运行都输出）
	broken, err := validate.CheckAllArticles(db, *dataDir)
	if err != nil {
		fatal(err)
	}
	if len(broken) == 0 {
		fmt.Println("断链检测：全部条文溯源链完好")
		return
	}
	fmt.Printf("断链检测：%d 条条文存在断点\n", len(broken))
	for id, issues := range broken {
		for _, is := range issues {
			fmt.Printf("  %s  [%s] %s (%s)\n", id, is.ErrorCode, is.Step, is.Reason)
		}
	}
	os.Exit(3)
}

func fatal(msg any) {
	fmt.Fprintln(os.Stderr, "错误:", msg)
	os.Exit(1)
}
