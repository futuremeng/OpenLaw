// regioninfer.go：从法规名称保守推导地域（OL-12 地点定位/地点标引的读侧通道）。
// 与 levelinfer.go 同一纪律：只取高置信名称模式，判定不出一律留空（绝不猜）；
// 推导属机器建议，展示时必须如实标注来源（UI 显示「按名称判定」或「地域未标注」）。
// 写库标引（law.region_code + 人工复核）属后续阶段，见 docs/tasks-opencourt-bridge.md。
package parse

import "github.com/futuremeng/OpenLaw/geo"

// 地域推导规则号（0=无法判定）。
const (
	RegionRuleProvince = 1 // 名称以省级行政区全称开头（河北省…、北京市…）
	RegionRuleCity     = 2 // 名称以地级行政区全称开头（玉溪市…、湘西土家族苗族自治州…）
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
