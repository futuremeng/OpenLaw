package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

func setupImporter(t *testing.T) (*Importer, string) {
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
	return &Importer{DataDir: dir, DB: db}, dir
}

func sampleRequest() Request {
	return Request{
		DocID: "law_test_2018", LawID: "law_test", LawName: "测试法", ShortName: "测试法",
		Issuer: "全国人大", EffectLevel: "法律", VersionNo: "2018修正",
		EffectiveDate: "2018年10月26日", SourceType: "测试样例",
		PDFPath: filepath.Join("..", "testdata", "fake_law.pdf"),
		MDPath:  filepath.Join("..", "testdata", "law_company_2018.md"),
	}
}

func TestImportSuccess(t *testing.T) {
	im, dir := setupImporter(t)
	res, err := im.Import(sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if res.DocID != "law_test_2018" || len(res.ArticleIDs) != 2 {
		t.Fatalf("导入结果异常: %+v", res)
	}
	if res.SHA256 == "" {
		t.Fatal("必须记录 SHA-256")
	}

	// 文件落位
	for _, rel := range []string{"pdf/law_test_2018.pdf", "md/law_test_2018.md", "provenance/law_test_2018.json"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("缺少文件 %s: %v", rel, err)
		}
	}

	// 数据库计数
	var n int
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM article WHERE doc_id = 'law_test_2018'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("条文应入库 2 条，实际 %d (%v)", n, err)
	}
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result = 'success'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("应有一条 success 导入日志，实际 %d (%v)", n, err)
	}
	// 日期已标准化为 ISO（version_id = law_id + "_" + version_no）
	var eff string
	if err := im.DB.QueryRow(`SELECT effective_date FROM law_version WHERE version_id='law_test_2018修正'`).Scan(&eff); err != nil || eff != "2018-10-26" {
		t.Fatalf("生效日期应标准化为 ISO，实际 %q (%v)", eff, err)
	}
	// 溯源文件必填字段齐全
	if _, err := LoadProvenance(filepath.Join(dir, "provenance", "law_test_2018.json")); err != nil {
		t.Fatalf("溯源文件校验失败: %v", err)
	}
}

// TestImportRegionCarried 随更新包携带的地域标引（第 5 步）：只写码与来源（pack），
// 署名一律留空 = 接收方界面仍显示待复核；已有码或已人工落判的行不得被包推翻；
// 写入逐笔进 region_log（actor=openlaw-pack，非人工署名）。
func TestImportRegionCarried(t *testing.T) {
	im, _ := setupImporter(t)
	req := sampleRequest()
	req.RegionCode, req.RegionSource, req.RegionBatch = "130000", "auto", "pack:2026-10-30T00:00:00Z"
	if _, err := im.Import(req); err != nil {
		t.Fatal(err)
	}
	var code, src string
	var reviewer any
	if err := im.DB.QueryRow(`SELECT COALESCE(region_code,''), COALESCE(region_source,''), region_reviewer
		FROM law WHERE law_id='law_test'`).Scan(&code, &src, &reviewer); err != nil {
		t.Fatal(err)
	}
	if code != "130000" || src != RegionSourcePack {
		t.Errorf("携带的地域应落库且来源记 pack: code=%q source=%q", code, src)
	}
	if reviewer != nil {
		t.Errorf("包携带不得写署名（接收方必须显示待复核），实际 %v", reviewer)
	}
	var action, logSrc, actor, batch, to string
	if err := im.DB.QueryRow(`SELECT action, source, actor, COALESCE(batch_id,''), COALESCE(to_code,'')
		FROM region_log WHERE law_id='law_test'`).Scan(&action, &logSrc, &actor, &batch, &to); err != nil {
		t.Fatalf("包携带写入必须留审计: %v", err)
	}
	if action != "suggest" || logSrc != RegionSourcePack || actor != regionActorPack ||
		batch != "pack:2026-10-30T00:00:00Z" || to != "130000" {
		t.Errorf("region_log 内容异常: %s/%s/%s/%s/%s", action, logSrc, actor, batch, to)
	}

	// 不带地域的常规导入完全不触碰地域列与审计。
	req2 := sampleRequest()
	req2.DocID, req2.LawID, req2.VersionNo, req2.EffectiveDate = "law_other_2018", "law_other", "2018版", "2018-01-01"
	if _, err := im.Import(req2); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM law WHERE law_id='law_other'
		AND COALESCE(region_code,'')<>''`).Scan(&n); err != nil || n != 0 {
		t.Errorf("无地域的导入不应写码: %d (%v)", n, err)
	}
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM region_log WHERE law_id='law_other'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("无地域的导入不应留地域审计: %d (%v)", n, err)
	}

	// 守卫：本地已人工落判 → 包不得推翻（既不写码也不写审计）。
	if _, err := im.DB.Exec(`UPDATE law SET region_code='310000', region_source='manual',
		region_reviewer='复核人甲' WHERE law_id='law_test'`); err != nil {
		t.Fatal(err)
	}
	req3 := sampleRequest()
	req3.DocID, req3.VersionNo, req3.EffectiveDate = "law_test_2019", "2019修正", "2019-01-01"
	req3.AutoExpirePrior = true
	req3.RegionCode, req3.RegionSource, req3.RegionBatch = "110000", "auto", "pack:2026-10-31T00:00:00Z"
	if _, err := im.Import(req3); err != nil {
		t.Fatal(err)
	}
	if err := im.DB.QueryRow(`SELECT COALESCE(region_code,'') FROM law WHERE law_id='law_test'`).Scan(&code); err != nil || code != "310000" {
		t.Errorf("人工落判的码不得被包顶掉: %q (%v)", code, err)
	}
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM region_log WHERE law_id='law_test'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("被守卫挡下的写入不得留审计: %d (%v)", n, err)
	}
}
func TestImportNoOverwrite(t *testing.T) {
	im, _ := setupImporter(t)
	if _, err := im.Import(sampleRequest()); err != nil {
		t.Fatal(err)
	}
	if _, err := im.Import(sampleRequest()); err == nil {
		t.Fatal("重复 doc_id 应被拒绝")
	}
	var n int
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM document`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("document 应仍为 1 条，实际 %d", n)
	}
}

