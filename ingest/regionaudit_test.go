// regionaudit_test.go 一致性自检（§6）：逐项计数如实、矛盾项逐条列出、只读不改库。
package ingest

import (
	"fmt"
	"strings"
	"testing"
)

// setupAudit 复用判定用例库，再补进各类矛盾项。
func setupAudit(t *testing.T) *RegionMarker {
	t.Helper()
	m := setupRegion(t) // 五条均无 effect_level，其中 r_marked 已人工落判 130000
	seeds := []string{
		// 地方性法规未标地域：应报
		`INSERT INTO law(law_id,name,effect_level,is_local) VALUES('a_local_none','某市市容条例','地方性法规',1)`,
		// 地方政府规章已标：地域一项不应报
		`INSERT INTO law(law_id,name,effect_level,region_code,region_source) VALUES('a_gov_ok','某市办法','地方政府规章','440300','auto')`,
		// 国家级效力却有地域：应报（护栏本该挡下，出现即异常）
		`INSERT INTO law(law_id,name,effect_level,region_code,region_source) VALUES('a_nat_code','某项部门办法','部门规章','130000','manual')`,
		// 效力等级未标注却有地域：应报（真实语料实测 2 条）
		`INSERT INTO law(law_id,name,region_code,region_source) VALUES('a_lvl_none','某项未定级文件','710000','auto')`,
	}
	for _, s := range seeds {
		if _, err := m.DB.Exec(s); err != nil {
			t.Fatalf("种子失败: %v\n%.60s", err, s)
		}
	}
	return m
}

func TestRegionAuditCounts(t *testing.T) {
	m := setupAudit(t)
	before := auditFingerprint(t, m)
	sum, err := m.Audit()
	if err != nil {
		t.Fatal(err)
	}
	// 逐项计数，不得只报总数了事。用例库 9 条：setupRegion 五条均无 effect_level，
	// 其中 r_marked 有码 → 计入「效力等级未标注却有地域」；四条无码不报。
	if sum.LocalNoRegion != 1 { // a_local_none
		t.Errorf("地方性法规未标地域应 1，实际 %d", sum.LocalNoRegion)
	}
	if sum.NationalWithRegion != 1 { // a_nat_code
		t.Errorf("国家级效力却有地域应 1，实际 %d", sum.NationalWithRegion)
	}
	if sum.LevelBlankWithRegion != 2 { // r_marked + a_lvl_none
		t.Errorf("效力等级未标注却有地域应 2，实际 %d", sum.LevelBlankWithRegion)
	}
	if sum.FlagLocalNoRegion != 1 { // a_local_none
		t.Errorf("is_local=1 未标地域应 1，实际 %d", sum.FlagLocalNoRegion)
	}
	if sum.FlagLevelLocalNoFlag != 1 { // a_gov_ok（地方政府规章但 is_local 未置位）
		t.Errorf("效力等级属地方级但 is_local 未标记应 1，实际 %d", sum.FlagLevelLocalNoFlag)
	}
	if sum.FlagCodeNoBasis != 3 { // a_nat_code + a_lvl_none + r_marked
		t.Errorf("已标地域但无地方效力依据应 3，实际 %d", sum.FlagCodeNoBasis)
	}
	if sum.Total != 9 {
		t.Errorf("总数应为 9，实际 %d", sum.Total)
	}
	byID := map[string]RegionAuditIssue{}
	for _, r := range sum.Rows {
		byID[r.LawID] = r
	}
	// 已标地域的地方政府规章不得算「无地方效力依据」（实测教训：那样等于把
	// is_local 自己的毛病算成地域的毛病）；它只应带 is_local 未标记这一项。
	if it, ok := byID["a_gov_ok"]; !ok || len(it.Issues) != 1 || !strings.Contains(it.Issues[0], "is_local") {
		t.Errorf("地方政府规章只应报 is_local 未标记，实际: %+v", it)
	}
	if it, ok := byID["a_nat_code"]; !ok || len(it.Issues) == 0 {
		t.Errorf("国家级效力却有地域必须逐条列出并给出问题描述: %+v", byID["a_nat_code"])
	}
	if it, ok := byID["a_lvl_none"]; !ok {
		t.Error("效力等级未标注却有地域的条目未列入报告")
	} else if it.EffectLevel != "" {
		t.Errorf("效力等级未标注应如实留空显示: %+v", it)
	}
	// 只读：自检不得改动任何数据（只报告、不自动改）。
	if auditFingerprint(t, m) != before {
		t.Error("一致性自检必须只读，不得改动数据")
	}
}

// auditFingerprint 关键列指纹：断言 Audit 前后数据完全未变。
func auditFingerprint(t *testing.T, m *RegionMarker) string {
	t.Helper()
	var n, code, rev int
	row := m.DB.QueryRow(`SELECT count(*),
		SUM(CASE WHEN COALESCE(region_code,'')<>'' THEN 1 ELSE 0 END),
		SUM(CASE WHEN region_reviewer IS NOT NULL THEN 1 ELSE 0 END) FROM law`)
	if err := row.Scan(&n, &code, &rev); err != nil {
		t.Fatal(err)
	}
	var logs int
	if err := m.DB.QueryRow(`SELECT count(*) FROM region_log`).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("laws=%d coded=%d reviewed=%d logs=%d", n, code, rev, logs)
}
