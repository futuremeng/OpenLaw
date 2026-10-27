package ingest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// govcnDetailFixture 详情页仿真（gov.cn TRS 惯例：TRS_UEDITOR 容器 + 嵌套 div +
// 国务院令前言落款 + 章条结构；容器外有页头页脚噪声）。
const govcnDetailFixture = `<html><head><title>事业单位登记管理条例_国务院文件_中国政府网</title></head><body>
<div class="header"><a href="/">首页</a> > 政策</div>
<div class="trs_editor_view TRS_UEDITOR trs_paper_default trs_word"><div class="inner">
<p>中华人民共和国国务院令</p>
<p>第849号</p>
<p>《事业单位登记管理条例》已经2026年9月28日国务院第98次常务会议通过，现予公布，自2026年12月1日起施行。</p>
<p>总理　　李强</p>
<p>2026年10月17日</p>
<p>事业单位登记管理条例</p>
<p>第一章　总　　则</p>
<p>第一条　　为了测试，制定本条例。</p>
<p>第二条　　本条例第二条内容&nbsp;测试实体。</p>
<p>第六章　附　　则</p>
<p>第三十四条　　本条例自2026年12月1日起施行。《事业单位登记管理暂行条例》同时废止。</p>
</div></div>
<div class="footer">相关链接：全国人大</div>
</body></html>`

func TestParseGovcnDetail(t *testing.T) {
	title, md, err := ParseGovcnDetail(govcnDetailFixture)
	if err != nil {
		t.Fatal(err)
	}
	if title != "事业单位登记管理条例" {
		t.Errorf("标题清理站点后缀失败: %q", title)
	}
	for _, want := range []string{"第一条", "第三十四条", "2026年12月1日起施行"} {
		if !strings.Contains(md, want) {
			t.Errorf("正文缺少 %q", want)
		}
	}
	if strings.Contains(md, "相关链接") {
		t.Error("容器外页脚内容混入正文（div 深度配对失效）")
	}
	if strings.Contains(md, "&nbsp;") || !strings.Contains(md, " 测试实体") {
		t.Errorf("实体未还原: %q", md)
	}
}

func TestParseGovcnDetailNoContainer(t *testing.T) {
	if _, _, err := ParseGovcnDetail("<html><head><title>x</title></head><body>门户页</body></html>"); err == nil {
		t.Fatal("缺正文容器必须报错（绝不静默入库）")
	}
}

func TestGovcnDocID(t *testing.T) {
	id, err := GovcnDocID("https://www.gov.cn/zhengce/zhengceku/202610/content_7084280.htm")
	if err != nil || id != "govcn_7084280" {
		t.Fatalf("doc_id 派生错误: %q %v", id, err)
	}
	// 国家规章库详情页可挂公报路径（Athena docurl 为站点给出的完整 URL）→ 接受。
	id, err = GovcnDocID("https://www.gov.cn/gongbao/2024/issue_11466/202407/content_6963168.html")
	if err != nil || id != "govcn_6963168" {
		t.Fatalf("公报详情页应接受: %q %v", id, err)
	}
	for _, bad := range []string{
		"http://www.gov.cn/zhengce/x/content_1.htm",    // 明文 http
		"https://evil.example/zhengce/x/content_1.htm", // 非 gov.cn
		"https://www.gov.cn/zhengce/x/page_1.htm",      // 非 content_<id>
	} {
		if _, err := GovcnDocID(bad); err == nil {
			t.Errorf("应拒绝: %s", bad)
		}
	}
}

// TestGovgzResolveLevel 规章库效力等级解析：分类优先、发布机关兜底、两者皆缺不猜。
func TestGovgzResolveLevel(t *testing.T) {
	if lv, iss, ok := govgzResolveLevel("部门规章", "教育部"); !ok || lv != "部门规章" || iss != "教育部" {
		t.Errorf("分类直取失败: %s %s %v", lv, iss, ok)
	}
	if lv, iss, ok := govgzResolveLevel("地方政府规章", "北京市人民政府"); !ok || lv != "地方政府规章" || iss != "北京市人民政府" {
		t.Errorf("地方政府规章失败: %s %s %v", lv, iss, ok)
	}
	if lv, _, ok := govgzResolveLevel("", "XX省人民政府"); !ok || lv != "地方政府规章" {
		t.Errorf("按机关归类（地方）失败: %s %v", lv, ok)
	}
	if lv, _, ok := govgzResolveLevel("", "交通运输部"); !ok || lv != "部门规章" {
		t.Errorf("按机关归类（部门）失败: %s %v", lv, ok)
	}
	if _, _, ok := govgzResolveLevel("", ""); ok {
		t.Error("分类与机关皆缺时必须拒绝（不猜）")
	}
	if _, _, ok := govgzResolveLevel("规范性文件", ""); ok {
		t.Error("未知分类必须拒绝（转人工）")
	}
}

