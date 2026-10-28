// govlist_agent.go：清单浏览的「代理执行层」——在 RunGovListBrowse（浏览护栏）
// 之上补齐产物落盘（list.json + govlist_report_*.json，原子写），使
// cmd/openlaw-govlist 与桌面端采集面板共用同一执行路径。
// b 由调用方构造（CLI/桌面端注入 BrowserOS；测试注入 fake），本层不碰端点配置。
package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// DefaultGovListOutPath 默认清单输出：<dataDir>/incoming/govgz/list_agent_<ts>.json。
func DefaultGovListOutPath(dataDir string) string {
	return filepath.Join(dataDir, "incoming", "govgz", "list_agent_"+time.Now().UTC().Format("20060102T150405")+".json")
}

// RunGovListAgent 浏览 + 落盘。即使浏览中途出错，已采条目也如实写入
// （partial 报告，绝不假装没发生或静默丢弃）。返回带 OutPath 的报告。
func RunGovListAgent(ctx context.Context, b PageBrowser, opts GovListOptions, outPath string, progress func(pages, entries int)) (GovListReport, error) {
	rep, err := RunGovListBrowse(ctx, b, opts, progress)
	out := GovListReport{StartURL: rep.StartURL, Cat: rep.Cat, StartPage: rep.StartPage,
		Pages: rep.Pages, Entries: rep.Entries, StoppedAt: rep.StoppedAt}
	if out.Entries == nil {
		out.Entries = []GovListEntry{}
	}
	body, merr := json.MarshalIndent(out.Entries, "", "  ")
	if merr != nil {
		return out, merr
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return out, err
	}
	tmp := outPath + ".part"
	if werr := os.WriteFile(tmp, body, 0o644); werr != nil {
		return out, werr
	}
	if rerr := os.Rename(tmp, outPath); rerr != nil {
		os.Remove(tmp)
		return out, rerr
	}
	out.OutPath = outPath
	if err != nil {
		// 中断也出报告：pages/entries 如实，stopped_at 标注 cancelled/error
		out.StoppedAt = "error: " + err.Error()
	}
	rb, merr := json.MarshalIndent(out, "", "  ")
	if merr != nil {
		return out, merr
	}
	repPath := filepath.Join(filepath.Dir(outPath), "govlist_report_"+filepath.Base(outPath))
	if werr := os.WriteFile(repPath, rb, 0o644); werr != nil {
		return out, werr
	}
	return out, err
}
