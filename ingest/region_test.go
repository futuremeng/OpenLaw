// region.go 测试：试采只读、只写高置信、审计留痕、按批次撤销、绝不覆盖人工落判。
package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/futuremeng/OpenLaw/store"
)

// setupRegion 建一个含各类判定情形的临时库（离线，不依赖网络或真实语料）。
func setupRegion(t *testing.T) *RegionMarker {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db", "openlaw.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mk := func(lawID, name, issuer string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO law(law_id,name,issuer) VALUES(?,?,?)`, lawID, name, issuer); err != nil {
			t.Fatal(err)
		}
	}
	mk("r_bj", "北京市河湖管理条例", "北京市人大常委会")             // 高置信
	mk("r_conflict", "石家庄市水污染防治条例", "山西省人大常委会")     // 冲突 → 不写
	mk("r_medium", "劳动和社会保障监察条例", "石家庄市人力资源和社会保障局") // 仅机关命中 → 不写
	mk("r_none", "中华人民共和国劳动法", "全国人民代表大会常务委员会")     // 无法判定
	mk("r_marked", "河北省地质灾害防治条例", "河北省人大常委会")
	// 已人工落判的一条：任何机器写入都不得覆盖
	if _, err := db.Exec(`UPDATE law SET region_code='130000', region_source='manual',
		region_reviewer='张三', region_reviewed_at='2026-01-01T00:00:00Z' WHERE law_id='r_marked'`); err != nil {
		t.Fatal(err)
	}
	return &RegionMarker{DB: db}
}

func regionCodeOf(t *testing.T, m *RegionMarker, lawID string) (code, source, reviewer string) {
	t.Helper()
	err := m.DB.QueryRow(`SELECT COALESCE(region_code,''), COALESCE(region_source,''),
		COALESCE(region_reviewer,'') FROM law WHERE law_id=?`, lawID).Scan(&code, &source, &reviewer)
	if err != nil {
		t.Fatal(err)
	}
	return
}

// TestRegionScan 分类计数必须如实分开，且扫描不改库。
func TestRegionScan(t *testing.T) {
	m := setupRegion(t)
	cands, cnt, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 5 || cnt.Total != 5 {
		t.Fatalf("总数应为 5，实际 cands=%d total=%d", len(cands), cnt.Total)
	}
	if cnt.High != 1 || cnt.Medium != 1 || cnt.Conflict != 1 || cnt.Undecided != 1 || cnt.AlreadyMarked != 1 {
		t.Errorf("分类计数失真: %+v", cnt)
	}
	var marked int
	if err := m.DB.QueryRow(`SELECT count(*) FROM law WHERE region_code IS NOT NULL AND region_code<>''`).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if marked != 1 {
		t.Errorf("扫描属只读，标注数不应变化，实际 %d", marked)
	}
}

// TestRegionApplyOnlyHigh 只有高置信写入；机器建议不带署名；每笔留审计。
func TestRegionApplyOnlyHigh(t *testing.T) {
	m := setupRegion(t)
	cands, _, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	batchID, n, err := m.Apply(cands)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || batchID == "" {
		t.Fatalf("应只写入 1 条高置信，实际 n=%d batch=%q", n, batchID)
	}
	// 同级并列取上级：北京市河湖管理条例属省级地方性法规 → 110000（不是市辖区 110100）
	if code, src, reviewer := regionCodeOf(t, m, "r_bj"); code != "110000" || src != "auto" || reviewer != "" {
		t.Errorf("机器建议应为 110000/auto/无署名，实际 %q/%q/%q", code, src, reviewer)
	}
	for _, lawID := range []string{"r_conflict", "r_medium", "r_none"} {
		if code, _, _ := regionCodeOf(t, m, lawID); code != "" {
			t.Errorf("%s 不应被写入（中置信/冲突/判不出），实际 %q", lawID, code)
		}
	}
	if code, src, reviewer := regionCodeOf(t, m, "r_marked"); code != "130000" || src != "manual" || reviewer != "张三" {
		t.Errorf("不得覆盖人工落判，实际 %q/%q/%q", code, src, reviewer)
	}
	var logs int
	var actor string
	if err := m.DB.QueryRow(`SELECT count(*) FROM region_log WHERE batch_id=? AND action='suggest'`, batchID).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if err := m.DB.QueryRow(`SELECT actor FROM region_log WHERE batch_id=? LIMIT 1`, batchID).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if logs != 1 || actor != "openlaw-region" {
		t.Errorf("审计应留 1 条 actor=openlaw-region，实际 %d 条 actor=%q", logs, actor)
	}
	// 再跑一次：已标注的行不得重复写入
	if _, n2, err := m.Apply(cands); err != nil || n2 != 0 {
		t.Errorf("重复执行应写 0 条，实际 n=%d (%v)", n2, err)
	}
}

// TestRegionUndo 撤销先干跑计数，再执行；撤销只清机器写过的行并留 undo 审计。
func TestRegionUndo(t *testing.T) {
	m := setupRegion(t)
	cands, _, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	batchID, n, err := m.Apply(cands)
	if err != nil || n != 1 {
		t.Fatalf("预置写入失败: n=%d %v", n, err)
	}
	if got, err := m.Undo(batchID, false); err != nil || got != 1 {
		t.Fatalf("撤销干跑应计数 1，实际 %d (%v)", got, err)
	}
	if code, _, _ := regionCodeOf(t, m, "r_bj"); code != "110000" {
		t.Errorf("撤销干跑不得改数据，实际 %q", code)
	}
	if got, err := m.Undo(batchID, true); err != nil || got != 1 {
		t.Fatalf("撤销应清 1 条，实际 %d (%v)", got, err)
	}
	if code, src, rule := func() (string, string, string) {
		var c, s, r string
		if err := m.DB.QueryRow(`SELECT COALESCE(region_code,''),COALESCE(region_source,''),COALESCE(region_rule,'')
			FROM law WHERE law_id='r_bj'`).Scan(&c, &s, &r); err != nil {
			t.Fatal(err)
		}
		return c, s, r
	}(); code != "" || src != "" || rule != "" {
		t.Errorf("撤销后应回到未标注，实际 %q/%q/%q", code, src, rule)
	}
	var undos int
	if err := m.DB.QueryRow(`SELECT count(*) FROM region_log WHERE batch_id=? AND action='undo'`, batchID).Scan(&undos); err != nil {
		t.Fatal(err)
	}
	if undos != 1 {
		t.Errorf("撤销必须留 undo 审计，实际 %d 条", undos)
	}
}

// TestRegionNationalEffectGuard 国家级效力（部门规章等）不得按名称前缀标地域。
// 实测反例：「台湾渔船停泊点边防治安管理办法」（issuer=公安部，部门规章）
// 按名称前缀会误标 710000 台湾省——名称里的地名不等于法规的地域归属。
func TestRegionNationalEffectGuard(t *testing.T) {
	m := setupRegion(t)
	if _, err := m.DB.Exec(`INSERT INTO law(law_id,name,issuer,effect_level)
		VALUES('r_national','台湾渔船停泊点边防治安管理办法','公安部','部门规章')`); err != nil {
		t.Fatal(err)
	}
	cands, cnt, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if cnt.National != 1 {
		t.Errorf("国家级护栏应命中 1 条，实际 %d", cnt.National)
	}
	if cnt.High != 1 {
		t.Errorf("高置信仍应只有 1 条（国家级那条不得进高置信），实际 %d", cnt.High)
	}
	var hit bool
	for _, c := range cands {
		if c.LawID != "r_national" {
			continue
		}
		hit = true
		if c.Confidence != RegionConfNational || c.Code != "" {
			t.Errorf("国家级那条应标为 national 且无建议码，实际 %q/%q", c.Confidence, c.Code)
		}
	}
	if !hit {
		t.Fatal("报告里找不到该条")
	}
	batchID, n, err := m.Apply(cands)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("Apply 不应写入国家级那条，实际写入 %d 条", n)
	}
	if code, _, _ := regionCodeOf(t, m, "r_national"); code != "" {
		t.Errorf("国家级法规被误标地域 %q（批次 %s）", code, batchID)
	}
	// 「其他」不在护栏内：它既可能国家级也可能地方级，不得据此否决
	if _, err := m.DB.Exec(`INSERT INTO law(law_id,name,issuer,effect_level)
		VALUES('r_other','河北省地质灾害防治条例','','其他')`); err != nil {
		t.Fatal(err)
	}
	cands2, cnt2, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if cnt2.High != 1 {
		// r_bj 此时已是「已标注」，高置信只剩 effect_level=其他 的那条
		t.Errorf("effect_level=其他 仍应可高置信标引，实际 high=%d", cnt2.High)
	}
	if _, n2, err := m.Apply(cands2); err != nil || n2 != 1 {
		t.Errorf("「其他」那条应可写入，实际 n=%d (%v)", n2, err)
	}
}
func TestRegionUndoKeepsHumanReview(t *testing.T) {
	m := setupRegion(t)
	cands, _, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	batchID, n, err := m.Apply(cands)
	if err != nil || n != 1 {
		t.Fatalf("预置写入失败: n=%d %v", n, err)
	}
	if _, err := m.DB.Exec(`UPDATE law SET region_reviewer='李四', region_reviewed_at='2026-10-29T00:00:00Z'
		WHERE law_id='r_bj'`); err != nil {
		t.Fatal(err)
	}
	if got, err := m.Undo(batchID, true); err != nil || got != 0 {
		t.Errorf("已人工落判的行不应被撤销，实际 %d (%v)", got, err)
	}
	if code, _, reviewer := regionCodeOf(t, m, "r_bj"); code != "110000" || reviewer != "李四" {
		t.Errorf("人工落判结果被破坏: %q/%q", code, reviewer)
	}
}
