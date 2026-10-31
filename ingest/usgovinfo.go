package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// usgovinfo.go 美国官方通道骨架（docs/sources-us.md、docs/design-multi-jurisdiction.md §6）。
// 本阶段只做「批量清单拉取与留证」：govinfo 批量数据 sitemap 索引（其路径由
// govinfo robots.txt 的 Sitemap 行显式公布，属官方开放的自动化入口）。
// 纪律与 govcn/cellar 同源：robots 强制（同一引擎，fail-closed）、≥2s 礼貌限速、
// 原始响应落 incoming/ 留证、每次访问写 import_log、中断如实失败。
// 全文解析入库不在本通道：那是写路径高风险变更（USLM/USC 结构化、规章切分、
// 生效废止关系），按阶段另行实施并带真实链路测试。

// USGovinfoBulkSitemapBase govinfo 批量数据 sitemap 索引基址（2026-10-31 实网核实）。
const USGovinfoBulkSitemapBase = "https://www.govinfo.gov/sitemap/bulkdata"

// usMinIntervalFloor 美国通道礼貌限速下限（不可绕过，同 govcn ≥2s 纪律）。
const usMinIntervalFloor = 2 * time.Second

// usBulkMaxBytes 清单响应大小上限（sitemap 索引为小文件，从严）。
const usBulkMaxBytes = 8 << 20

// USBulkCollections govinfo 批量仓库收录的集合名（2026-10-31 页面实况，
// docs/sources-us.md §1；USCODE 无批量包，联邦法律走 STATUTE/Statute Compilations）。
var USBulkCollections = map[string]bool{
	"BILLSTATUS": true, "BILLSUM": true, "BILLS": true, "CBD": true,
	"CFR": true, "ECFR": true, "FR": true, "GOVMAN": true, "HMAN": true,
	"PAI": true, "PPP": true, "PLAW": true, "SCD": true, "STATUTE": true,
}

// USGovinfo govinfo 批量清单客户端（骨架）。
type USGovinfo struct {
	DataDir     string
	DB          *store.DB
	Policy      FetchPolicy
	MinInterval time.Duration // 请求间礼貌限速；低于 2s 一律按 2s
	SitemapBase string        // 空 = USGovinfoBulkSitemapBase（仅测试注入 httptest）
	robots      *RobotsCache
	lastFetch   time.Time
}

func (u *USGovinfo) base() string {
	if u.SitemapBase != "" {
		return strings.TrimRight(u.SitemapBase, "/")
	}
	return USGovinfoBulkSitemapBase
}

func (u *USGovinfo) interval() time.Duration {
	if u.MinInterval < usMinIntervalFloor {
		return usMinIntervalFloor
	}
	return u.MinInterval
}

// USSitemapIndex 一次批量清单拉取的结果（如实：条目数 + 留证路径）。
type USSitemapIndex struct {
	Collection string   `json:"collection"`
	URL        string   `json:"url"`
	Locs       []string `json:"locs"`
	RawPath    string   `json:"raw_path"` // incoming/ 下留证文件（相对数据目录）
	SHA256     string   `json:"sha256"`
	FetchedAt  string   `json:"fetched_at"`
}

// BulkSitemapURL 集合 → 批量清单地址；名单外集合直接拒绝（防拼错路径乱枪打鸟）。
func (u *USGovinfo) BulkSitemapURL(collection string) (string, error) {
	col := strings.ToUpper(strings.TrimSpace(collection))
	if !USBulkCollections[col] {
		names := make([]string, 0, len(USBulkCollections))
		for k := range USBulkCollections {
			names = append(names, k)
		}
		sort.Strings(names)
		return "", fmt.Errorf("未知集合 %q——govinfo 批量仓库收录集合见 docs/sources-us.md（%s）",
			collection, strings.Join(names, "/"))
	}
	return fmt.Sprintf("%s/%s/sitemapindex.xml", u.base(), col), nil
}

var locPattern = regexp.MustCompile(`<loc>\s*([^<\s]+)\s*</loc>`)

