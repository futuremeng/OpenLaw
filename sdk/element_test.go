package sdk

import (
	"strings"
	"testing"

	"github.com/futuremeng/OpenLaw/element"
)

// TestAnalyzeElementsAfterImport OL-2 验收：导入（pending）→ AnalyzeElements
// 返回非空 covered/missing_elements，并在 Warnings 提示待人工确认；
// 人工落判后提示消失。
func TestAnalyzeElementsAfterImport(t *testing.T) {
	o := openSDK(t)
	rep, err := element.Import(o.db, []element.Input{
		{ElementID: "mjj_e1", ArticleID: "l1_v1_第一条", CaseType: "民间借贷",
			Name: "借贷合意", HowToAttack: "核对借条等债权凭证"},
		{ElementID: "mjj_e2", ArticleID: "l1_v1_第一条", CaseType: "民间借贷",
			Name: "款项实际交付", HowToAttack: "补充转账凭证"},
	}, true)
	if err != nil || rep.Added != 2 {
		t.Fatalf("导入失败: %+v %v", rep, err)
	}
	res := o.AnalyzeElements("民间借贷", []string{"双方经协商达成借贷合意并签署借条"})
	if res.Status != StatusOK {
		t.Fatalf("导入后应可分析: %+v", res)
	}
	if c, _ := res.Data["covered"].([]map[string]any); len(c) != 1 {
		t.Errorf("covered 应 1: %v", res.Data["covered"])
	}
	if m, _ := res.Data["missing_elements"].([]map[string]any); len(m) != 1 {
		t.Errorf("missing_elements 应 1: %v", res.Data["missing_elements"])
	}
	if !hasWarning(res.Warnings, "待人工确认") {
		t.Errorf("pending 要件必须提示待人工确认: %v", res.Warnings)
	}
	if n, err := element.Confirm(o.db, []string{"mjj_e1", "mjj_e2"}, "confirm", "王复核", "对照原文"); err != nil || n != 2 {
		t.Fatalf("落判失败: %v", err)
	}
	res2 := o.AnalyzeElements("民间借贷", []string{"双方经协商达成借贷合意并签署借条"})
	if hasWarning(res2.Warnings, "待人工确认") {
		t.Errorf("全部确认后不应再有提示: %v", res2.Warnings)
	}
}

func hasWarning(ws []string, sub string) bool {
	for _, w := range ws {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}
