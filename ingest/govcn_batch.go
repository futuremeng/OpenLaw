// govcn_batch.go：gov.cn 清单批量抓取导入的共享 runner（cmd/openlaw-govcn 与
// 桌面端采集面板同一代码路径——CLI 是薄壳，桌面端不是第二写路径）。
// 纪律与 CLI 完全一致：报告逐行落盘（中断不丢已完成记录）、badURLs 如实记
// skip_bad_url 转人工、全部入库 confirm_status=pending 人工闸口。
package ingest

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// GovcnBatchResult 批量 runner 汇总。
type GovcnBatchResult struct {
	ReportPath string
	Counts     map[string]int
	Total      int
}

// DefaultGovcnReportPath 生成默认报告路径：<dataDir>/incoming/<subdir>/report_<ts>.csv。
func DefaultGovcnReportPath(dataDir, subdir string) string {
	return filepath.Join(dataDir, "incoming", subdir, "report_"+time.Now().UTC().Format("20060102T150405")+".csv")
}

// RunGovcnBatch 按清单逐条抓取导入并写 CSV 报告。progress(i,total,it) 每条回调
// （可 nil）。ctx 取消时停止并在 Result 里保留已写入部分（如实交回，不假装完成）。
func RunGovcnBatch(ctx context.Context, g *Govcn, im *Importer, entries, badURLs []GovcnEntry, reportPath string, progress func(i, total int, it GovcnItem)) (*GovcnBatchResult, error) {
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(reportPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"url", "doc_id", "status", "articles", "reason"})

	res := &GovcnBatchResult{ReportPath: reportPath, Counts: map[string]int{}}
	// 清单中不符合本通道详情页形态的条目：如实记入报告 skip_bad_url 转人工，
	// 不静默丢弃、也不越主机策略抓取。
	for _, e := range badURLs {
		res.Counts["skip_bad_url"]++
		res.Total++
		if err := w.Write([]string{e.URL, "", "skip_bad_url", "", "不符合本通道详情页形态（主机锁 " + GovcnHost + "），转人工"}); err != nil {
			return res, err
		}
	}
	w.Flush()

	for i, e := range entries {
		if ctx.Err() != nil {
			w.Flush()
			res.Counts["cancelled"] = len(entries) - i
			return res, ctx.Err()
		}
		it := g.FetchAndImport(ctx, im, e)
		res.Counts[it.Status]++
		res.Total++
		if err := w.Write([]string{it.URL, it.DocID, it.Status, fmt.Sprint(it.Article), it.Reason}); err != nil {
			return res, err
		}
		w.Flush()
		if progress != nil {
			progress(i+1, len(entries), it)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return res, err
	}
	return res, nil
}
