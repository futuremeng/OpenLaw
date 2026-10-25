package ingest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Cellar 配额探测（docs/tasks-eu-rate-limit.md RL-2）：判定当前客户端指纹桶的
// 限流窗口状态，避免盲跑正式四步链浪费配额。关键纪律：探测必须与正式抓取走
// 完全相同的指纹链（同一 policy/Transport/UA/robots 缓存）——EU 站点按客户端
// 指纹分桶配额，curl 探到 200 不代表 Go 桶可用（2026-10-25 实网取证）。

// ProbeResult 一次探测的结构化结果。
type ProbeResult struct {
	When         string `json:"when"`
	ExprURI      string `json:"expr_uri"`
	RobotsStatus int    `json:"robots_status"` // robots.txt 获取的 HTTP 状态（0=未发起）
	RobotsAllow  bool   `json:"robots_allow"`  // 策略判定允许抓取
	ExprStatus   int    `json:"expr_status"`   // expression RDF 的 HTTP 状态（0=未到达）
	ExprOK       bool   `json:"expr_ok"`       // expression 200
	RateLimited  bool   `json:"rate_limited"`  // 任一层 429
	Open         bool   `json:"open"`          // 窗口开：两层全通过
	Suggest      string `json:"suggest"`       // 人读建议
}

// Probe 轻量探测：robots.txt + 一次 expression RDF GET（不下载 manifestation/item，
// 不写正文留证；结果行写 import_log [cellar-probe] 留痕）。
func (c *Cellar) Probe(ctx context.Context, celex, lang string) (*ProbeResult, error) {
	celex = strings.ToUpper(strings.TrimSpace(celex))
	lang = strings.ToLower(strings.TrimSpace(lang))
	if !celexPattern.MatchString(celex) {
		return nil, fmt.Errorf("CELEX 编号格式非法: %q（示例 32016R0679）", celex)
	}
	if !langPattern.MatchString(lang) {
		return nil, fmt.Errorf("语言代码非法: %q（ISO 639-3，如 eng）", lang)
	}
	uri := cellarResBase + "/celex/" + celex + "." + strings.ToUpper(lang)
	res := &ProbeResult{When: time.Now().UTC().Format(time.RFC3339), ExprURI: uri}

	// 1) robots 层（与 getCellar 同一缓存与指纹）
	reqURL := strings.Replace(uri, cellarResBase, CellarBase, 1)
	if c.Endpoint != "" {
		reqURL = c.Endpoint + strings.TrimPrefix(uri, cellarResBase)
	}
	rc := c.robotsCache()
	res.RobotsAllow, _ = rc.Allowed(ctx, reqURL)
	res.RobotsStatus = rc.LastStatus(reqURL)

	// 2) expression 层（一次 GET；robots 拒绝时不发起——不发才合规）
	if res.RobotsAllow {
		_, err := c.getCellar(ctx, uri, "application/rdf+xml", 16<<20, "cellar-probe")
		if err == nil {
			res.ExprOK, res.ExprStatus = true, http.StatusOK
		} else {
			var he *HTTPStatusErr
			if errors.As(err, &he) {
				res.ExprStatus = he.Status
			}
		}
	}

	res.RateLimited = res.RobotsStatus == http.StatusTooManyRequests || res.ExprStatus == http.StatusTooManyRequests
	res.Open = res.RobotsAllow && res.ExprOK
	switch {
	case res.Open:
		res.Suggest = "窗口开：可直接执行 pilot/抓取序列"
	case res.RateLimited:
		res.Suggest = "限流中：≥30 分钟后再探测（指纹桶冷却可达小时级，勿密集重试）"
	case !res.RobotsAllow && res.RobotsStatus == 0:
		res.Suggest = "robots 策略拒绝（非限流）：核对 robots.txt 规则"
	case res.ExprStatus == http.StatusNotFound:
		res.Suggest = "未限流，但该 CELEX.语言 不存在：核对输入"
	default:
		res.Suggest = fmt.Sprintf("异常：robots=%d expr=%d，人工复核 import_log", res.RobotsStatus, res.ExprStatus)
	}

	// 3) 探测结论留痕（一行汇总；底层 expression 行已由 getCellar 以 [cellar-probe] 记录）
	_, _ = c.DB.Exec(`INSERT INTO import_log(doc_id, source_url, file_path, file_size, sha256,
		http_status, content_type, result, reason, created_at)
		VALUES(NULL,?,NULL,NULL,NULL,?,NULL,?,?,?)`,
		uri, res.ExprStatus,
		map[bool]string{true: "success", false: "failed"}[res.Open],
		fmt.Sprintf("[cellar-probe] robots=%d allow=%v expr=%d open=%v: %s",
			res.RobotsStatus, res.RobotsAllow, res.ExprStatus, res.Open, res.Suggest),
		res.When)
	return res, nil
}