// FetchSitemapIndex 拉取一个集合的批量清单：robots 前置（fail-closed）→ 限速 →
// GET（UA 如实标识 + 大小上限）→ 原始留证 incoming/us_<COL>_sitemapindex.xml →
// 解析 <loc> 清单 → import_log 留痕。任何一步失败都如实报错并留 failed 记录，
// 绝不产出「看起来成功」的空清单。
func (u *USGovinfo) FetchSitemapIndex(ctx context.Context, collection string) (*USSitemapIndex, error) {
	endpoint, err := u.BulkSitemapURL(collection)
	if err != nil {
		return nil, err
	}
	pol := u.Policy
	if pol.Timeout == 0 {
		pol.Timeout = 30 * time.Second
	}
	if u.robots == nil {
		u.robots = NewRobotsCache(pol)
	}
	// 礼貌限速：两次请求之间不小于 MinInterval（context 取消贯穿等待点）。
	if d := u.interval() - time.Since(u.lastFetch); d > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(d):
		}
	}
	u.lastFetch = time.Now()
	col := strings.ToUpper(strings.TrimSpace(collection))
	if ok, why := u.robots.Allowed(ctx, endpoint); !ok {
		u.logFetch(endpoint, "failed", "robots 策略拒绝: "+why, 0, "", 0)
		return nil, fmt.Errorf("robots 策略拒绝: %s", why)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if pol.UserAgent == "" {
		pol.UserAgent = defaultUserAgent()
	}
	req.Header.Set("User-Agent", pol.UserAgent)
	client := &http.Client{Timeout: pol.Timeout, Transport: transportOf(pol)}
	resp, err := client.Do(req)
	if err != nil {
		u.logFetch(endpoint, "failed", fmt.Sprintf("请求失败: %v", err), 0, "", 0)
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, usBulkMaxBytes+1))
	if err != nil {
		u.logFetch(endpoint, "failed", fmt.Sprintf("读取失败: %v", err), resp.StatusCode, "", 0)
		return nil, fmt.Errorf("读取失败: %w", err)
	}
	if int64(len(body)) > usBulkMaxBytes {
		u.logFetch(endpoint, "failed", "响应超过大小上限", resp.StatusCode, "", len(body))
		return nil, fmt.Errorf("清单响应超过 %d 字节上限", usBulkMaxBytes)
	}
	if resp.StatusCode != http.StatusOK {
		u.logFetch(endpoint, "failed", fmt.Sprintf("HTTP %d", resp.StatusCode), resp.StatusCode, "", len(body))
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	sum := sha256.Sum256(body)
	rawName := "us_" + col + "_sitemapindex.xml"
	if err := os.MkdirAll(filepath.Join(u.DataDir, "incoming"), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(u.DataDir, "incoming", rawName), body, 0o644); err != nil {
		u.logFetch(endpoint, "failed", "留证写入失败: "+err.Error(), resp.StatusCode, hex.EncodeToString(sum[:]), len(body))
		return nil, fmt.Errorf("留证写入失败: %w", err)
	}
	locs := []string{}
	for _, m := range locPattern.FindAllStringSubmatch(string(body), -1) {
		locs = append(locs, m[1])
	}
	if len(locs) == 0 {
		// 200 但一条清单都没有：要么格式变了要么拿错文件——如实失败，不留假成功。
		u.logFetch(endpoint, "failed", "清单为空（格式可能已变更）", resp.StatusCode, hex.EncodeToString(sum[:]), len(body))
		return nil, fmt.Errorf("清单为空（govinfo 批量清单格式可能已变更，见 docs/sources-us.md）")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	u.logFetch(endpoint, "success", fmt.Sprintf("清单 %d 项", len(locs)), resp.StatusCode, hex.EncodeToString(sum[:]), len(body))
	return &USSitemapIndex{Collection: col, URL: endpoint, Locs: locs,
		RawPath: filepath.Join("incoming", rawName), SHA256: hex.EncodeToString(sum[:]), FetchedAt: now}, nil
}

// logFetch 每次访问进 import_log（与 cellar 同一留痕口径：来源、状态、哈希、结果）。
func (u *USGovinfo) logFetch(endpoint, result, reason string, status int, sha string, size int) {
	if u.DB == nil {
		return
	}
	_, _ = u.DB.Exec(`INSERT INTO import_log(doc_id, source_url, file_path, file_size, sha256,
		http_status, content_type, result, reason, created_at)
		VALUES(NULL,?,NULL,?,?,?,'application/xml',?,?,?)`,
		endpoint, size, nullable(sha), status, result, "[us-govinfo] "+reason,
		time.Now().UTC().Format(time.RFC3339))
}
