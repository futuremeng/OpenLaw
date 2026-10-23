package ingest

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/hashutil"
	"github.com/futuremeng/OpenLaw/store"
)

// 抓取（下载）实现（开发文档 6.1 第一步、4.2 import_log、18 章合规）。
// 硬性约束：
//   - 默认离线：抓取只在用户显式执行 openlaw-fetch 时发生，程序绝不自动联网（6.4）；
//   - 只进暂存区 data/incoming/，不直接写原始层；解析入库仍走 openlaw-ingest 闭环，
//     confirm_status=pending 转人工确认；
//   - robots.txt 强制执行（见 robots.go）：站点禁止即拒绝，flk.npc.gov.cn 明确禁止
//     任何自动化工具采集，已列入禁止名单，不得对其抓取（18 章）；
//   - source_url、http_status、content_type、抓取时间必须记入 import_log。

// FetchPolicy 下载策略。零值字段使用安全默认值。
type FetchPolicy struct {
	AllowHosts []string      // 来源域名白名单（如 www.gov.cn）；空 = 允许全部（不推荐）
	MaxSize    int64         // 响应体大小上限，默认 64 MiB
	Timeout    time.Duration // 单次请求超时，默认 30s
	UserAgent  string        // 自定义 UA，默认 openlaw/<versionTag> (local legal research; noindex)
	AllowHTTP  bool          // 允许 http（仅建议测试用途；回环地址默认放行）
}

// blockedHosts 硬禁止名单：明确禁止自动化采集的官方源（robots.txt 声明，2026-10 复核）。
// flk.npc.gov.cn（国家法律法规数据库）robots.txt 全文 Disallow 并注明禁止任何爬虫。
var blockedHosts = []string{"flk.npc.gov.cn"}

// FetchResult 一次抓取的产出（全部字段写入 import_log）。
type FetchResult struct {
	Path        string `json:"path"` // 暂存路径（相对数据目录）
	FinalURL    string `json:"final_url"`
	HTTPStatus  int    `json:"http_status"`
	ContentType string `json:"content_type"`
	SHA256      string `json:"sha256"`
	MD5         string `json:"md5"` // 仅判下载损坏
	Size        int64  `json:"size"`
	FetchedAt   string `json:"fetched_at"`
}

// Fetcher 显式触发的 PDF 抓取器。
type Fetcher struct {
	DataDir string
	DB      *store.DB
	Policy  FetchPolicy
	robots  *RobotsCache
}

// Robots 返回（惰性初始化的）robots 策略缓存。
func (f *Fetcher) Robots() *RobotsCache {
	if f.robots == nil {
		f.robots = NewRobotsCache(f.policy())
	}
	return f.robots
}

