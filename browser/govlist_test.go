package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeBrowser 模拟规章库列表页：跳转控件 + 选中页码核验 + 每页条目。
type fakeBrowser struct {
	pageCount int
	entries   func(page int) []govListRawEntry
	cur       int
	jumpCalls []int
	closed    bool
}

func (f *fakeBrowser) Enabled() bool { return true }
func (f *fakeBrowser) Open(ctx context.Context, url string) (int, error) {
	if url != GovListStartURL {
		return 0, fmt.Errorf("只允许打开官方列表页: %s", url)
	}
	return 1, nil
}
func (f *fakeBrowser) ClosePage(ctx context.Context, pageID int) error {
	f.closed = true
	return nil
}
func (f *fakeBrowser) Evaluate(ctx context.Context, pageID int, code string) (string, error) {
	switch {
	case strings.Contains(code, "li.sel-page"):
		return fmt.Sprintf("%d", f.cur), nil
	case strings.Contains(code, "ok.click()"):
		idx := strings.Index(code, "inp.value = '")
		if idx < 0 {
			return "no_input", nil
		}
		rest := code[idx+len("inp.value = '"):]
		end := strings.Index(rest, "'")
		if end < 0 {
			return "no_input", nil
		}
		p, err := strconv.Atoi(rest[:end])
		if err != nil {
			return "no_input", nil
		}
		f.jumpCalls = append(f.jumpCalls, p)
		if p > f.pageCount { // 页码越界：选中页码不变 → 核验不过
			return "jumped", nil
		}
		f.cur = p
		return "jumped", nil
	case strings.Contains(code, "'ready' : 'wait'"):
		return "ready", nil
	case strings.Contains(code, "div.depart_m"):
		if f.cur == 0 || f.cur > f.pageCount {
			return "[]", nil
		}
		b, _ := json.Marshal(f.entries(f.cur))
		return string(b), nil
	}
	return "", fmt.Errorf("未知脚本（不得自由代码）")
}

func govcnEntries(page int) []govListRawEntry {
	var out []govListRawEntry
	for i := 1; i <= 3; i++ {
		org := "教育部"
		if i == 2 {
			org = "安顺市人民政府"
		}
		out = append(out, govListRawEntry{
			Title:  fmt.Sprintf("规章甲-%d-%d", page, i),
			URL:    fmt.Sprintf("http://www.gov.cn/zhengce/2026/content_%d_%d.htm", page, i),
			PubOrg: org,
		})
	}
	// 噪声：非 gov.cn 与资源链接必须被过滤
	out = append(out, govListRawEntry{Title: "噪声", URL: "https://evil.example/a.html", PubOrg: "x"})
	out = append(out, govListRawEntry{Title: "噪声2", URL: "https://www.gov.cn/static/style.css", PubOrg: "x"})
	return out
}