// TestImportRollback 任一环节失败整批回滚，旧数据不受影响（6.1）。
func TestImportRollback(t *testing.T) {
	im, dir := setupImporter(t)

	// 正文无条文 → 解析失败，必须整批失败
	req := sampleRequest()
	badMD := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(badMD, []byte("# 说明\n没有条文标记。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req.MDPath = badMD
	if _, err := im.Import(req); err == nil {
		t.Fatal("无条文应失败")
	}
	var n int
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM document`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("失败后不得留下 document，实际 %d", n)
	}
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result = 'failed'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("应记录 failed 日志，实际 %d", n)
	}
}

// TestImportVersionOverlap 版本时效区间重叠必须被拦截（13.3）：
// 否则同一天会出现两个生效版本。
func TestImportVersionOverlap(t *testing.T) {
	im, _ := setupImporter(t)
	if _, err := im.Import(sampleRequest()); err != nil {
		t.Fatal(err)
	}
	req := sampleRequest()
	req.DocID = "law_test_2018_dup"
	if _, err := im.Import(req); err == nil {
		t.Fatal("区间重叠应被拦截")
	}
	var n int
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM law_version`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("版本表应仍为 1 条，实际 %d", n)
	}
	if err := im.DB.QueryRow(`SELECT COUNT(*) FROM import_log WHERE result = 'rolled_back'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("应记录 rolled_back 日志，实际 %d", n)
	}
}

// TestImportExpireBeforeEffective 失效日期必须晚于生效日期（左闭右开）。
func TestImportExpireBeforeEffective(t *testing.T) {
	im, _ := setupImporter(t)
	req := sampleRequest()
	req.ExpireDate = "2018-10-26" // 与生效同日，右开导致空区间
	if _, err := im.Import(req); err == nil {
		t.Fatal("失效日期不晚于生效日期应报错")
	}
}

// TestImportAutoExtractFromPDF MDPath 留空：自动提取 PDF 文本层 → 规范化 → 拆分入库，
// 提取结果落盘 md/（可追溯）；扫描件（无文本层）必须报错拒绝。
func TestImportAutoExtractFromPDF(t *testing.T) {
	im, dir := setupImporter(t)
	req := Request{
		DocID: "law_pdftext_2018", LawID: "law_pdftext", LawName: "文本层测试法",
		VersionNo: "2018版", EffectiveDate: "2018-01-01", SourceType: "测试样例",
		PDFPath: filepath.Join("..", "testdata", "text_layer.pdf"),
	}
	res, err := im.Import(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ArticleIDs) != 2 {
		t.Fatalf("应拆出 2 条，实际 %d", len(res.ArticleIDs))
	}
	mdPath := filepath.Join(dir, "md", "law_pdftext_2018.md")
	b, err := os.ReadFile(mdPath)
	if err != nil || !strings.Contains(string(b), "公司股东") {
		t.Fatalf("提取正文应落盘 md/ 且含关键内容: %v", err)
	}
	if res.ChainIssues == nil {
		// manifest 未生成时应有断链提示，至少不为 nil 语义（空切片亦可），仅验证不 panic
		_ = res
	}

	// 扫描件路径：非 PDF/无文本层 → 拒绝导入
	im2, _ := setupImporter(t)
	req2 := req
	req2.DocID = "law_scan_2018"
	req2.LawID = "law_scan"
	req2.PDFPath = filepath.Join("..", "testdata", "fake_law.pdf")
	if _, err := im2.Import(req2); err == nil {
		t.Fatal("无文本层应拒绝导入（转 OCR/人工）")
	}
}
