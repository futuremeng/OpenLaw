// regioninfer.go：从法规名称保守推导地域（OL-12 地点定位/地点标引的读侧通道）。
// 与 levelinfer.go 同一纪律：只取高置信名称模式，判定不出一律留空（绝不猜）；
// 推导属机器建议，展示时必须如实标注来源（UI 显示「按名称判定」或「地域未标注」）。
// 写库标引（law.region_code + 人工复核）属后续阶段，见 docs/tasks-opencourt-bridge.md。
package parse

import (
	"fmt"

	"github.com/futuremeng/OpenLaw/geo"
)

// 地域推导规则号（0=无法判定）。
const (
	RegionRuleProvince = 1 // 名称以省级行政区全称开头（河北省…、北京市…）
	RegionRuleCity     = 2 // 名称以地级行政区全称开头（玉溪市…、湘西土家族苗族自治州…）
	RegionRuleIssuer   = 3 // 发布机关名含行政区名（中置信：默认不写库，只进报告，见 §9.2）
)

// InferRegion 按名称前缀推导地域；返回 区划码、行政区名、规则号（0=无法判定）。
// 只认名称开头，不做全文匹配：「关于《石家庄市…》的决定」仍由开头的
// 「石家庄市政府…」这类前缀判定，判不出就留空。
func InferRegion(name string) (code, regionName string, rule int) {
	r, ok := geo.MatchPrefix(name)
	if !ok {
		return "", "", 0
	}
	switch r.Level {
	case geo.LevelProvince:
		return r.Code, r.Name, RegionRuleProvince
	case geo.LevelCity:
		return r.Code, r.Name, RegionRuleCity
	}
	return "", "", 0
}

// RegionOf 只要行政区名（UI 展示用）；判不出返回空串，由上层显示「地域未标注」。
func RegionOf(name string) string {
	_, n, _ := InferRegion(name)
	return n
}

// ---- 写库标引的建议层（F3，docs/design-region-write.md §6）----
// InferRegion 是读侧口径（界面显示用，行为不变）；下面的 SuggestRegion 是写侧口径：
// 综合名称与发布机关，并显式给出置信与冲突，供 openlaw-region 试采与落判使用。

// RegionSuggestion 地域建议（机器建议，未经人工复核；写入时 region_reviewer 必须留空）。
type RegionSuggestion struct {
	Code     string // 区划码；空 = 无法判定
	Name     string // 行政区路径名（geo.PathName 口径，如「河北省 · 石家庄市」）
	Rule     int    // 1/2=名称前缀（高置信）；3=发布机关（中置信）；0=无法判定
	Conflict bool   // 名称与发布机关指向互不隶属的两个行政区 → 不写库，转人工
	Note     string // 人类可读依据，如 name-prefix(R2)、issuer(R3)
}

// SuggestRegion 综合法规名称与发布机关给出地域建议。
// 纪律：判不出留空（绝不猜）；机关名推导只算中置信；名称与机关互不隶属时算冲突。
func SuggestRegion(name, issuer string) RegionSuggestion {
	code, _, rule := InferRegion(name)
	iReg, iOK := geo.MatchContained(issuer)
	switch {
	case rule != 0 && iOK && !sameRegionOrNested(code, iReg.Code):
		return RegionSuggestion{
			Code: code, Name: geo.PathName(code), Rule: rule, Conflict: true,
			Note: fmt.Sprintf("名称判%s、机关判%s，互不隶属", geo.NameOf(code), iReg.Name),
		}
	case rule != 0:
		return RegionSuggestion{
			Code: code, Name: geo.PathName(code), Rule: rule,
			Note: fmt.Sprintf("name-prefix(R%d)", rule),
		}
	case iOK:
		return RegionSuggestion{
			Code: iReg.Code, Name: geo.PathName(iReg.Code), Rule: RegionRuleIssuer,
			Note: fmt.Sprintf("issuer(R%d)", RegionRuleIssuer),
		}
	}
	return RegionSuggestion{}
}

// HighConfidence 是否属可写库的高置信：名称前缀命中（R1/R2）且不冲突。
// 中置信（R3）与冲突一律只进报告（§9.2 拍板：默认不写）。
func (s RegionSuggestion) HighConfidence() bool {
	return s.Code != "" && !s.Conflict &&
		(s.Rule == RegionRuleProvince || s.Rule == RegionRuleCity)
}

// sameRegionOrNested 两个区划码是否同一条链（相等或互为上下级）。
// 「石家庄市…」+ 机关「河北省人大常委会」不算冲突（同链）；
// 「石家庄市…」+ 机关「山西省人大常委会」才算冲突。
func sameRegionOrNested(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	if a == b {
		return true
	}
	for _, r := range geo.Path(a) {
		if r.Code == b {
			return true
		}
	}
	for _, r := range geo.Path(b) {
		if r.Code == a {
			return true
		}
	}
	return false
}
