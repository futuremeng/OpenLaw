// backoff.go Cellar 429 指数退避（docs/tasks-eu-rate-limit.md RL-3）。
// 站点 429 不带 Retry-After（2026-10-25 实网取证）→ 自定策略：60s→120s→240s，
// 最多 3 次退避（第 4 次仍 429 即如实失败，绝不无上限等待）；出现 Retry-After
// 头即遵从（取较大者，封顶 10 分钟）。每次尝试都已写 import_log（getCellarOnce
// 职责）；退避等待中响应 context 取消——中断时已获取部分如实保留。
// 探测（RL-2）不走本包装：探测的意义是立刻如实报告当前窗口状态。
package ingest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	euBackoffBaseDefault = 60 * time.Second // 退避基准（60→120→240s）
	euBackoffMaxRetries  = 3                // 最多退避次数（不含首次尝试）
	euBackoffCap         = 10 * time.Minute // 单次等待上限（不得无上限等待）
)

func (c *Cellar) backoffBase() time.Duration {
	if c.BackoffBase > 0 {
		return c.BackoffBase
	}
	return euBackoffBaseDefault
}

// backoffWait 第 attempt 次退避（0 起）的等待时长：指数退避与 Retry-After 取大，
// 封顶 euBackoffCap。
func (c *Cellar) backoffWait(attempt int, retryAfter time.Duration) time.Duration {
	w := c.backoffBase() << attempt
	if retryAfter > w {
		w = retryAfter
	}
	if w > euBackoffCap {
		w = euBackoffCap
	}
	return w
}

// getCellar 带 429 退避的 Cellar GET（正式抓取路径；批量与单文档共用）。
func (c *Cellar) getCellar(ctx context.Context, uri, accept string, maxBytes int64, logTag string) ([]byte, error) {
	return c.getCellarRetry(ctx, uri, accept, maxBytes, logTag, euBackoffMaxRetries)
}

// getCellarRetry retries 次为退避上限；0 = 单次即返（探测用）。
func (c *Cellar) getCellarRetry(ctx context.Context, uri, accept string, maxBytes int64, logTag string, retries int) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		body, err := c.getCellarOnce(ctx, uri, accept, maxBytes, logTag)
		if err == nil {
			return body, nil
		}
		var he *HTTPStatusErr
		if attempt >= retries || !errors.As(err, &he) || he.Status != http.StatusTooManyRequests {
			return nil, err
		}
		select {
		case <-time.After(c.backoffWait(attempt, he.RetryAfter)):
		case <-ctx.Done():
			return nil, fmt.Errorf("429 退避等待中取消（已尝试 %d 次）: %w", attempt+1, ctx.Err())
		}
	}
}

// parseRetryAfter Retry-After 头：delta-seconds 或 HTTP-date；解析不出按 0（用退避策略）。
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if s, err := strconv.Atoi(v); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
