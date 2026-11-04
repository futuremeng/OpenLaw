package pack

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

// setupMultiSource 构造含国家级 / 广东 / 北京三条法规的源数据目录（发布日各异，
// 供地域切分与日期窗口切分测试）。
func setupMultiSource(t *testing.T) (string, *store.DB, string) {
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
	type item struct {
		docID, lawID, name, level, pub, region string
	}
	for _, it := range []item{
		{"law_cn", "law_cn", "中央测试法", "法律", "2015-05-01", ""},
		{"law_gd", "law_gd", "广东测试条例", "地方性法规", "2020-03-01", "440000"},
		{"law_bj", "law_bj", "北京测试办法", "地方性法规", "2024-06-01", "110000"},
	} {
		req := ingest.Request{
			DocID: it.docID, LawID: it.lawID, LawName: it.name,
			Issuer: "测试机关", EffectLevel: it.level, VersionNo: "2020版",
			EffectiveDate: "2020-01-01", PublishDate: it.pub, SourceType: "测试样例",
			PDFPath: filepath.Join("..", "testdata", "fake_law.pdf"),
			MDPath:  filepath.Join("..", "testdata", "law_company_2018.md"),
		}
		if it.region != "" {
			req.RegionCode, req.RegionSource = it.region, "auto"
		}
		if _, err := im.Import(req); err != nil {
			t.Fatal(err)
		}
	}
	key := filepath.Join(t.TempDir(), "pack.key")
	if err := validate.GenerateKey(key); err != nil {
		t.Fatal(err)
	}
	return dir, db, key
}

// openTarget 新建空目标数据目录（导入侧）。
func openTarget(t *testing.T) (string, *store.DB) {
	t.Helper()
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	tdb, err := store.Open(filepath.Join(target, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tdb.Close() })
	return target, tdb
}

