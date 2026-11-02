package ingest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// fakeCellar429 假 Cellar：expression 层按脚本序列返回状态码；robots 全放行。
func fakeCellar429(t *testing.T, script func(n int) int) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	n := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "User-agent: *\nAllow: /\n")
	})
	mux.HandleFunc("/celex/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		code := script(n)
		cur := n
		mu.Unlock()
		if code == http.StatusTooManyRequests {
			if t.Name() == "TestBackoffHonorsRetryAfter" && cur == 1 {
				w.Header().Set("Retry-After", "1")
			}
		}
		w.WriteHeader(code)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() int { mu.Lock(); defer mu.Unlock(); return n }
}

func newBackoffCellar(t *testing.T, srvURL string) (*Cellar, *store.DB, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := &Cellar{DataDir: dir, DB: db, Endpoint: srvURL, MinInterval: time.Millisecond, BackoffBase: 5 * time.Millisecond}
	return c, db, cellarResBase + "/celex/32016R0679.ENG"
}

func countLog(t *testing.T, db *store.DB, result string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result = ?`, result).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestBackoffRecovers 429,429,200：第三次成功；import_log 两条 failed + 一条 success。
func TestBackoffRecovers(t *testing.T) {
	srv, hits := fakeCellar429(t, func(n int) int {
		if n <= 2 {
			return http.StatusTooManyRequests
		}
		return http.StatusOK
	})
	c, db, uri := newBackoffCellar(t, srv.URL)
	body, err := c.getCellar(context.Background(), uri, "application/rdf+xml", 1<<20, "cellar-text")
	if err != nil || len(body) != 0 {
		t.Fatalf("应第三次成功: %v", err)
	}
	if hits() != 3 {
		t.Errorf("应尝试 3 次，实际 %d", hits())
	}
	if f, s := countLog(t, db, "failed"), countLog(t, db, "success"); f != 2 || s != 1 {
		t.Errorf("留痕计数错: failed=%d success=%d", f, s)
	}
}

// TestBackoffGivesUp 429×4（3 次退避后仍 429）：如实失败且不再重试；4 条 failed。
func TestBackoffGivesUp(t *testing.T) {
	srv, hits := fakeCellar429(t, func(int) int { return http.StatusTooManyRequests })
	c, db, uri := newBackoffCellar(t, srv.URL)
	_, err := c.getCellar(context.Background(), uri, "", 1<<20, "cellar-text")
	var he *HTTPStatusErr
	if !errors.As(err, &he) || he.Status != http.StatusTooManyRequests {
		t.Fatalf("应报 429 限流: %v", err)
	}
	if hits() != 1+euBackoffMaxRetries {
		t.Errorf("应尝试 1+3 次封顶，实际 %d", hits())
	}
	if f := countLog(t, db, "failed"); f != 4 {
		t.Errorf("留痕应 4 条 failed，实际 %d", f)
	}
}

// TestBackoffHonorsRetryAfter Retry-After:1（秒）出现时遵从——等待明显长于退避基准。
func TestBackoffHonorsRetryAfter(t *testing.T) {
	srv, _ := fakeCellar429(t, func(n int) int {
		if n == 1 {
			return http.StatusTooManyRequests
		}
		return http.StatusOK
	})
	c, _, uri := newBackoffCellar(t, srv.URL)
	start := time.Now()
	if _, err := c.getCellar(context.Background(), uri, "", 1<<20, "cellar-text"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < time.Second {
		t.Errorf("Retry-After:1 应等待约 1 秒，实际 %v", d)
	}
}

// TestBackoffContextCancel 退避等待中取消：立即返回 context 错误，不空等。
func TestBackoffContextCancel(t *testing.T) {
	srv, _ := fakeCellar429(t, func(int) int { return http.StatusTooManyRequests })
	c, _, uri := newBackoffCellar(t, srv.URL)
	c.BackoffBase = 30 * time.Second // 若不退避即取消，测试必须秒级返回
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	start := time.Now()
	_, err := c.getCellar(ctx, uri, "", 1<<20, "cellar-text")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("应报取消: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("取消未在退避等待点生效")
	}
}

// TestBackoffWaitStrategy 退避策略表：60→120→240；Retry-After 取大；封顶 10 分钟。
func TestBackoffWaitStrategy(t *testing.T) {
	c := &Cellar{}
	want := []time.Duration{60 * time.Second, 120 * time.Second, 240 * time.Second}
	for i, w := range want {
		if got := c.backoffWait(i, 0); got != w {
			t.Errorf("第 %d 次退避应 %v，实际 %v", i, w, got)
		}
	}
	if got := c.backoffWait(0, 90*time.Second); got != 90*time.Second {
		t.Errorf("Retry-After 更长应遵从: %v", got)
	}
	if got := c.backoffWait(2, time.Hour); got != euBackoffCap {
		t.Errorf("必须封顶: %v", got)
	}
	if got := parseRetryAfter("90"); got != 90*time.Second {
		t.Errorf("delta-seconds 解析: %v", got)
	}
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("缺头应为 0: %v", got)
	}
}
