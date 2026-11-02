// eubatch.go 欧盟全文批量抓取（docs/tasks-eu-rate-limit.md RL-4，M2 主体前置）。
// 纪律：
//   - 队列文件驱动（JSON 数组 [{"celex":"32016R0679","lang":"eng"}]），断点续传：
//     每处理一部立即原子回写队列（done/failed/pending），重跑只处理 pending——
//     doc_id 幂等由 importer 兜底，中断-恢复不重复下载不重复入库；
//   - 熔断：连续 ≥CircuitBreak 部（默认 5）退避后仍 429 → 整批中止，剩余如实留
//     pending，次日再排（429 后拉长间隔而非缩短——共同纪律）；
//   - 语言纪律：默认 eng 单语（多语言=多 document，配额成倍，另行排期）；
//   - 每部产出批次报告 CSV（celex/lang/状态/条数/sha256/错误）。
package ingest

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// EUQueueItem 队列条目。Status 空按 pending 处理。
type EUQueueItem struct {
	Celex  string `json:"celex"`
	Lang   string `json:"lang,omitempty"` // 空 = eng
	Status string `json:"status,omitempty"`
	DocID  string `json:"doc_id,omitempty"`
	Error  string `json:"error,omitempty"`
}

// LoadEUQueue 读队列文件并校验（CELEX 必填；done 之外的状态一律按 pending 处理——
// 失败与未知状态都要重跑，不得静默跳过）。
func LoadEUQueue(path string) ([]EUQueueItem, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []EUQueueItem
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, fmt.Errorf("队列文件必须是 JSON 数组（[{\"celex\":…}]）: %w", err)
	}
	if len(items) == 0 {
		return nil, errors.New("队列为空——不得产出「看起来成功」的空批次")
	}
	for i := range items {
		items[i].Celex = strings.ToUpper(strings.TrimSpace(items[i].Celex))
		if items[i].Celex == "" {
			return nil, fmt.Errorf("队列第 %d 项缺 CELEX 编号", i+1)
		}
		if items[i].Lang == "" {
			items[i].Lang = "eng"
		}
		// done/failed 保留（failed 须人工处置，不得静默重跑）；其余一律 pending。
		if items[i].Status != "done" && items[i].Status != "failed" {
			items[i].Status = "pending"
		}
	}
	return items, nil
}

// saveEUQueue 原子回写（tmp+rename）——中断不留半个队列。
func saveEUQueue(path string, items []EUQueueItem) error {
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EUBatchOptions 批量参数。CircuitBreak ≤0 用默认 5。
type EUBatchOptions struct {
	QueuePath    string
	ReportPath   string // 空 = 队列同目录 eu_batch_report_<日期>.csv
	CircuitBreak int
	DryRun       bool // 试采：不标 done、不回写状态（429 熔断照常）
}

// EUBatchResult 批次结果（如实：熔断时 pending 是剩余量，不是失败量）。
type EUBatchResult struct {
	Total          int    `json:"total"`
	Done           int    `json:"done"`
	Failed         int    `json:"failed"`
	Pending        int    `json:"pending"`
	Aborted        bool   `json:"aborted"` // 熔断中止
	Cancelled      bool   `json:"cancelled"`
	Consecutive429 int    `json:"consecutive_429"`
	ReportPath     string `json:"report_path"`
}

type euBatchRow struct {
	item   EUQueueItem
	status string // done | failed | pending | dryrun | skipped(done)
	arts   int
	sha    string
	err    string
}

// RunEUBatch 批量执行队列。与单文档共用 RunEUText（含 RL-3 退避）——同一代码路径。
func RunEUBatch(ctx context.Context, dataDir string, db *store.DB, c *Cellar, o EUBatchOptions,
	progress func(done, total int, celex, status string)) (*EUBatchResult, error) {
	items, err := LoadEUQueue(o.QueuePath)
	if err != nil {
		return nil, err
	}
	breakN := o.CircuitBreak
	if breakN <= 0 {
		breakN = 5
	}
	report := o.ReportPath
	if report == "" {
		report = filepath.Join(filepath.Dir(o.QueuePath),
			"eu_batch_report_"+time.Now().UTC().Format("20060102")+".csv")
	}
	res := &EUBatchResult{Total: len(items), ReportPath: report}
	rows := []euBatchRow{}
	consecutive := 0
	for i := range items {
		it := &items[i]
		if it.Status == "done" {
			rows = append(rows, euBatchRow{item: *it, status: "skipped(done)"})
			res.Done++
			continue
		}
		if it.Status == "failed" {
			rows = append(rows, euBatchRow{item: *it, status: "skipped(failed)", err: it.Error})
			res.Failed++
			continue
		}
		if err := ctx.Err(); err != nil {
			res.Cancelled = true
			break
		}
		r, err := RunEUText(ctx, dataDir, db, c, EUTextOptions{Celex: it.Celex, Lang: it.Lang, DryRun: o.DryRun})
		row := euBatchRow{item: *it}
		switch {
		case err == nil:
			consecutive = 0
			row.sha, row.arts = r.SHA256, r.Articles
			if o.DryRun {
				row.status = "dryrun"
			} else {
				row.status = "done"
				it.Status, it.DocID, it.Error = "done", r.DocID, ""
				res.Done++
			}
		case errors.Is(err, context.Canceled):
			row.status, row.err = "pending", "已取消（已获取部分如实保留）"
			res.Cancelled = true
		default:
			var he *HTTPStatusErr
			if errors.As(err, &he) && he.Status == http.StatusTooManyRequests {
				consecutive++
				row.status = "pending"
				it.Error = fmt.Sprintf("限流 429（退避后仍失败，连续第 %d 部）", consecutive)
			} else {
				consecutive = 0
				row.status = "failed"
				it.Status, it.Error = "failed", err.Error()
				res.Failed++
			}
		}
		rows = append(rows, row)
		if saveErr := saveEUQueue(o.QueuePath, items); saveErr != nil {
			return res, fmt.Errorf("队列回写失败（断点续传依赖它，中止批次）: %w", saveErr)
		}
		if progress != nil {
			progress(res.Done+res.Failed, res.Total, it.Celex, row.status)
		}
		if consecutive >= breakN {
			res.Aborted = true
			res.Consecutive429 = consecutive
			break
		}
		if res.Cancelled {
			break
		}
	}
	for _, it := range items {
		if it.Status == "pending" {
			res.Pending++
		}
	}
	if err := writeEUBatchReport(report, rows); err != nil {
		return res, fmt.Errorf("批次报告写入失败: %w", err)
	}
	return res, nil
}

func writeEUBatchReport(path string, rows []euBatchRow) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"celex", "lang", "status", "articles", "sha256", "error"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{r.item.Celex, r.item.Lang, r.status,
			fmt.Sprint(r.arts), r.sha, r.err}); err != nil {
			return err
		}
	}
	return w.Error()
}
