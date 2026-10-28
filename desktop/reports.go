// reports.go 导入/采集报告分诊（总览面板数据源）：读 data/incoming/ 下既有
// 报告文件（与 CLI 产物同格式，桌面端只读不发明新格式）——
//   - report_<ts>.csv（openlaw-govcn 三通道：url,doc_id,status,articles,reason）
//   - batch_report.csv（openlaw-batch：doc_id,law_id,name,date,status,articles,detail）
//   - govlist_report_*.json（openlaw-govlist：GovListReport）
//
// 失败原因直方图 = 终端 awk 分诊套路的 Go 化；缺列/坏行如实标注不静默。
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FailCount 失败原因聚合一行。
type FailCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// ReportSummary 单个报告文件的分诊汇总。
type ReportSummary struct {
	Path        string      `json:"path"`
	Source      string      `json:"source"` // govcn / govgz / govlocal / batch / govlist
	Kind        string      `json:"kind"`   // csv / json
	ModifiedAt  string      `json:"modified_at"`
	Total       int         `json:"total"`
	OK          int         `json:"ok"`
	Failed      int         `json:"failed"`
	Skipped     int         `json:"skipped"`
	Other       int         `json:"other"`
	TopFailures []FailCount `json:"top_failures,omitempty"`
	Pages       int         `json:"pages,omitempty"`      // govlist：浏览页数
	StoppedAt   string      `json:"stopped_at,omitempty"` // govlist：done/max_pages/empty_page/end_of_list
	Note        string      `json:"note,omitempty"`       // malformed 等如实标注
}

// normalizeFailReason 归一失败原因做直方图：截断到首个冒号/括号前（同终端分诊
// 套路），空原因归入「（无原因）」。
func normalizeFailReason(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{"：", "(", "（"} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "（无原因）"
	}
	return s
}

// classifyStatus 状态归类：ok* 成功；failed 失败；skip* 跳过；其余 Other。
func classifyStatus(s string) string {
	switch {
	case strings.HasPrefix(s, "ok"):
		return "ok"
	case s == "failed":
		return "failed"
	case strings.HasPrefix(s, "skip"):
		return "skipped"
	default:
		return "other"
	}
}

// parseCSVReport 解析一份 CSV 报告。列名定位（不假设列序）；缺 status 列 →
// malformed 标注。
func parseCSVReport(path, source string) ReportSummary {
	rs := ReportSummary{Path: path, Source: source, Kind: "csv"}
	f, err := os.Open(path)
	if err != nil {
		rs.Note = "读取失败: " + err.Error()
		return rs
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		rs.Note = "解析失败: " + err.Error()
		return rs
	}
	if len(recs) < 2 {
		rs.Note = "报告无数据行"
		return rs
	}
	hdr := map[string]int{}
	for i, c := range recs[0] {
		hdr[strings.TrimSpace(strings.ToLower(c))] = i
	}
	iStatus, okStatus := hdr["status"]
	if !okStatus {
		rs.Note = "缺 status 列（格式不符，未分诊）"
		return rs
	}
	iReason, okReason := hdr["reason"]
	if !okReason {
		iReason, okReason = hdr["detail"]
	}
	fails := map[string]int{}
	for _, rec := range recs[1:] {
		if iStatus >= len(rec) {
			rs.Other++
			rs.Total++
			continue
		}
		rs.Total++
		switch classifyStatus(strings.TrimSpace(rec[iStatus])) {
		case "ok":
			rs.OK++
		case "failed":
			rs.Failed++
			if okReason && iReason < len(rec) {
				fails[normalizeFailReason(rec[iReason])]++
			} else {
				fails["（无原因）"]++
			}
		case "skipped":
			rs.Skipped++
		default:
			rs.Other++
		}
	}
	rs.TopFailures = topN(fails, 5)
	return rs
}

// parseGovListReport 解析 govlist JSON 报告（GovListReport 的最小读取面）。
func parseGovListReport(path, source string) ReportSummary {
	rs := ReportSummary{Path: path, Source: source, Kind: "json"}
	b, err := os.ReadFile(path)
	if err != nil {
		rs.Note = "读取失败: " + err.Error()
		return rs
	}
	var g struct {
		Pages     int    `json:"pages"`
		Entries   []any  `json:"entries"`
		StoppedAt string `json:"stopped_at"`
	}
	if err := json.Unmarshal(b, &g); err != nil {
		rs.Note = "解析失败: " + err.Error()
		return rs
	}
	rs.Pages = g.Pages
	rs.Total = len(g.Entries)
	rs.OK = len(g.Entries)
	rs.StoppedAt = g.StoppedAt
	rs.Note = fmt.Sprintf("浏览 %d 页 · 清单 %d 条 · 停止于 %s", g.Pages, len(g.Entries), g.StoppedAt)
	return rs
}

func topN(m map[string]int, n int) []FailCount {
	out := make([]FailCount, 0, len(m))
	for r, c := range m {
		out = append(out, FailCount{Reason: r, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Reason < out[j].Reason
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// ScanReports 扫描 data/incoming/<source>/ 下报告文件，每个来源按修改时间取
// 最近 perSource 份（govcn 三通道 + batch + govlist JSON）。
func ScanReports(dataDir string, perSource int) []ReportSummary {
	if perSource <= 0 {
		perSource = 2
	}
	root := filepath.Join(dataDir, "incoming")
	subs, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []ReportSummary
	for _, sub := range subs {
		if !sub.IsDir() {
			continue
		}
		source := sub.Name()
		dir := filepath.Join(root, source)
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var reps []ReportSummary
		for _, fe := range files {
			if fe.IsDir() {
				continue
			}
			name := fe.Name()
			p := filepath.Join(dir, name)
			var rep ReportSummary
			switch {
			case strings.HasPrefix(name, "report_") && strings.HasSuffix(name, ".csv"):
				rep = parseCSVReport(p, source)
			case name == "batch_report.csv":
				rep = parseCSVReport(p, "batch")
			case strings.HasPrefix(name, "govlist_report_") && strings.HasSuffix(name, ".json"):
				rep = parseGovListReport(p, "govlist")
			default:
				continue
			}
			if fi, err := fe.Info(); err == nil {
				rep.ModifiedAt = fi.ModTime().UTC().Format(time.RFC3339)
			}
			reps = append(reps, rep)
		}
		sort.Slice(reps, func(i, j int) bool { return reps[i].ModifiedAt > reps[j].ModifiedAt })
		if len(reps) > perSource {
			reps = reps[:perSource]
		}
		out = append(out, reps...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModifiedAt > out[j].ModifiedAt })
	return out
}
