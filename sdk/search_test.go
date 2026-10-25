package sdk

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

// setupCorpus 最小合法数据目录：db/ + pdf/（哈希与库内一致，trace 不断链）。
func setupCorpus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"db", "pdf"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	content := []byte("x")
	if err := os.WriteFile(filepath.Join(dir, "pdf", "d1.pdf"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO law(law_id, name) VALUES('l1', '测试法')`)
	must(`INSERT INTO document(doc_id, file_name, file_path, file_size, sha256, hash_algo, hash_computed_at, confirm_status)
		VALUES('d1','d1.pdf','pdf/d1.pdf',1,?,'SHA-256','2026-01-01T00:00:00Z','confirmed')`, hex.EncodeToString(sum[:]))
	must(`INSERT INTO law_version(version_id, law_id, version_no, effective_date, doc_id)
		VALUES('l1_v1','l1','v1','2020-01-01','d1')`)
	must(`INSERT INTO article(article_id, version_id, article_no, content, md_path, doc_id, parser_version)
		VALUES('l1_v1_第一条','l1_v1','第一条','公司股东滥用公司法人独立地位和股东有限责任，逃避债务，严重损害公司债权人利益的，应当对公司债务承担连带责任。','md/d1.md','d1','test')`)
	return dir
}

func openSDK(t *testing.T) *OpenLaw {
	t.Helper()
	o, err := New(setupCorpus(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { o.Close() })
	return o
}

func TestSDKSearch(t *testing.T) {
	o := openSDK(t)
	res := o.Search("连带责任", 5)
	if res.Status != StatusOK {
		t.Fatalf("Status = %s", res.Status)
	}
	data := res.Data
	hits := data["hits"].([]map[string]any)
	if len(hits) != 1 {
		t.Fatalf("hits = %d, 期望 1", len(hits))
	}
	h := hits[0]
	for k, want := range map[string]string{
		"article_id": "l1_v1_第一条", "law_name": "测试法", "doc_id": "d1",
		"effective_date": "2020-01-01", "expire_date": "", "confirm_status": "confirmed", "lang": "zho",
	} {
		if h[k] != want {
			t.Errorf("hits[0][%s] = %v, 期望 %q", k, h[k], want)
		}
	}
	if len(res.Warnings) != 0 {
		t.Errorf("confirmed 语料不应有 warnings: %v", res.Warnings)
	}
	// 空结果不是错误：ok + 提示
	empty := o.Search("不存在词", 5)
	if empty.Status != StatusOK {
		t.Errorf("零命中应为 OK+warning, got %s", empty.Status)
	}
	// 中文多词 AND：须同时含两词（unicode61 不切 CJK，走 LIKE-AND 路径）
	multi := o.Search("股东 连带责任", 5)
	if mh, _ := multi.Data["hits"].([]map[string]any); len(mh) != 1 {
		t.Errorf("多词 AND 应命中 1 条, got %+v", multi.Data)
	}
	none := o.Search("股东 不存在的词", 5)
	if nh, _ := none.Data["hits"].([]map[string]any); len(nh) != 0 {
		t.Errorf("多词 AND 缺一词应零命中, got %+v", none.Data)
	}
}

func TestSDKGetArticle(t *testing.T) {
	o := openSDK(t)
	// 最小数据目录无 manifest → 断链是正确契约（OL-1006：禁止输出结论但数据照常返回供人工核对）
	res := o.GetArticle("l1_v1_第一条")
	if res.Status != StatusBrokenChain || res.ErrorCode != CodeBrokenChain {
		t.Fatalf("无 manifest 数据目录应 broken_chain/OL-1006, got %+v", res)
	}
	d := res.Data
	if d["law_name"] != "测试法" || d["content"] == "" {
		t.Errorf("断链时详情字段仍须完整: %v", d)
	}
	if res.Trace == nil || res.Trace.ChainOK || res.Trace.DocID != "d1" {
		t.Errorf("trace 应标记断链且携带 doc_id: %+v", res.Trace)
	}
	miss := o.GetArticle("不存在")
	if miss.Status != StatusNotFound || miss.ErrorCode != CodeArticleNotFound {
		t.Errorf("缺失条文应 OL-1003, got %+v", miss)
	}
}

// TestSDKSearchPendingWarning pending 语料必须带提示（12.3）。
func TestSDKSearchPendingWarning(t *testing.T) {
	dir := setupCorpus(t)
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE document SET confirm_status='pending'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	o, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	res := o.Search("连带责任", 5)
	if len(res.Warnings) == 0 {
		t.Errorf("pending 命中应有 warning: %+v", res)
	}
}
