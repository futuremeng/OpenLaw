// region.go 法规地域标引的库层执行路径（F3，docs/design-region-write.md §5、§7）。
// CLI（openlaw-region）与桌面端必须共用这里——桌面端不是第二条写路径。
//
// 纪律：
//   - 机器只出建议，且只写 region_code 为空（未标注）的行——同 openlaw-level 的 WHERE 护栏，
//     因此撤销 = 把该批次写过的行清回空，无需记录原值；
//   - 人工复核过的行（region_reviewer 非空）机器一律不再写：驳回后地域留空但署名仍在，
//     不得让后续自动写入把人工结论顶回去；
//   - 机器写入 region_reviewer 一律留空（人工落判才填），界面据此显示「机器建议 · 待复核」；
//   - 每一笔都写 region_log（law 级审计），批量带 batch_id，可整批撤销；
//   - 中置信（发布机关）与冲突一律不写库，只进报告（§9.2 拍板）。
package ingest

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/futuremeng/OpenLaw/geo"
	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
)

// 建议置信口径（报告与界面共用，避免各处自造说法）。
const (
	RegionConfMarked   = "marked"   // 已有地域标注（本次不动）
	RegionConfHigh     = "high"     // 高置信：名称前缀命中且不冲突 → 可写
	RegionConfMedium   = "medium"   // 中置信：仅发布机关命中 → 只进报告
	RegionConfConflict = "conflict" // 名称与发布机关互不隶属 → 转人工
	RegionConfNational = "national" // 国家级效力：名称里的行政区名不是本法规的地域 → 不写
	RegionConfRejected = "rejected" // 人工已驳回：留空但已复核，机器不得再写
	RegionConfNone     = "none"     // 无法判定 → 留空（绝不猜）
)

// RegionSourcePack 随更新包携带的地域来源（§9.4 拍板：机器建议随包分发，但
// region_reviewer 留空 → 接收方界面仍显示待复核）。pack 既不是「已复核」也不等于
// 本库机器判定，它的含义是「这个码来自签名包，本库尚未复核」。
const (
	RegionSourcePack = "pack"
	// regionActorPack region_log.actor：包携带写入的机器署名（非人工，不得冒充复核人）。
	regionActorPack = "openlaw-pack"
)

// nationalEffectLevels 国家级效力等级：这类法规的地域不是「名称开头的行政区」。
// 实测抓出的反例：「台湾渔船停泊点边防治安管理办法」（issuer=公安部，部门规章）
// 按名称前缀会误标 710000 台湾省——名称里的地名不等于法规的地域归属。
// 「其他」不在列内：语料里它既可能国家级也可能地方级，不据此否决。
var nationalEffectLevels = map[string]bool{
	"宪法": true, "法律": true, "行政法规": true, "监察法规": true,
	"部门规章": true, "司法解释": true,
	"欧盟条例": true, "欧盟指令": true, "欧盟决定": true,
}

// RegionMarker 地域标引执行器（库层）。
type RegionMarker struct {
	DB *store.DB
}

// RegionCandidate 一条法规的地域建议（报告一行）。
type RegionCandidate struct {
	LawID       string
	Name        string
	Issuer      string
	EffectLevel string
	Code        string // 建议区划码；空 = 无建议
	RegionPath  string // 行政区路径名（空 = 未标注/未知码）
	Rule        int    // 规则号（0 = 无）
	Confidence  string // RegionConf* 之一
	Note        string // 依据（写入 region_rule，供复核追溯）
}

// RegionCounts 试采统计。分类计数，不给单一「覆盖率」了事。
type RegionCounts struct {
	Total         int
	AlreadyMarked int
	High          int
	Medium        int
	Conflict      int
	National      int
	Rejected      int
	Undecided     int
}