// TestGovgzImport 国家规章库入库：cat=部门规章 + puborg → effect_level/issuer 落库；
// 公报路径 URL（.html）可抓可解析；留证目录分离 incoming/govgz/。
func TestGovgzImport(t *testing.T) {
	srv := newGovcnServer(t, "User-agent: *\nAllow: /\n")
	defer srv.Close()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g := &Govcn{DataDir: dir, DB: db, MinInterval: time.Millisecond, TestHost: "127.0.0.1", Kind: "gzk"}
	im := &Importer{DataDir: dir, DB: db}

	it := g.FetchAndImport(context.Background(), im, GovcnEntry{
		Title:  "事业单位登记管理条例",
		PCode:  "教育部令第53号",
		Cat:    "部门规章",
		PubOrg: "教育部",
		URL:    srv.URL + "/zhengce/zhengceku/202610/content_7084280.htm",
	})
	if it.Status != "ok_inferred" {
		t.Fatalf("gzk 导入失败: %s (%s)", it.Status, it.Reason)
	}
	var level, issuer string
	if err := db.QueryRow(`SELECT l.effect_level, l.issuer FROM law l JOIN law_version v ON v.law_id=l.law_id JOIN document d ON d.doc_id=v.doc_id WHERE d.doc_id='govcn_7084280'`).Scan(&level, &issuer); err != nil {
		t.Fatal(err)
	}
	if level != "部门规章" {
		t.Errorf("effect_level 应为部门规章: %q", level)
	}
	if issuer != "教育部" {
		t.Errorf("issuer 应为发布机关: %q", issuer)
	}
	if _, err := os.Stat(filepath.Join(dir, "incoming", "govgz", "govcn_7084280.html")); err != nil {
		t.Errorf("留证应在 incoming/govgz/: %v", err)
	}
}

// TestGovgzSkipNoMeta 分类与发布机关皆缺 → skip_no_meta（效力等级不可保守判定，转人工）。
func TestGovgzSkipNoMeta(t *testing.T) {
	srv := newGovcnServer(t, "User-agent: *\nAllow: /\n")
	defer srv.Close()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g := &Govcn{DataDir: dir, DB: db, MinInterval: time.Millisecond, TestHost: "127.0.0.1", Kind: "gzk"}
	im := &Importer{DataDir: dir, DB: db}
	it := g.FetchAndImport(context.Background(), im, GovcnEntry{
		URL: srv.URL + "/zhengce/zhengceku/202610/content_7084280.htm",
	})
	if it.Status != "skip_no_meta" {
		t.Fatalf("缺元数据应 skip_no_meta: %s (%s)", it.Status, it.Reason)
	}
}

func TestLoadGovcnList(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "list.json")
	bad := `[
{"title":"甲条例","pcode":"国令第849号","pubtime":"2026.10.26","url":"https://www.gov.cn/zhengce/zhengceku/202610/content_1.htm"},
{"title":"乙","url":"https://evil.example/zhengce/a/content_2.htm"}
]`
	good := `[
{"title":"甲条例","pcode":"国令第849号","pubtime":"2026.10.26","url":"https://www.gov.cn/zhengce/zhengceku/202610/content_1.htm"},
{"title":"甲条例","url":"https://www.gov.cn/zhengce/zhengceku/202610/content_1.htm"}
]`
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGovcnList(p); err == nil {
		t.Fatal("含非法主机条目应整单拒绝（清单是人工产物，逐条校验从严）")
	}
	if err := os.WriteFile(p, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadGovcnList(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("应按 URL 去重: %d 条", len(entries))
	}
}

// newGovcnServer robots + 详情页测试服务器（httptest 回环，配合 TestHost 注入）。
func newGovcnServer(t *testing.T, robots string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(robots))
	})
	mux.HandleFunc("/zhengce/zhengceku/202610/content_7084280.htm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(govcnDetailFixture))
	})
	return httptest.NewServer(mux)
}

func govcnTestEntry(srvURL string) GovcnEntry {
	return GovcnEntry{
		Title: "事业单位登记管理条例", PCode: "国令第849号", PubTime: "2026.10.26",
		URL: srvURL + "/zhengce/zhengceku/202610/content_7084280.htm",
	}
}

