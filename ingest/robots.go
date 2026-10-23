package ingest

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// robots.txt 策略引擎（18 章合规；批量抓取层以其为前提，19 章）。
// 判定规则（RFC 9309 子集）：
//   - 仅采用 User-agent 为 * 或匹配本产品 token 的组；
//   - 路径按最长前缀匹配；同长度下 Allow 优先于 Disallow；空 Disallow 值 = 允许；
//   - robots 缺失（404/403）= 无限制；网络错误/5xx = 拒绝（fail-closed，宁可不抓也不违规）；
//   - 每个主机只取一次并缓存于本次运行。

// RobotsCache 按主机缓存 robots 策略。
type RobotsCache struct {
	pol   FetchPolicy
	cache map[string]*robotsHost
}

type robotsHost struct {
	allow    []string
	disallow []string
	denied   bool // 无法获取 robots（网络错误/5xx）→ 全站拒绝
	err      string
}

func NewRobotsCache(pol FetchPolicy) *RobotsCache {
	return &RobotsCache{pol: pol, cache: map[string]*robotsHost{}}
}

// uaToken 从 UA 提取产品 token（如 openlaw/0.1 → openlaw）。
func (rc *RobotsCache) uaToken() string {
	ua := rc.pol.UserAgent
	if ua == "" {
		ua = defaultUserAgent()
	}
	tok := strings.Fields(ua)[0]
	if i := strings.IndexAny(tok, "/ :"); i >= 0 {
		tok = tok[:i]
	}
	return strings.ToLower(strings.TrimSpace(tok))
}

// Allowed 判定 rawURL 是否允许抓取。返回 (允许, 原因)。
func (rc *RobotsCache) Allowed(ctx context.Context, rawURL string) (bool, string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, "URL 非法"
	}
	key := u.Scheme + "://" + u.Host
	h, ok := rc.cache[key]
	if !ok {
		h = rc.fetch(ctx, key)
		rc.cache[key] = h
	}
	if h.denied {
		return false, "robots.txt 无法获取（" + h.err + "），按 fail-closed 拒绝"
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	best, bestLen, verdict := "", -1, true
	for _, p := range h.allow {
		if strings.HasPrefix(path, p) && len(p) > bestLen {
			best, bestLen, verdict = p, len(p), true
		}
	}
	for _, p := range h.disallow {
		if p == "" {
			continue
		}
		if strings.HasPrefix(path, p) && len(p) > bestLen {
			best, bestLen, verdict = p, len(p), false
		}
	}
	if !verdict {
		return false, fmt.Sprintf("robots.txt 禁止抓取（规则 %q）", best)
	}
	return true, ""
}

// fetch 获取并解析 robots.txt。
func (rc *RobotsCache) fetch(ctx context.Context, origin string) *robotsHost {
	pol := rc.pol
	client := &http.Client{Timeout: pol.Timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("重定向过多")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/robots.txt", nil)
	if err != nil {
		return &robotsHost{denied: true, err: err.Error()}
	}
	req.Header.Set("User-Agent", pol.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return &robotsHost{denied: true, err: err.Error()}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden:
		return &robotsHost{} // 无 robots = 无限制（惯例）
	case resp.StatusCode >= 400:
		return &robotsHost{denied: true, err: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return &robotsHost{denied: true, err: "读取 robots 失败"}
	}
	return parseRobots(string(body), rc.uaToken())
}

// parseRobots 解析与 token 相关的组规则。
func parseRobots(body, token string) *robotsHost {
	h := &robotsHost{}
	var matching bool
	seenAny := false
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "user-agent":
			ua := strings.ToLower(val)
			seenAny = true
			matching = ua == "*" || (token != "" && strings.HasPrefix(ua, token))
		case "allow":
			if matching && val != "" {
				h.allow = append(h.allow, val)
			}
		case "disallow":
			if matching {
				h.disallow = append(h.disallow, val) // 空值保留 = 显式允许全部
			}
		}
	}
	if !seenAny {
		return &robotsHost{}
	}
	return h
}