// Scan 只读扫描全库，给出每条法规的地域建议与分类计数。
func (m *RegionMarker) Scan() ([]RegionCandidate, RegionCounts, error) {
	rows, err := m.DB.Query(`SELECT law_id, name, COALESCE(issuer,''), COALESCE(region_code,''),
		COALESCE(effect_level,''), COALESCE(region_reviewer,'') FROM law ORDER BY law_id`)
	if err != nil {
		return nil, RegionCounts{}, fmt.Errorf("读取法规清单失败: %w", err)
	}
	defer rows.Close()
	var out []RegionCandidate
	var cnt RegionCounts
	for rows.Next() {
		var lawID, name, issuer, existing, effectLevel, reviewer string
		if err := rows.Scan(&lawID, &name, &issuer, &existing, &effectLevel, &reviewer); err != nil {
			return nil, RegionCounts{}, err
		}
		c := RegionCandidate{LawID: lawID, Name: name, Issuer: issuer, EffectLevel: effectLevel}
		cnt.Total++
		switch classify(&c, existing, reviewer) {
		case "marked":
			cnt.AlreadyMarked++
		case "rejected":
			cnt.Rejected++
		case "national":
			cnt.National++
		case "high":
			cnt.High++
		case "medium":
			cnt.Medium++
		case "conflict":
			cnt.Conflict++
		default:
			cnt.Undecided++
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, RegionCounts{}, err
	}
	return out, cnt, nil
}

// classify 单条法规的地域判定分类。Scan（全量）与 SuggestOne（单条）共用这一份实现，
// 保证面板里看到的「为什么没写」与批量扫描的口径完全一致，不会两边各说一套。
// 返回桶名：marked / rejected / national / high / medium / conflict / none。
func classify(c *RegionCandidate, existing, reviewer string) string {
	switch {
	case existing != "":
		c.Confidence, c.Code = RegionConfMarked, existing
		c.RegionPath = geo.PathName(existing)
		c.Note = "已标注"
		if reviewer != "" {
			c.Note = "已标注（人工已复核：" + reviewer + "）"
		}
		if c.RegionPath == "" {
			c.Note = "已标注（区划码不在内置区划表内）" // 如实呈现，不假装可解释
		}
		return "marked"
	case reviewer != "":
		// 人工驳回/清除过的行：留空但已复核，机器不得再写（Apply 的 WHERE 同样拦）
		c.Confidence = RegionConfRejected
		c.Note = "人工已复核并留空（" + reviewer + "），机器不再写入"
		return "rejected"
	case nationalEffectLevels[c.EffectLevel]:
		// 护栏：国家级效力的法规，名称开头的地名不是它的地域归属（见 nationalEffectLevels 注释）
		c.Confidence = RegionConfNational
		c.Note = "国家级效力（" + c.EffectLevel + "），不按名称标地域"
		return "national"
	default:
		s := parse.SuggestRegion(c.Name, c.Issuer)
		c.Code, c.RegionPath, c.Rule, c.Note = s.Code, s.Name, s.Rule, s.Note
		switch {
		case s.Conflict:
			c.Confidence = RegionConfConflict
			return "conflict"
		case s.HighConfidence():
			c.Confidence = RegionConfHigh
			return "high"
		case s.Code != "":
			c.Confidence = RegionConfMedium
			return "medium"
		default:
			c.Confidence = RegionConfNone
			return "none"
		}
	}
}

// SuggestOne 单条法规的地域判定（面板预览用；与 Scan 同一分类口径）。
func (m *RegionMarker) SuggestOne(lawID string) (RegionCandidate, error) {
	if m.DB == nil {
		return RegionCandidate{}, errors.New("未连接数据库")
	}
	var name, issuer, existing, effectLevel, reviewer string
	err := m.DB.QueryRow(`SELECT name, COALESCE(issuer,''), COALESCE(region_code,''),
		COALESCE(effect_level,''), COALESCE(region_reviewer,'') FROM law WHERE law_id=?`, lawID).
		Scan(&name, &issuer, &existing, &effectLevel, &reviewer)
	if errors.Is(err, sql.ErrNoRows) {
		return RegionCandidate{}, fmt.Errorf("法规 %s 不存在", lawID)
	}
	if err != nil {
		return RegionCandidate{}, err
	}
	c := RegionCandidate{LawID: lawID, Name: name, Issuer: issuer, EffectLevel: effectLevel}
	classify(&c, existing, reviewer)
	return c, nil
}

// Apply 写入高置信建议（调用方负责先给用户看过报告）。返回 batch_id 与实际写入条数。
// 只写 region_code 为空的行：已被人工或其他批次标注的一律跳过，不覆盖。
func (m *RegionMarker) Apply(cands []RegionCandidate) (string, int, error) {
	batchID := fmt.Sprintf("region-%s", time.Now().UTC().Format("20060102T150405Z"))
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := m.DB.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	n := 0
	for _, c := range cands {
		if c.Confidence != RegionConfHigh {
			continue // 中置信与冲突一律不写（§9.2）
		}
		res, err := tx.Exec(`UPDATE law SET region_code=?, region_source='auto', region_rule=?,
			region_reviewer=NULL, region_reviewed_at=NULL
			WHERE law_id=? AND (region_code IS NULL OR region_code='') AND region_reviewer IS NULL`,
			c.Code, c.Note, c.LawID)
		if err != nil {
			return "", 0, fmt.Errorf("写入地域失败（%s）: %w", c.LawID, err)
		}
		if aff, _ := res.RowsAffected(); aff == 1 {
			n++
			if _, err := tx.Exec(`INSERT INTO region_log(law_id,action,to_code,source,rule,actor,batch_id,created_at)
				VALUES(?,'suggest',?,'auto',?,'openlaw-region',?,?)`,
				c.LawID, c.Code, c.Note, batchID, now); err != nil {
				return "", 0, fmt.Errorf("写地域审计失败（%s）: %w", c.LawID, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return batchID, n, nil
}

// Undo 撤销某批次的机器建议。apply=false 只计数（干跑），与批量流转「先干跑再执行」一致。
// 只撤销仍是该批原样的行（region_reviewer 为空且 region_code 仍等于该批写入值）——
// 已被人工落判的绝不清除。
func (m *RegionMarker) Undo(batchID string, apply bool) (int, error) {
	rows, err := m.DB.Query(`SELECT r.law_id, r.to_code FROM region_log r JOIN law l ON l.law_id = r.law_id
		WHERE r.batch_id = ? AND r.action = 'suggest'
		  AND l.region_reviewer IS NULL AND l.region_code = r.to_code`, batchID)
	if err != nil {
		return 0, fmt.Errorf("查询批次失败: %w", err)
	}
	type target struct{ lawID, code string }
	var targets []target
	for rows.Next() {
		var x target
		if err := rows.Scan(&x.lawID, &x.code); err != nil {
			rows.Close()
			return 0, err
		}
		targets = append(targets, x)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if !apply {
		return len(targets), nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := m.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n := 0
	for _, x := range targets {
		res, err := tx.Exec(`UPDATE law SET region_code=NULL, region_source=NULL, region_rule=NULL,
			region_reviewer=NULL, region_reviewed_at=NULL
			WHERE law_id=? AND region_reviewer IS NULL AND region_code=?`, x.lawID, x.code)
		if err != nil {
			return 0, fmt.Errorf("撤销失败（%s）: %w", x.lawID, err)
		}
		if aff, _ := res.RowsAffected(); aff == 1 {
			n++
			if _, err := tx.Exec(`INSERT INTO region_log(law_id,action,from_code,source,actor,batch_id,reason,created_at)
				VALUES(?,'undo',?,'auto','openlaw-region',?,?,?)`,
				x.lawID, x.code, batchID, "撤销批次 "+batchID, now); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}
