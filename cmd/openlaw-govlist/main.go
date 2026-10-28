// openlaw-govlist 国家规章库清单浏览采集（人在环代理浏览；合规画像与护栏见
// browser/govlist.go）。驱动用户自装 BrowserOS neo（本机回环 MCP）在官方列表页
// 人工式跳页浏览，产出 openlaw-govcn -source gzk 兼容的 list.json。
// 默认离线：端点未配置直接报错并给指引，绝不降级到其他抓取方式。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/futuremeng/OpenLaw/browser"
)

func main() {
	var (
		dataDir   = flag.String("data", "data", "数据目录（清单与报告落盘 incoming/govgz/）")
		endpoint  = flag.String("endpoint", os.Getenv("OPENLAW_BROWSEROS_ENDPOINT"), "BrowserOS neo MCP 端点（本机回环；默认读环境变量 OPENLAW_BROWSEROS_ENDPOINT）")
		cat       = flag.String("cat", "", "分类过滤：部门规章 / 地方政府规章（空 = 混合清单全收）")
		maxPages  = flag.Int("max-pages", 50, "单会话翻页上限（硬上限 200）")
		startPage = flag.Int("start-page", 1, "起始页（跨会话续接：上一会话 stop 前最后成功页 +1）")
		delay     = flag.Duration("delay", 2500*time.Millisecond, "跳页间隔（礼貌限速，下限 2s）")
		emptyStop = flag.Int("empty-stop", 10, "连续真空页停止阈值（补采稀疏段可调高）")
		out       = flag.String("out", "", "list.json 输出路径（默认 data/incoming/govgz/list_agent_<ts>.json）")
	)
	flag.Parse()

	b := browser.NewBrowserOS(*endpoint)
	if !b.Enabled() {
		fatal(fmt.Errorf("%s", browser.Guidance))
	}

	outPath := *out
	if outPath == "" {
		outPath = browser.DefaultGovListOutPath(*dataDir)
	}
	rep, err := browser.RunGovListAgent(context.Background(), b, browser.GovListOptions{
		Cat: *cat, MaxPages: *maxPages, Delay: *delay, StartPage: *startPage, EmptyStop: *emptyStop,
	}, outPath, nil)
	if err != nil {
		fatal(fmt.Errorf("清单浏览失败（已采 %d 页 %d 条如实报告，清单已落盘 %s）: %w", rep.Pages, len(rep.Entries), rep.OutPath, err))
	}

	body, err := os.ReadFile(rep.OutPath)
	if err != nil {
		fatal(err)
	}
	sum := sha256.Sum256(body)
	fmt.Printf("规章库清单浏览完成: 页 %d（起始 %d）条 %d 停止原因 %s\n清单: %s (sha256 %s)\n下一步: go run ./cmd/openlaw-govcn -source gzk -local -list %s\n",
		rep.Pages, rep.StartPage, len(rep.Entries), rep.StoppedAt, rep.OutPath, hex.EncodeToString(sum[:16]), rep.OutPath)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
