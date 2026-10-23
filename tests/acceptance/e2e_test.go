// Package acceptance 端到端验收（开发文档 8.2）：
// 导入 → manifest 签名 → SDK 引用校验命中 → 篡改 PDF → 断链禁止输出结论。
// 对应验收标准「任意一条输出结论都能反查到 PDF」。
package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/sdk"
	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

const (
	quote26 = "公司股东滥用公司法人独立地位和股东有限责任，逃避债务，严重损害公司债权人利益的，应当对公司债务承担连带责任。"
)

func setup(t *testing.T) (string, *ingest.Importer, *store.DB) {
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
	return dir, &ingest.Importer{DataDir: dir, DB: db}, db
}

func req2018() ingest.Request {
	return ingest.Request{
		DocID: "law_company_2018", LawID: "law_company", LawName: "中华人民共和国公司法",
		ShortName: "公司法", Issuer: "全国人大常委会", EffectLevel: "法律",
		VersionNo: "2018修正", EffectiveDate: "2018-10-26",
		SourceType: "测试样例", PDFPath: filepath.Join("..", "..", "testdata", "fake_law.pdf"),
		MDPath: filepath.Join("..", "..", "testdata", "law_company_2018.md"),
	}
}

func TestClosedLoop(t *testing.T) {
	dir, im, _ := setup(t)

	// 1) 导入
	res, err := im.Import(req2018())
	if err != nil {
		t.Fatal(err)
	}
	articleID := res.VersionID + "_第二十六条"

	// 2) 生成 manifest 并签名（防篡改基准）
	key := filepath.Join(t.TempDir(), "sign.key")
	if err := validate.GenerateKey(key); err != nil {
		t.Fatal(err)
	}
	if err := validate.BuildManifest(dir, key); err != nil {
		t.Fatal(err)
	}

	// 3) SDK 引用校验：命中、相似度 1.0、溯源完好、携带 doc_id 与 PDF 路径
	law, err := sdk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer law.Close()

	cit := law.CheckCitation("中华人民共和国公司法", "第二十六条", quote26, "2018-11-01")
	if cit.Status != sdk.StatusOK {
		t.Fatalf("应命中，实际 %+v", cit)
	}
	if cit.Data["similarity"] != float64(1) || cit.Data["hit"] != true {
		t.Fatalf("引用一致应 similarity=1 hit=true，实际 %v %v", cit.Data["similarity"], cit.Data["hit"])
	}
	if cit.Trace == nil || cit.Trace.DocID != "law_company_2018" || !cit.Trace.ChainOK {
		t.Fatalf("结果必须携带完好溯源: %+v", cit.Trace)
	}
	if cit.Trace.PDFPath == "" {
		t.Fatal("必须给出 PDF 路径供界面定位（12.5）")
	}
	// 确认状态 pending → 仅提示不报错（12.3）
	if len(cit.Warnings) == 0 {
		t.Fatal("pending 确认状态应给出人工确认提示")
	}

	// 4) 时间回溯边界：生效前一日无版本（OL-1002），生效当天命中
	before := law.ResolveVersion("公司法", "2018-10-25")
	if before.Status != sdk.StatusNotFound || before.ErrorCode != sdk.CodeNoVersionAtDate {
		t.Fatalf("生效前应 OL-1002，实际 %+v", before)
	}
	on := law.ResolveVersion("公司法", "2018-10-26")
	if on.Status != sdk.StatusOK {
		t.Fatalf("生效当天应命中（左闭），实际 %+v", on)
	}

	// 5) 法规名不存在 → OL-1001
	if r := law.ResolveVersion("不存在法", "2020-01-01"); r.ErrorCode != sdk.CodeLawNotFound {
		t.Fatalf("应 OL-1001，实际 %+v", r)
	}

	// 6) 引用差异大 → OL-1004 展示原文
	bad := law.CheckCitation("公司法", "第二十六条", "股东应当按期足额缴纳公司章程中规定的认缴出资额。", "2019-01-01")
	if bad.ErrorCode != sdk.CodeQuoteMismatch {
		t.Fatalf("引用差异大应 OL-1004，实际 %+v", bad)
	}
	if bad.Data["content"] == "" {
		t.Fatal("OL-1004 必须返回原文供人工比对")
	}

	// 7) 篡改 PDF → 断链，禁止输出结论（验收 8.2）
	pdf := filepath.Join(dir, "pdf", "law_company_2018.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4 TAMPERED BY ATTACKER\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	brk := law.DetectBreak(articleID)
	if brk.Status != sdk.StatusBrokenChain || brk.ErrorCode != sdk.CodeHashFail {
		t.Fatalf("篡改后应断链 OL-1007，实际 %+v", brk)
	}
	cit2 := law.CheckCitation("公司法", "第二十六条", quote26, "2018-11-01")
	if cit2.Status != sdk.StatusBrokenChain {
		t.Fatalf("断链时引用校验必须 broken_chain，禁止静默通过，实际 %+v", cit2)
	}

	// 8) 脱敏：四类信息无残留
	des := law.Desensitize("原告张伟，案号（2018）京0105民初12345号，身份证号 110101199003071234，北京未来法律科技有限公司", "张伟")
	text, _ := des.Data["text"].(string)
	for _, r := range []string{"张伟", "110101199003071234", "（2018）京0105民初12345号", "北京未来法律科技有限公司"} {
		if strings.Contains(text, r) {
			t.Fatalf("脱敏残留 %q: %s", r, text)
		}
	}
}

// TestVersionExpiryFlow 版本失效流程：旧版本失效后引用旧日期版本 → OL-1005 并给出当前版本。
func TestVersionExpiryFlow(t *testing.T) {
	dir, im, _ := setup(t)

	old := req2018()
	old.ExpireDate = "2020-01-01"
	if _, err := im.Import(old); err != nil {
		t.Fatal(err)
	}
	// 新版本（区间不重叠）
	newReq := req2018()
	newReq.DocID = "law_company_2023_rev"
	newReq.VersionNo = "2023修订"
	newReq.EffectiveDate = "2020-01-01"
	if _, err := im.Import(newReq); err != nil {
		t.Fatal(err)
	}

	key := filepath.Join(t.TempDir(), "sign.key")
	if err := validate.GenerateKey(key); err != nil {
		t.Fatal(err)
	}
	if err := validate.BuildManifest(dir, key); err != nil {
		t.Fatal(err)
	}

	law, err := sdk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer law.Close()

	// 时间点解析正确落在旧版本
	r := law.ResolveVersion("公司法", "2019-06-01")
	if r.Data["version_no"] != "2018修正" {
		t.Fatalf("2019-06-01 应解析到 2018修正，实际 %v", r.Data["version_no"])
	}
	// 引用旧版本：已失效 → expired + 当前有效版本
	cit := law.CheckCitation("公司法", "第二十六条", quote26, "2019-06-01")
	if cit.Status != sdk.StatusExpired || cit.ErrorCode != sdk.CodeVersionExpired {
		t.Fatalf("应 expired/OL-1005，实际 %+v", cit)
	}
	if cit.Data["current_version"] != "2023修订" {
		t.Fatalf("应给出当前有效版本，实际 %v", cit.Data["current_version"])
	}
	// 失效当天命中新版本（右开）
	r2 := law.ResolveVersion("公司法", "2020-01-01")
	if r2.Data["version_no"] != "2023修订" {
		t.Fatalf("2020-01-01 应命中新版本（右开），实际 %v", r2.Data["version_no"])
	}
}