// changeDocIDs 读出包内 changes.csv 的 doc_id 集合。
func changeDocIDs(t *testing.T, packDir string) map[string]bool {
	t.Helper()
	f, err := os.Open(filepath.Join(packDir, "changes.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, r := range recs[1:] {
		out[r[1]] = true
	}
	return out
}

// TestBuildSplitByRegion 基础包按地域切分：CN=国家级（区划码为空），区划码前缀匹配；
// 包内只含清单引用到的文件，manifest 携带包身份字段且随签名保护。
func TestBuildSplitByRegion(t *testing.T) {
	dir, db, key := setupMultiSource(t)

	cnDir := filepath.Join(t.TempDir(), "base-cn")
	if err := BuildWithOptions(db, dir, cnDir, key, "", "",
		BuildOptions{PackID: "base-cn-2026", Kind: "base", Regions: []string{"CN"}}); err != nil {
		t.Fatal(err)
	}
	ids := changeDocIDs(t, cnDir)
	if len(ids) != 1 || !ids["law_cn"] {
		t.Errorf("国家级包应只含 law_cn，实际 %v", ids)
	}
	if _, err := os.Stat(filepath.Join(cnDir, "pdf", "law_gd.pdf")); !os.IsNotExist(err) {
		t.Error("未被清单引用的文件不得进包")
	}
	if err := Verify(cnDir); err != nil {
		t.Fatalf("切分包应校验通过: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(cnDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m.PackID != "base-cn-2026" || m.Kind != "base" || m.DocCount != 1 || len(m.Regions) != 1 {
		t.Errorf("manifest 包身份字段缺失或不符: %+v", m)
	}

	gdDir := filepath.Join(t.TempDir(), "base-gd")
	if err := BuildWithOptions(db, dir, gdDir, key, "", "",
		BuildOptions{PackID: "base-44-2026", Kind: "base", Regions: []string{"44"}}); err != nil {
		t.Fatal(err)
	}
	if ids := changeDocIDs(t, gdDir); len(ids) != 1 || !ids["law_gd"] {
		t.Errorf("广东省包应只含 law_gd，实际 %v", ids)
	}
}

// TestBuildSplitByJurisdiction 基础包按法域切分（多法域设计 §5）：EU 桶只含欧盟条目、
// US 桶经 importer 按效力等级真实推导、none 桶如实为空、manifest 携带法域身份；
// 名单外取值拒绝（不得静默产出空包）。
func TestBuildSplitByJurisdiction(t *testing.T) {
	dir, db, key := setupMultiSource(t)
	// 模拟欧盟通道的显式落 EU（真实链路为 cellar 通道 UpsertLaw 显式写 EU）。
	if _, err := db.Exec(`UPDATE law SET jurisdiction='EU' WHERE law_id='law_gd'`); err != nil {
		t.Fatal(err)
	}
	// 美国联邦规章经真实写路径入库——法域由效力等级推导（判不出留空，此处应为 US）。
	im := &ingest.Importer{DataDir: dir, DB: db}
	if _, err := im.Import(ingest.Request{
		DocID: "law_us", LawID: "law_us", LawName: "测试联邦规章", Issuer: "Test Agency",
		EffectLevel: "联邦规章", VersionNo: "2025版", EffectiveDate: "2025-01-01",
		PublishDate: "2025-01-01", SourceType: "us-govinfo",
		PDFPath: filepath.Join("..", "testdata", "fake_law.pdf"),
		MDPath:  filepath.Join("..", "testdata", "law_company_2018.md"),
	}); err != nil {
		t.Fatal(err)
	}
	var jur string
	if err := db.QueryRow(`SELECT jurisdiction FROM law WHERE law_id='law_us'`).Scan(&jur); err != nil {
		t.Fatal(err)
	}
	if jur != "US" {
		t.Fatalf("联邦规章应推导法域 US，实际 %q", jur)
	}

	cases := []struct {
		juris []string
		want  map[string]bool
	}{
		{[]string{"EU"}, map[string]bool{"law_gd": true}},
		{[]string{"US"}, map[string]bool{"law_us": true}},
		{[]string{"CN"}, map[string]bool{"law_cn": true, "law_bj": true}},
		{[]string{"EU", "US"}, map[string]bool{"law_gd": true, "law_us": true}},
	}
	for _, c := range cases {
		out := filepath.Join(t.TempDir(), "base")
		if err := BuildWithOptions(db, dir, out, key, "", "",
			BuildOptions{PackID: "base-x-2026", Kind: "base", Jurisdictions: c.juris}); err != nil {
			t.Fatal(err)
		}
		got := changeDocIDs(t, out)
		if len(got) != len(c.want) {
			for k := range c.want {
				if !got[k] {
					t.Errorf("法域 %v 缺条目 %s", c.juris, k)
				}
			}
			for k := range got {
				if !c.want[k] {
					t.Errorf("法域 %v 多出条目 %s", c.juris, k)
				}
			}
		}
	}

	// manifest 携带法域身份（随签名保护），none 桶空包也如实记空清单。
	out := filepath.Join(t.TempDir(), "base-eu")
	if err := BuildWithOptions(db, dir, out, key, "", "",
		BuildOptions{PackID: "base-eu-2026", Kind: "base", Jurisdictions: []string{"EU"}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Jurisdictions) != 1 || m.Jurisdictions[0] != "EU" || m.PackID != "base-eu-2026" {
		t.Errorf("manifest 法域身份缺失: %+v", m)
	}

	if err := BuildWithOptions(db, dir, filepath.Join(t.TempDir(), "bad"), key, "", "",
		BuildOptions{Kind: "base", Jurisdictions: []string{"JP"}}); !errors.Is(err, ErrBadJurisdiction) {
		t.Errorf("名单外法域必须拒绝，got %v", err)
	}
	// none 桶（法域未标注）在本语料为空——空包必须如实失败，不得产出「看起来成功」的空包。
	if err := BuildWithOptions(db, dir, filepath.Join(t.TempDir(), "none"), key, "", "",
		BuildOptions{Kind: "base", Jurisdictions: []string{"none"}}); err == nil {
		t.Error("法域过滤后无条目必须如实失败，不得打空包")
	}
}

// TestBuildDeltaWindow 增量包按发布日期窗口切分（闭区间）；窗口外条目不进包；
// 非法包种类必须拒绝。
func TestBuildDeltaWindow(t *testing.T) {
	dir, db, key := setupMultiSource(t)
	deltaDir := filepath.Join(t.TempDir(), "delta-2020")
	if err := BuildWithOptions(db, dir, deltaDir, key, "", "",
		BuildOptions{PackID: "delta-20200101-20211231", Kind: "delta",
			Since: "2020-01-01", Until: "2021-12-31"}); err != nil {
		t.Fatal(err)
	}
	if ids := changeDocIDs(t, deltaDir); len(ids) != 1 || !ids["law_gd"] {
		t.Errorf("2020–2021 窗口应只含 law_gd，实际 %v", ids)
	}
	if _, err := os.Stat(filepath.Join(deltaDir, "md", "law_bj.md")); !os.IsNotExist(err) {
		t.Error("窗口外条目的文件不得进增量包")
	}
	if err := BuildWithOptions(db, dir, filepath.Join(t.TempDir(), "bad"), key, "", "",
		BuildOptions{Kind: "full"}); !errors.Is(err, ErrBadPackKind) {
		t.Errorf("kind=full 应被拒绝: %v", err)
	}
}

// TestImportLedgerIdempotent 台账幂等：已装包整包跳过；不同包重叠条目按哈希跳过；
// pack_log 如实留痕（applied + 计数）。
func TestImportLedgerIdempotent(t *testing.T) {
	dir, db, key := setupMultiSource(t)
	packDir := filepath.Join(t.TempDir(), "base-all")
	if err := BuildWithOptions(db, dir, packDir, key, "", "",
		BuildOptions{PackID: "base-all-2026", Kind: "base"}); err != nil {
		t.Fatal(err)
	}
	trustPack(t, packDir)
	target, tdb := openTarget(t)

	rep, err := ImportPack(tdb, target, packDir)
	if err != nil || rep.Imported != 3 || rep.Skipped != 0 || rep.AlreadyApplied {
		t.Fatalf("首次导入应 3 条: %v %+v", err, rep)
	}
	var status, kind string
	var imported, skipped int
	if err := tdb.QueryRow(`SELECT status, kind, imported, skipped FROM pack_log WHERE pack_id='base-all-2026'`).
		Scan(&status, &kind, &imported, &skipped); err != nil {
		t.Fatalf("台账应留痕: %v", err)
	}
	if status != "applied" || kind != "base" || imported != 3 {
		t.Errorf("台账内容不符: %s %s %d", status, kind, imported)
	}

	// 同一包再导入：台账已 applied → 整包跳过，不重跑。
	rep2, err := ImportPack(tdb, target, packDir)
	if err != nil || !rep2.AlreadyApplied || rep2.Imported != 0 {
		t.Fatalf("已装包应整包跳过: %v %+v", err, rep2)
	}

	// 不同包（pack_id 不同）覆盖同批条目：逐条哈希一致 → 全部跳过、无错误。
	packDir2 := filepath.Join(t.TempDir(), "base-all-dup")
	if err := BuildWithOptions(db, dir, packDir2, key, "", "",
		BuildOptions{PackID: "base-all-2026-repack", Kind: "base"}); err != nil {
		t.Fatal(err)
	}
	trustPack(t, packDir2)
	rep3, err := ImportPack(tdb, target, packDir2)
	if err != nil || rep3.Imported != 0 || rep3.Skipped != 3 {
		t.Fatalf("重叠包应全部跳过且无错误: %v %+v", err, rep3)
	}
	var n int
	if err := tdb.QueryRow(`SELECT COUNT(*) FROM document`).Scan(&n); err != nil || n != 3 {
		t.Errorf("幂等导入不得重复写入，实际 %d 条", n)
	}
}

// TestImportLegacyLedgerFallback 旧整包（无 pack_id）：台账回退 "pack:"+generated_at，
// 二次导入同样整包跳过。
func TestImportLegacyLedgerFallback(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "legacy")
	if err := Build(db, dir, packDir, key, "", ""); err != nil {
		t.Fatal(err)
	}
	trustPack(t, packDir)
	target, tdb := openTarget(t)
	rep, err := ImportPack(tdb, target, packDir)
	if err != nil || rep.Imported != 1 || rep.Kind != "full" {
		t.Fatalf("旧包导入应成功且 kind=full: %v %+v", err, rep)
	}
	if len(rep.PackID) < 6 || rep.PackID[:5] != "pack:" {
		t.Errorf("旧包台账主键应回退 pack: 前缀，实际 %q", rep.PackID)
	}
	rep2, err := ImportPack(tdb, target, packDir)
	if err != nil || !rep2.AlreadyApplied {
		t.Fatalf("旧包二次导入应整包跳过: %v %+v", err, rep2)
	}
}

// TestImportDocConflict 同 doc_id 内容不一致 → ErrDocConflict 中止转人工，
// 台账记 partial，禁止覆盖原始层。
func TestImportDocConflict(t *testing.T) {
	dir, db, key := setupSource(t)
	packDir := filepath.Join(t.TempDir(), "pack-a")
	if err := Build(db, dir, packDir, key, "", ""); err != nil {
		t.Fatal(err)
	}
	trustPack(t, packDir)
	target, tdb := openTarget(t)
	if rep, err := ImportPack(tdb, target, packDir); err != nil || rep.Imported != 1 {
		t.Fatalf("首次导入应成功: %v %+v", err, rep)
	}

	// 另一来源：同 doc_id、原始层内容不同（磁盘上的 pdf 追加一个字节再打包）。
	// 直接改源目录落位文件模拟「不同内容撞同一 doc_id」，不重复走导入器（它会拒重复 doc_id）。
	dir2, db2, key2 := setupSource(t)
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "fake_law.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	altered := append(append([]byte{}, raw...), 'X')
	if err := os.WriteFile(filepath.Join(dir2, "pdf", "law_pack_2018.pdf"), altered, 0o644); err != nil {
		t.Fatal(err)
	}
	packDir2 := filepath.Join(t.TempDir(), "pack-b")
	if err := Build(db2, dir2, packDir2, key2, "", ""); err != nil {
		t.Fatal(err)
	}
	trustPack(t, packDir2)

	rep, err := ImportPack(tdb, target, packDir2)
	if !errors.Is(err, ErrDocConflict) {
		t.Fatalf("内容不一致必须报 ErrDocConflict: %v", err)
	}
	if rep.Imported != 0 || rep.Skipped != 0 {
		t.Errorf("冲突包不得写入/跳过条目: %+v", rep)
	}
	var status string
	if err := tdb.QueryRow(`SELECT status FROM pack_log WHERE pack_id = ?`, rep.PackID).Scan(&status); err != nil || status != "partial" {
		t.Errorf("冲突中止台账应记 partial（实际 %q, %v）", status, err)
	}
}

// TestBuildCoreByNameFilter 精简核心包（产品评审 P0-3）：按名称名单过滤——
// 全称与简称都算命中；与其他过滤取交集；名单一条不中必须如实失败（不得产出空包）。
func TestBuildCoreByNameFilter(t *testing.T) {
	dir, db, key := setupMultiSource(t)
	if _, err := db.Exec(`UPDATE law SET short_name='中央法' WHERE law_id='law_cn'`); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "core")
	if err := BuildWithOptions(db, dir, out, key, "", "",
		BuildOptions{PackID: "core-2026", Kind: "base", Names: []string{"中央测试法", "广东测试条例"}}); err != nil {
		t.Fatal(err)
	}
	if ids := changeDocIDs(t, out); len(ids) != 2 || !ids["law_cn"] || !ids["law_gd"] {
		t.Errorf("核心包应只含名单内两条，实际 %v", ids)
	}

	// 简称命中：名单里写简称也应命中同一部法（命中口径 = 全称 OR 简称）。
	out2 := filepath.Join(t.TempDir(), "core-short")
	if err := BuildWithOptions(db, dir, out2, key, "", "",
		BuildOptions{PackID: "core-short-2026", Kind: "base", Names: []string{"中央法"}}); err != nil {
		t.Fatal(err)
	}
	if ids := changeDocIDs(t, out2); len(ids) != 1 || !ids["law_cn"] {
		t.Errorf("简称应命中 law_cn，实际 %v", ids)
	}

	// 与地域过滤取交集：名单含两部，地域限定广东 → 只剩 law_gd。
	out3 := filepath.Join(t.TempDir(), "core-44")
	if err := BuildWithOptions(db, dir, out3, key, "", "",
		BuildOptions{PackID: "core-44-2026", Kind: "base",
			Names: []string{"中央测试法", "广东测试条例"}, Regions: []string{"44"}}); err != nil {
		t.Fatal(err)
	}
	if ids := changeDocIDs(t, out3); len(ids) != 1 || !ids["law_gd"] {
		t.Errorf("名单与地域应取交集，实际 %v", ids)
	}

	// 名单一条不中：如实失败，不得产出「看起来成功」的空包。
	if err := BuildWithOptions(db, dir, filepath.Join(t.TempDir(), "core-none"), key, "", "",
		BuildOptions{Kind: "base", Names: []string{"不存在的法规"}}); err == nil {
		t.Error("名单过滤后无条目必须如实失败，不得打空包")
	}
}
