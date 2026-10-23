package ingest

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

// 批量抓取（19 章）：两阶段、仅对 robots 允许的源生效。
//   1. ListCandidates：抓目录页（HTML），提取 PDF 直链 → 候选清单（可人工编辑）；
//   2. BatchFetch：逐条下载进 data/incoming/，限速（默认 2s/条），每条写 import_log，
//      失败记录跳过不静默；解析入库仍走 openlaw-ingest 闭环 + 人工确认。
// 国务院公报 www.gov.cn/gongbao/ 为当前确认的可批量源（robots 无 Disallow，2026-10 复核）。

// Candidate 一个待下载候选（candidates.csv 行）。
type Candidate struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

var hrefPDFRe = regexp.MustCompile(`(?i)href="([^"]+\.pdf)"`)

// ListCandidates 从目录页提取 PDF 直链候选（先过 robots/白名单/禁止名单）。
func (f *Fetcher) ListCandidates(ctx context.Context, indexURL string) ([]Candidate, error) {
	u, err := url.Parse(indexURL)
	if err != nil {
		return nil, fmt.Errorf("URL 非法: %w", err)
	}
	if err := f.checkURL(u); err != nil {
		return nil, err
	}
	if ok, why := f.Robots().Allowed(ctx, indexURL); !ok {
		return nil, fmt.Errorf("robots 策略拒绝: %s", why)
	}

	pol := f.policy()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", pol.UserAgent)
	resp, err := (&http.Client{Timeout: pol.Timeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("目录页请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("目录页 HTTP %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(strings.ToLower(ct), "text/html") {
		return nil, fmt.Errorf("目录页 Content-Type 非 HTML（%q）", ct)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var out []Candidate
	for _, m := range hrefPDFRe.FindAllStringSubmatch(string(body), -1) {
		href := m[1]
		if strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "#") {
			continue
		}
		abs, err := u.Parse(href)
		if err != nil {
			continue
		}
		if err := f.checkURL(abs); err != nil {
			continue // 白名单/协议/禁止名单不过的候选直接过滤
		}
		key := abs.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Candidate{URL: key, Name: DocIDFromURL(key)})
	}
	return out, nil
}

// BatchItem 批量下载的一条结果。
type BatchItem struct {
	URL    string `json:"url"`
	DocID  string `json:"doc_id"`
	Status string `json:"status"` // success / failed
	Reason string `json:"reason,omitempty"`
	Path   string `json:"path,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

// BatchFetch 逐条下载候选（delay 限速，默认建议 ≥2s），每条独立走 Fetch 完整策略与 import_log。
// 单条失败不中断整批（失败已写日志，不得静默），汇总返回。
func (f *Fetcher) BatchFetch(ctx context.Context, candidates []Candidate, delay time.Duration) []BatchItem {
	items := make([]BatchItem, 0, len(candidates))
	for i, c := range candidates {
		if err := ctx.Err(); err != nil {
			items = append(items, BatchItem{URL: c.URL, Status: "aborted", Reason: "上下文取消"})
			break
		}
		if i > 0 && delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
			}
		}
		docID := c.Name
		if docID == "" {
			docID = DocIDFromURL(c.URL)
		}
		item := BatchItem{URL: c.URL, DocID: docID}
		res, err := f.Fetch(ctx, c.URL, docID)
		if err != nil {
			item.Status = "failed"
			item.Reason = err.Error()
		} else {
			item.Status = "success"
			item.Path = res.Path
			item.SHA256 = res.SHA256
		}
		items = append(items, item)
	}
	return items
}

// DocIDFromURL 由 URL 文件名派生 doc_id（小写，非字母数字转下划线）。
func DocIDFromURL(raw string) string {
	u, err := url.Parse(raw)
	base := path.Base(u.Path)
	if err != nil || base == "." || base == "/" {
		return "doc"
	}
	base = strings.TrimSuffix(strings.ToLower(base), ".pdf")
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "doc"
	}
	return b.String()
}

// WriteCandidatesCSV 输出候选清单（url,name），供人工审阅编辑后再批量下载。
func WriteCandidatesCSV(p string, cs []Candidate) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"url", "name"}); err != nil {
		return err
	}
	for _, c := range cs {
		if err := w.Write([]string{c.URL, c.Name}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// ReadCandidatesCSV 读取候选清单；首行表头可省，'#' 开头行为注释。
func ReadCandidatesCSV(p string) ([]Candidate, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	var out []Candidate
	for _, r := range records {
		if len(r) == 0 || strings.HasPrefix(r[0], "#") {
			continue
		}
		if r[0] == "url" {
			continue
		}
		c := Candidate{URL: strings.TrimSpace(r[0])}
		if len(r) > 1 {
			c.Name = strings.TrimSpace(r[1])
		}
		if c.URL != "" {
			out = append(out, c)
		}
	}
	return out, nil
}