// Fetch 下载 url 指向的 PDF 到 data/incoming/<docID>.pdf。
// 失败同样写 import_log（result=failed），满足「不得静默」要求。
func (f *Fetcher) Fetch(ctx context.Context, rawURL, docID string) (*FetchResult, error) {
	if docID == "" {
		return nil, fmt.Errorf("缺少 doc_id")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("URL 非法: %w", err)
	}
	if err := f.checkURL(u); err != nil {
		return nil, err
	}
	if ok, why := f.Robots().Allowed(ctx, rawURL); !ok {
		err := fmt.Errorf("robots 策略拒绝抓取: %s", why)
		f.logFetch(docID, rawURL, &FetchResult{FetchedAt: time.Now().UTC().Format(time.RFC3339)}, "failed", err.Error())
		return nil, err
	}

	pol := f.policy()
	client := &http.Client{
		Timeout: pol.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("重定向次数过多")
			}
			if err := f.checkURL(req.URL); err != nil { // 每一跳都重新过策略
				return err
			}
			if ok, why := f.Robots().Allowed(req.Context(), req.URL.String()); !ok {
				return fmt.Errorf("重定向目标被 robots 拒绝: %s", why)
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", pol.UserAgent)

	res := &FetchResult{FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	resp, err := client.Do(req)
	if err != nil {
		f.logFetch(docID, rawURL, res, "failed", fmt.Sprintf("请求失败: %v", err))
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	res.FinalURL = resp.Request.URL.String()
	res.HTTPStatus = resp.StatusCode
	res.ContentType = resp.Header.Get("Content-Type")

	if resp.StatusCode != http.StatusOK {
		reason := fmt.Sprintf("HTTP %d", resp.StatusCode)
		f.logFetch(docID, rawURL, res, "failed", reason)
		return nil, fmt.Errorf("抓取失败: %s", reason)
	}
	if !strings.HasPrefix(strings.ToLower(res.ContentType), "application/pdf") {
		reason := fmt.Sprintf("Content-Type 不是 PDF（%q），URL 可能是门户页而非文件直链", res.ContentType)
		f.logFetch(docID, rawURL, res, "failed", reason)
		return nil, fmt.Errorf("%s", reason)
	}

	// 落盘到暂存区，带大小上限（多读 1 字节判断越限）
	incoming := filepath.Join(f.DataDir, "incoming")
	if err := os.MkdirAll(incoming, 0o755); err != nil {
		return nil, err
	}
	dst := filepath.Join(incoming, docID+".pdf")
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, pol.MaxSize+1))
	closeErr := out.Close()
	if err == nil && n > pol.MaxSize {
		err = fmt.Errorf("响应体超过大小上限 %d 字节", pol.MaxSize)
	}
	if err != nil {
		os.Remove(tmp)
		f.logFetch(docID, rawURL, res, "failed", err.Error())
		return nil, err
	}
	if closeErr != nil {
		os.Remove(tmp)
		return nil, closeErr
	}
	if err := os.Rename(tmp, dst); err != nil {
		return nil, err
	}

	// 哈希：SHA-256 入溯源基准；MD5 仅判下载损坏
	sha, md5Hex, size, err := hashutil.HashFile(dst)
	if err != nil {
		f.logFetch(docID, rawURL, res, "failed", err.Error())
		return nil, err
	}
	res.Path = filepath.Join("incoming", docID+".pdf")
	res.SHA256, res.MD5, res.Size = sha, md5Hex, size

	f.logFetch(docID, rawURL, res, "success", "已抓取至暂存区，待解析与人工确认")
	return res, nil
}

// checkURL 抓取前策略检查：协议 + 禁止名单 + 域名白名单。
func (f *Fetcher) checkURL(u *url.URL) error {
	pol := f.policy()
	host := u.Hostname()
	loopback := net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()

	// 硬禁止名单：robots.txt 明确「禁止使用任何自动化工具、脚本、爬虫程序采集或复制网站数据」
	// 的官方源，无论白名单如何配置都不得抓取（18 章合规优先于便利）。
	for _, b := range blockedHosts {
		if strings.EqualFold(host, b) || strings.HasSuffix(host, "."+b) {
			return fmt.Errorf("域名 %s 明确禁止自动化采集（robots.txt 声明），不得抓取；请人工下载后放入 data/incoming/", host)
		}
	}

	switch u.Scheme {
	case "https":
	case "http":
		if !pol.AllowHTTP && !loopback {
			return fmt.Errorf("仅允许 HTTPS 抓取（明文 http 被拒绝）: %s", u)
		}
	default:
		return fmt.Errorf("不支持的协议 %q: %s", u.Scheme, u)
	}

	if len(pol.AllowHosts) > 0 {
		for _, h := range pol.AllowHosts {
			if strings.EqualFold(host, h) || strings.HasSuffix(host, "."+strings.TrimPrefix(h, ".")) {
				return nil
			}
		}
		return fmt.Errorf("域名 %s 不在来源白名单内（18 章：优先官方公报或官方发布渠道）", host)
	}
	return nil
}

func (f *Fetcher) policy() FetchPolicy {
	pol := f.Policy
	if pol.MaxSize <= 0 {
		pol.MaxSize = 64 << 20 // 64 MiB
	}
	if pol.Timeout <= 0 {
		pol.Timeout = 30 * time.Second
	}
	if pol.UserAgent == "" {
		pol.UserAgent = defaultUserAgent()
	}
	return pol
}

// defaultUserAgent 如实标识机器人身份（robots 合规的前提）。
func defaultUserAgent() string {
	return "openlaw/" + versionTag + " (local legal research; read-only)"
}

// logFetch 写 import_log（含 http_status 与 content_type，4.2）。
// document 尚未入库，doc_id 置 NULL，标识保留在 reason 中。
func (f *Fetcher) logFetch(docID, sourceURL string, res *FetchResult, result, reason string) {
	reason = fmt.Sprintf("[fetch doc_id=%s] %s", docID, reason)
	httpStatus := any(nil)
	if res.HTTPStatus != 0 {
		httpStatus = res.HTTPStatus
	}
	_, _ = f.DB.Exec(`INSERT INTO import_log(doc_id, source_url, file_path, file_size, sha256,
		http_status, content_type, result, reason, created_at)
		VALUES(NULL,?,?,?,?,?,?,?,?,?)`,
		sourceURL, nullable(res.Path), res.Size, nullable(res.SHA256),
		httpStatus, nullable(res.ContentType), result, reason, res.FetchedAt)
}
