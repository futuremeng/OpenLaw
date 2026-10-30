// regionaudit.go 地域一致性自检（docs/design-region-write.md §6）：**只报告、不自动改**。
// 矛盾项一律交人工在「地域标引」面板落判——机器不自作主张改数据（写路径从严）。
package ingest

// localEffectLevels 明确属地方效力的等级：这类法规本应有地域。
// 「其他」不在列内（语料里它既可能国家级也可能地方级，据此判定只会误报）；
// 自治条例/单行条例不在 effect_level 的 CHECK 枚举内，无需列入。
var localEffectLevels = map[string]bool{"地方性法规": true, "地方政府规章": true}

// RegionAuditIssue 一条法规的矛盾项（同一法规可有数条，合并成一行如实列出）。
type RegionAuditIssue struct {
	LawID       string   `json:"law_id"`
	Name        string   `json:"name"`
	EffectLevel string   `json:"effect_level"`
	IsLocal     int      `json:"is_local"`
	RegionCode  string   `json:"region_code"`
	Issues      []string `json:"issues"`
}

// RegionAuditSummary 自检计数：逐项计数，不得只报总数了事。
type RegionAuditSummary struct {
	Total                int                `json:"total"`
	LocalNoRegion        int                `json:"local_no_region"`          // 地方性法规/规章却无区划码
	NationalWithRegion   int                `json:"national_with_region"`     // 国家级效力却有区划码
	LevelBlankWithRegion int                `json:"level_blank_with_region"`  // 效力等级未标注却有区划码
	FlagLocalNoRegion    int                `json:"flag_local_no_region"`     // is_local=1 却无区划码
	FlagLevelLocalNoFlag int                `json:"flag_level_local_no_flag"` // 效力等级属地方级但 is_local=0
	FlagCodeNoBasis      int                `json:"flag_code_no_basis"`       // 有码但无任何地方效力依据
	Rows                 []RegionAuditIssue `json:"rows"`
}

// Audit 只读扫描全库的地域一致性矛盾（§6）。不写库、不改任何数据。
//
// is_local 口径按真实语料实测修正（不是照抄设计稿）：原先设想的「is_local=0 却有地域」
// 在 28,952 条上命中 3,611 条，其中 3,609 条是地方政府规章——它们本来就是地方级，
// 只是 is_local 从未被置位。照那条口径报，等于把 is_local 自己的毛病算成地域的毛病。
// 故拆成两条：效力等级属地方级但 is_local 未标记（is_local 不可靠的证据）、
// 已标地域但既无 is_local 也无地方级效力（真正需要人工看的可疑项）。
func (m *RegionMarker) Audit() (*RegionAuditSummary, error) {
	rows, err := m.DB.Query(`SELECT law_id, name, COALESCE(effect_level,''), is_local,
		COALESCE(region_code,'') FROM law ORDER BY law_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sum := &RegionAuditSummary{}
	for rows.Next() {
		var it RegionAuditIssue
		if err := rows.Scan(&it.LawID, &it.Name, &it.EffectLevel, &it.IsLocal, &it.RegionCode); err != nil {
			return nil, err
		}
		sum.Total++
		hasCode := it.RegionCode != ""
		isLocalLevel := localEffectLevels[it.EffectLevel]
		switch {
		case it.EffectLevel == "" && hasCode:
			it.Issues = append(it.Issues, "效力等级未标注却标了地域")
			sum.LevelBlankWithRegion++
		case nationalEffectLevels[it.EffectLevel] && hasCode:
			it.Issues = append(it.Issues, "国家级效力（"+it.EffectLevel+"）却标了地域")
			sum.NationalWithRegion++
		case isLocalLevel && !hasCode:
			it.Issues = append(it.Issues, it.EffectLevel+"未标地域")
			sum.LocalNoRegion++
		}
		if it.IsLocal == 1 && !hasCode {
			it.Issues = append(it.Issues, "标记为地方效力但未标地域")
			sum.FlagLocalNoRegion++
		}
		if it.IsLocal == 0 && isLocalLevel {
			it.Issues = append(it.Issues, "效力等级属地方级但 is_local 未标记（is_local 本身不可靠）")
			sum.FlagLevelLocalNoFlag++
		}
		if hasCode && it.IsLocal == 0 && !isLocalLevel {
			it.Issues = append(it.Issues, "已标地域但无地方效力依据（is_local 未标记且效力等级不属地方级）")
			sum.FlagCodeNoBasis++
		}
		if len(it.Issues) > 0 {
			sum.Rows = append(sum.Rows, it)
		}
	}
	return sum, rows.Err()
}