func TestGovListMultiPageToEnd(t *testing.T) {
	f := &fakeBrowser{pageCount: 3, entries: govcnEntries}
	rep, err := RunGovListBrowse(context.Background(), f, GovListOptions{MaxPages: 50, Delay: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if rep.StoppedAt != "end_of_list" {
		t.Errorf("应以 end_of_list 结束: %s", rep.StoppedAt)
	}
	if rep.Pages != 3 || len(rep.Entries) != 9 { // 3 页 × 3 条（噪声被过滤）
		t.Fatalf("pages=%d entries=%d", rep.Pages, len(rep.Entries))
	}
	if !f.closed {
		t.Error("页面应被关闭")
	}
	for _, e := range rep.Entries {
		if e.Cat == "" {
			t.Errorf("cat 应逐条推断: %+v", e)
		}
	}
}

func TestGovListCatFilterAndStartPage(t *testing.T) {
	f := &fakeBrowser{pageCount: 4, entries: govcnEntries}
	rep, err := RunGovListBrowse(context.Background(), f, GovListOptions{
		Cat: "地方政府规章", MaxPages: 2, StartPage: 3, Delay: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.StoppedAt != "max_pages" {
		t.Errorf("到达上限应 max_pages: %s", rep.StoppedAt)
	}
	if len(f.jumpCalls) == 0 || f.jumpCalls[0] != 3 { // 起始页经官方跳转控件到达（跨会话续接）
		t.Errorf("起始页应跳 3: %v", f.jumpCalls)
	}
	for _, e := range rep.Entries {
		if e.Cat != "地方政府规章" {
			t.Errorf("过滤失效: %+v", e)
		}
	}
	if len(rep.Entries) != 2 { // 每页 1 条地方政府规章 × 2 页
		t.Fatalf("entries=%d", len(rep.Entries))
	}
}

func TestGovListGuardrails(t *testing.T) {
	f := &fakeBrowser{pageCount: 1, entries: govcnEntries}
	if _, err := RunGovListBrowse(context.Background(), f, GovListOptions{Cat: "瞎填"}); err == nil {
		t.Error("未知分类必须拒绝")
	}
	rep, err := runGovListBrowse(context.Background(), &fakeBrowser{pageCount: 50, entries: govcnEntries},
		GovListOptions{}, new(time.Time), make(chan time.Duration, 200), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rep.StoppedAt != "max_pages" || rep.Pages != govListDefaultPages {
		t.Errorf("默认上限应为 %d 页: %+v", govListDefaultPages, rep)
	}
}

// TestGovListDelayFloor 虚拟时钟：所有跳页等待必须 ≥2s（低于下限被钳到默认）。
func TestGovListDelayFloor(t *testing.T) {
	clock := time.Now()
	pauseCh := make(chan time.Duration, 200)
	f := &fakeBrowser{pageCount: 4, entries: govcnEntries}
	rep, err := runGovListBrowse(context.Background(), f, GovListOptions{MaxPages: 4, Delay: time.Millisecond},
		&clock, pauseCh, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rep.StoppedAt != "max_pages" {
		t.Fatalf("stopped=%s", rep.StoppedAt)
	}
	var got []time.Duration
	for {
		select {
		case d := <-pauseCh:
			got = append(got, d)
		default:
			goto done
		}
	}
done:
	if len(got) == 0 {
		t.Fatal("应有跳页限速等待")
	}
	for _, d := range got {
		if d < govListMinDelay {
			t.Errorf("跳页等待低于 2s 下限: %v", d)
		}
	}
}

func TestParseGovListExtractFilter(t *testing.T) {
	raw := `noise before [{"title":"甲","url":"https://www.gov.cn/a/content_1.htm","puborg":"教育部"},` +
		`{"title":"乙","url":"http://sfj.zibo.gov.cn/x/doc_2.html","puborg":"淄博市人民政府"},` +
		`{"title":"","url":"https://www.gov.cn/a/content_3.htm","puborg":"x"},` +
		`{"title":"丁","url":"https://gov.cn.evil.example/a.html","puborg":"x"}] noise after`
	got := ParseGovListExtract(raw)
	if len(got) != 2 {
		t.Fatalf("应过滤空标题与伪装域: %+v", got)
	}
	if got[0].Cat != "部门规章" || got[1].Cat != "地方政府规章" {
		t.Errorf("cat 推断错误: %+v", got)
	}
}

// TestMCPRejectsRemoteEndpoint 回环硬护栏：非本机端点直接拒绝。
func TestMCPRejectsRemoteEndpoint(t *testing.T) {
	for _, bad := range []string{"http://8.8.8.8/mcp", "https://evil.example/mcp", "ws://127.0.0.1/mcp", "not a url"} {
		if _, err := newMCPClient(bad); err == nil {
			t.Errorf("应拒绝端点: %s", bad)
		}
	}
	if _, err := newMCPClient("http://127.0.0.1:9211/mcp"); err != nil {
		t.Errorf("回环端点应放行: %v", err)
	}
}

// TestMCPHandshakeAgainstFakeServer SSE 帧 + 会话头 + tools/call 全链路。
func TestMCPHandshakeAgainstFakeServer(t *testing.T) {
	var sidSeen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcpEnvelope
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "S-42")
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":\"2025-03-26\"}}\n\n", req.ID)
		case "notifications/initialized":
			sidSeen = r.Header.Get("Mcp-Session-Id")
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"tabs"},{"name":"read"},{"name":"grep"},{"name":"evaluate"}]}}`, req.ID)
		case "tools/call":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"opened page 7"}]}}`, req.ID)
		}
	}))
	defer srv.Close()
	c, err := newMCPClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.initialize(context.Background(), "openlaw-test"); err != nil {
		t.Fatal(err)
	}
	if sidSeen != "S-42" {
		t.Errorf("通知应携带会话头: %q", sidSeen)
	}
	tools, err := c.listTools(context.Background())
	if err != nil || len(tools) != 4 {
		t.Fatalf("tools/list: %v %d", err, len(tools))
	}
	res, err := c.callTool(context.Background(), "tabs", map[string]any{"action": "new", "url": "https://x"})
	if err != nil || res.Text != "opened page 7" {
		t.Fatalf("tools/call: %v %q", err, res.Text)
	}
}