func TestGovcnFetchAndImport(t *testing.T) {
	srv := newGovcnServer(t, "User-agent: *\nAllow: /\n")
	defer srv.Close()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g := &Govcn{DataDir: dir, DB: db, MinInterval: time.Millisecond, TestHost: "127.0.0.1"}
	im := &Importer{DataDir: dir, DB: db}

	it := g.FetchAndImport(context.Background(), im, govcnTestEntry(srv.URL))
	if it.Status != "ok_inferred" {
		t.Fatalf("导入状态异常: %s (%s)", it.Status, it.Reason)
	}
	if it.DocID != "govcn_7084280" {
		t.Errorf("doc_id: %s", it.DocID)
	}

	var confirm, level, eff string
	var arts int
	if err := db.QueryRow(`SELECT confirm_status FROM document WHERE doc_id='govcn_7084280'`).Scan(&confirm); err != nil || confirm != "pending" {
		t.Errorf("document 应为 pending 人工闸口: %q %v", confirm, err)
	}
	if err := db.QueryRow(`SELECT effect_level FROM law WHERE name='事业单位登记管理条例'`).Scan(&level); err != nil || level != "行政法规" {
		t.Errorf("law.effect_level 应为行政法规: %q %v", level, err)
	}
	if err := db.QueryRow(`SELECT effective_date FROM law_version WHERE doc_id='govcn_7084280'`).Scan(&eff); err != nil || eff != "2026-12-01" {
		t.Errorf("生效日应取自落款推断 2026-12-01: %q %v", eff, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM article WHERE doc_id='govcn_7084280'`).Scan(&arts); err != nil || arts < 3 {
		t.Errorf("条文数应 ≥3: %d %v", arts, err)
	}

	// 留证：原始 HTML 与溯源 JSON
	if _, err := os.Stat(filepath.Join(dir, "incoming", "govcn", "govcn_7084280.html")); err != nil {
		t.Error("原始 HTML 未留证 incoming/govcn/")
	}
	pj, err := os.ReadFile(filepath.Join(dir, "provenance", "govcn_7084280.json"))
	if err != nil {
		t.Fatal(err)
	}
	var prov map[string]any
	if err := json.Unmarshal(pj, &prov); err != nil {
		t.Fatal(err)
	}
	if prov["source_type"] != "gov.cn 国家行政法规库（官方详情页抓取）" || prov["confirm_status"] != "pending" {
		t.Errorf("溯源字段异常: %v", prov)
	}
	if prov["effective_date_inferred"] != true {
		t.Error("落款推断日期必须打标 effective_date_inferred")
	}

	// 联网留痕：import_log 有 [govcn] success
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result='success' AND reason LIKE '%[govcn doc_id=govcn_7084280]%'`).Scan(&n); err != nil || n < 1 {
		t.Errorf("import_log 缺少 govcn 成功记录: %d %v", n, err)
	}

	// 幂等续跑：已入库不再联网
	it2 := g.FetchAndImport(context.Background(), im, govcnTestEntry(srv.URL))
	if it2.Status != "skip_dup_db" {
		t.Errorf("重跑应 skip_dup_db: %s", it2.Status)
	}
}

// TestGovcnDecisionWholeText 修改/废止决定：正文「第X条」是被引法规条文（fixture 里
// 第一条/第二条/第三十四条会被误拆）→ WholeTextForce 强制全文即一节（1 条 article）。
func TestGovcnDecisionWholeText(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nAllow: /\n"))
	})
	mux.HandleFunc("/zhengce/zhengceku/202608/content_9000001.htm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(strings.ReplaceAll(govcnDetailFixture,
			"事业单位登记管理条例", "国务院关于修改《事业单位登记管理条例》的决定")))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g := &Govcn{DataDir: dir, DB: db, MinInterval: time.Millisecond, TestHost: "127.0.0.1"}
	im := &Importer{DataDir: dir, DB: db}

	it := g.FetchAndImport(context.Background(), im, GovcnEntry{
		PubTime: "2026.10.26",
		URL:     srv.URL + "/zhengce/zhengceku/202608/content_9000001.htm",
	})
	if it.Status != "ok_inferred_whole" {
		t.Fatalf("修改决定应走全文即一节: %s (%s)", it.Status, it.Reason)
	}
	var no string
	var arts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM article WHERE doc_id='govcn_9000001'`).Scan(&arts); err != nil || arts != 1 {
		t.Errorf("应恰 1 条 article（防误拆）: %d %v", arts, err)
	}
	if err := db.QueryRow(`SELECT article_no FROM article WHERE doc_id='govcn_9000001'`).Scan(&no); err != nil || no != "全文" {
		t.Errorf("article_no 应为全文: %q %v", no, err)
	}
}

func TestGovcnRobotsReject(t *testing.T) {
	srv := newGovcnServer(t, "User-agent: *\nDisallow: /zhengce/\n")
	defer srv.Close()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g := &Govcn{DataDir: dir, DB: db, MinInterval: time.Millisecond, TestHost: "127.0.0.1"}
	im := &Importer{DataDir: dir, DB: db}

	it := g.FetchAndImport(context.Background(), im, govcnTestEntry(srv.URL))
	if it.Status != "failed" || !strings.Contains(it.Reason, "robots") {
		t.Fatalf("robots 禁止必须拒绝并报告: %s (%s)", it.Status, it.Reason)
	}
	if _, err := os.Stat(filepath.Join(dir, "incoming", "govcn", "govcn_7084280.html")); err == nil {
		t.Error("被 robots 拒绝的页面不得落盘")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result='failed' AND reason LIKE '%robots%'`).Scan(&n); err != nil || n < 1 {
		t.Errorf("robots 拒绝必须写 import_log（不得静默）: %d %v", n, err)
	}
}
