package pack

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

// trustPack 把包公钥登记进受信名单（测试辅助，模拟发版内置名单的效果）。
func trustPack(t *testing.T, packDir string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(packDir, "pubkey.hex"))
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("test-%s-%d", t.Name(), time.Now().UnixNano())
	if err := RegisterTrustedPubkey(id, strings.TrimSpace(string(b))); err != nil {
		t.Fatal(err)
	}
}

// setupSource 构造一个已导入一份法规的数据目录（复用 ingest 闭环）。
func setupSource(t *testing.T) (string, *store.DB, string) {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"pdf", "md", "db", "provenance"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	im := &ingest.Importer{DataDir: dir, DB: db}
	_, err = im.Import(ingest.Request{
		DocID: "law_pack_2018", LawID: "law_pack", LawName: "打包测试法", ShortName: "打包法",
		Issuer: "测试机关", EffectLevel: "法律", VersionNo: "2018版",
		EffectiveDate: "2018-01-01", SourceType: "测试样例",
		PDFPath: filepath.Join("..", "testdata", "fake_law.pdf"),
		MDPath:  filepath.Join("..", "testdata", "law_company_2018.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "pack.key")
	if err := validate.GenerateKey(key); err != nil {
		t.Fatal(err)
	}
	return dir, db, key
}

// TestBuildVerifyRoundtrip build → verify 往返：签名有效、哈希一致。
func TestBuildVerifyRoundtrip(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-20261023")
	if err := Build(db, dir, packDir, key, "", ""); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"manifest.json", "manifest.sig", "pubkey.hex", "changes.csv",
		"pdf/law_pack_2018.pdf", "md/law_pack_2018.md", "provenance/law_pack_2018.json"} {
		if _, err := os.Stat(filepath.Join(packDir, f)); err != nil {
			t.Fatalf("包内缺少 %s", f)
		}
	}
	if err := Verify(packDir); err != nil {
		t.Fatalf("完整包应校验通过: %v", err)
	}
}

// TestBuildManifestLicense manifest 必须携带共享合规声明（欧盟等来源要求重使用署名）；
// 空参数得到非空默认值，自定义参数原样入签名字段。
func TestBuildManifestLicense(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-lic")
	if err := Build(db, dir, packDir, key, "CC-BY-TEST", "© TEST source"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(packDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m.License != "CC-BY-TEST" || m.Attribution != "© TEST source" {
		t.Fatalf("署名字段未入 manifest: %+v", m)
	}
	if err := Verify(packDir); err != nil { // 署名字段随 manifest 一起被签名保护
		t.Fatalf("含署名字段的包应校验通过: %v", err)
	}

	packDir2 := filepath.Join(t.TempDir(), "pack-lic2")
	if err := Build(db, dir, packDir2, key, "", ""); err != nil {
		t.Fatal(err)
	}
	body, _ = os.ReadFile(filepath.Join(packDir2, "manifest.json"))
	var m2 Manifest
	if err := json.Unmarshal(body, &m2); err != nil {
		t.Fatal(err)
	}
	if m2.License == "" || m2.Attribution == "" {
		t.Fatal("空参数应回退到默认共享许可/署名声明")
	}
}

// TestVerifyDetectsTamper 包内文件被替换 / 塞入未收录文件 → 校验必须失败。
func TestVerifyDetectsTamper(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-tamper")
	if err := Build(db, dir, packDir, key, "", ""); err != nil {
		t.Fatal(err)
	}

	// 替换 PDF
	if err := os.WriteFile(filepath.Join(packDir, "pdf", "law_pack_2018.pdf"),
		[]byte("TAMPERED"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(packDir); err == nil {
		t.Fatal("替换文件应校验失败")
	}

	// 重新打包后塞入多余文件
	packDir2 := filepath.Join(t.TempDir(), "pack-extra")
	if err := Build(db, dir, packDir2, key, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir2, "pdf", "evil.pdf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(packDir2); err == nil {
		t.Fatal("未收录文件应校验失败")
	}
}

// TestImportPackFailNoOverwrite（17.2 追溯矩阵 test_pack_fail_no_overwrite）
// 校验失败的包必须整体拒绝，目标数据目录保持原样。
func TestImportPackFailNoOverwrite(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-bad")
	if err := Build(db, dir, packDir, key, "", ""); err != nil {
		t.Fatal(err)
	}
	// 篡改 changes.csv（不改签名）
	cp := filepath.Join(packDir, "changes.csv")
	b, err := os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cp, append(b, "add,x,y,z\n"...), 0o644); err != nil {
		t.Fatal(err)
	}

	// 全新目标目录
	target := t.TempDir()
	tdb, err := store.Open(filepath.Join(target, "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer tdb.Close()

	trustPack(t, packDir) // 来源可信，失败应来自篡改本身而非信任门禁
	if _, err := Import(tdb, target, packDir); err == nil {
		t.Fatal("篡改包应拒绝导入")
	}
	var n int
	if err := tdb.QueryRow(`SELECT COUNT(*) FROM document`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("失败导入不得写入任何数据，实际 %d 条", n)
	}
}

// TestBuildVerifyImportDocxLayer docx 原始层必须完整走 pack 闭环：
// manifest 收录 docx/<doc_id>.docx、包内复制、verify 通过、导入空目录后按 docx 提取入库。
func TestBuildVerifyImportDocxLayer(t *testing.T) {
	dir, db, key := setupSource(t)

	// 追加一份 docx 文档（最小 OOXML：zip 容器 + word/document.xml）
	src := filepath.Join(t.TempDir(), "docx测试条例_20200101.docx")
	writeMinimalDocx(t, src, "第一条　docx 正文测试。")
	im := &ingest.Importer{DataDir: dir, DB: db}
	if _, err := im.Import(ingest.Request{
		DocID: "law_packdocx_2020", LawID: "law_packdocx", LawName: "docx测试条例",
		EffectLevel: "法律", VersionNo: "2020版", EffectiveDate: "2020-01-01",
		SourceType: "测试样例", PDFPath: src,
	}); err != nil {
		t.Fatal(err)
	}

	packDir := filepath.Join(t.TempDir(), "pack-docx")
	if err := Build(db, dir, packDir, key, "", ""); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"docx/law_packdocx_2020.docx", "provenance/law_packdocx_2020.json"} {
		if _, err := os.Stat(filepath.Join(packDir, f)); err != nil {
			t.Fatalf("包内缺少 %s", f)
		}
	}
	if err := Verify(packDir); err != nil {
		t.Fatalf("docx 层完整包应校验通过: %v", err)
	}

	// 导入空目录：pack 侧必须找到 docx 源并完成提取入库
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	tdb, err := store.Open(filepath.Join(target, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer tdb.Close()
	trustPack(t, packDir)
	if _, err := Import(tdb, target, packDir); err != nil {
		t.Fatalf("docx 包导入失败: %v", err)
	}
	var n int
	if err := tdb.QueryRow(`SELECT COUNT(*) FROM article WHERE doc_id='law_packdocx_2020'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("docx 条文应入库 1 条，实际 %d (%v)", n, err)
	}
	var fp string
	if err := tdb.QueryRow(`SELECT file_path FROM document WHERE doc_id='law_packdocx_2020'`).Scan(&fp); err != nil || fp != "docx/law_packdocx_2020.docx" {
		t.Fatalf("file_path 应为 docx 层，实际 %q (%v)", fp, err)
	}
}

func writeMinimalDocx(t *testing.T, path, para string) {
	t.Helper()
	docXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:r><w:t>` + para + `</w:t></w:r></w:p></w:body></w:document>`
	zf, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zf.Close()
	zw := zip.NewWriter(zf)
	for _, e := range [][2]string{
		{"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`},
		{"word/document.xml", docXML},
	} {
		fw, err := zw.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(e[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestImportPackWholeText 全文即一节文档的 pack 往返：changes.csv 携带 whole=1，
// 导入空目录后仍按全文模式入库（不会因无条文标记而失败）。
func TestImportPackWholeText(t *testing.T) {
	dir, db, key := setupSource(t)
	src := filepath.Join(t.TempDir(), "名单公告_.docx")
	writeMinimalDocx(t, src, "关于公布第一批名单的公告。")
	im := &ingest.Importer{DataDir: dir, DB: db}
	if _, err := im.Import(ingest.Request{
		DocID: "law_packwhole_2021", LawID: "law_packwhole", LawName: "名单公告",
		EffectLevel: "其他", VersionNo: "2021版", EffectiveDate: "2021-01-01",
		SourceType: "测试样例", PDFPath: src, WholeTextMode: true,
	}); err != nil {
		t.Fatal(err)
	}
	packDir := filepath.Join(t.TempDir(), "pack-whole")
	if err := Build(db, dir, packDir, key, "", ""); err != nil {
		t.Fatal(err)
	}
	changes, err := ReadChanges(packDir)
	if err != nil {
		t.Fatal(err)
	}
	whole := 0
	for _, c := range changes {
		if c.Whole {
			whole++
		}
	}
	if whole != 1 {
		t.Fatalf("changes.csv 应恰有 1 条 whole=1，实际 %d", whole)
	}
	if err := Verify(packDir); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	tdb, err := store.Open(filepath.Join(target, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer tdb.Close()
	trustPack(t, packDir)
	if _, err := Import(tdb, target, packDir); err != nil {
		t.Fatalf("whole 包导入应成功: %v", err)
	}
	var no string
	if err := tdb.QueryRow(`SELECT article_no FROM article WHERE doc_id='law_packwhole_2021'`).Scan(&no); err != nil || no != "全文" {
		t.Fatalf("导入后应为全文节，实际 %q (%v)", no, err)
	}
}

// TestImportPackLang EU 文档 pack 往返：changes.csv 携带 lang=eng，
// 导入空目录后 lang 保持、英文条文按 SplitArticlesEU 重新切分。
func TestImportPackLang(t *testing.T) {
	dir, db, key := setupSource(t)
	src := filepath.Join(t.TempDir(), "eu_act.xml")
	if err := os.WriteFile(src, []byte("<ACT>xml</ACT>"), 0o644); err != nil {
		t.Fatal(err)
	}
	im := &ingest.Importer{DataDir: dir, DB: db}
	if _, err := im.Import(ingest.Request{
		DocID: "eu_32016r0679_eng", LawID: "eu_reg_2016_679", LawName: "Regulation (EU) 2016/679",
		EffectLevel: "欧盟条例", VersionNo: "OJ原文", EffectiveDate: "2016-05-24",
		SourceType: "cellar", PDFPath: src, Lang: "eng",
		Articles: []parse.Article{
			{No: "Article 1", Content: "Article 1\nScope\n\n1. Text one."},
			{No: "Article 2", Content: "Article 2\n\n1. Text two."},
		},
		MDContent:     "## Article 1 Scope\n\n1. Text one.\n\n## Article 2\n\n1. Text two.",
		ParserVersion: parse.ParserVersionEU,
	}); err != nil {
		t.Fatal(err)
	}
	packDir := filepath.Join(t.TempDir(), "pack-lang")
	if err := Build(db, dir, packDir, key, "CC BY 4.0", "(c) European Union"); err != nil {
		t.Fatal(err)
	}
	changes, err := ReadChanges(packDir)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range changes {
		if c.DocID == "eu_32016r0679_eng" {
			found = true
			if c.Lang != "eng" {
				t.Fatalf("changes.csv lang = %q, 期望 eng", c.Lang)
			}
		}
	}
	if !found {
		t.Fatal("changes.csv 缺 EU 文档行")
	}
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	tdb, err := store.Open(filepath.Join(target, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer tdb.Close()
	trustPack(t, packDir)
	if _, err := Import(tdb, target, packDir); err != nil {
		t.Fatalf("lang 包导入应成功: %v", err)
	}
	var lang string
	if err := tdb.QueryRow(`SELECT lang FROM document WHERE doc_id='eu_32016r0679_eng'`).Scan(&lang); err != nil || lang != "eng" {
		t.Fatalf("导入后 document.lang = %q (%v)", lang, err)
	}
	var n int
	if err := tdb.QueryRow(`SELECT COUNT(*) FROM article WHERE doc_id='eu_32016r0679_eng'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("回灌应按 SplitArticlesEU 切出 2 条，实际 %d (%v)", n, err)
	}
}

// TestImportPackSuccess 正常包导入到空目录：数据落位且走同一闭环（pending 转人工）。
func TestImportPackSuccess(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-ok")
	if err := Build(db, dir, packDir, key, "", ""); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	tdb, err := store.Open(filepath.Join(target, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer tdb.Close()

	trustPack(t, packDir)
	results, err := Import(tdb, target, packDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].DocID != "law_pack_2018" {
		t.Fatalf("应导入 1 条: %+v", results)
	}
	var n int
	if err := tdb.QueryRow(`SELECT COUNT(*) FROM article WHERE doc_id='law_pack_2018'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("条文应入库 2 条，实际 %d (%v)", n, err)
	}
	var cs string
	if err := tdb.QueryRow(`SELECT confirm_status FROM document WHERE doc_id='law_pack_2018'`).Scan(&cs); err != nil || cs != "pending" {
		t.Fatalf("导入包条目应为 pending 待人工确认，实际 %q", cs)
	}
}
